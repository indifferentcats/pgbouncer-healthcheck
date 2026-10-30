#!/bin/bash
set -uo pipefail

source common.sh

title "Testing readiness against a real Postgres behind PGBouncer"

init_creds
export CHECK_CACHE_TTL=0
export BACKEND_CONNSTR="host=127.0.0.1 port=6543 dbname=app user=app password=$APP_PASSWORD sslmode=disable"
export READY_CHECKS=headroom,databases,pools,backend
start_postgres
start_healthcheck
start_pgbouncer

desc "Postgres reachable through PGBouncer: ready OK, including backend"
expect_health /health/ready 200 backend=true pools=true
desc "/status/servers lists the server connection with the expected fields"
test_json_endpoint /status/servers "Status servers" \
    'type == "array" and any(.[]; .type == "S" and .database == "app" and .user == "app"
     and (.port | type) == "number" and (.tls | length) > 0)'

desc "Pool saturated (pool_size=1, a long query holds it): ready fails on pools"
stop_healthcheck
export READY_CHECKS=pools MAX_WAIT=1s
start_healthcheck
app_psql "SELECT pg_sleep(5)" >/dev/null &
long_query=$!
sleep 0.5
app_psql "SELECT 1" >/dev/null &
waiting_query=$!
sleep 2
expect_health /health/ready 503 pools=false
wait "$long_query" "$waiting_query"
expect_health /health/ready 200 pools=true

desc "Postgres down: ready fails on backend, live stays OK"
stop_healthcheck
export READY_CHECKS=backend
start_healthcheck
stop_postgres
expect_health /health/ready 503 backend=false
expect_health /health/live 200 console=true

end_tests
summary
