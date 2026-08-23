#!/usr/bin/env bash
# Every check that does not need hardware, in one command. Green before
# anything is handed over — see the working agreement in CLAUDE.md.
#
# Database-backed tests need a real Postgres and skip without one. Provide it
# the way CI does:
#
#   signet exec --secret construct-server/CHRONICLE_DB_PASSWORD \
#               --secret construct-server/CHRONICLE_TIER1_DB_PASSWORD -- ./verify.sh
#
# and both test DSNs are assembled below, or export them yourself. The tier-1
# DSN is what the isolation test (CHRN-71's sixth Done-when, CHRN-52's subject)
# connects as; without it that test skips rather than passing vacuously.
set -uo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

host="${CHRONICLE_TEST_DB_HOST:-127.0.0.1:5432}"
if [ -z "${CHRONICLE_TEST_DATABASE_URL:-}" ] && [ -n "${CHRONICLE_DB_PASSWORD:-}" ]; then
  export CHRONICLE_TEST_DATABASE_URL="postgres://chronicle:${CHRONICLE_DB_PASSWORD}@${host}/chronicle_test?sslmode=disable"
fi
if [ -z "${CHRONICLE_TEST_TIER1_DATABASE_URL:-}" ] && [ -n "${CHRONICLE_TIER1_DB_PASSWORD:-}" ]; then
  export CHRONICLE_TEST_TIER1_DATABASE_URL="postgres://chronicle_tier1:${CHRONICLE_TIER1_DB_PASSWORD}@${host}/chronicle_test?sslmode=disable"
fi

fail=0
step() {
  local name="$1"; shift
  printf '\n=== %s\n' "$name"
  if "$@"; then
    printf '    ok\n'
  else
    printf '    FAILED\n'
    fail=1
  fi
}

gofmt_check() {
  local out
  out=$(gofmt -l . 2>/dev/null)
  [ -z "$out" ] || { echo "unformatted:"; echo "$out"; return 1; }
}

step "gofmt"   gofmt_check
step "go vet"  go vet ./...
step "build"   go build ./...
step "test"    go test ./... -count=1

if [ -z "${CHRONICLE_TEST_DATABASE_URL:-}" ]; then
  printf '\nNOTE: CHRONICLE_TEST_DATABASE_URL unset — database tests were skipped.\n'
fi
if [ -z "${CHRONICLE_TEST_TIER1_DATABASE_URL:-}" ]; then
  printf 'NOTE: CHRONICLE_TEST_TIER1_DATABASE_URL unset — the tier-isolation test was skipped.\n'
fi

printf '\n'
if [ "$fail" -eq 0 ]; then
  echo "verify: PASS"
else
  echo "verify: FAIL"
fi
exit "$fail"
