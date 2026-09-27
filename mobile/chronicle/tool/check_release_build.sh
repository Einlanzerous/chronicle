#!/usr/bin/env bash
#
# Release-build hygiene guard (CHRN-125). A port of Lyceum's
# tool/check_store_build.sh (LYCM-104), with one check it did not need.
#
# Two compile-time defines exist in this app, and a release must carry neither:
#
#   CHRONICLE_BASE_URL           would point every install at whoever built it
#                                and hide the connect prompt the invite QR starts
#                                from (lib/api/server_url.dart)
#   CHRONICLE_PRUNE_LOCAL_AUDIO  turns on the phone's deletion of its own audio,
#                                which CHRN-120 ships OFF until the database has
#                                a backup (lib/queue/prune.dart)
#
# Nothing in the Dart source can hold that line: it is a property of the build
# command. So it is checked from four sides:
#
#   config    nothing on the release build path passes a dart-define
#   hostnames no shipped file and no artifact names the estate's private hosts
#   origins   the Dart snapshot holds no absolute URL that is not expected
#   marker    the snapshot says, in a string, that the prune is off
#
# The last one exists because the prune define is a bool: const-folded, it
# leaves NO string in libapp.so, so the origin scan -- which catches a baked
# base URL whatever it points at -- is blind to it. `pruneBuildMarker` in
# prune.dart is folded the same way, so exactly one of its two literals is in
# the snapshot, and this asserts which.
#
# Where it runs: config-only in mobile.yml on every PR and push to main (advisory
# -- main has no required checks), and in full in mobile-release.yml on the built
# APK before upload (the hard stop). tool/build_rotcheck.sh runs the artifact
# half on its scratch APK too, so a release never meets these checks first.
#
# Usage:
#   tool/check_release_build.sh                 # config + source-host checks
#   tool/check_release_build.sh app.apk ...     # the same, plus scan each APK
#   tool/check_release_build.sh --self-test     # prove every check can fail
set -euo pipefail

here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
app=$(dirname -- "$here")     # mobile/chronicle
repo=$(cd -- "$app/../.." && pwd)

# The estate's own domain, and the app's host under it. `chronicle-direct` alone
# is deliberately NOT on the list: the doc comments in server_url.dart and
# transport.dart name it as `chronicle-direct.…`, which is documentation, and
# a bare-word match would fail on them. The full host is what a leak looks like.
FORBIDDEN_HOSTS=(
  "zerogravity.industries"
  "chronicle-direct.zerogravity"
)

# Every absolute URL a clean release snapshot is expected to contain, read off a
# real one (CHRN-125 step 0). The first is the generated API client's default
# basePath -- openapi.yaml's `servers` entry, the compose service name on the
# estate's Docker network. The app never uses it (it always passes the one
# address the invite supplied) and no phone can resolve it, so it is a
# placeholder rather than a default. The rest are Flutter's own diagnostics.
#
# Adding to this list should be a deliberate, reviewed act. The likeliest honest
# cause is a Flutter upgrade bringing a new diagnostic link: confirm that is what
# it is, add it here, and re-dispatch the tag.
ALLOWED_ORIGINS=(
  "http://chronicle:4009"
  "https://api.flutter.dev"
  "https://docs.flutter.dev"
  "https://github.com"
  "https://pub.dev"
)

# The two literals of pruneBuildMarker, byte for byte (prune_test.dart pins the
# first). When CHRN-124 flips the default, it swaps these two.
MARKER_REQUIRED="chronicle: local-audio prune off"
MARKER_FORBIDDEN="chronicle: local-audio prune ON"

# Everything that decides how a release is built. A define can only reach a
# release through one of these.
RELEASE_WORKFLOW="$repo/.github/workflows/mobile-release.yml"
config_files() {
  printf '%s\n' "$RELEASE_WORKFLOW" "$here/build_release.sh" "$here/release_lib.sh"
  find "$app/android" \( -name build -o -name .gradle \) -prune -o \
    -type f \( -name '*.gradle' -o -name '*.gradle.kts' -o -name '*.properties' \) -print
}

