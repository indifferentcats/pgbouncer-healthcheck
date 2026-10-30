package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type configuration struct {
	Port                 int    `default:"8000"`
	Connstr              string `default:"host=localhost port=6543 dbname=pgbouncer sslmode=disable"`
	EnhancedCheck        bool   `default:"false" split_words:"true"`
	EnableDebugEndpoints bool   `default:"false" split_words:"true"`
	PGBouncerPort        int    `envconfig:"pgbouncer_port" default:"6543"`

	// /health/live and /health/ready (see checks.go)
	HealthMode        string        `envconfig:"health_mode" default:"legacy"`
	ReadyChecks       []string      `envconfig:"ready_checks" default:"headroom,databases"`
	ReadyDatabases    []string      `envconfig:"ready_databases"`
	ClientHeadroomPct int           `envconfig:"client_headroom_pct" default:"90"`
	MaxWait           time.Duration `envconfig:"max_wait" default:"5s"`
	MaxClWaiting      int           `envconfig:"max_cl_waiting" default:"-1"`
	BackendConnstr    string        `envconfig:"backend_connstr"`
	BackendBackoff    time.Duration `envconfig:"backend_failure_backoff" default:"15s"`
	CheckCacheTTL     time.Duration `envconfig:"check_cache_ttl" default:"1s"`
}

// Optional readiness checks; "console" always runs.
var knownReadyChecks = []string{"headroom", "databases", "pools", "backend"}

var healthModes = []string{"legacy", "live", "ready"}

// loadConfig reads configuration from the environment, then lets
// command-line flags override it: flag > environment > default.
func loadConfig(args []string, usageOut io.Writer) (configuration, error) {
	var c configuration
	if err := envconfig.Process("", &c); err != nil {
		return c, err
	}
	// envconfig splits lists on commas but doesn't trim spaces
	c.ReadyChecks = splitList(strings.Join(c.ReadyChecks, ","))
	c.ReadyDatabases = splitList(strings.Join(c.ReadyDatabases, ","))

	fs := flag.NewFlagSet("pgbouncer-healthcheck", flag.ContinueOnError)
	fs.SetOutput(usageOut)
	fs.IntVar(&c.Port, "port", c.Port, "HTTP listen port (env PORT)")
	fs.StringVar(&c.Connstr, "connstr", c.Connstr, "PGBouncer admin console connection string (env CONNSTR; pass passwords via env or PGPASSWORD, not flags)")
	fs.BoolVar(&c.EnhancedCheck, "enhanced-check", c.EnhancedCheck, "legacy /health: query PGBouncer instead of a port probe (env ENHANCED_CHECK)")
	fs.BoolVar(&c.EnableDebugEndpoints, "enable-debug-endpoints", c.EnableDebugEndpoints, "enable /debug/* (env ENABLE_DEBUG_ENDPOINTS)")
	fs.IntVar(&c.PGBouncerPort, "pgbouncer-port", c.PGBouncerPort, "port for the legacy /health port probe (env PGBOUNCER_PORT)")
	fs.StringVar(&c.HealthMode, "health-mode", c.HealthMode, "what /health does: legacy, live or ready (env HEALTH_MODE)")
	fs.Var((*listFlag)(&c.ReadyChecks), "ready-checks", "comma-separated /health/ready checks: "+strings.Join(knownReadyChecks, ",")+" (env READY_CHECKS)")
	fs.Var((*listFlag)(&c.ReadyDatabases), "ready-databases", "limit the databases and pools checks to these databases; default all except pgbouncer (env READY_DATABASES)")
	fs.IntVar(&c.ClientHeadroomPct, "client-headroom-pct", c.ClientHeadroomPct, "headroom check fails at this % of max_client_conn (env CLIENT_HEADROOM_PCT)")
	fs.DurationVar(&c.MaxWait, "max-wait", c.MaxWait, "pools check fails when a pool's maxwait exceeds this (env MAX_WAIT)")
	fs.IntVar(&c.MaxClWaiting, "max-cl-waiting", c.MaxClWaiting, "pools check fails when a pool's cl_waiting exceeds this; -1 disables (env MAX_CL_WAITING)")
	fs.StringVar(&c.BackendConnstr, "backend-connstr", c.BackendConnstr, "connection string through PGBouncer to an application database, for the backend check (env BACKEND_CONNSTR; pass passwords via env)")
	fs.DurationVar(&c.BackendBackoff, "backend-failure-backoff", c.BackendBackoff, "after the backend check fails, report that failure without retrying for this long; 0 disables (env BACKEND_FAILURE_BACKOFF)")
	fs.DurationVar(&c.CheckCacheTTL, "check-cache-ttl", c.CheckCacheTTL, "cache /health/live and /health/ready results for this long; 0 disables (env CHECK_CACHE_TTL)")
	fs.Usage = func() {
		version(usageOut)
		fmt.Fprintln(usageOut, "\nOptions (flags override environment variables):")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return c, err
	}
	if fs.NArg() > 0 {
		return c, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	return c, c.validate()
}

func (c configuration) validate() error {
	var errs []error
	if !slices.Contains(healthModes, c.HealthMode) {
		errs = append(errs, fmt.Errorf("health mode %q must be one of %v", c.HealthMode, healthModes))
	}
	for _, name := range c.ReadyChecks {
		if name != "console" && !slices.Contains(knownReadyChecks, name) {
			errs = append(errs, fmt.Errorf("unknown ready check %q (known: %v)", name, knownReadyChecks))
		}
	}
	if slices.Contains(c.ReadyChecks, "backend") && c.BackendConnstr == "" {
		errs = append(errs, errors.New("the backend ready check needs BACKEND_CONNSTR / --backend-connstr"))
	}
	if c.ClientHeadroomPct < 1 || c.ClientHeadroomPct > 100 {
		errs = append(errs, fmt.Errorf("client headroom %d%% must be between 1 and 100", c.ClientHeadroomPct))
	}
	if c.CheckCacheTTL < 0 || c.MaxWait < 0 || c.BackendBackoff < 0 {
		errs = append(errs, errors.New("durations must not be negative"))
	}
	return errors.Join(errs...)
}

// listFlag is a comma-separated flag value that replaces (not appends to)
// the list, so a flag fully overrides the environment.
type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }

func (l *listFlag) Set(s string) error {
	*l = splitList(s)
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
