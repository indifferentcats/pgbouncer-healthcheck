package main

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julienschmidt/httprouter"
)

// withGlobals sets the package-level config and db for one test.
func withGlobals(t *testing.T, cfg configuration, d *sql.DB) {
	t.Helper()
	oldCfg, oldDB := config, db
	config, db = cfg, d
	t.Cleanup(func() { config, db = oldCfg, oldDB })
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// statusFixtures answers every SHOW command used by /status/* with one row.
func statusFixtures() map[string]fakeResult {
	return map[string]fakeResult{
		"SHOW USERS":     {columns: []string{"name", "pool_size", "pool_mode"}, rows: [][]driver.Value{{"dd-agent", "", nil}}},
		"SHOW CONFIG":    {columns: []string{"key", "value", "default", "changeable"}, rows: [][]driver.Value{{"listen_port", "6543", "6432", "no"}}},
		"SHOW DATABASES": {columns: []string{"name", "host", "port", "pool_size"}, rows: [][]driver.Value{{"dummy", "10.0.0.1", int64(5432), int64(100)}}},
		"SHOW POOLS":     {columns: []string{"database", "user", "cl_active", "pool_mode"}, rows: [][]driver.Value{{"pgbouncer", "pgbouncer", int64(1), nil}}},
		"SHOW CLIENTS":   {columns: []string{"type", "user", "port"}, rows: [][]driver.Value{{"C", "dd-agent", int64(5000)}}},
		"SHOW SERVERS":   {columns: []string{"type", "user", "port"}, rows: [][]driver.Value{{"S", "app", int64(5432)}}},
		"SHOW MEM":       {columns: []string{"name", "size", "used", "free", "memtotal"}, rows: [][]driver.Value{{"user_cache", int64(1), int64(2), int64(3), int64(4)}}},
		"SHOW STATS":     {columns: []string{"database", "total_query_count"}, rows: [][]driver.Value{{"pgbouncer", int64(9)}}},
	}
}

var statusChecks = []struct {
	path, key, want string
}{
	{"/status/users", "name", "dd-agent"},
	{"/status/configs", "key", "listen_port"},
	{"/status/databases", "name", "dummy"},
	{"/status/pools", "database", "pgbouncer"},
	{"/status/clients", "type", "C"},
	{"/status/servers", "type", "S"},
	{"/status/memory", "name", "user_cache"},
	{"/status/stats", "database", "pgbouncer"},
}

func TestStatusEndpoints(t *testing.T) {
	withGlobals(t, configuration{}, openFakeDB(t, statusFixtures()))
	router := httprouter.New()
	addStatusHandlers(router)

	for _, c := range statusChecks {
		t.Run(c.path, func(t *testing.T) {
			rec := get(t, router, c.path)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q", ct)
			}
			var rows []map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
				t.Fatalf("invalid JSON %s: %s", rec.Body, err)
			}
			if len(rows) != 1 || rows[0][c.key] != c.want {
				t.Errorf("body = %s, want %s=%q", rec.Body, c.key, c.want)
			}
		})
	}
}

func TestStatusEndpointsErrors(t *testing.T) {
	failing := map[string]fakeResult{}
	for q := range statusFixtures() {
		failing[q] = fakeResult{err: errors.New("pgbouncer down")}
	}

	for name, d := range map[string]*sql.DB{
		"not connected": nil,
		"query fails":   openFakeDB(t, failing),
	} {
		t.Run(name, func(t *testing.T) {
			withGlobals(t, configuration{}, d)
			router := httprouter.New()
			addStatusHandlers(router)
			for _, c := range statusChecks {
				if rec := get(t, router, c.path); rec.Code != http.StatusInternalServerError {
					t.Errorf("%s: status %d, want 500", c.path, rec.Code)
				}
			}
		})
	}
}

func TestHealthPortProbe(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	withGlobals(t, configuration{PGBouncerPort: port}, nil)
	router := httprouter.New()
	addHealthHandlers(router)

	if rec := get(t, router, "/health"); rec.Code != http.StatusOK {
		t.Errorf("listening: status %d: %s", rec.Code, rec.Body)
	}
	ln.Close()
	if rec := get(t, router, "/health"); rec.Code != http.StatusInternalServerError ||
		!strings.Contains(rec.Body.String(), "port probe check failed") {
		t.Errorf("closed: status %d: %s", rec.Code, rec.Body)
	}
}

func TestHealthEnhanced(t *testing.T) {
	tests := []struct {
		name string
		db   func(t *testing.T) *sql.DB
		want int
	}{
		{"ok", func(t *testing.T) *sql.DB { return openFakeDB(t, statusFixtures()) }, http.StatusOK},
		{"query fails", func(t *testing.T) *sql.DB {
			return openFakeDB(t, map[string]fakeResult{"SHOW USERS": {err: errors.New("down")}})
		}, http.StatusInternalServerError},
		{"not connected", func(*testing.T) *sql.DB { return nil }, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withGlobals(t, configuration{EnhancedCheck: true}, tt.db(t))
			router := httprouter.New()
			addHealthHandlers(router)
			if rec := get(t, router, "/health"); rec.Code != tt.want {
				t.Errorf("status %d, want %d: %s", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

func TestServerRoutes(t *testing.T) {
	withGlobals(t, configuration{Port: 8000}, nil)
	srv := initServer()
	if srv.Addr != ":8000" || srv.ReadHeaderTimeout == 0 || srv.WriteTimeout == 0 {
		t.Errorf("server = addr %q, timeouts %v/%v", srv.Addr, srv.ReadHeaderTimeout, srv.WriteTimeout)
	}
	if rec := get(t, srv.Handler, "/"); rec.Code != http.StatusOK {
		t.Errorf("/: status %d", rec.Code)
	}
	// Debug endpoints are off by default
	if rec := get(t, srv.Handler, "/debug/meminfo"); rec.Code != http.StatusNotFound {
		t.Errorf("/debug/meminfo: status %d, want 404", rec.Code)
	}

	withGlobals(t, configuration{EnableDebugEndpoints: true}, nil)
	if rec := get(t, initServer().Handler, "/debug/meminfo"); rec.Code == http.StatusNotFound {
		t.Error("/debug/meminfo not registered with EnableDebugEndpoints")
	}
}

func TestFileHandler(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ami_version")
	if err := os.WriteFile(path, []byte("ami-123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	router := httprouter.New()
	router.GET("/ok", makeRequestHandlerFile("version info", path))
	router.GET("/missing", makeRequestHandlerFile("version info", path+".missing"))

	if rec := get(t, router, "/ok"); rec.Code != http.StatusOK || rec.Body.String() != "ami-123\n" {
		t.Errorf("/ok: status %d body %q", rec.Code, rec.Body)
	}
	if rec := get(t, router, "/missing"); rec.Code != http.StatusInternalServerError ||
		!strings.Contains(rec.Body.String(), "Error fetching version info") {
		t.Errorf("/missing: status %d body %q", rec.Code, rec.Body)
	}
}

func TestCommandHandler(t *testing.T) {
	router := httprouter.New()
	router.GET("/ok", makeRequestHandlerCommand("greeting", "echo", "hello"))
	router.GET("/fail", makeRequestHandlerCommand("nothing", "false"))

	if rec := get(t, router, "/ok"); rec.Code != http.StatusOK || rec.Body.String() != "hello\n" {
		t.Errorf("/ok: status %d body %q", rec.Code, rec.Body)
	}
	if rec := get(t, router, "/fail"); rec.Code != http.StatusInternalServerError {
		t.Errorf("/fail: status %d", rec.Code)
	}
}
