#!/usr/bin/env bash
# Regenerate mobile/chronicle/packages/chronicle_api from openapi.yaml.
#
#   scripts/gen-dartapi.sh
#
# That package is a GENERATED artefact: the payload types and the per-tag API
# classes for the Android client (CHRN-59). CI regenerates it and fails if the
# result differs from what is committed -- the same guard scripts/gen-api.sh puts
# on internal/api/wire, scripts/gen-webapi.sh puts on the web client's types and
# scripts/gen-schema.sh puts on schema.sql, for the same reason: a generated
# artefact with no guard is a generated artefact somebody hand-edits.
#
# GEN_DARTAPI_OUT redirects the output, which is how the staleness check
# regenerates WITHOUT touching the working tree. A check that rewrites the file
# it is checking cannot tell you what was committed.
#
# The generator is PINNED TWICE, and it has to be: the npm package
# (@openapitools/openapi-generator-cli in mobile/package.json, resolved through
# mobile/bun.lock) is only a launcher, and the thing that writes the Dart is the
# JAR whose version mobile/openapitools.json names. Pinning the launcher alone
# would leave the generator itself floating, which makes the staleness guard a
# coin flip -- two versions disagree about formatting and neither is wrong.
#
# It needs a JDK, unlike the repo's other three generators. That is why this
# lives in .github/workflows/mobile.yml rather than in verify.sh, which is the
# one command a Go change has to pass and should not need Java to run. The
# workflow watches openapi.yaml as well as mobile/**, so a change to the
# contract alone still trips the guard.
#
# This is the Android client's end of a document with three others -- Chronicle's
# own Go server, E8's web client, E10's MCP.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
REPO_ROOT="$(pwd)"

OUT="${GEN_DARTAPI_OUT:-$REPO_ROOT/mobile/chronicle/packages/chronicle_api}"

cd mobile
bun install --frozen-lockfile >/dev/null

# The generator resolves relative paths against the CWD, so an absolute -o is
# the only form that survives GEN_DARTAPI_OUT pointing outside this directory.
mkdir -p "$OUT"

# A redirected run has no .openapi-generator-ignore of its own, so the two files
# the committed package excludes would reappear and the comparison would fail on
# files nobody generates into the tree. Seed it.
if [ ! -f "$OUT/.openapi-generator-ignore" ]; then
  cp "$REPO_ROOT/mobile/chronicle/packages/chronicle_api/.openapi-generator-ignore" "$OUT/"
fi

bunx openapi-generator-cli generate -c openapi-dart.yaml -o "$OUT" >/dev/null
echo "wrote $OUT ($(find "$OUT" -name '*.dart' | wc -l | tr -d ' ') dart files)"
