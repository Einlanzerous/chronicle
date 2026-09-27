# shellcheck shell=bash
#
# The release signing path (CHRN-125), sourced by tool/build_release.sh and
# tool/build_rotcheck.sh so that the scratch build step 0 walks on the device is
# the SAME build, signature and set of assertions the release job runs -- and
# only the package name differs.
#
# Why a re-sign after Gradle at all: the phone's installed Chronicle was signed
# by a debug key, and Android refuses an update across a signature change unless
# the new APK proves the old key handed over to it. That proof is a lineage
# (android/signing/lineage.bin, written once by `apksigner rotate` from the debug
# key to the release key; certificates and signatures only, no private key). AGP
# cannot declare one, so `apksigner sign --lineage` replaces Gradle's signature.
#
# The form is v3.0, release key only: --rotation-min-sdk-version 30 (the app's
# minSdk, so every supported device sees the one rotated signer) with v1 and v2
# OFF, because a v1/v2 block would need the lineage's OLDEST signer -- the debug
# key -- and that key never goes to CI. apksigner refuses the release-key-only
# form at 33 (v3.1) for exactly that reason; measured, and recorded in the
# CHRN-125 plan.

set -euo pipefail

RL_BUILD_TOOLS_VERSION="36.0.0"

# The two certificates the lineage chains. Pinned here rather than read from
# lineage.bin, because the assertion's job is to catch lineage.bin being wrong.
RL_DEBUG_CERT_SHA256="eff5b632d5ba62cec2958c5013bf9286a0fb638d9fbc2640215a63ee6039b9bc"
RL_RELEASE_CERT_SHA256="3815eafbaaf1ae8fb144058888837524563ab0e58e203c83d3b6d802a576f730"

RL_RELEASE_PACKAGE="dev.dodson.chronicle"

# A fat APK: `flutter build apk` with no --split-per-abi or --target-platform.
# x86_64 is kept deliberately -- it is what an emulator runs.
RL_ABIS=(arm64-v8a armeabi-v7a x86_64)

rl_die() { printf 'release: %s\n' "$*" >&2; exit 1; }

rl_tools() {
  local home=${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}
  [ -n "$home" ] || rl_die "ANDROID_HOME is not set"
  local bt="$home/build-tools/$RL_BUILD_TOOLS_VERSION"
  [ -x "$bt/apksigner" ] && [ -x "$bt/aapt2" ] ||
    rl_die "build-tools $RL_BUILD_TOOLS_VERSION missing under $home (sdkmanager 'build-tools;$RL_BUILD_TOOLS_VERSION')"
  printf '%s\n' "$bt"
}

# The release build command. The ONE place it is written, and it takes nothing
# but a version: tool/check_release_build.sh fails on any dart-define in this
# file, and there is no argument through which one could arrive.
rl_flutter_build() {
  local name=$1 code=$2
  flutter build apk --release --build-name="$name" --build-number="$code"
}

# rl_sign IN OUT -- re-sign with the release key and the lineage.
# Needs ANDROID_KEYSTORE_BASE64, ANDROID_KEYSTORE_PASSWORD, ANDROID_KEY_ALIAS,
# ANDROID_KEY_PASSWORD (the mobile-release environment in CI; `signet exec
# --secret chronicle/…` locally). The keystore is decoded outside the tree and
# removed on return.
rl_sign() {
  local in=$1 out=$2 bt lineage ks
  bt=$(rl_tools)
  lineage=${RL_LINEAGE:-"$(dirname -- "${BASH_SOURCE[0]}")/../android/signing/lineage.bin"}
  [ -f "$lineage" ] || rl_die "lineage not found: $lineage"
  local v
  for v in ANDROID_KEYSTORE_BASE64 ANDROID_KEYSTORE_PASSWORD ANDROID_KEY_ALIAS ANDROID_KEY_PASSWORD; do
    [ -n "${!v:-}" ] || rl_die "$v is not set"
  done

  # Removed explicitly on both paths rather than by a RETURN trap: a RETURN
  # trap set in a function stays set and fires on every later return too.
  ks=$(mktemp "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/release.XXXXXX")
  local rc=0
  printf '%s' "$ANDROID_KEYSTORE_BASE64" | base64 -d > "$ks" || rc=$?
  if [ "$rc" -eq 0 ]; then
    KEYSTORE_PASSWORD=$ANDROID_KEYSTORE_PASSWORD KEY_PASSWORD=$ANDROID_KEY_PASSWORD \
      "$bt/apksigner" sign \
      --ks "$ks" --ks-key-alias "$ANDROID_KEY_ALIAS" \
      --ks-pass env:KEYSTORE_PASSWORD --key-pass env:KEY_PASSWORD \
      --lineage "$lineage" \
      --rotation-min-sdk-version 30 \
      --v1-signing-enabled false --v2-signing-enabled false \
      --out "$out" "$in" || rc=$?
  fi
  rm -f "$ks"
  [ "$rc" -eq 0 ] || rl_die "signing failed (rc=$rc)"
}

