package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/lib/pq"
)

// /health/live answers "is PGBouncer wedged?" and never depends on the
// databases behind it. /health/ready answers "should this instance receive
// traffic?" and runs the configured checks over a fresh login.

var (
	// probeDB keeps no idle connections, so every readiness probe logs in
	// fresh and exercises authentication and client admission.
	probeDB *sql.DB
	// backendDB connects through PGBouncer to an application database, for
	// the optional backend check.
	backendDB *sql.DB

	liveProbe, readyProbe *cachedProbe
)

// openProbeDB opens a database whose connections are each used for a single
// probe: none are kept idle, and each has an absolute I/O deadline.
func openProbeDB(connstr string) (*sql.DB, error) {
	connector, err := pq.NewConnector(connstr)
	if err != nil {
		return nil, err
	}
	connector.Dialer(deadlineDialer{timeout: probeTimeout})
	d := sql.OpenDB(connector)
	d.SetMaxIdleConns(0)
	return d, nil
}

// probeTimeout bounds a probe connection's lifetime, leaving time to write
// the response before requestTimeout.
func probeTimeout() time.Duration { return requestTimeout * 9 / 10 }

// deadlineDialer sets an I/O deadline on every connection it dials. Context
// cancellation alone isn't enough: lib/pq then sends a cancel request but
// keeps waiting for the server, and PGBouncer doesn't answer a client queued
// for an unreachable server until query_wait_timeout (120s by default). The
// deadline makes the blocked read fail on time, so the probe returns and the
// connection is closed instead of leaking a PGBouncer client slot.
type deadlineDialer struct {
	timeout func() time.Duration
}

func (d deadlineDialer) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, address)
}

func (d deadlineDialer) DialTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return d.DialContext(ctx, network, address)
}

