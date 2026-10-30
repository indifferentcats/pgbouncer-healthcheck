# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/).

## [3.0] - Unreleased

### ⚠️ Breaking changes

#### `CHECK_DDAGENT` removed: `/health` no longer checks the Datadog agent

`CHECK_DDAGENT=1` made `/health` also run `sudo -n datadog-agent health`, so a
host with a broken Datadog agent was reported unhealthy. That check has been
removed, along with its `sudo` dependency:

- It cannot work in the minimal containers this binary now targets: they have
  neither `sudo` nor the `datadog-agent` binary, so `/health` would always fail.
- It coupled PGBouncer's health to the monitoring agent's health. Under a load
  balancer or orchestrator, a broken agent would take healthy PGBouncer
  instances out of service, turning a monitoring problem into a database
  outage.

**The variable is now ignored, without an error.** If you still set
`CHECK_DDAGENT=1`, `/health` silently stops checking the agent, so put a
replacement in place before upgrading.

**What to do instead:** monitor the Datadog agent through its own health
check, separately from PGBouncer:

- **Containers (ECS, Kubernetes):** give the agent container its own liveness
  and readiness probes on the agent's health endpoints, `GET /live` and
  `GET /ready`. They are served on the agent's `health_port` (`DD_HEALTH_PORT`),
  which the official Datadog agent image and Helm chart set to `5555`.
- **Hosts (EC2):** run `datadog-agent health` from your host monitoring or
  instance health tooling, or enable `health_port` in `datadog.yaml` and probe
  `http://localhost:<port>/ready`.
- If you still want "no agent, no traffic", alert on agent health (for example
  a Datadog monitor on the `datadog.agent.running` metric or the agent's
  service check) rather than failing PGBouncer's health check.

#### `/status/*` JSON model

The JSON returned by every `/status/*` endpoint has changed. Previously,
`SHOW …` results were scanned by column position into fixed structs, which
broke on any PGBouncer newer than ~1.8 (enhanced health checks and most
`/status/*` endpoints returned 500). Columns are now matched by name, and the
JSON keys are the PGBouncer column names.

- All keys are snake_case PGBouncer column names instead of Go field names.
- Empty results serialise as `[]` instead of `null`.
- New fields are exposed for columns added in newer PGBouncer versions.

Field mapping (2.x → 3.0). Fields not listed under "Added" are new in 3.0.

| Endpoint | 2.x | 3.0 |
|---|---|---|
| `/status/users` | `Name`, `PoolMode` | `name`, `pool_mode` |
| | — | Added: `pool_size`, `reserve_pool_size` (number or `null`), `max_user_connections`, `current_connections`, `max_user_client_connections`, `current_client_connections` |
| `/status/configs` | `Key`, `Value`, `Changeable` | `key`, `value`, `changeable` (still a boolean) |
| | — | Added: `default` |
| `/status/databases` | `Name`, `Host`, `Database`, `ForceUser`, `PoolSize`, `PoolMode`, `MaxConnections`, `CurrentConnections` | `name`, `host`, `database`, `force_user`, `pool_size`, `pool_mode`, `max_connections`, `current_connections` |
| | `Port` (string) | `port` (**number**) |
| | `ReservePool` | `reserve_pool_size` |
| | — | Added: `min_pool_size`, `server_lifetime`, `load_balance_hosts`, `max_client_connections`, `current_client_connections`, `paused`, `disabled` (0/1) |
| `/status/pools` | `Database`, `User`, `ClActive`, `ClWaiting`, `SvActive`, `SvIdle`, `SvUsed`, `SvTested`, `SvLogin` | `database`, `user`, `cl_active`, `cl_waiting`, `sv_active`, `sv_idle`, `sv_used`, `sv_tested`, `sv_login` |
| | `MaxWait` | `maxwait` |
| | `PoolMode` (string) | `pool_mode` (string or **`null`**) |
| | — | Added: `cl_active_cancel_req`, `cl_waiting_cancel_req`, `sv_active_cancel`, `sv_being_canceled`, `maxwait_us`, `load_balance_hosts` |
| `/status/clients`, `/status/servers` | `Type`, `User`, `Database`, `State`, `Addr`, `Port`, `LocalAddr`, `LocalPort`, `ConnectTime`, `RequestTime`, `Ptr`, `Link`, `RemotePid` | `type`, `user`, `database`, `state`, `addr`, `port`, `local_addr`, `local_port`, `connect_time`, `request_time`, `ptr`, `link`, `remote_pid` |
| | `TLS` | `tls` |
| | — | Added: `replication`, `wait`, `wait_us`, `close_needed`, `application_name`, `prepared_statements`, `id` |
| `/status/memory` | `Name`, `Size`, `Used`, `Free` | `name`, `size`, `used`, `free` |
| | `MemTotal` | `memtotal` |
| `/status/stats` | `Database`, `TotalReceived`, `TotalSent`, `TotalQueryTime`, `AvgRecv`, `AvgSent` | `database`, `total_received`, `total_sent`, `total_query_time`, `avg_recv`, `avg_sent` |
| | **Removed:** `TotalRequests`, `AvgReq`, `AvgQuery` (dropped by PGBouncer 1.8) | Use `total_query_count` / `total_xact_count`, `avg_query_count`, `avg_query_time` |
| | — | Added: `total_server_assignment_count`, `total_xact_count`, `total_query_count`, `total_xact_time`, `total_wait_time`, `total_client_parse_count`, `total_server_parse_count`, `total_bind_count`, and the matching `avg_*` fields |

### Changed

- Build system: `dep` + `vendor/` replaced by Go modules; `Makefile` replaced
  by `Taskfile.yml`; builds and tests run under Podman (default) or Docker.
- The binary is fully static (`CGO_ENABLED=0`) and a `FROM scratch` runtime
  image target is provided (`task image`).
- Version and build date are set at link time (`-ldflags -X`) instead of
  rewriting `version.go`.
- Built with Go 1.27; test image uses Alpine 3.24 / PGBouncer 1.25.
- Dependencies updated: `lib/pq` 1.0.0 → 1.12.3, `httprouter` 1.2.0 → 1.3.0,
  `envconfig` 1.3.0 → 1.4.0. `github.com/pkg/errors` removed in favour of the
  standard library.
- Error responses: "occured" typo fixed to "occurred". `lib/pq` errors now
  include the SQLSTATE code.
- HTTP server now has read/write/idle timeouts.
- `sudo` is no longer used, and has been removed from the test image. The
  integration test PGBouncer stats user is renamed from `dd-agent` to `stats`.
- The "Connected to PGBouncer database" log line is only printed after a
  successful query.
- CI: CircleCI uses Task and a current remote Docker; CodeQL actions updated.
  All GitHub Actions are pinned by commit SHA, base images by digest, and the
  CI go-task download by SHA-256.

### Fixed

- Enhanced health check and `/status/*` endpoints failing on PGBouncer > 1.8
  (see Breaking changes).
- Port probe ignored its `port` argument.
- Enhanced health check panicked if the database handle could not be created.
- Port probe now honours the request timeout.

### Added

- Integration tests covering the JSON shape of every `/status/*` endpoint.
- Go unit tests for the scanner and all HTTP handlers, using a fake
  `database/sql` driver (`task test:unit`, `task coverage`). These include the
  10-second request timeout, which was previously only tested through the
  removed Datadog check.
- GitHub Actions workflows: build and integration tests on native amd64 and
  arm64 runners, and unit tests with coverage. README badges show build, unit
  test, coverage and CodeQL status.
- `task release` to tag and publish a GitHub release with binaries.

## [2.0]

Last release built with `dep` and Go 1.10.
