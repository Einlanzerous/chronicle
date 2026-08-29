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
#
# The ASR service (CHRN-25) has its OWN database and its own role, so it has its
# own test DSN — ASR_TEST_DATABASE_URL, assembled from ASR_DB_PASSWORD the same
# way. It is deliberately not Chronicle's: a test suite that reached Chronicle's
# database to exercise the ASR service would prove the opposite of what the
# separate store is for.
set -uo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

host="${CHRONICLE_TEST_DB_HOST:-127.0.0.1:5432}"
if [ -z "${CHRONICLE_TEST_DATABASE_URL:-}" ] && [ -n "${CHRONICLE_DB_PASSWORD:-}" ]; then
  export CHRONICLE_TEST_DATABASE_URL="postgres://chronicle:${CHRONICLE_DB_PASSWORD}@${host}/chronicle_test?sslmode=disable"
fi
if [ -z "${CHRONICLE_TEST_TIER1_DATABASE_URL:-}" ] && [ -n "${CHRONICLE_TIER1_DB_PASSWORD:-}" ]; then
  export CHRONICLE_TEST_TIER1_DATABASE_URL="postgres://chronicle_tier1:${CHRONICLE_TIER1_DB_PASSWORD}@${host}/chronicle_test?sslmode=disable"
fi
if [ -z "${ASR_TEST_DATABASE_URL:-}" ] && [ -n "${ASR_DB_PASSWORD:-}" ]; then
  export ASR_TEST_DATABASE_URL="postgres://asr:${ASR_DB_PASSWORD}@${host}/asr_test?sslmode=disable"
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

# internal/asrclient AND asr/internal/wire are GENERATED from asr/openapi.yaml.
# A generated artefact with no guard is a generated artefact somebody hand-edits
# -- the same sentence CLAUDE.md applies to the schema, and the same remedy.
#
# Regenerated to TEMPORARY FILES and byte-compared, so a check never rewrites
# the thing it is checking. The generator is pinned by the `tool` directive in
# go.mod; an unpinned one would make this a coin flip.
#
# This is the half of CHRN-25's first Done-when that does not need hardware,
# which is why it belongs here and not only in CI. The contract has two clients
# in two languages, and the failure it prevents is the second one being
# generated against a spec the first has already drifted from. The wire copy
# is CHRN-82's: the service holds its own types so the subtree imports nothing
# outside itself, and the guard is what keeps two copies one definition.
asrclient_check() {
  local ctmp wtmp rc=0
  ctmp="$(mktemp -t asrclient.XXXXXX.go)"
  wtmp="$(mktemp -t asrwire.XXXXXX.go)"
  # shellcheck disable=SC2064
  trap "rm -f '$ctmp' '$wtmp'" RETURN
  GEN_ASRCLIENT_OUT="$ctmp" GEN_ASRWIRE_OUT="$wtmp" scripts/gen-asrclient.sh >/dev/null || return 1
  if ! diff -q "$ctmp" internal/asrclient/client.gen.go >/dev/null; then
    echo "internal/asrclient/client.gen.go does not match asr/openapi.yaml."
    diff -u internal/asrclient/client.gen.go "$ctmp" | head -40
    rc=1
  fi
  if ! diff -q "$wtmp" asr/internal/wire/wire.gen.go >/dev/null; then
    echo "asr/internal/wire/wire.gen.go does not match asr/openapi.yaml."
    diff -u asr/internal/wire/wire.gen.go "$wtmp" | head -40
    rc=1
  fi
  [ "$rc" -eq 0 ] || echo "Run scripts/gen-asrclient.sh and commit the result."
  return "$rc"
}

# asr/ is a SUBTREE with a sealed boundary (docs/decisions/chrn-82-asr-subtree-
# and-publish.md, section 2): nothing under it imports anything else in this
# module, so that `git filter-repo --subdirectory-filter asr` yields a
# repository that builds. asr/Dockerfile enforces the outward half for free by
# copying only go.mod, go.sum and asr/ into the asrd stage; this is the same
# check without Docker, tests included, plus the inward half -- the only package
# outside the subtree may import from it is asr/asrtest, the harness Chronicle's
# integration test runs the service through. A boundary with no guard is a
# directory.
asr_boundary_check() {
  local mod out
  mod="$(go list -m)"
  out="$(go list -deps -test ./asr/... | grep "^$mod/" | grep -v "^$mod/asr/" || true)"
  if [ -n "$out" ]; then
    echo "asr/ imports outside the subtree:"
    echo "$out"
    return 1
  fi
  out="$(grep -rn --include='*.go' --exclude-dir=asr "\"$mod/asr/" . | grep -v "\"$mod/asr/asrtest\"" || true)"
  if [ -n "$out" ]; then
    echo "something outside asr/ imports the subtree through a door other than asr/asrtest:"
    echo "$out"
    return 1
  fi
}

step "gofmt"        gofmt_check
step "go vet"       go vet ./...
step "build"        go build ./...
step "asr client"   asrclient_check
step "asr boundary" asr_boundary_check
# -p 1: ONE TEST BINARY AT A TIME.
#
# `go test ./...` runs packages in parallel by default, and more than one
# package now resets the shared test database -- internal/store and
# internal/transcribe both roll the migrations down and back up to start from
# empty. Overlapped, they drop tables out from under each other and produce
# failures that read as migration bugs ("relation tier2.users does not exist")
# and are not.
#
# The alternative, an advisory lock in each harness, is more precise and has to
# be remembered by whoever adds the third database-backed package. This cannot
# be forgotten. The suite is seconds long; serialising it costs nothing worth
# having.
step "test"         go test ./... -count=1 -p 1

if [ -z "${CHRONICLE_TEST_DATABASE_URL:-}" ]; then
  printf '\nNOTE: CHRONICLE_TEST_DATABASE_URL unset — database tests were skipped.\n'
fi
if [ -z "${CHRONICLE_TEST_TIER1_DATABASE_URL:-}" ]; then
  printf 'NOTE: CHRONICLE_TEST_TIER1_DATABASE_URL unset — the tier-isolation test was skipped.\n'
fi
if [ -z "${ASR_TEST_DATABASE_URL:-}" ]; then
  printf 'NOTE: ASR_TEST_DATABASE_URL unset — the ASR job tests were skipped, including the kill -9 lease test.\n'
fi

printf '\n'
if [ "$fail" -eq 0 ]; then
  echo "verify: PASS"
else
  echo "verify: FAIL"
fi
exit "$fail"