# Files that ship, or that decide what ships. Test fixtures are left out on
# purpose: they use reserved .example / .invalid domains and reach no artifact.
SHIPPED_PATHS=(
  "$app/lib"
  "$app/packages/chronicle_api/lib"
  "$app/android"
  "$app/pubspec.yaml"
  "$RELEASE_WORKFLOW"
  "$here/build_release.sh"
  "$here/release_lib.sh"
)

fail=0
note() { printf '%s\n' "$*" >&2; }
bad() { fail=1; note "FAIL: $*"; }

# One scratch root for the whole run, removed on exit. (Per-function RETURN traps
# fire after the function's locals are gone -- Lyceum's comment, kept.)
workroot=$(mktemp -d)
trap 'rm -rf "$workroot"' EXIT

# `dart-define` covers --dart-define, --dart-define-from-file and gradle's
# -Pdart-defines. Numbering comes off the real file and comment lines (#, //, *)
# are dropped after, so the line a failure names is the line to go and look at.
scan_config() {
  local f defines rc clean=1
  for f in "$@"; do
    if [ ! -f "$f" ]; then
      bad "release config file not found: $f"
      clean=0
      continue
    fi
    defines=$(grep -n -- 'dart-define' "$f") && rc=0 || rc=$?
    if [ "$rc" -gt 1 ]; then
      bad "could not read $f (grep rc=$rc)"
      clean=0
      continue
    fi
    defines=$(printf '%s\n' "$defines" | grep -vE '^[0-9]+:[[:space:]]*(#|//|\*)' || true)
    if [ -n "$defines" ]; then
      note "$(printf '%s\n' "$defines" | sed "s#^#  ${f#"$repo"/}:#")"
      bad "${f#"$repo"/} passes a dart-define; a release bakes in no server and no prune (CHRN-125)"
      clean=0
    fi
  done
  [ "$clean" -eq 1 ] && note "ok: nothing on the release build path passes a dart-define"
  return 0
}

check_config() {
  local files=()
  mapfile -t files < <(config_files)
  scan_config "${files[@]}"
  scan_paths_for_hosts "shipped sources and build config" "${SHIPPED_PATHS[@]}"
  return 0
}

# grep's exit codes are not two-valued: 1 is "no match", 2 is "something went
# wrong" -- a path that no longer exists -- and it returns 2 even when it matched
# on the paths that do exist. An error is a failure, never a pass.
scan_paths_for_hosts() {
  local label=$1
  shift
  local host hits rc clean=1
  for host in "${FORBIDDEN_HOSTS[@]}"; do
    # -I skips binaries (icons, fonts) -- those are the artifact scan's job.
    # Markdown is skipped: the docs DO name the hosts, and docs ship to nobody.
    hits=$(grep -rInF --exclude='*.md' \
      --exclude-dir=build --exclude-dir=.gradle --exclude-dir=.dart_tool \
      -- "$host" "$@" 2>/dev/null) && rc=0 || rc=$?
    if [ "$rc" -eq 0 ]; then
      note "$hits"
      bad "'$host' appears in $label (CHRN-125)"
      clean=0
    elif [ "$rc" -gt 1 ]; then
      bad "the '$host' scan over $label errored (grep rc=$rc) -- a renamed or missing path? Not calling that clean."
      clean=0
    fi
  done
  [ "$clean" -eq 1 ] && note "ok: no private hostname in $label"
  return 0
}

