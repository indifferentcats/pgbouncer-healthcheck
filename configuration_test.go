package main

import (
	"errors"
	"flag"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigDefaults(t *testing.T) {
	c, err := loadConfig(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := configuration{
		Port: 8000, Connstr: "host=localhost port=6543 dbname=pgbouncer sslmode=disable", PGBouncerPort: 6543,
		HealthMode: "legacy", ReadyChecks: []string{"headroom", "databases"}, ClientHeadroomPct: 90,
		MaxWait: 5 * time.Second, MaxClWaiting: -1, BackendBackoff: 15 * time.Second, CheckCacheTTL: time.Second,
	}
	if !equalConfig(c, want) {
		t.Errorf("defaults =\n%+v\nwant\n%+v", c, want)
	}
}

// Flags override environment variables, which override defaults.
func TestLoadConfigPrecedence(t *testing.T) {
	t.Setenv("CLIENT_HEADROOM_PCT", "80")
	t.Setenv("READY_CHECKS", "headroom, pools")
	t.Setenv("MAX_WAIT", "2s")
	t.Setenv("PORT", "9000")

	c, err := loadConfig([]string{"--client-headroom-pct=70", "--ready-checks", "databases", "--port=9100"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.ClientHeadroomPct != 70 || !slices.Equal(c.ReadyChecks, []string{"databases"}) || c.Port != 9100 {
		t.Errorf("flags did not override env: %+v", c)
	}
	if c.MaxWait != 2*time.Second {
		t.Errorf("env MAX_WAIT not applied: %s", c.MaxWait)
	}

	c, err = loadConfig(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.ClientHeadroomPct != 80 || !slices.Equal(c.ReadyChecks, []string{"headroom", "pools"}) {
		t.Errorf("env not applied (or list not trimmed): %+v", c)
	}
}

func TestLoadConfigLists(t *testing.T) {
	c, err := loadConfig([]string{"--ready-databases", " app, ,billing ", "--ready-checks="}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.ReadyDatabases, []string{"app", "billing"}) || len(c.ReadyChecks) != 0 {
		t.Errorf("lists = %q %q", c.ReadyDatabases, c.ReadyChecks)
	}
}

func TestLoadConfigInvalid(t *testing.T) {
	tests := map[string]struct {
		args []string
		want string
	}{
		"mode":           {[]string{"--health-mode=sometimes"}, `health mode "sometimes"`},
		"check":          {[]string{"--ready-checks=headroom,typo"}, `unknown ready check "typo"`},
		"backend":        {[]string{"--ready-checks=backend"}, "backend ready check needs"},
		"headroom":       {[]string{"--client-headroom-pct=0"}, "must be between 1 and 100"},
		"duration":       {[]string{"--check-cache-ttl=-1s"}, "must not be negative"},
		"unknown flag":   {[]string{"--nope"}, "flag provided but not defined"},
		"positional arg": {[]string{"extra"}, "unexpected arguments"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := loadConfig(tt.args, io.Discard)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoadConfigHelp(t *testing.T) {
	var out strings.Builder
	_, err := loadConfig([]string{"-h"}, &out)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
	for _, want := range []string{"version", "-ready-checks", "env READY_CHECKS", "flags override environment"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("usage missing %q:\n%s", want, out.String())
		}
	}
}

func equalConfig(a, b configuration) bool {
	return slices.Equal(a.ReadyChecks, b.ReadyChecks) && slices.Equal(a.ReadyDatabases, b.ReadyDatabases) &&
		a.Port == b.Port && a.Connstr == b.Connstr && a.EnhancedCheck == b.EnhancedCheck &&
		a.EnableDebugEndpoints == b.EnableDebugEndpoints && a.PGBouncerPort == b.PGBouncerPort &&
		a.HealthMode == b.HealthMode && a.ClientHeadroomPct == b.ClientHeadroomPct &&
		a.MaxWait == b.MaxWait && a.MaxClWaiting == b.MaxClWaiting &&
		a.BackendConnstr == b.BackendConnstr && a.BackendBackoff == b.BackendBackoff && a.CheckCacheTTL == b.CheckCacheTTL
}
