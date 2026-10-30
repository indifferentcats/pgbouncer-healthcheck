package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/julienschmidt/httprouter"
)

// probeFixtures returns a healthy PGBouncer: 10 of 100 clients used, no
// paused databases, no waiting pools.
func probeFixtures() map[string]fakeResult {
	return map[string]fakeResult{
		"SHOW VERSION": {columns: []string{"version"}, rows: [][]driver.Value{{"PgBouncer 1.25.2"}}},
		"SHOW LISTS": {columns: []string{"list", "items"}, rows: [][]driver.Value{
			{"databases", int64(2)}, {"used_clients", int64(10)}, {"free_clients", int64(40)},
		}},
		"SHOW CONFIG": {columns: []string{"key", "value", "default", "changeable"}, rows: [][]driver.Value{
			{"listen_port", "6543", "6432", "no"}, {"max_client_conn", "100", "100", "yes"},
		}},
		"SHOW DATABASES": {columns: []string{"name", "paused", "disabled"}, rows: [][]driver.Value{
			{"app", int64(0), int64(0)}, {"other", int64(0), int64(0)}, {"pgbouncer", int64(0), int64(0)},
		}},
		"SHOW POOLS": {columns: []string{"database", "user", "cl_waiting", "maxwait", "maxwait_us"}, rows: [][]driver.Value{
			{"app", "app", int64(0), int64(0), int64(0)}, {"other", "app", int64(0), int64(0), int64(0)},
		}},
	}
}

func backendFixtures() map[string]fakeResult {
	return map[string]fakeResult{"SELECT 1": {columns: []string{"?column?"}, rows: [][]driver.Value{{int64(1)}}}}
}

func with(fixtures map[string]fakeResult, query string, res fakeResult) map[string]fakeResult {
	fixtures[query] = res
	return fixtures
}

// probeEnv sets the config and databases used by /health/live and
// /health/ready for one test.
type probeEnv struct {
	cfg                configuration
	db, probe, backend *sql.DB
}

func setupProbes(t *testing.T, env probeEnv) http.Handler {
	t.Helper()
	cfg := env.cfg
	if cfg.ClientHeadroomPct == 0 {
		cfg.ClientHeadroomPct = 90
	}
	if cfg.MaxWait == 0 {
		cfg.MaxWait = 5 * time.Second
	}
	if cfg.MaxClWaiting == 0 {
		cfg.MaxClWaiting = -1
	}
	withGlobals(t, cfg, env.db)
	backendFailure.at, backendFailure.err = time.Time{}, nil
	oldProbe, oldBackend := probeDB, backendDB
	probeDB, backendDB = env.probe, env.backend
	t.Cleanup(func() { probeDB, backendDB = oldProbe, oldBackend })

	router := httprouter.New()
	addHealthHandlers(router)
	return router
}

func getReport(t *testing.T, h http.Handler, path string) (int, healthReport) {
	t.Helper()
	rec := get(t, h, path)
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("%s: Content-Type %q, body %s", path, ct, rec.Body)
	}
	var r healthReport
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("%s: invalid JSON %s: %s", path, rec.Body, err)
	}
	if (rec.Code == http.StatusOK) != (r.Status == "ok") {
		t.Errorf("%s: HTTP %d inconsistent with status %q", path, rec.Code, r.Status)
	}
	return rec.Code, r
}

func findCheck(r healthReport, name string) (checkResult, bool) {
	for _, c := range r.Checks {
		if c.Name == name {
			return c, true
		}
	}
	return checkResult{}, false
}

func checkNames(r healthReport) string {
	var names []string
	for _, c := range r.Checks {
		names = append(names, c.Name)
	}
	return strings.Join(names, ",")
}

func TestLive(t *testing.T) {
	h := setupProbes(t, probeEnv{db: openFakeDB(t, probeFixtures())})
	code, r := getReport(t, h, "/health/live")
	c, _ := findCheck(r, "console")
	if code != http.StatusOK || checkNames(r) != "console" || c.Detail != "PgBouncer 1.25.2" {
		t.Errorf("live = %d %+v", code, r)
	}

	h = setupProbes(t, probeEnv{db: openFakeDB(t, map[string]fakeResult{"SHOW VERSION": {err: errors.New("down")}})})
	if code, r := getReport(t, h, "/health/live"); code != http.StatusServiceUnavailable || r.Status != "fail" {
		t.Errorf("live with failing console = %d %+v", code, r)
	}

	h = setupProbes(t, probeEnv{})
	if code, _ := getReport(t, h, "/health/live"); code != http.StatusServiceUnavailable {
		t.Errorf("live without db = %d", code)
	}
}