# Unpack an APK and read every entry. A define's string value is const-folded
# into the Dart AOT snapshot (lib/<abi>/libapp.so), compressed inside the zip,
# so the archive has to be expanded rather than grepped where it lies.
scan_artifact() {
  local artifact=$1
  if [ ! -f "$artifact" ]; then
    bad "artifact not found: $artifact"
    return 0
  fi

  local dir name
  name=$(basename -- "$artifact")
  dir=$(mktemp -d "$workroot/scan.XXXXXX")

  if ! unzip -q -o "$artifact" -d "$dir" 2>/dev/null; then
    bad "could not unpack $artifact"
    return 0
  fi

  local host hits clean=1
  for host in "${FORBIDDEN_HOSTS[@]}"; do
    # -a: the snapshot and the dex are binary; the string sits in them plainly.
    if hits=$(grep -rlaF -- "$host" "$dir" 2>/dev/null); then
      note "$(printf '%s\n' "$hits" | sed "s#^$dir/#  #")"
      bad "'$host' is baked into $name (CHRN-125)"
      clean=0
    fi
  done
  [ "$clean" -eq 1 ] &&
    note "ok: $name -- no private hostname in $(find "$dir" -type f | wc -l) entries"

  local snapshots
  snapshots=$(find "$dir" -type f -name 'libapp.so')
  if [ -z "$snapshots" ]; then
    # A release APK carries one per ABI. None means a debug build (a JIT kernel
    # blob) or something this does not understand -- either way the checks below
    # would run over nothing, which must not read as a pass.
    bad "$name contains no libapp.so -- nothing to scan; is this a release build?"
    return 0
  fi
  scan_snapshot_origins "$dir" "$name"
  scan_snapshot_marker "$name" "$snapshots"
  return 0
}

