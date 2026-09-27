#!/usr/bin/env bash
#
# Build, sign and check a Chronicle release APK (CHRN-125).
#
#   tool/build_release.sh <versionName> <versionCode>
#
# The only way a release APK is made: mobile-release.yml calls this and nothing
# else. It takes a version and NOTHING that could reach the build -- no
# pass-through arguments, no flavour, no package override -- because
# tool/check_release_build.sh can only guard a build command it can read, and a
# `bool.fromEnvironment` define leaves no trace in the APK for anything else to
# find. The scratch build for device passes is tool/build_rotcheck.sh, a
# separate script that refuses to run in CI.
#
# Output: build/release/chronicle-<versionName>.apk and its .sha256.
set -euo pipefail

usage() { echo "usage: tool/build_release.sh <versionName X.Y.Z> <versionCode>" >&2; exit 2; }
[ $# -eq 2 ] || usage
name=$1 code=$2
[[ $name =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || usage
[[ $code =~ ^[0-9]+$ ]] || usage

here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
app=$(dirname -- "$here")
# shellcheck source=release_lib.sh
. "$here/release_lib.sh"

cd "$app"
rl_flutter_build "$name" "$code"

mkdir -p build/release
out="build/release/chronicle-$name.apk"
rl_sign build/app/outputs/flutter-apk/app-release.apk "$out"
rl_assert "$out" "$RL_RELEASE_PACKAGE" "$name" "$code"

(cd build/release && sha256sum "chronicle-$name.apk" > "chronicle-$name.apk.sha256")
echo "built $out"
