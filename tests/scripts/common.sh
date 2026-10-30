#!/bin/bash

if [[ VERBOSE -gt 1 ]]; then
    set -x
fi

if [[ -t 2 ]]; then
    # Output is a terminal
    TC_RED="\e[00;31m"
    TC_GRN="\e[00;32m"
    TC_CYN="\e[00;36m"
    TC_YEL="\e[01;33m"
    TC_LBLU="\e[01;34m"
    TC_LPUR="\e[01;35m"

    TC_BLD="\e[1m"
    TC_UND="\e[4m"
    TC_RST="\e[0m"
else
    TC_RED=""
    TC_GRN=""
    TC_CYN=""
    TC_YEL=""
    TC_LBLU=""
    TC_LPUR=""
    TC_BLD=""
    TC_UND=""
    TC_RST=""
fi

# Squelch Shellcheck Warning
export TC_LBLU

passed=0
failed=0

FAIL_HDR="${TC_RED}FAIL: ${TC_RST}"
PASS_HDR="${TC_GRN}PASS: ${TC_RST}"
INFO_HDR="${TC_CYN}INFO: ${TC_RST}"
FATL_HDR="${TC_RED}FATAL: ${TC_RST}"
TITLE_HDR="${TC_UND}${TC_LPUR}**: "
TITLE_END=" :**${TC_RST}"
DESC_HDR="${TC_YEL}>>>> ${TC_RST}"

if [[ $VERBOSE -gt 0 ]]; then
   curl_opts=("--silent" "--output" "/dev/stderr" "--write-out" "%{http_code}")
else
   curl_opts=("--silent" "--output" "/dev/null" "--write-out" "%{http_code}")
fi

# Location to put fake datadog-agent binary
DDAGENT_FAKE=/usr/bin/datadog-agent

# Succeeds only if the request completed with a non-error HTTP status. A
# connection failure (curl exits non-zero) counts as an error.
_curl() {
    url=$1
    if ! STATUSCODE=$(curl "${curl_opts[@]}" "$url"); then
        info "Request to $url failed (no HTTP response)"
        return 1
    fi
    if [[ $STATUSCODE -ge 400 ]]; then
        info "Request returned $STATUSCODE"
        return 1
    fi
}

# wait_for_port PORT NAME: wait up to 10s for something to accept connections
wait_for_port() {
    port=$1
    name=$2
    for _ in $(seq 50); do
        if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
            return 0
        fi
        sleep 0.2
    done
    fatal "$name did not start listening on port $port"
}

init_creds() {
    export PGUSER="dd-agent"
    export PGPASSWORD="3c5c04a7-c21a-480f-bc42-30a1e0550fbc"
}

start_healthcheck() {
    info "Starting Healthcheck process"
    /usr/local/bin/pgbouncer-healthcheck &
    health_pid=$!
    wait_for_port 8000 "Healthcheck"
}

start_pgbouncer() {
    info "Starting PGBouncer"
    pgbouncer -u pgbouncer /etc/pgbouncer/pgbouncer.ini >&/dev/null &
    pgbouncer_pid=$!
    wait_for_port 6543 "PGBouncer"
}

stop_pgbouncer() {
    info "Stoppping PGBouncer (PID $pgbouncer_pid)"
    kill -INT "$pgbouncer_pid"
    retry=5
    while kill -0 "$pgbouncer_pid" 2>/dev/null && ((retry--)); do
        info "Waiting for PGBouncer to stop"
        sleep 1
    done
    if kill -0 "$pgbouncer_pid" 2>/dev/null; then
        info "Sending SIGTERM to PGBouncer"
        kill -TERM "$pgbouncer_pid"
    fi
}

end_tests() {
    kill -0 $health_pid 2>/dev/null || fail "Health-check process ended early"
}

fail() {
    echo -e "$FAIL_HDR$*">&2
    (( failed++ ))
}

pass() {
    echo -e "$PASS_HDR$*">&2
    (( passed++ ))
}

info() {
    echo -e "$INFO_HDR$*">&2
}

fatal() {
    echo -e "$FATL_HDR$*">&2
    exit 1
}

title() {
    echo -e "$TITLE_HDR$*$TITLE_END\n">&2
}

desc() {
    echo -e "\n\n$DESC_HDR$*">&2
}

test_ok_endpoint() {
    path=$1
    name=$2
    info "Testing for sucessful $name"
    if _curl "localhost:8000$path"; then
        pass "$name returned success"
    else
        fail "$name return error"
    fi
}