func (d deadlineDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	if err := conn.SetDeadline(time.Now().Add(d.timeout())); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

type checkResult struct {
	Name       string  `json:"name"`
	OK         bool    `json:"ok"`
	Detail     string  `json:"detail,omitempty"`
	DurationMs float64 `json:"duration_ms"`
}

type healthReport struct {
	Status string        `json:"status"`
	Checks []checkResult `json:"checks"`
}

// checkFunc returns a detail message on success, or an error.
type checkFunc func(ctx context.Context, q queryer) (string, error)

var readyChecks = map[string]checkFunc{
	"headroom":  checkHeadroom,
	"databases": checkDatabases,
	"pools":     checkPools,
}

func addLiveReadyHandlers(router *httprouter.Router) {
	liveProbe = &cachedProbe{run: runLive}
	readyProbe = &cachedProbe{run: runReady}
	router.GET("/health/live", makeRequestHandlerWithContext(reportHandler(liveProbe)))
	router.GET("/health/ready", makeRequestHandlerWithContext(reportHandler(readyProbe)))
}

// reportHandler writes the report as JSON: 200 if every check passed,
// otherwise 503.
func reportHandler(p *cachedProbe) requestHandlerWithContext {
	return func(ctx context.Context, w http.ResponseWriter, _ httprouter.Params) error {
		report := p.get(ctx)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if report.Status != "ok" {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		return json.NewEncoder(w).Encode(report)
	}
}

func runLive(ctx context.Context) healthReport {
	var r reportBuilder
	if db == nil {
		r.fail("console", errors.New("PGBouncer database is not configured"))
	} else {
		r.run(ctx, "console", db, checkConsole)
	}
	return r.report()
}

func runReady(ctx context.Context) healthReport {
	var r reportBuilder
	if probeDB == nil {
		r.fail("login", errors.New("PGBouncer database is not configured"))
		return r.report()
	}

	start := time.Now()
	conn, err := probeDB.Conn(ctx)
	if err != nil {
		r.add("login", start, "", fmt.Errorf("could not log in to PGBouncer: %w", err))
		return r.report()
	}
	defer conn.Close()
	r.add("login", start, "fresh admin console login", nil)

	r.run(ctx, "console", conn, checkConsole)
	for _, name := range config.ReadyChecks {
		if fn, ok := readyChecks[name]; ok {
			r.run(ctx, name, conn, fn)
		}
	}
	if slices.Contains(config.ReadyChecks, "backend") {
		r.runFunc(ctx, "backend", checkBackend)
	}
	return r.report()
}

type reportBuilder struct {
	checks []checkResult
	failed bool
}

func (r *reportBuilder) add(name string, start time.Time, detail string, err error) {
	res := checkResult{
		Name:       name,
		OK:         err == nil,
		Detail:     detail,
		DurationMs: float64(time.Since(start).Microseconds()) / 1000,
	}
	if err != nil {
		res.Detail = err.Error()
		r.failed = true
	}
	r.checks = append(r.checks, res)
}

func (r *reportBuilder) fail(name string, err error) { r.add(name, time.Now(), "", err) }

func (r *reportBuilder) run(ctx context.Context, name string, q queryer, fn checkFunc) {
	if r.skipped(ctx, name) {
		return
	}
	start := time.Now()
	detail, err := fn(ctx, q)
	r.add(name, start, detail, err)
}

func (r *reportBuilder) runFunc(ctx context.Context, name string, fn func(context.Context) (string, error)) {
	if r.skipped(ctx, name) {
		return
	}
	start := time.Now()
	detail, err := fn(ctx)
	r.add(name, start, detail, err)
}

// skipped records a check as not run once the probe has run out of time.
func (r *reportBuilder) skipped(ctx context.Context, name string) bool {
	if err := ctx.Err(); err != nil {
		r.fail(name, fmt.Errorf("not run: %w", err))
		return true
	}
	return false
}

func (r *reportBuilder) report() healthReport {
	status := "ok"
	if r.failed {
		status = "fail"
	}
	return healthReport{Status: status, Checks: r.checks}
}

// cachedProbe shares one result between callers for config.CheckCacheTTL,
// so several load balancers probing at once don't flood the admin console.
// Concurrent callers wait for the in-flight probe rather than starting more.
type cachedProbe struct {
	run func(context.Context) healthReport

	mu     sync.Mutex
	at     time.Time
	report healthReport
}

func (p *cachedProbe) get(ctx context.Context) healthReport {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ttl := config.CheckCacheTTL; ttl > 0 && !p.at.IsZero() && time.Since(p.at) < ttl {
		return p.report
	}
	p.report = p.run(ctx)
	p.at = time.Now()
	return p.report
}

func checkConsole(ctx context.Context, q queryer) (string, error) {
	rows, err := queryRows[struct {
		Version string `db:"version"`
	}](ctx, q, "SHOW VERSION")
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", errors.New("SHOW VERSION returned no rows")
	}
	return rows[0].Version, nil
}

// checkHeadroom fails when client connections approach max_client_conn, at
// which point PGBouncer rejects new application clients (admin console
// logins are still accepted, so a login alone doesn't detect this).
func checkHeadroom(ctx context.Context, q queryer) (string, error) {
	lists, err := queryRows[struct {
		List  string `db:"list"`
		Items int64  `db:"items"`
	}](ctx, q, "SHOW LISTS")
	if err != nil {
		return "", err
	}
	used := int64(-1)
	for _, l := range lists {
		if l.List == "used_clients" {
			used = l.Items
		}
	}
	if used < 0 {
		return "", errors.New("SHOW LISTS has no used_clients")
	}

	configs, err := queryRows[Config](ctx, q, "SHOW CONFIG")
	if err != nil {
		return "", err
	}
	var maxClients int64
	for _, c := range configs {
		if c.Key == "max_client_conn" && c.Value != nil {
			maxClients, _ = strconv.ParseInt(*c.Value, 10, 64)
		}
	}
	if maxClients <= 0 {
		return "", errors.New("could not read max_client_conn from SHOW CONFIG")
	}

	pct := float64(used) * 100 / float64(maxClients)
	msg := fmt.Sprintf("used_clients %d of max_client_conn %d (%.0f%%)", used, maxClients, pct)
	if pct >= float64(config.ClientHeadroomPct) {
		return "", fmt.Errorf("%s is at or above %d%%", msg, config.ClientHeadroomPct)
	}
	return msg, nil
}

// inScope reports whether a database is covered by the databases and pools
// checks: those in READY_DATABASES, or all but the admin console.
func inScope(name string) bool {
	if len(config.ReadyDatabases) > 0 {
		return slices.Contains(config.ReadyDatabases, name)
	}
	return name != "pgbouncer"
}

func checkDatabases(ctx context.Context, q queryer) (string, error) {
	dbs, err := queryRows[Database](ctx, q, "SHOW DATABASES")
	if err != nil {
		return "", err
	}
	var problems []string
	seen := map[string]bool{}
	checked := 0
	for _, d := range dbs {
		seen[d.Name] = true
		if !inScope(d.Name) {
			continue
		}
		checked++
		if d.Paused != 0 {
			problems = append(problems, d.Name+" is paused")
		}
		if d.Disabled != 0 {
			problems = append(problems, d.Name+" is disabled")
		}
	}
	for _, name := range config.ReadyDatabases {
		if !seen[name] {
			problems = append(problems, name+" is not configured in PGBouncer")
		}
	}
	if len(problems) > 0 {
		return "", errors.New(strings.Join(problems, "; "))
	}
	return fmt.Sprintf("%d databases ok", checked), nil
}

func checkPools(ctx context.Context, q queryer) (string, error) {
	pools, err := queryRows[Pool](ctx, q, "SHOW POOLS")
	if err != nil {
		return "", err
	}
	var problems []string
	checked := 0
	for _, p := range pools {
		if !inScope(p.Database) {
			continue
		}
		checked++
		name := p.Database + "/" + p.User
		wait := time.Duration(p.MaxWait)*time.Second + time.Duration(p.MaxWaitUs)*time.Microsecond
		if wait > config.MaxWait {
			problems = append(problems, fmt.Sprintf("%s maxwait %s > %s", name, wait.Round(time.Millisecond), config.MaxWait))
		}
		if config.MaxClWaiting >= 0 && p.ClWaiting > int64(config.MaxClWaiting) {
			problems = append(problems, fmt.Sprintf("%s cl_waiting %d > %d", name, p.ClWaiting, config.MaxClWaiting))
		}
	}
	if len(problems) > 0 {
		return "", errors.New(strings.Join(problems, "; "))
	}
	return fmt.Sprintf("%d pools ok", checked), nil
}

// backendFailure remembers the last backend check failure for the backoff.
var backendFailure struct {
	mu  sync.Mutex
	at  time.Time
	err error
}

// checkBackend runs SELECT 1 through PGBouncer on a fresh connection, which
// proves PGBouncer can reach and log in to the database behind it.
//
// After a failure it isn't retried for config.BackendBackoff. When the
// database is unreachable, PGBouncer keeps each abandoned probe client
// waiting (holding a max_client_conn slot) until its query_wait_timeout, so
// retrying on every probe would use up client slots during an outage.
func checkBackend(ctx context.Context) (string, error) {
	backendFailure.mu.Lock()
	defer backendFailure.mu.Unlock()
	if backoff := config.BackendBackoff; backoff > 0 && !backendFailure.at.IsZero() {
		if left := backoff - time.Since(backendFailure.at); left > 0 {
			return "", fmt.Errorf("%w (last failure; next attempt in %s)", backendFailure.err, left.Round(time.Second))
		}
	}
	detail, err := queryBackend(ctx)
	if err != nil {
		backendFailure.at, backendFailure.err = time.Now(), err
	} else {
		backendFailure.at, backendFailure.err = time.Time{}, nil
	}
	return detail, err
}

func queryBackend(ctx context.Context) (string, error) {
	if backendDB == nil {
		return "", errors.New("backend database is not configured")
	}
	conn, err := backendDB.Conn(ctx)
	if err != nil {
		return "", fmt.Errorf("could not connect through PGBouncer: %w", err)
	}
	defer conn.Close()
	var one int
	if err := conn.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		return "", fmt.Errorf("SELECT 1 failed: %w", err)
	}
	return "SELECT 1 ok", nil
}
