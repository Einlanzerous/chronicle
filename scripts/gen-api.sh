#!/usr/bin/env bash
# Regenerate internal/api/wire from openapi.yaml.
#
#   scripts/gen-api.sh
#
# `internal/api/wire/wire.gen.go` is a GENERATED artefact: the payload types and
# the route registration for Chronicle's own HTTP surface. CI regenerates it and
# fails if the result differs from what is committed -- the same guard
# scripts/gen-asrclient.sh puts on the ASR client and scripts/gen-schema.sh puts
# on schema.sql, for the same reason: a generated artefact with no guard is a
# generated artefact somebody hand-edits.
#
# GEN_API_OUT redirects the output, which is how the staleness check in
# verify.sh regenerates WITHOUT touching the working tree. A check that rewrites
# the file it is checking cannot tell you what was committed.
#
# The generator is PINNED: `tool github.com/oapi-codegen/oapi-codegen/v2/...` in
# go.mod, resolved through go.sum. An unpinned generator makes the staleness
# guard a coin flip, because two versions disagree about formatting and neither
# is wrong.
#
# This document has three other clients, in three other languages -- E8's web
# client, E9's Android capture, E10's MCP -- which is the whole reason the
# contract is a spec rather than a Go package. This script generates Chronicle's
# own end of it.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

SPEC=openapi.yaml
OUT="${GEN_API_OUT:-internal/api/wire/wire.gen.go}"

go tool oapi-codegen -config internal/api/wire/oapi-codegen.yaml -o "$OUT" "$SPEC"
gofmt -w "$OUT"
echo "wrote $OUT ($(wc -l < "$OUT") lines)"
