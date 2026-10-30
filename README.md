# PGBouncer healthcheck

[![Build](https://github.com/indifferentcats/pgbouncer-healthcheck/actions/workflows/build.yml/badge.svg?branch=master)](https://github.com/indifferentcats/pgbouncer-healthcheck/actions/workflows/build.yml)
[![Unit tests](https://github.com/indifferentcats/pgbouncer-healthcheck/actions/workflows/unit-tests.yml/badge.svg?branch=master)](https://github.com/indifferentcats/pgbouncer-healthcheck/actions/workflows/unit-tests.yml)
[![Coverage](https://raw.githubusercontent.com/indifferentcats/pgbouncer-healthcheck/badges/coverage.svg)](https://github.com/indifferentcats/pgbouncer-healthcheck/actions/workflows/unit-tests.yml)
[![CodeQL](https://github.com/indifferentcats/pgbouncer-healthcheck/actions/workflows/codeql-analysis.yml/badge.svg?branch=master)](https://github.com/indifferentcats/pgbouncer-healthcheck/actions/workflows/codeql-analysis.yml)
[![Security](https://github.com/indifferentcats/pgbouncer-healthcheck/actions/workflows/security.yml/badge.svg?branch=master)](https://github.com/indifferentcats/pgbouncer-healthcheck/actions/workflows/security.yml)

## Introduction

This implements a http `/health` check for PGBouncer hosts.
This healthcheck provides a simple yes/no health response based
on attempting to connect to PGBouncer and execute a request.

There are also several endpoints used for diagnostics.

> **Upgrading from 2.x?** Version 3.0 removes the Datadog agent check
> (`CHECK_DDAGENT`) and changes the JSON returned by every `/status/*`
> endpoint. See [Upgrading from 2.x](#upgrading-from-2x) and the
> [CHANGELOG](CHANGELOG.md).

## Building and running

Builds and tests run in containers, driven by [Task](https://taskfile.dev).
You need `task` and either Podman (default) or Docker. A local Go toolchain is
only needed for `task vet` / `task tidy`.

    $ task build                  # static binary for the host arch, in dist/linux-<arch>/
    $ task build ARCH=amd64       # release (x86-64) binary
    $ task test                   # integration tests against a real PGBouncer
    $ task test -- enhanced.sh    # run a subset of test scripts
    $ task test:unit              # Go unit tests (needs local Go), with coverage
    $ task coverage               # HTML coverage report in dist/coverage.html
    $ task vuln                   # dependency vulnerability scan
    $ task image                  # minimal FROM-scratch runtime image
    $ task --list                 # everything else

Use Docker instead of Podman with `CONTAINER_TOOL=docker`, e.g.
`task build CONTAINER_TOOL=docker`.

The binary is fully statically linked (`CGO_ENABLED=0`) with no runtime
dependencies, so it can be copied into a very slim container, e.g.:

```dockerfile
FROM scratch
COPY pgbouncer-healthcheck /pgbouncer-healthcheck
ENTRYPOINT ["/pgbouncer-healthcheck"]
```

Note that `/ami-version` and the `/debug/*` endpoints run external commands
or read host files (`dmesg`, `ps`, `journalctl`, `/etc/ami_version`), which
are not present in a `scratch` image.
The `/health` port probe and enhanced check, and all `/status/*` endpoints,
work without them.

Run the binary with:

    $ ./pgbouncer-healthcheck

### Continuous integration

GitHub Actions run on every push to `master` and every PR:

- **Build** (`build.yml`): `task build` and `task test` on native `linux/amd64`
  (`ubuntu-24.04`) and `linux/arm64` (`ubuntu-24.04-arm`) runners. Checks the
  binary is static and uploads it as a workflow artifact.
- **Unit tests** (`unit-tests.yml`): `go test -race` with coverage. Results and
  per-function coverage appear in the job summary, and the raw results are
  uploaded as an artifact. On `master`, the coverage badge is regenerated and
  pushed to the `badges` branch; no third-party coverage service is used.
- **CodeQL** (`codeql-analysis.yml`): static analysis.
- **Security** (`security.yml`), on every change and daily:
  - `govulncheck` checks the Go vulnerability database against dependencies
    and the Go standard library, and fails on vulnerabilities in code that is
    actually reachable.
  - `osv-scanner` checks OSV, which aggregates the Go database, GitHub
    advisories and OpenSSF malicious-package reports, so it also catches
    dependencies announced as compromised. Results go to the Security tab.
  - On PRs, `dependency-review` blocks newly added or updated dependencies,
    including GitHub Actions, that have known advisories.

The unit-test, CodeQL and govulncheck jobs run inside the digest-pinned
`golang:<version>-trixie` image with `GOTOOLCHAIN=local`, so CI never downloads
an unverified Go toolchain.

### Supply-chain pinning

Build inputs are pinned:
- Go modules: by SHA-256 in `go.sum`, checked against the public Go checksum
  database. CI tools such as govulncheck are pinned in `tools/go.mod`.
- The Go toolchain: the `toolchain` line in `go.mod`, plus digest-pinned Go
  images in the `Dockerfile` and CI.
- Base images: by digest.
- GitHub Actions: by commit SHA, with runners pinned to a specific Ubuntu
  release.
- Downloaded tools (go-task, osv-scanner): by SHA-256.

Dependabot (`.github/dependabot.yml`) checks weekly for updates to Go
modules, GitHub Actions and Dockerfile images, and keeps them pinned. It
waits 7 days after a release before proposing it, so a compromised release is
more likely to be caught first; security updates aren't delayed. **Dependabot
only opens pull requests. Nothing auto-merges them**: the repository's
auto-merge setting is off, no workflow merges PRs, and every update needs a
person to review and merge it. The owner is requested as a reviewer through
`.github/CODEOWNERS` and assigned to each PR, which sends notification emails.
Branch protection on `master` enforces this. Merging needs an approving
review from the code owner, plus passing Build (amd64 and arm64), Unit tests,
CodeQL and Security checks, all reported by GitHub Actions. Approvals are
dismissed when new commits are pushed, and force-pushes and branch deletion
are blocked.

Dependabot doesn't update these, so bump them by hand:
- the `golang:<version>-trixie@sha256:` image in the workflows' `container:`
  blocks
- `toolchain` in `go.mod` and `tools/go.mod`
- the SHA-256-pinned go-task and osv-scanner downloads When bumping any of them, update the version and the
hash together.

## Configuration

Every option can be set with an environment variable or a command-line flag.
Flags override environment variables, which override the defaults. Run
`pgbouncer-healthcheck -h` to list them all. Pass secrets, such as passwords
in connection strings, through environment variables rather than flags,
because flags are visible to other users in `ps`.

### Postgres connection string for PGBouncer connection

The connection string comes from the env var `CONNSTR`. The default is
`host=localhost port=6543 dbname=pgbouncer sslmode=disable`. This should
be fine for most use cases.

The intention is that the username and password will be supplied by setting the
`PGUSER` and `PGPASSWORD` standard variables.

The driver supports SCRAM-SHA-256, so configure PGBouncer with
`auth_type = scram-sha-256` rather than `md5`. `tests/scram-secret.py PASSWORD`
prints a SCRAM secret for PGBouncer's `auth_file`.

### Other parameters

| Environment variable | Flag | Default | Description |
|---|---|---|---|
| `PORT` | `--port` | `8000` | TCP port to listen on for HTTP |
| `CONNSTR` | `--connstr` | see above | PGBouncer admin console connection string |
| `ENHANCED_CHECK` | `--enhanced-check` | `false` | Legacy `/health`: query PGBouncer instead of a port probe |
| `PGBOUNCER_PORT` | `--pgbouncer-port` | `6543` | Port for the legacy `/health` port probe |
| `ENABLE_DEBUG_ENDPOINTS` | `--enable-debug-endpoints` | `false` | Enable the `/debug/` endpoints (see below) |
| `HEALTH_MODE` | `--health-mode` | `legacy` | What `/health` does: `legacy`, `live` or `ready` |
| `READY_CHECKS` | `--ready-checks` | `headroom,databases` | Checks run by `/health/ready`: any of `headroom`, `databases`, `pools`, `backend` |
| `READY_DATABASES` | `--ready-databases` | all but `pgbouncer` | Limit the `databases` and `pools` checks to these databases |
| `CLIENT_HEADROOM_PCT` | `--client-headroom-pct` | `90` | `headroom` fails when `used_clients` reaches this % of `max_client_conn` |
| `MAX_WAIT` | `--max-wait` | `5s` | `pools` fails when a pool's `maxwait` is longer than this |
| `MAX_CL_WAITING` | `--max-cl-waiting` | `-1` (off) | `pools` fails when a pool has more waiting clients than this |
| `BACKEND_CONNSTR` | `--backend-connstr` | | Connection string *through PGBouncer* to an application database, for `backend` |
| `BACKEND_FAILURE_BACKOFF` | `--backend-failure-backoff` | `15s` | After `backend` fails, report that failure without retrying for this long |
| `CHECK_CACHE_TTL` | `--check-cache-ttl` | `1s` | Share `/health/live` and `/health/ready` results between callers for this long |

With the defaults, `/health` just checks that something accepts TCP
connections on port 6543, and `/health/live` and `/health/ready` are available
alongside it.

## Main Endpoints

Note: See https://www.pgbouncer.org/usage.html for details of the PGBouncer
`SHOW` command outputs.

### /

This always returns a `200 OK`

### /health
The healthcheck returns a 200 OK response or a `500 Internal Server Error` response.
It will probe the PGBouncer using a method depending on  `ENHANCED_CHECK`.

This legacy check can report healthy while PGBouncer can't serve clients. For
example, when `max_client_conn` is reached, PGBouncer rejects new application
clients but still accepts the TCP connection and admin console logins. Prefer
`/health/live` and `/health/ready`, or set `HEALTH_MODE=live` or
`HEALTH_MODE=ready` to make `/health` use them.

### /health/live

Is PGBouncer itself working? Use this for liveness probes, the ones that
restart the process. It checks that the admin console answers `SHOW VERSION`,
and it **never depends on the databases behind PGBouncer**: restarting
PGBouncer can't fix a database outage, and restarting every instance at once
would drop every client.

### /health/ready

Should this instance receive traffic? Use this for readiness and load
balancer health checks. Each probe logs in to the admin console fresh, so it
also tests authentication, then runs the configured checks:

| Check | Fails when | Source |
|---|---|---|
| `login`, `console` (always) | the admin console can't be logged in to or doesn't answer | `SHOW VERSION` |
| `headroom` (default) | `used_clients` ≥ `CLIENT_HEADROOM_PCT`% of `max_client_conn` | `SHOW LISTS`, `SHOW CONFIG` |
| `databases` (default) | a database in scope is paused or disabled, or a `READY_DATABASES` entry isn't configured | `SHOW DATABASES` |
| `pools` | a pool in scope has `maxwait` > `MAX_WAIT`, or `cl_waiting` > `MAX_CL_WAITING` | `SHOW POOLS` |
| `backend` | `SELECT 1` through PGBouncer via `BACKEND_CONNSTR` fails | the real database |

Every check except `backend` only needs the permissions of a PGBouncer
`stats_users` user.

Both endpoints return `200` or `503` with a JSON report:

```json
{
  "status": "fail",
  "checks": [
    {"name": "login", "ok": true, "detail": "fresh admin console login", "duration_ms": 3.1},
    {"name": "console", "ok": true, "detail": "PgBouncer 1.25.2", "duration_ms": 0.4},
    {"name": "headroom", "ok": false, "detail": "used_clients 7 of max_client_conn 10 (70%) is at or above 50%", "duration_ms": 0.8},
    {"name": "databases", "ok": true, "detail": "2 databases ok", "duration_ms": 0.5}
  ]
}
```

Probes are bounded by the request timeout, even if PGBouncer never answers.

**Things to know before enabling `pools` or `backend`:**
- **A database-wide problem fails readiness on every instance at once.**
  Kubernetes then removes all endpoints, and an AWS NLB sends traffic to all
  targets anyway ("fails open"). That's why these checks are off by default.
  Use `READY_DATABASES` so that one tenant's database can't take a shared
  PGBouncer out of service.
- **While the database is unreachable, PGBouncer keeps each abandoned
  `backend` probe waiting** until its `query_wait_timeout` (120s by default),
  and each one holds a `max_client_conn` slot. `BACKEND_FAILURE_BACKOFF`
  limits this to one attempt per backoff period. Consider lowering
  `query_wait_timeout` as well.

## PGBouncer Information Endpoints

Each endpoint returns the matching PGBouncer `SHOW` command as a JSON array.
Keys are the PGBouncer column names. Columns are matched by name, so columns
added in newer PGBouncer versions are ignored, and fields for columns an older
PGBouncer doesn't return are left at their zero value. Nullable columns are
returned as `null`. An empty result is `[]`.

The examples below are from PGBouncer 1.25.

### /status/users

Returns the results of PGBouncer's `SHOW USERS` command.

```json
[
  {
    "name": "stats",
    "pool_size": null,
    "reserve_pool_size": null,
    "pool_mode": null,
    "max_user_connections": 0,
    "current_connections": 0,
    "max_user_client_connections": 0,
    "current_client_connections": 1
  }
]
```

### /status/configs

Returns the results of PGBouncer's `SHOW CONFIG` command. `changeable` is a boolean.

```json
[
  {
    "key": "listen_port",
    "value": "6543",
    "default": "6432",
    "changeable": false
  },
  {
    "key": "pool_mode",
    "value": "transaction",
    "default": "session",
    "changeable": true
  }
]
```
*(Truncated for brevity)*

### /status/databases

Returns the results of PGBouncer's `SHOW DATABASES` command.
This is essentially the content of the `[databases]` section in the config file.

```json
[
  {
    "name": "dummy",
    "host": "10.0.0.1",
    "port": 5432,
    "database": "dummy",
    "force_user": null,
    "pool_size": 100,
    "min_pool_size": 0,
    "reserve_pool_size": 1,
    "server_lifetime": 3600,
    "pool_mode": null,
    "load_balance_hosts": null,
    "max_connections": 0,
    "current_connections": 0,
    "max_client_connections": 0,
    "current_client_connections": 0,
    "paused": 0,
    "disabled": 0
  }
]
```

### /status/pools

Returns the results of PGBouncer's `SHOW POOLS` command.
This is similar to `SHOW DATABASES`, except it's a product of that set by
the users, since the pooling is per user per logical DB.

```json
[
  {
    "database": "pgbouncer",
    "user": "pgbouncer",
    "cl_active": 1,
    "cl_waiting": 0,
    "cl_active_cancel_req": 0,
    "cl_waiting_cancel_req": 0,
    "sv_active": 0,
    "sv_active_cancel": 0,
    "sv_being_canceled": 0,
    "sv_idle": 0,
    "sv_used": 0,
    "sv_tested": 0,
    "sv_login": 0,
    "maxwait": 0,
    "maxwait_us": 0,
    "pool_mode": "statement",
    "load_balance_hosts": null
  }
]
```

### /status/clients

Returns the results of PGBouncer's `SHOW CLIENTS` command.

```json
[
  {
    "type": "C",
    "user": "stats",
    "database": "pgbouncer",
    "replication": "none",
    "state": "idle",
    "addr": "127.0.0.1",
    "port": 58578,
    "local_addr": "127.0.0.1",
    "local_port": 6543,
    "connect_time": "2026-10-30 15:39:01 UTC",
    "request_time": "2026-10-30 15:39:04 UTC",
    "wait": 2,
    "wait_us": 39073,
    "close_needed": 0,
    "ptr": "0xffffbd460400",
    "link": "",
    "remote_pid": 0,
    "tls": "",
    "application_name": "",
    "prepared_statements": 0,
    "id": 1
  }
]
```

### /status/servers

Returns the results of PGBouncer's `SHOW SERVERS` command, with the same fields
as `/status/clients` (`type` is `"S"` for server connections).

### /status/memory

Returns the results of PGBouncer's `SHOW MEM` command.

```json
[
  {
    "name": "user_cache",
    "size": 2432,
    "used": 2,
    "free": 48,
    "memtotal": 121600
  }
]
```

### /status/stats

Returns the results of PGBouncer's `SHOW STATS` command.

```json
[
  {
    "database": "pgbouncer",
    "total_server_assignment_count": 0,
    "total_xact_count": 1,
    "total_query_count": 1,
    "total_received": 0,
    "total_sent": 0,
    "total_xact_time": 0,
    "total_query_time": 0,
    "total_wait_time": 0,
    "total_client_parse_count": 0,
    "total_server_parse_count": 0,
    "total_bind_count": 0,
    "avg_server_assignment_count": 0,
    "avg_xact_count": 0,
    "avg_query_count": 0,
    "avg_recv": 0,
    "avg_sent": 0,
    "avg_xact_time": 0,
    "avg_query_time": 0,
    "avg_wait_time": 0,
    "avg_client_parse_count": 0,
    "avg_server_parse_count": 0,
    "avg_bind_count": 0
  }
]
```

## Server Debug Endpoints

These were added to help debug unreachable servers. You need to start the binary with
`ENABLE_DEBUG_ENDPOINTS=1` to make the available.

### /debug/dmesg

Returns the raw output of `dmesg`

### /debug/processes

Returns the raw output of `ps -eo user,pid,ppid,c,stime,tty,%cpu,%mem,vsz,rsz,cmd`

### /debug/meminfo

Returns the contents of `/proc/meminfo`.

### /debug/logs

Returns the output of `journalctl --reverse -b --no-pager -n 50`

## Upgrading from 2.x

3.0 is a breaking release in two ways.

**The Datadog agent check (`CHECK_DDAGENT`) is removed.** The variable is now
silently ignored, so `/health` no longer reflects the agent's health. Monitor
the agent through its own health check instead, such as container liveness and
readiness probes on the agent's `/live` and `/ready` endpoints, or
`datadog-agent health` on hosts. See the [CHANGELOG](CHANGELOG.md) for details.

**The `/status/*` JSON has changed:**

1. **All keys are now snake_case PGBouncer column names** instead of Go field
   names (`"PoolMode"` → `"pool_mode"`, `"ClActive"` → `"cl_active"`, …).
   A few don't follow the mechanical rename: `ReservePool` → `reserve_pool_size`,
   `MaxWait` → `maxwait`, `MemTotal` → `memtotal`, `TLS` → `tls`.
2. **Empty results are `[]`**, not `null`.
3. **`/status/databases` `port` is a number**, not a string.
4. **`/status/stats` no longer has `TotalRequests`, `AvgReq` or `AvgQuery`**
   (PGBouncer removed them in 1.8). Use `total_query_count` /
   `total_xact_count`, `avg_query_count` and `avg_query_time`.
5. **`/status/pools` `pool_mode` may be `null`.**
6. Many new fields are included; see the examples above.

The full old → new field mapping is in the [CHANGELOG](CHANGELOG.md).

Apart from `CHECK_DDAGENT`, nothing changes for `/health`, `/`,
`/ami-version` or `/debug/*`, or for configuration environment variables. The binary is published to S3 under the
new version (`pgbouncer-healthcheck-3.0`); existing consumers of
`pgbouncer-healthcheck-2.0` are unaffected until they switch.
