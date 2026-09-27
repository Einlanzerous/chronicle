#!/usr/bin/env bash
#
# Prove tool/release_lib.sh's assertions can fail (CHRN-125).
#
# rl_assert is what stands between a mis-signed APK and the phone refusing it --
# and `apksigner verify` alone prints the SAME output for an APK signed with and
# without a lineage, so an assertion that looked only there would pass the one
# defect that matters most. This builds tiny APKs with aapt2 from throwaway keys
# (never the real ones) and checks rl_assert passes the right one and rejects
# each wrong one: no lineage, the old key alone, the wrong package, debuggable,
# an ABI missing, versionCode 1.
#
# Needs ANDROID_HOME with build-tools 36.0.0 and platforms/android-36 (mobile.yml
# has both after its debug build), keytool and python3.
set -euo pipefail

here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=release_lib.sh
. "$here/release_lib.sh"
set +e  # rl_assert failures are the point; each case checks its own verdict

bt=$(rl_tools) || exit 1
jar="${ANDROID_HOME:-$ANDROID_SDK_ROOT}/platforms/android-36/android.jar"
[ -f "$jar" ] || { echo "no $jar" >&2; exit 1; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

key() { # key NAME -> $work/NAME.jks, password "throwaway", alias "k"
  keytool -genkeypair -keystore "$work/$1.jks" -storepass throwaway -keypass throwaway \
    -alias k -keyalg RSA -keysize 2048 -validity 2 -dname "CN=$1 throwaway" >/dev/null 2>&1
}
digest() { # digest NAME -> the cert SHA-256, lowercase hex
  keytool -list -v -keystore "$work/$1.jks" -storepass throwaway -alias k 2>/dev/null |
    awk '/SHA256:/{print tolower($2)}' | tr -d ':'
}

# apk OUT PACKAGE CODE NAME DEBUGGABLE ABIS... -> an unsigned, aligned APK
apk() {
  local out=$1 pkg=$2 code=$3 name=$4 dbg=$5
  shift 5
  cat > "$work/AndroidManifest.xml" <<EOF
<manifest xmlns:android="http://schemas.android.com/apk/res/android"
    package="$pkg" android:versionCode="$code" android:versionName="$name">
  <uses-sdk android:minSdkVersion="30" android:targetSdkVersion="36"/>
  <application android:debuggable="$dbg" android:hasCode="false"/>
</manifest>
EOF
  "$bt/aapt2" link -o "$work/base.apk" --manifest "$work/AndroidManifest.xml" -I "$jar" 2>/dev/null || return 1
  python3 - "$work/base.apk" "$@" <<'EOF'
import sys, zipfile
with zipfile.ZipFile(sys.argv[1], "a", zipfile.ZIP_STORED) as z:
    for abi in sys.argv[2:]:
        z.writestr(f"lib/{abi}/libapp.so", b"\x7fELF fixture")
EOF
  "$bt/zipalign" -f -p 4 "$work/base.apk" "$out"
}

key old; key new
"$bt/apksigner" rotate --out "$work/lineage.bin" \
  --old-signer --ks "$work/old.jks" --ks-pass pass:throwaway \
  --new-signer --ks "$work/new.jks" --ks-pass pass:throwaway >/dev/null
RL_DEBUG_CERT_SHA256=$(digest old)
RL_RELEASE_CERT_SHA256=$(digest new)

ANDROID_KEYSTORE_BASE64=$(base64 -w0 "$work/new.jks")
ANDROID_KEYSTORE_PASSWORD=throwaway ANDROID_KEY_ALIAS=k ANDROID_KEY_PASSWORD=throwaway
export ANDROID_KEYSTORE_BASE64 ANDROID_KEYSTORE_PASSWORD ANDROID_KEY_ALIAS ANDROID_KEY_PASSWORD
export RL_LINEAGE="$work/lineage.bin"

pkg=dev.dodson.chronicle
abis=("${RL_ABIS[@]}")

# expect WANT LABEL APK [PACKAGE] [CODE]
expect() {
  local want=$1 label=$2 f=$3 p=${4:-$pkg} c=${5:-7} rc=0 why
  why=$( ( rl_assert "$f" "$p" 1.2.3 "$c" ) 2>&1 >/dev/null) || rc=$?
  if { [ "$want" = pass ] && [ "$rc" -ne 0 ]; } || { [ "$want" = fail ] && [ "$rc" -eq 0 ]; }; then
    echo "FAIL: self-test: rl_assert did not $want $label${why:+ ($why)}" >&2
    fail=1
  elif [ "$want" = pass ]; then
    echo "ok: self-test -- rl_assert accepts $label"
  else
    # The reason, so a rejection for the WRONG reason is visible in the log.
    echo "ok: self-test -- rl_assert rejects $label: ${why%%$'\n'*}"
  fi
}

# The good one, through the real rl_sign.
apk "$work/u.apk" "$pkg" 7 1.2.3 false "${abis[@]}"
( rl_sign "$work/u.apk" "$work/good.apk" ) >/dev/null 2>&1 || { echo "FAIL: rl_sign could not sign the fixture" >&2; exit 1; }
expect pass "a release-signed APK carrying the lineage" "$work/good.apk"

# Signed by the release key, v3 only -- but with NO lineage. `verify` cannot tell.
"$bt/apksigner" sign --ks "$work/new.jks" --ks-pass pass:throwaway \
  --v1-signing-enabled false --v2-signing-enabled false \
  --out "$work/nolineage.apk" "$work/u.apk" >/dev/null 2>&1
expect fail "an APK with no lineage" "$work/nolineage.apk"

# Signed by the OLD key alone: what skipping the re-sign would ship.
"$bt/apksigner" sign --ks "$work/old.jks" --ks-pass pass:throwaway --out "$work/old.apk" "$work/u.apk" >/dev/null 2>&1
expect fail "an APK signed by the old key" "$work/old.apk"

expect fail "a package other than the one asked for" "$work/good.apk" "$pkg.rotcheck"
expect fail "a version other than the one asked for" "$work/good.apk" "$pkg" 8

apk "$work/u1.apk" "$pkg" 1 1.2.3 false "${abis[@]}"
( rl_sign "$work/u1.apk" "$work/code1.apk" ) >/dev/null 2>&1
expect fail "versionCode 1 (not above the installed debug build)" "$work/code1.apk" "$pkg" 1

apk "$work/ud.apk" "$pkg" 7 1.2.3 true "${abis[@]}"
( rl_sign "$work/ud.apk" "$work/debuggable.apk" ) >/dev/null 2>&1
expect fail "a debuggable APK" "$work/debuggable.apk"

apk "$work/ua.apk" "$pkg" 7 1.2.3 false arm64-v8a armeabi-v7a
( rl_sign "$work/ua.apk" "$work/thin.apk" ) >/dev/null 2>&1
expect fail "an APK missing the x86_64 snapshot" "$work/thin.apk"

[ "$fail" -eq 0 ] || exit 1
echo "release_lib self-test: clean"