test_notok_endpoint() {
    path=$1
    name=$2
    info "Testing for error $name"
    if _curl "localhost:8000$path"; then
        fail "$name returned success"
    else
        pass "$name return error"
    fi
}

test_ok_healthcheck() {
    test_ok_endpoint "/health" "Health-check"
}

test_notok_healthcheck() {
    test_notok_endpoint "/health" "Health-check"
}

test_ok_debug() {
    test_ok_endpoint "/debug/meminfo" "Debug Endpoint"
}

test_notok_debug() {
    test_notok_endpoint "/debug/meminfo" "Debug Endpoint"
}

test_ok_status() {
    test_ok_endpoint "/status/users" "Status Endpoint"
}

test_notok_status() {
    test_notok_endpoint "/status/users" "Status Endpoint"
}

# test_json_endpoint PATH NAME JQ_FILTER
# Passes if PATH returns HTTP 200 with a JSON body for which JQ_FILTER
# evaluates to true (jq -e).
test_json_endpoint() {
    path=$1
    name=$2
    filter=$3
    info "Testing JSON from $name ($path)"
    body=$(curl --silent --write-out '\n%{http_code}' "localhost:8000$path")
    code=${body##*$'\n'}
    body=${body%$'\n'*}
    if [[ $VERBOSE -gt 0 ]]; then
        echo "$body" >&2
    fi
    if [[ $code != 200 ]]; then
        fail "$name returned HTTP $code: ${body:0:300}"
    elif ! jq -e "$filter" >/dev/null 2>&1 <<<"$body"; then
        fail "$name JSON did not match '$filter': ${body:0:300}"
    else
        pass "$name returned expected JSON"
    fi
}

# Check every /status endpoint returns a JSON array with the expected shape.
# Requires PGBouncer running and stats-user credentials (init_creds).
test_all_status_json() {
    test_json_endpoint /status/users "Status users" \
        'type == "array" and any(.[]; .name == "dd-agent" and (.max_user_connections | type) == "number")'
    test_json_endpoint /status/configs "Status configs" \
        'type == "array" and any(.[]; .key == "listen_port" and .value == "6543")
         and all(.[]; (.changeable | type) == "boolean")'
    test_json_endpoint /status/databases "Status databases" \
        'type == "array" and any(.[]; .name == "dummy" and .host == "10.0.0.1" and .port == 5432 and .pool_size == 100)
         and any(.[]; .name == "pgbouncer" and .host == null)'
    test_json_endpoint /status/pools "Status pools" \
        'type == "array" and any(.[]; .database == "pgbouncer" and (.cl_active | type) == "number")'
    test_json_endpoint /status/clients "Status clients" \
        'type == "array" and any(.[]; .user == "dd-agent" and .database == "pgbouncer" and (.port | type) == "number")'
    # No backend Postgres in the test environment, so there are no servers
    test_json_endpoint /status/servers "Status servers" \
        '. == []'
    test_json_endpoint /status/memory "Status memory" \
        'type == "array" and length > 0 and all(.[]; (.name | type) == "string" and (.memtotal | type) == "number")'
    test_json_endpoint /status/stats "Status stats" \
        'type == "array" and any(.[]; .database == "pgbouncer" and (.total_query_count | type) == "number")'
}

make_dd_stub_hang() {
    printf '#!/bin/sh\n' >"$DDAGENT_FAKE"
    printf 'sleep 1000000\n' >>"$DDAGENT_FAKE"
    chmod 755 "$DDAGENT_FAKE"
}

make_dd_stub_pass() {
    printf '#!/bin/true\n' >"$DDAGENT_FAKE"
    chmod 755 "$DDAGENT_FAKE"
}

make_dd_stub_fail() {
    printf '#!/bin/false\n' >"$DDAGENT_FAKE"
    chmod 755 "$DDAGENT_FAKE"
}

summary() {
    echo
    echo -e "${TC_BLD}== TESTING SUMMARY ==${TC_RST}"
    if (( failed >0 )); then
        echo -e "${TC_BLD}${TC_GRN}Passed: $passed ${TC_RED}Failed: $failed${TC_RST}"
    else
        echo -e "${TC_BLD}${TC_GRN}Passed: $passed Failed: $failed${TC_RST}"
    fi
    if [[ $failed -gt 0 ]]; then
        exit 1
    fi
}
