# PGBouncer healthcheck

[![Build](https://github.com/indifferentcats/pgbouncer-healthcheck/actions/workflows/build.yml/badge.svg?branch=master)](https://github.com/indifferentcats/pgbouncer-healthcheck/actions/workflows/build.yml)
[![Unit tests](https://github.com/indifferentcats/pgbouncer-healthcheck/actions/workflows/unit-tests.yml/badge.svg?branch=master)](https://github.com/indifferentcats/pgbouncer-healthcheck/actions/workflows/unit-tests.yml)
[![Coverage](https://raw.githubusercontent.com/indifferentcats/pgbouncer-healthcheck/badges/coverage.svg)](https://github.com/indifferentcats/pgbouncer-healthcheck/actions/workflows/unit-tests.yml)
[![CodeQL](https://github.com/indifferentcats/pgbouncer-healthcheck/actions/workflows/codeql-analysis.yml/badge.svg?branch=master)](https://github.com/indifferentcats/pgbouncer-healthcheck/actions/workflows/codeql-analysis.yml)

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

### Supply-chain pinning

Build inputs are pinned: Go modules via `go.sum`, base images in the
`Dockerfile` by digest, GitHub Actions by commit SHA, runners to a specific
Ubuntu release, and CI's go-task downloads by SHA-256. When bumping any of them, update the version and the
hash together.

## Configuration

The binary takes its configuration from several environment variables.

### Postgres connection string for PGBouncer connection

The connection string comes from the env var `CONNSTR`. The default is
`host=localhost port=6543 dbname=pgbouncer sslmode=disable`. This should
be fine for most use cases.

The intention is that the username and password will be supplied by setting the
`PGUSER` and `PGPASSWORD` standard variables.

### Other parameters

- `PORT`: The TCP port the daemon should use to listen for HTTP connections (default: 8000)
- `ENHANCED_CHECK`: If true, test the PGBouncer health by connecting and sending a query.
    If false, use a simple port probe. (default: false)
- `PGBOUNCER_PORT`: The TCP port used if using the basic port probe healthcheck (default: 6543)
- `ENABLE_DEBUG_ENDPOINTS`: If true, the extra `/debug/` endpoints will be enabled. See below (default: false)

The result of the defaults is that the standard mode is just to do a TCP port probe of 6543
on the localhost to test if PGBouncer is alive.

## Main Endpoints

Note: See https://www.pgbouncer.org/usage.html for details of the PGBouncer
`SHOW` command outputs.

### /

This always returns a `200 OK`

### /health
The healthcheck returns a 200 OK response or a `500 Internal Server Error` response.
It will probe the PGBouncer using a method depending on  `ENHANCED_CHECK`.

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