func TestReadyAllChecksPass(t *testing.T) {
	h := setupProbes(t, probeEnv{
		cfg:     configuration{ReadyChecks: []string{"headroom", "databases", "pools", "backend"}},
		probe:   openFakeDB(t, probeFixtures()),
		backend: openFakeDB(t, backendFixtures()),
	})
	code, r := getReport(t, h, "/health/ready")
	if code != http.StatusOK || checkNames(r) != "login,console,headroom,databases,pools,backend" {
		t.Fatalf("ready = %d %+v", code, r)
	}
	for name, want := range map[string]string{
		"headroom":  "used_clients 10 of max_client_conn 100 (10%)",
		"databases": "2 databases ok", // pgbouncer admin db excluded by default
		"pools":     "2 pools ok",
		"backend":   "SELECT 1 ok",
	} {
		if c, _ := findCheck(r, name); !c.OK || c.Detail != want {
			t.Errorf("%s = %+v, want detail %q", name, c, want)
		}
	}
}

func TestReadyOnlyRunsConfiguredChecks(t *testing.T) {
	h := setupProbes(t, probeEnv{cfg: configuration{ReadyChecks: nil}, probe: openFakeDB(t, probeFixtures())})
	if _, r := getReport(t, h, "/health/ready"); checkNames(r) != "login,console" {
		t.Errorf("checks = %s, want login,console", checkNames(r))
	}
}

func TestReadyFailures(t *testing.T) {
	tests := []struct {
		name     string
		cfg      configuration
		fixtures map[string]fakeResult
		check    string
		detail   string
	}{
		{
			name: "headroom exhausted",
			cfg:  configuration{ReadyChecks: []string{"headroom"}},
			fixtures: with(probeFixtures(), "SHOW LISTS", fakeResult{columns: []string{"list", "items"},
				rows: [][]driver.Value{{"used_clients", int64(95)}}}),
			check:  "headroom",
			detail: "used_clients 95 of max_client_conn 100 (95%) is at or above 90%",
		},
		{
			name:   "headroom threshold configurable",
			cfg:    configuration{ReadyChecks: []string{"headroom"}, ClientHeadroomPct: 10},
			check:  "headroom",
			detail: "(10%) is at or above 10%",
		},
		{
			name: "database paused and disabled",
			cfg:  configuration{ReadyChecks: []string{"databases"}},
			fixtures: with(probeFixtures(), "SHOW DATABASES", fakeResult{columns: []string{"name", "paused", "disabled"},
				rows: [][]driver.Value{{"app", int64(1), int64(0)}, {"other", int64(0), int64(1)}}}),
			check:  "databases",
			detail: "app is paused; other is disabled",
		},
		{
			name:   "ready database not configured",
			cfg:    configuration{ReadyChecks: []string{"databases"}, ReadyDatabases: []string{"app", "missing"}},
			check:  "databases",
			detail: "missing is not configured in PGBouncer",
		},
		{
			name: "pool maxwait",
			cfg:  configuration{ReadyChecks: []string{"pools"}},
			fixtures: with(probeFixtures(), "SHOW POOLS", fakeResult{columns: []string{"database", "user", "cl_waiting", "maxwait", "maxwait_us"},
				rows: [][]driver.Value{{"app", "app", int64(3), int64(6), int64(500000)}}}),
			check:  "pools",
			detail: "app/app maxwait 6.5s > 5s",
		},
		{
			name: "pool cl_waiting",
			cfg:  configuration{ReadyChecks: []string{"pools"}, MaxClWaiting: 2},
			fixtures: with(probeFixtures(), "SHOW POOLS", fakeResult{columns: []string{"database", "user", "cl_waiting", "maxwait", "maxwait_us"},
				rows: [][]driver.Value{{"app", "app", int64(3), int64(0), int64(0)}}}),
			check:  "pools",
			detail: "app/app cl_waiting 3 > 2",
		},
		{
			name:     "console fails",
			fixtures: with(probeFixtures(), "SHOW VERSION", fakeResult{err: errors.New("console gone")}),
			check:    "console",
			detail:   "console gone",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixtures := tt.fixtures
			if fixtures == nil {
				fixtures = probeFixtures()
			}
			h := setupProbes(t, probeEnv{cfg: tt.cfg, probe: openFakeDB(t, fixtures)})
			code, r := getReport(t, h, "/health/ready")
			c, ok := findCheck(r, tt.check)
			if code != http.StatusServiceUnavailable || !ok || c.OK || !strings.Contains(c.Detail, tt.detail) {
				t.Errorf("ready = %d, %s = %+v; want failure containing %q", code, tt.check, c, tt.detail)
			}
		})
	}
}

