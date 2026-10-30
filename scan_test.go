package main

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type scanTarget struct {
	Name     string      `db:"name" json:"name"`
	Count    int64       `db:"count" json:"count"`
	Optional *string     `db:"optional" json:"optional"`
	Flag     YesNo       `db:"flag" json:"flag"`
	Limit    OptionalInt `db:"limit" json:"limit"`
	Missing  int64       `db:"missing" json:"missing"`
	Untagged string
}

func TestQueryRowsMatchesColumnsByName(t *testing.T) {
	db := openFakeDB(t, map[string]fakeResult{
		"SHOW THINGS": {
			// Order differs from the struct, includes an unknown column,
			// and omits "missing".
			columns: []string{"flag", "unknown", "limit", "name", "optional", "count"},
			rows: [][]driver.Value{
				{"yes", "ignored", int64(7), []byte("a"), "x", int64(3)},
				{"no", nil, "", "b", nil, nil},
			},
		},
	})

	got, err := queryRows[scanTarget](context.Background(), db, "SHOW THINGS")
	if err != nil {
		t.Fatalf("queryRows: %s", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}

	a := got[0]
	if a.Name != "a" || a.Count != 3 || a.Optional == nil || *a.Optional != "x" ||
		!bool(a.Flag) || a.Limit != (OptionalInt{7, true}) || a.Missing != 0 || a.Untagged != "" {
		t.Errorf("row 0 = %+v", a)
	}

	b := got[1]
	// NULL into a non-pointer field leaves the zero value; NULL into a
	// pointer field is nil; "" into OptionalInt is unset.
	if b.Name != "b" || b.Count != 0 || b.Optional != nil || bool(b.Flag) || b.Limit.Valid {
		t.Errorf("row 1 = %+v", b)
	}
}

func TestQueryRowsEmptyResultIsEmptyArray(t *testing.T) {
	db := openFakeDB(t, map[string]fakeResult{
		"SHOW NOTHING": {columns: []string{"name"}},
	})
	got, err := queryRows[scanTarget](context.Background(), db, "SHOW NOTHING")
	if err != nil {
		t.Fatalf("queryRows: %s", err)
	}
	out, _ := json.Marshal(got)
	if string(out) != "[]" {
		t.Errorf("JSON = %s, want []", out)
	}
}

func TestQueryRowsErrors(t *testing.T) {
	db := openFakeDB(t, map[string]fakeResult{
		"SHOW BROKEN": {err: errors.New("boom")},
		"SHOW BADTYPE": {
			columns: []string{"count"},
			rows:    [][]driver.Value{{"not-a-number"}},
		},
	})
	ctx := context.Background()

	if _, err := queryRows[scanTarget](ctx, db, "SHOW BROKEN"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("query error = %v, want wrapped boom", err)
	}
	if _, err := queryRows[scanTarget](ctx, db, "SHOW BADTYPE"); err == nil || !strings.Contains(err.Error(), "failed to scan row") {
		t.Errorf("scan error = %v, want scan failure", err)
	}
	if _, err := queryRows[int](ctx, db, "SHOW BADTYPE"); err == nil || !strings.Contains(err.Error(), "not a struct") {
		t.Errorf("non-struct error = %v, want not a struct", err)
	}
}

func TestYesNoScan(t *testing.T) {
	tests := []struct {
		src     any
		want    bool
		wantErr bool
	}{
		{"yes", true, false},
		{[]byte("YES"), true, false},
		{"no", false, false},
		{"", false, false},
		{nil, false, false},
		{true, true, false},
		{int64(1), true, false},
		{"maybe", false, true},
		{3.5, false, true},
	}
	for _, tt := range tests {
		var b YesNo = !YesNo(tt.want) // ensure Scan overwrites
		err := b.Scan(tt.src)
		if (err != nil) != tt.wantErr {
			t.Errorf("Scan(%#v) err = %v, wantErr %v", tt.src, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && bool(b) != tt.want {
			t.Errorf("Scan(%#v) = %v, want %v", tt.src, b, tt.want)
		}
	}
}

func TestOptionalIntScanAndJSON(t *testing.T) {
	tests := []struct {
		src      any
		wantJSON string
		wantErr  bool
	}{
		{int64(42), "42", false},
		{"17", "17", false},
		{[]byte(" 5 "), "5", false},
		{"", "null", false},
		{nil, "null", false},
		{"abc", "", true},
		{3.5, "", true},
	}
	for _, tt := range tests {
		n := OptionalInt{99, true} // ensure Scan resets
		err := n.Scan(tt.src)
		if (err != nil) != tt.wantErr {
			t.Errorf("Scan(%#v) err = %v, wantErr %v", tt.src, err, tt.wantErr)
			continue
		}
		if tt.wantErr {
			continue
		}
		out, err := json.Marshal(n)
		if err != nil || string(out) != tt.wantJSON {
			t.Errorf("Scan(%#v) JSON = %s (%v), want %s", tt.src, out, err, tt.wantJSON)
		}
	}
}
