#!/usr/bin/env bash
# Regenerate internal/asrclient from deploy/asr/openapi.yaml.
#
# internal/asrclient/client.gen.go is a GENERATED artefact: the Go client you
# get from the contract. CI regenerates it and fails if the result differs from
# what is committed, which is what stops the two drifting — the same guard
# scripts/gen-schema.sh puts on schema.sql, for the same reason.
#
#   scripts/gen-asrclient.sh
#
# GEN_ASRCLIENT_OUT redirects the output, which is how the staleness check in
# verify.sh regenerates WITHOUT touching the working tree. A check that rewrites
# the file it is checking cannot tell you what was committed.
#
# The generator is PINNED: `tool github.com/oapi-codegen/oapi-codegen/v2/...` in
# go.mod, resolved through go.sum. An unpinned generator makes the staleness
# guard a coin flip, because two versions disagree about formatting and neither
# is wrong.
#
# Chronicle generates this one. Catenary generates its own, in Dart, from the
# same file — which is the entire point of the contract being a spec rather than
# a Go package.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

OUT="${GEN_ASRCLIENT_OUT:-internal/asrclient/client.gen.go}"

go tool oapi-codegen -config internal/asrclient/oapi-codegen.yaml -o "$OUT" deploy/asr/openapi.yaml

gofmt -w "$OUT"
echo "wrote $OUT ($(wc -l < "$OUT") lines)"