func TestReadyScopesDatabasesAndPools(t *testing.T) {
	fixtures := with(probeFixtures(), "SHOW DATABASES", fakeResult{columns: []string{"name", "paused"},
		rows: [][]driver.Value{{"app", int64(0)}, {"other", int64(1)}}})
	fixtures = with(fixtures, "SHOW POOLS", fakeResult{columns: []string{"database", "user", "maxwait"},
		rows: [][]driver.Value{{"app", "app", int64(0)}, {"other", "app", int64(60)}}})

	// "other" is paused and waiting, but only "app" is in scope
	h := setupProbes(t, probeEnv{
		cfg:   configuration{ReadyChecks: []string{"databases", "pools"}, ReadyDatabases: []string{"app"}},
		probe: openFakeDB(t, fixtures),
	})
	if code, r := getReport(t, h, "/health/ready"); code != http.StatusOK {
		t.Errorf("scoped ready = %d %+v", code, r)
	}
}

func TestReadyLoginAndBackendFailures(t *testing.T) {
	broken, err := sql.Open("fakeshow", "no-such-dsn") // Open fails on connect
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { broken.Close() })

	h := setupProbes(t, probeEnv{probe: broken})
	code, r := getReport(t, h, "/health/ready")
	if c, _ := findCheck(r, "login"); code != http.StatusServiceUnavailable || c.OK || checkNames(r) != "login" {
		t.Errorf("login failure = %d %+v", code, r)
	}

	h = setupProbes(t, probeEnv{
		cfg:     configuration{ReadyChecks: []string{"backend"}},
		probe:   openFakeDB(t, probeFixtures()),
		backend: broken,
	})
	code, r = getReport(t, h, "/health/ready")
	if c, _ := findCheck(r, "backend"); code != http.StatusServiceUnavailable || c.OK ||
		!strings.Contains(c.Detail, "could not connect through PGBouncer") {
		t.Errorf("backend failure = %d %+v", code, r)
	}

	h = setupProbes(t, probeEnv{})
	if code, _ := getReport(t, h, "/health/ready"); code != http.StatusServiceUnavailable {
		t.Errorf("ready without db = %d", code)
	}
}

func TestHealthModeSelectsHandler(t *testing.T) {
	for mode, wantJSON := range map[string]bool{"legacy": false, "live": true, "ready": true} {
		t.Run(mode, func(t *testing.T) {
			fixtures := with(probeFixtures(), "SHOW USERS", fakeResult{columns: []string{"name"}})
			h := setupProbes(t, probeEnv{
				cfg: configuration{HealthMode: mode, EnhancedCheck: true},
				db:  openFakeDB(t, fixtures), probe: openFakeDB(t, probeFixtures()),
			})
			rec := get(t, h, "/health")
			isJSON := rec.Header().Get("Content-Type") == "application/json"
			if rec.Code != http.StatusOK || isJSON != wantJSON {
				t.Errorf("/health in %s mode: %d json=%v body %s", mode, rec.Code, isJSON, rec.Body)
			}
		})
	}
}

func TestProbeCache(t *testing.T) {
	for _, tt := range []struct {
		ttl       time.Duration
		wantCalls int64
	}{
		{time.Hour, 1},
		{0, 3},
	} {
		var calls atomic.Int64
		fixtures := with(probeFixtures(), "SHOW VERSION", fakeResult{
			columns: []string{"version"}, rows: [][]driver.Value{{"PgBouncer"}}, calls: &calls,
		})
		h := setupProbes(t, probeEnv{cfg: configuration{CheckCacheTTL: tt.ttl}, probe: openFakeDB(t, fixtures)})
		for range 3 {
			getReport(t, h, "/health/ready")
		}
		if got := calls.Load(); got != tt.wantCalls {
			t.Errorf("ttl %s: %d SHOW VERSION queries, want %d", tt.ttl, got, tt.wantCalls)
		}
	}
}

