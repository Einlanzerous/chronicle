#!/usr/bin/env bash
# CHRN-54's Done-when made mechanical: "no component contains a hex literal,
# and swapping a token changes every surface that uses it." Every colour is a
# CSS custom property defined once, in web/src/styles/tokens.css; this greps
# everything else under web/src for a hex colour literal and fails if it
# finds one.
#
#   scripts/check-tokens.sh
#
# Two files are exempt, and only these two:
#   - web/src/styles/tokens.css, where the tokens are DEFINED, once
#   - web/src/api/schema.d.ts, GENERATED from openapi.yaml and not a colour
#     value in the first place
#
# The regex is deliberately simple, per CHRN-54: `#` followed by 3-8 hex
# digits, word-bounded so it does not partially match into a longer token.
# The one false positive worth naming is a same-page URL fragment that
# happens to look hex -- `href="#fff"` -- filtered below BY ATTRIBUTE
# (href / xlink:href only), not by shape: an earlier version filtered any
# `="#…"`, which also swallowed every SVG presentation attribute --
# `fill="#e2623d"`, `stroke="…"`, `stop-color="…"` -- exactly the shape
# Mark.vue's own colour would take if it ever hardcoded one. Caught in
# review before merge; see the CHRN-54 ticket comment.
set -uo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

HEX_RE='#[0-9a-fA-F]{3,8}\b'

matches="$(grep -rnE "$HEX_RE" web/src \
  --include='*.vue' --include='*.ts' --include='*.css' --include='*.svg' --include='*.js' \
  | grep -v '^web/src/styles/tokens\.css:' \
  | grep -v '^web/src/api/schema\.d\.ts:' \
  | grep -vE '(href|xlink:href)="#[0-9a-fA-F]{3,8}"' \
  || true)"

if [ -n "$matches" ]; then
  echo "hex colour literal(s) found outside web/src/styles/tokens.css:"
  echo "$matches"
  echo "Add or reuse a token in web/src/styles/tokens.css instead."
  exit 1
fi

echo "no hex colour literals outside tokens.css."
