#!/usr/bin/env bash
# Regenerate internal/apiclient from openapi.yaml.
#
#   scripts/gen-apiclient.sh
#
# `internal/apiclient/client.gen.go` is a GENERATED artefact: the Go client the
# MCP transports (CHRN-65) call Chronicle's own HTTP API through. It is
# generated from the same document scripts/gen-api.sh generates the server
# from, so the two ends cannot disagree about a payload without one of the two
# staleness guards going red.
#
# GEN_APICLIENT_OUT redirects the output, which is how the staleness check in
# verify.sh and ci.yml regenerates WITHOUT touching the working tree. A check
# that rewrites the file it is checking cannot tell you what was committed.
#
# The generator is the one pinned by go.mod's `tool` directive, shared with
# gen-api.sh and gen-asrclient.sh.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

SPEC=openapi.yaml
OUT="${GEN_APICLIENT_OUT:-internal/apiclient/client.gen.go}"

go tool oapi-codegen -config internal/apiclient/oapi-codegen.yaml -o "$OUT" "$SPEC"
gofmt -w "$OUT"
echo "wrote $OUT ($(wc -l < "$OUT") lines)"
