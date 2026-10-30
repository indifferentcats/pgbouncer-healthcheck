package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
)

// A minimal database/sql driver that returns canned SHOW results, so the
// scanning and handler code can be tested without a running PGBouncer.

type fakeResult struct {
	columns []string
	rows    [][]driver.Value
	err     error
}

var (
	fakeFixtures sync.Map // DSN -> map[string]fakeResult (query -> result)
	fakeDSNSeq   atomic.Int64
)

func init() {
	sql.Register("fakeshow", fakeDriver{})
}

// openFakeDB returns a *sql.DB whose queries are answered from results.
func openFakeDB(t *testing.T, results map[string]fakeResult) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("fake-%d", fakeDSNSeq.Add(1))
	fakeFixtures.Store(dsn, results)
	db, err := sql.Open("fakeshow", dsn)
	if err != nil {
		t.Fatalf("open fake db: %s", err)
	}
	t.Cleanup(func() {
		db.Close()
		fakeFixtures.Delete(dsn)
	})
	return db
}

type fakeDriver struct{}

func (fakeDriver) Open(dsn string) (driver.Conn, error) {
	v, ok := fakeFixtures.Load(dsn)
	if !ok {
		return nil, fmt.Errorf("unknown fake dsn %q", dsn)
	}
	return &fakeConn{results: v.(map[string]fakeResult)}, nil
}

type fakeConn struct {
	results map[string]fakeResult
}

func (c *fakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}
func (c *fakeConn) Close() error              { return nil }
func (c *fakeConn) Begin() (driver.Tx, error) { return nil, errors.New("tx not supported") }

// QueryContext implements driver.QueryerContext.
func (c *fakeConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	res, ok := c.results[query]
	if !ok {
		return nil, fmt.Errorf("unexpected query %q", query)
	}
	if res.err != nil {
		return nil, res.err
	}
	return &fakeRows{columns: res.columns, rows: res.rows}, nil
}

type fakeRows struct {
	columns []string
	rows    [][]driver.Value
	pos     int
}

func (r *fakeRows) Columns() []string { return r.columns }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.pos])
	r.pos++
	return nil
}
