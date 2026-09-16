#!/usr/bin/env bash
# Regenerate web/src/api/schema.d.ts from openapi.yaml.
#
#   scripts/gen-webapi.sh
#
# web/src/api/schema.d.ts is a GENERATED artefact: openapi-typescript's
# reading of Chronicle's own HTTP contract -- the same document
# internal/api/wire (scripts/gen-api.sh) and asr/internal/wire
# (scripts/gen-asrclient.sh) generate from. CI regenerates it and fails if
# the result differs from what is committed -- the same guard those two put
# on their own generated files, for the same reason: a generated artefact
# with no guard is a generated artefact somebody hand-edits.
#
# GEN_WEBAPI_OUT redirects the output, which is how the staleness check in
# verify.sh regenerates WITHOUT touching the working tree. A check that
# rewrites the file it is checking cannot tell you what was committed.
#
# The generator is PINNED as a devDependency in web/package.json
# (openapi-typescript), resolved through web/bun.lock. An unpinned generator
# makes the staleness guard a coin flip, because two versions disagree about
# formatting and neither is wrong.
#
# This is the web client's end of a document with three other clients in
# three other languages -- Chronicle's own Go server, E9's Android capture,
# E10's MCP.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
REPO_ROOT="$(pwd)"

SPEC="$REPO_ROOT/openapi.yaml"
OUT="${GEN_WEBAPI_OUT:-$REPO_ROOT/web/src/api/schema.d.ts}"

cd web
bun install --frozen-lockfile >/dev/null
bunx openapi-typescript "$SPEC" -o "$OUT" >/dev/null
echo "wrote $OUT ($(wc -l < "$OUT") lines)"
