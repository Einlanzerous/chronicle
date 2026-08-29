#!/usr/bin/env bash
# Regenerate the two packages generated from asr/openapi.yaml:
#
#   internal/asrclient/client.gen.go   Chronicle's client — models AND client
#   asr/internal/wire/wire.gen.go      the service's wire types — models only
#
# Both are GENERATED artefacts. CI regenerates each and fails if the result
# differs from what is committed, which is what stops either drifting from the
# spec — the same guard scripts/gen-schema.sh puts on schema.sql, for the same
# reason. Two copies from one file rather than one shared package because the
# service lives in a subtree that imports nothing outside itself (CHRN-82 §2);
# the property that matters — a spec change is a compile error on BOTH ends —
# survives, because both copies come from the same file in the same run.
#
#   scripts/gen-asrclient.sh
#
# GEN_ASRCLIENT_OUT and GEN_ASRWIRE_OUT redirect the outputs, which is how the
# staleness check in verify.sh regenerates WITHOUT touching the working tree. A
# check that rewrites the file it is checking cannot tell you what was
# committed.
#
# The generator is PINNED: `tool github.com/oapi-codegen/oapi-codegen/v2/...` in
# go.mod, resolved through go.sum. An unpinned generator makes the staleness
# guard a coin flip, because two versions disagree about formatting and neither
# is wrong.
#
# Chronicle generates these two. Catenary generates its own, in Dart, from the
# same file — which is the entire point of the contract being a spec rather than
# a Go package.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

SPEC=asr/openapi.yaml
CLIENT_OUT="${GEN_ASRCLIENT_OUT:-internal/asrclient/client.gen.go}"
WIRE_OUT="${GEN_ASRWIRE_OUT:-asr/internal/wire/wire.gen.go}"

go tool oapi-codegen -config internal/asrclient/oapi-codegen.yaml -o "$CLIENT_OUT" "$SPEC"
gofmt -w "$CLIENT_OUT"
echo "wrote $CLIENT_OUT ($(wc -l < "$CLIENT_OUT") lines)"

go tool oapi-codegen -config asr/internal/wire/oapi-codegen.yaml -o "$WIRE_OUT" "$SPEC"
gofmt -w "$WIRE_OUT"
echo "wrote $WIRE_OUT ($(wc -l < "$WIRE_OUT") lines)"
