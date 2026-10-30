#!/bin/bash

cd "$(dirname "$0")" || exit

source scripts/common.sh

run_test() {
    echo ""
    echo "==================================="
    echo "Running test: $1"
    echo "==================================="
    echo ""
    if "$CONTAINER_TOOL" run "${tty_opts[@]}" --rm -e VERBOSE -w="/tests" "$IMAGE" "/tests/$1"; then
        pass "-* $1 script passed *-"
    else
        fail "-* $1 script failed *-"
    fi
}

IMAGE=${1:-pgbouncer-healthcheck:test}
shift

CONTAINER_TOOL=${CONTAINER_TOOL:-podman}
export VERBOSE=${VERBOSE:-0}

# Only allocate a TTY when we have one (e.g. not in CI)
tty_opts=()
if [[ -t 1 ]]; then
    tty_opts=("-t")
fi

if [[ $# -eq 0 ]]; then
    tests=()
    for script in scripts/*.sh; do
        name=$(basename "$script")
        [[ $name == common.sh ]] || tests+=("$name")
    done
else
    tests=("$@")
fi

for test in "${tests[@]}"; do
    run_test "$test"
done

echo -e "${TC_LBLU}
==========================
== META TESTING SUMMARY ==
==========================
${TC_RST}"

echo -ne "${TC_BLD}${TC_GRN}Scripts Passed: $passed "
if (( failed >0 )); then
    echo -ne "${TC_RED}"
fi
echo -e "Failed: $failed${TC_RST}"

if [[ $failed -gt 0 ]]; then
    exit 1
fi