// A server that accepts but never answers must not block a probe connection
// past its deadline.
func TestDeadlineDialer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close() // hold the connection open, never respond
		}
	}()

	d := deadlineDialer{timeout: func() time.Duration { return 100 * time.Millisecond }}
	for name, dial := range map[string]func() (net.Conn, error){
		"Dial":        func() (net.Conn, error) { return d.Dial("tcp", ln.Addr().String()) },
		"DialTimeout": func() (net.Conn, error) { return d.DialTimeout("tcp", ln.Addr().String(), time.Second) },
	} {
		conn, err := dial()
		if err != nil {
			t.Fatalf("%s: %s", name, err)
		}
		start := time.Now()
		_, err = conn.Read(make([]byte, 1))
		conn.Close()
		var ne net.Error
		if !errors.As(err, &ne) || !ne.Timeout() || time.Since(start) > 2*time.Second {
			t.Errorf("%s: read err %v after %s, want timeout after ~100ms", name, err, time.Since(start))
		}
	}

	ln.Close()
	if _, err := d.Dial("tcp", ln.Addr().String()); err == nil {
		t.Error("dial to closed listener succeeded")
	}
}

func TestReadySkipsChecksAfterDeadline(t *testing.T) {
	var r reportBuilder
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.run(ctx, "headroom", nil, checkHeadroom)
	r.runFunc(ctx, "backend", checkBackend)
	rep := r.report()
	if rep.Status != "fail" || checkNames(rep) != "headroom,backend" {
		t.Fatalf("report = %+v", rep)
	}
	for _, c := range rep.Checks {
		if c.OK || !strings.HasPrefix(c.Detail, "not run: context canceled") {
			t.Errorf("%s = %+v", c.Name, c)
		}
	}
}

func TestOpenProbeDB(t *testing.T) {
	d, err := openProbeDB("host=127.0.0.1 port=1 dbname=x sslmode=disable connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Conn(context.Background()); err == nil {
		t.Error("connect to closed port succeeded")
	}
	if _, err := openProbeDB("host='unterminated"); err == nil {
		t.Error("invalid connection string accepted")
	}
}

func TestBackendFailureBackoff(t *testing.T) {
	for _, tt := range []struct {
		backoff   time.Duration
		wantCalls int64
	}{
		{time.Hour, 1},
		{0, 3},
	} {
		var calls atomic.Int64
		backend := openFakeDB(t, map[string]fakeResult{"SELECT 1": {err: errors.New("no server"), calls: &calls}})
		h := setupProbes(t, probeEnv{
			cfg:     configuration{ReadyChecks: []string{"backend"}, BackendBackoff: tt.backoff},
			probe:   openFakeDB(t, probeFixtures()),
			backend: backend,
		})
		var last checkResult
		for range 3 {
			_, r := getReport(t, h, "/health/ready")
			last, _ = findCheck(r, "backend")
		}
		if got := calls.Load(); got != tt.wantCalls {
			t.Errorf("backoff %s: %d backend queries, want %d", tt.backoff, got, tt.wantCalls)
		}
		if last.OK || !strings.Contains(last.Detail, "no server") {
			t.Errorf("backoff %s: last backend result %+v", tt.backoff, last)
		}
		if tt.backoff > 0 && !strings.Contains(last.Detail, "next attempt in") {
			t.Errorf("backoff detail missing retry time: %q", last.Detail)
		}
	}

	// Success clears the backoff
	ok := openFakeDB(t, backendFixtures())
	h := setupProbes(t, probeEnv{cfg: configuration{ReadyChecks: []string{"backend"}, BackendBackoff: time.Hour},
		probe: openFakeDB(t, probeFixtures()), backend: ok})
	if code, _ := getReport(t, h, "/health/ready"); code != http.StatusOK || !backendFailure.at.IsZero() {
		t.Errorf("successful backend check: %d, failure state %v", code, backendFailure.at)
	}
}
