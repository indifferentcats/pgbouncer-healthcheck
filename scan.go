package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

var scannerType = reflect.TypeOf((*sql.Scanner)(nil)).Elem()

// queryRows runs query and scans every row into a T by column name.
//
// Each result column is matched to the T field whose `db` tag equals the
// column name. Columns without a matching field are discarded, and fields
// whose column is absent are left at their zero value, so the same struct
// works across PGBouncer versions that add or remove SHOW columns.
//
// A NULL in a column bound to a non-pointer field leaves that field at its
// zero value; use pointer fields (or a NULL-aware sql.Scanner) where NULL must
// be distinguishable. The returned slice is never nil, so it serialises to
// JSON as [] rather than null when there are no rows.
func queryRows[T any](ctx context.Context, db *sql.DB, query string) ([]T, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query PGBouncer (%s): %w", query, err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to read columns (%s): %w", query, err)
	}

	typ := reflect.TypeOf((*T)(nil)).Elem()
	if typ.Kind() != reflect.Struct {
		return nil, fmt.Errorf("queryRows: %s is not a struct", typ)
	}
	fieldByColumn := make(map[string]int, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if tag, ok := f.Tag.Lookup("db"); ok && tag != "" && tag != "-" && f.IsExported() {
			fieldByColumn[tag] = i
		}
	}
	// Field index for each column, or -1 if the column is discarded.
	fieldIdx := make([]int, len(columns))
	for c, name := range columns {
		if i, ok := fieldByColumn[name]; ok {
			fieldIdx[c] = i
		} else {
			fieldIdx[c] = -1
		}
	}

	results := make([]T, 0)
	dests := make([]any, len(columns))
	for rows.Next() {
		var item T
		v := reflect.ValueOf(&item).Elem()

		// Non-pointer, non-Scanner fields are scanned via a temporary **T so a
		// NULL doesn't fail the scan; copied into the field afterwards.
		type deferred struct {
			field reflect.Value
			tmp   reflect.Value // **T
		}
		var copies []deferred

		for c := range columns {
			if fieldIdx[c] < 0 {
				dests[c] = new(any)
				continue
			}
			field := v.Field(fieldIdx[c])
			if field.Kind() == reflect.Pointer || field.Addr().Type().Implements(scannerType) {
				dests[c] = field.Addr().Interface()
				continue
			}
			tmp := reflect.New(reflect.PointerTo(field.Type()))
			dests[c] = tmp.Interface()
			copies = append(copies, deferred{field, tmp})
		}

		if err := rows.Scan(dests...); err != nil {
			return nil, fmt.Errorf("failed to scan row (%s): %w", query, err)
		}
		for _, d := range copies {
			if p := d.tmp.Elem(); !p.IsNil() {
				d.field.Set(p.Elem())
			}
		}
		results = append(results, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read rows (%s): %w", query, err)
	}
	return results, nil
}

// asString converts a raw driver value to a string.
func asString(src any) (string, error) {
	switch s := src.(type) {
	case string:
		return s, nil
	case []byte:
		return string(s), nil
	case int64:
		return strconv.FormatInt(s, 10), nil
	case bool:
		return strconv.FormatBool(s), nil
	default:
		return "", fmt.Errorf("unsupported type %T", src)
	}
}

// YesNo is a boolean scanned from PGBouncer's "yes"/"no" text columns.
type YesNo bool

// Scan implements sql.Scanner. NULL scans as false.
func (b *YesNo) Scan(src any) error {
	if src == nil {
		*b = false
		return nil
	}
	if v, ok := src.(bool); ok {
		*b = YesNo(v)
		return nil
	}
	s, err := asString(src)
	if err != nil {
		return fmt.Errorf("YesNo: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "yes", "y", "true", "t", "on", "1":
		*b = true
	case "no", "n", "false", "f", "off", "0", "":
		*b = false
	default:
		return fmt.Errorf("YesNo: cannot parse %q", s)
	}
	return nil
}

// OptionalInt is an integer that may be unset. PGBouncer reports some
// optional numeric settings (e.g. SHOW USERS pool_size) as an empty string
// when unset; that, and SQL NULL, are treated as unset and serialise as null.
type OptionalInt struct {
	Int64 int64
	Valid bool
}

// Scan implements sql.Scanner.
func (n *OptionalInt) Scan(src any) error {
	*n = OptionalInt{}
	if src == nil {
		return nil
	}
	if v, ok := src.(int64); ok {
		*n = OptionalInt{v, true}
		return nil
	}
	s, err := asString(src)
	if err != nil {
		return fmt.Errorf("OptionalInt: %w", err)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("OptionalInt: %w", err)
	}
	*n = OptionalInt{v, true}
	return nil
}

// MarshalJSON implements json.Marshaler: a number, or null when unset.
func (n OptionalInt) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(n.Int64)
}
