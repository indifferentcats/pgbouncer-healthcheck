#!/bin/bash
set -uo pipefail

source common.sh

title "Testing /health/live and /health/ready"

init_creds
# A small client limit so the headroom check can be exhausted
sed -i 's/^max_client_conn = .*/max_client_conn = 10/' /etc/pgbouncer/pgbouncer.ini
export CLIENT_HEADROOM_PCT=50 CHECK_CACHE_TTL=0
start_healthcheck
start_pgbouncer

desc "PGBouncer healthy: live and ready return OK"
expect_health /health/live 200 console=true
expect_health /health/ready 200 login=true console=true headroom=true databases=true
desc "/health keeps its legacy behaviour by default"
test_ok_healthcheck

desc "Clients use most of max_client_conn: ready fails on headroom, live stays OK"
hold_clients 5
expect_health /health/ready 503 headroom=false
expect_health /health/live 200 console=true
release_clients
expect_health /health/ready 200 headroom=true

desc "Database paused: ready fails on databases, live stays OK"
admin_psql "PAUSE dummy" >/dev/null
expect_health /health/ready 503 databases=false
expect_health /health/live 200 console=true
admin_psql "RESUME dummy" >/dev/null
expect_health /health/ready 200 databases=true

desc "HEALTH_MODE=ready makes /health run the readiness checks"
stop_healthcheck
export HEALTH_MODE=ready
start_healthcheck
expect_health /health 200 headroom=true

desc "PGBouncer stopped: live and ready fail"
stop_pgbouncer
expect_health /health/live 503 console=false
expect_health /health/ready 503 login=false

end_tests
summary