# An origin passes if it IS an allowed one, or -- for an allowed origin with an
# explicit port -- if it is that origin followed only by digits. The snapshot
# packs strings back to back, and in a real one the byte after
# `http://chronicle:4009` is the next object's tag, which happens to be ASCII
# '2'; the regex cannot tell a port from the digit after it. A different HOST on
# the same port still fails (--self-test proves it).
origin_allowed() {
  local found=$1 ok
  for ok in "${ALLOWED_ORIGINS[@]}"; do
    [ "$found" = "$ok" ] && return 0
    if [[ $ok =~ :[0-9]+$ ]] && [[ ${found#"$ok"} != "$found" ]] &&
      [[ ${found#"$ok"} =~ ^[0-9]+$ ]]; then
      return 0
    fi
  done
  return 1
}

scan_snapshot_origins() {
  local dir=$1 name=$2
  # Scheme + host + port only: the part that says WHICH server. `|| true` is
  # load-bearing under pipefail: a snapshot with no URL at all makes grep exit 1.
  local found unexpected="" count o
  found=$(find "$dir" -type f -name 'libapp.so' -print0 |
    xargs -0 grep -ohaE 'https?://[A-Za-z0-9._:-]+' | sort -u) || true
  while IFS= read -r o; do
    [ -z "$o" ] && continue
    origin_allowed "$o" || unexpected+="$o"$'\n'
  done <<<"$found"

  if [ -n "$unexpected" ]; then
    note "$(printf '%s' "$unexpected" | sed 's/^/  /')"
    bad "$name -- unexpected absolute URL(s) in the Dart snapshot (CHRN-125)"
  else
    count=$(printf '%s' "$found" | grep -c . || true)
    note "ok: $name -- snapshot holds only the $count expected URL(s)"
  fi
  return 0
}

# EVERY snapshot must say off, and none may say on: one libapp.so per ABI, and a
# build is only as clean as the one the phone happens to load.
scan_snapshot_marker() {
  local name=$1 snapshots=$2 s clean=1
  while IFS= read -r s; do
    if grep -qaF -- "$MARKER_FORBIDDEN" "$s"; then
      bad "$name -- ${s##*/lib/} says '$MARKER_FORBIDDEN': built with the prune on (CHRN-120/125)"
      clean=0
    elif ! grep -qaF -- "$MARKER_REQUIRED" "$s"; then
      bad "$name -- ${s##*/lib/} holds neither prune marker; cannot tell how it was built, so not calling it off"
      clean=0
    fi
  done <<<"$snapshots"
  [ "$clean" -eq 1 ] && note "ok: $name -- every snapshot says '$MARKER_REQUIRED'"
  return 0
}

# Build a zip without assuming `zip` is installed. python3 ships with both the
# runner image and the toolchain.
pack() {
  local src=$1 out=$2
  python3 -c 'import shutil,sys; shutil.make_archive(sys.argv[1], "zip", sys.argv[2])' "$out" "$src"
  mv -- "$out.zip" "$out"
}

# fixture_apk OUT BODY -- a zip shaped like an APK whose one libapp.so holds
# BODY among binary bytes, the shape of a real snapshot.
fixture_apk() {
  local out=$1 body=$2 stage
  stage=$(mktemp -d "$workroot/stage.XXXXXX")
  mkdir -p "$stage/lib/arm64-v8a"
  printf '\x7fELF\x02\x01\x01\x00 %s \x00\x00' "$body" > "$stage/lib/arm64-v8a/libapp.so"
  pack "$stage" "$out"
}

# expect_scan WANT LABEL APK -- run the artifact scan in a subshell and check its
# verdict. WANT is pass or fail.
expect_scan() {
  local want=$1 label=$2 apk=$3 rc=0
  ( fail=0; scan_artifact "$apk"; exit "$fail" ) >/dev/null 2>&1 || rc=$?
  if [ "$want" = fail ] && [ "$rc" -eq 0 ]; then
    bad "self-test: the scan passed $label"
  elif [ "$want" = pass ] && [ "$rc" -ne 0 ]; then
    bad "self-test: the scan rejected $label"
  else
    if [ "$want" = pass ]; then note "ok: self-test -- $label passes"
    else note "ok: self-test -- $label is caught"; fi
  fi
}

# A scanner that never fails is worse than no scanner, because it reads as proof.
# Every check above is shown here to fail on the thing it exists to catch, and to
# pass on a clean input -- or the failures prove nothing but that everything fails.
self_test() {
  local dir rc f
  dir=$(mktemp -d "$workroot/selftest.XXXXXX")
  local off="$MARKER_REQUIRED"

  # Artifact scan.
  fixture_apk "$dir/clean.apk" "$off ${ALLOWED_ORIGINS[0]}2 ${ALLOWED_ORIGINS[1]}"
  expect_scan pass "a clean snapshot (with the real run-on digit)" "$dir/clean.apk"
  fixture_apk "$dir/host.apk" "$off https://${FORBIDDEN_HOSTS[0]}/sign-in"
  expect_scan fail "a baked '${FORBIDDEN_HOSTS[0]}'" "$dir/host.apk"
  fixture_apk "$dir/direct.apk" "$off https://${FORBIDDEN_HOSTS[1]}.industries/"
  expect_scan fail "a baked '${FORBIDDEN_HOSTS[1]}'" "$dir/direct.apk"
  fixture_apk "$dir/origin.apk" "$off https://memos.example.invalid"
  expect_scan fail "an origin on no denylist" "$dir/origin.apk"
  fixture_apk "$dir/port.apk" "$off http://elsewhere:4009"
  expect_scan fail "an unlisted host on the allowed port" "$dir/port.apk"
  fixture_apk "$dir/on.apk" "$MARKER_FORBIDDEN"
  expect_scan fail "a snapshot built with the prune on" "$dir/on.apk"
  fixture_apk "$dir/both.apk" "$off $MARKER_FORBIDDEN"
  expect_scan fail "a snapshot holding both markers" "$dir/both.apk"
  fixture_apk "$dir/none.apk" "https://pub.dev"
  expect_scan fail "a snapshot holding no marker" "$dir/none.apk"
  fixture_apk "$dir/short.apk" "local-audio prune off"
  expect_scan fail "a snapshot holding only a fragment of the marker" "$dir/short.apk"
  mkdir -p "$dir/debugstage/assets"
  printf 'kernel' > "$dir/debugstage/assets/kernel_blob.bin"
  pack "$dir/debugstage" "$dir/debug.apk"
  expect_scan fail "an APK with no libapp.so" "$dir/debug.apk"

  # Config scan: each define, each spelling, and a clean file.
  local define
  for define in \
    '--dart-define=CHRONICLE_BASE_URL=https://memos.example.invalid' \
    '--dart-define=CHRONICLE_PRUNE_LOCAL_AUDIO=true' \
    '--dart-define-from-file=defines.json' \
    '-Pdart-defines=Q0hST05JQ0xFX1BSVU5FX0xPQ0FMX0FVRElPPXRydWU='; do
    f="$dir/build_release.sh"
    printf '#!/bin/sh\n# a comment may say --dart-define\nflutter build apk --release %s\n' "$define" > "$f"
    rc=0
    ( fail=0; scan_config "$f"; exit "$fail" ) >/dev/null 2>&1 || rc=$?
    if [ "$rc" -eq 0 ]; then
      bad "self-test: the config scan passed a build carrying $define"
    else
      note "ok: self-test -- a build carrying ${define:0:48}… is caught"
    fi
  done
  printf '#!/bin/sh\n# --dart-define is banned here, and this comment says so\n// and so does this\nflutter build apk --release\n' > "$f"
  rc=0
  ( fail=0; scan_config "$f"; exit "$fail" ) >/dev/null 2>&1 || rc=$?
  if [ "$rc" -ne 0 ]; then bad "self-test: the config scan rejected a clean build (comments only)"
  else note "ok: self-test -- a define named only in comments passes"; fi
  rc=0
  ( fail=0; scan_config "$dir/nonexistent.yml"; exit "$fail" ) >/dev/null 2>&1 || rc=$?
  if [ "$rc" -eq 0 ]; then bad "self-test: the config scan called a missing file clean"
  else note "ok: self-test -- a missing config file is an error, not a pass"; fi

  # Source host scan: a hit, a missing path, a clean tree -- and the doc-comment
  # spelling that must NOT fail.
  mkdir -p "$dir/src"
  printf 'const base = "https://%s/";\n' "${FORBIDDEN_HOSTS[0]}" > "$dir/src/leak.dart"
  rc=0
  ( fail=0; scan_paths_for_hosts "fixture" "$dir/src"; exit "$fail" ) >/dev/null 2>&1 || rc=$?
  if [ "$rc" -eq 0 ]; then bad "self-test: the source scan passed a file naming '${FORBIDDEN_HOSTS[0]}'"
  else note "ok: self-test -- the source scan catches a private host in a shipped file"; fi
  rc=0
  ( fail=0; scan_paths_for_hosts "fixture" "$dir/nonexistent"; exit "$fail" ) >/dev/null 2>&1 || rc=$?
  if [ "$rc" -eq 0 ]; then bad "self-test: the source scan called a missing path clean"
  else note "ok: self-test -- a missing scan path is an error, not a pass"; fi
  rm -f "$dir/src/leak.dart"
  printf '/// the app host is `chronicle-direct.…`, not the browser one\nconst base = "";\n' > "$dir/src/clean.dart"
  rc=0
  ( fail=0; scan_paths_for_hosts "fixture" "$dir/src"; exit "$fail" ) >/dev/null 2>&1 || rc=$?
  if [ "$rc" -ne 0 ]; then bad "self-test: the source scan rejected the doc-comment spelling"
  else note "ok: self-test -- a clean tree (with the doc-comment spelling) passes"; fi
  return 0
}

if [ "${1:-}" = "--self-test" ]; then
  self_test
else
  check_config
  for artifact in "$@"; do
    scan_artifact "$artifact"
  done
fi

if [ "$fail" -ne 0 ]; then
  note ""
  note "Release-build hygiene failed. A release carries no server address and no"
  note "prune: the invite QR supplies the one, CHRN-124 turns on the other."
  note "See mobile/chronicle/README.md, 'Releases'."
  exit 1
fi
note "release-build hygiene: clean"