# rl_assert APK PACKAGE NAME CODE -- everything the phone will check, checked
# here first, before anything is uploaded. Prints one ok line per property;
# exits non-zero on the first that does not hold.
rl_assert() {
  local apk=$1 pkg=$2 name=$3 code=$4 bt out
  bt=$(rl_tools)
  [ -f "$apk" ] || rl_die "no APK at $apk"

  # 1. Signed by the release key, alone, under v3. `verify` cannot see a
  #    lineage -- its output is byte-identical with and without one -- so this
  #    only proves WHO signed.
  out=$("$bt/apksigner" verify --print-certs -v "$apk" 2>&1) ||
    rl_die "apksigner verify failed on $apk:"$'\n'"$out"
  grep -qx 'Verified using v3 scheme (APK Signature Scheme v3): true' <<<"$out" ||
    rl_die "$apk is not v3-signed"
  local signers
  signers=$(grep -E '^Signer #[0-9]+ certificate SHA-256 digest: ' <<<"$out" | awk '{print $NF}')
  [ "$(grep -c . <<<"$signers")" -eq 1 ] || rl_die "$apk has $(grep -c . <<<"$signers") signers, want exactly 1"
  [ "$signers" = "$RL_RELEASE_CERT_SHA256" ] ||
    rl_die "$apk is signed by $signers, not the release key $RL_RELEASE_CERT_SHA256"
  echo "ok: signed by the release key alone (v3)"

  # 2. The lineage IN THE APK: debug key first, release key second, nothing
  #    else. Read from the artifact, not from lineage.bin -- an APK signed
  #    without --lineage exits non-zero here, and would otherwise pass every
  #    other check and be refused by the phone as an update.
  out=$("$bt/apksigner" lineage --in "$apk" --print-certs 2>&1) ||
    rl_die "$apk carries no signing lineage -- the installed app would refuse it:"$'\n'"$out"
  local chain
  chain=$(grep -E '^Signer #[0-9]+ in lineage certificate SHA-256 digest: ' <<<"$out" | awk '{print $NF}' | paste -sd' ')
  [ "$chain" = "$RL_DEBUG_CERT_SHA256 $RL_RELEASE_CERT_SHA256" ] ||
    rl_die "$apk lineage is [$chain], want [debug release]"
  echo "ok: lineage is debug -> release"

  # 3. Identity. A wrong package installs BESIDE the real app instead of over
  #    it and would pass everything above; a debuggable one is a debug build.
  out=$("$bt/aapt2" dump badging "$apk" 2>/dev/null) || rl_die "aapt2 could not read $apk"
  local want="package: name='$pkg' versionCode='$code' versionName='$name'"
  grep -q "^$want" <<<"$out" ||
    rl_die "$apk identity is [$(grep '^package:' <<<"$out" | cut -c1-160)], want [$want]"
  [ "$code" -gt 1 ] || rl_die "versionCode $code must be above the installed debug build's 1"
  if grep -q '^application-debuggable' <<<"$out"; then
    rl_die "$apk is debuggable"
  fi
  echo "ok: $pkg $name ($code), not debuggable"

  # 4. Fat, as documented.
  local listing abi
  listing=$(unzip -Z1 "$apk")
  for abi in "${RL_ABIS[@]}"; do
    grep -qx "lib/$abi/libapp.so" <<<"$listing" || rl_die "$apk has no lib/$abi/libapp.so"
  done
  echo "ok: libapp.so for ${RL_ABIS[*]}"
}
