#!/usr/bin/env bash
#
# A scratch release for device passes (CHRN-125 step 0): the release build,
# signature and assertions, under package `dev.dodson.chronicle.rotcheck`,
# labelled "Chronicle rotcheck", so it installs BESIDE the real app and never
# touches its captures or session.
#
#   signet exec --secret chronicle/ANDROID_KEYSTORE_BASE64 \
#     --secret chronicle/ANDROID_KEYSTORE_PASSWORD \
#     --secret chronicle/ANDROID_KEY_ALIAS \
#     --secret chronicle/ANDROID_KEY_PASSWORD -- tool/build_rotcheck.sh [OUT_DIR]
#
# Writes OUT_DIR/rotcheck-debug.apk (debug key, the "before") and
# OUT_DIR/rotcheck-release.apk (release key + lineage, the "after"). Installing
# the second over the first is the key rotation, walked on a package that holds
# nothing.
#
# The package change is made in a COPY of the app, by editing that copy's
# build.gradle.kts. Nothing in the release path (build_release.sh, the gradle
# file in the tree, mobile-release.yml) has a knob for it, and this script
# refuses to run in CI -- a release that shipped under another id would install
# beside the real app instead of over it while passing every signing check.
set -euo pipefail

if [ -n "${CI:-}" ] || [ -n "${GITHUB_ACTIONS:-}" ]; then
  echo "build_rotcheck.sh is for a device pass on a workstation; it does not run in CI." >&2
  exit 1
fi

here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
app=$(dirname -- "$here")
out_dir=$(realpath -m -- "${1:-$app/build/rotcheck}")
mkdir -p "$out_dir"

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
rsync -a --exclude build --exclude .dart_tool --exclude .gradle "$app/" "$scratch/app/"

gradle="$scratch/app/android/app/build.gradle.kts"
python3 - "$gradle" <<'EOF'
import sys
p = sys.argv[1]
s = open(p).read()
for old, new in [
    ('applicationId = "dev.dodson.chronicle"', 'applicationId = "dev.dodson.chronicle.rotcheck"'),
    ('manifestPlaceholders["appLabel"] = "Chronicle"\n', 'manifestPlaceholders["appLabel"] = "Chronicle rotcheck"\n'),
    # The "before" build must be the SAME package, so the scratch copy's debug
    # build type loses the .dev suffix.
    ('applicationIdSuffix = ".dev"', '// applicationIdSuffix removed by build_rotcheck.sh'),
]:
    if s.count(old) != 1:
        sys.exit(f"build_rotcheck: expected exactly one {old!r} in {p}")
    s = s.replace(old, new)
open(p, "w").write(s)
EOF

# shellcheck source=release_lib.sh
. "$here/release_lib.sh"
name="0.0.0"
code=$(date +%s)

cd "$scratch/app"
flutter pub get >/dev/null
flutter build apk --debug
cp build/app/outputs/flutter-apk/app-debug.apk "$out_dir/rotcheck-debug.apk"

rl_flutter_build "$name" "$code"
RL_LINEAGE="$app/android/signing/lineage.bin" \
  rl_sign build/app/outputs/flutter-apk/app-release.apk "$out_dir/rotcheck-release.apk"
rl_assert "$out_dir/rotcheck-release.apk" "$RL_RELEASE_PACKAGE.rotcheck" "$name" "$code"

# The artifact half of the release guard, on this APK, before any tag exists:
# the prune marker and the origin allowlist are proven here first, so the first
# release job repeats something already seen instead of discovering it.
"$here/check_release_build.sh" "$out_dir/rotcheck-release.apk"

echo "before: $out_dir/rotcheck-debug.apk"
echo "after:  $out_dir/rotcheck-release.apk"
