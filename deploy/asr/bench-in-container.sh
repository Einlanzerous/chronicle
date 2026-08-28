#!/usr/bin/env bash
# CHRN-24 — run the CHRN-12 benchmark harness, unmodified, inside estate-asr.
#
# The point is that bench.sh is not rewritten, forked or adapted for the
# container. The image lays itself out as $WHISPER/build-vk/bin and
# $WHISPER/models precisely so the harness that produced the reference numbers
# is the harness that produces these, and the two are therefore comparable. A
# container benchmark written fresh would measure something nobody has a
# baseline for.
#
# Reference, from results/bench-chrn12.csv on the same box, host-native:
#
#   per invocation   base.en 63.9x   small.en 43.2x   medium.en 25.6x   large-v3 12.6x
#   resident         base.en 76.4x   small.en 59.6x   medium.en 36.8x   large-v3 18.3x
#
# CHRN-24's gate is the per-invocation small.en row: 1390 ms total for the 60 s
# clip, decode counted, within noise. Do NOT gate on IDEA-26's published 59.6x
# per invocation — that figure was taken with -nt and is 44% optimistic.
#
# Knobs: IMAGE RIG MODELS_DIR RENDER_NODE OUT_DIR MODELS CLIP REPEATS MAXLOAD
#        MODES — which halves to run, default both. The load guard can refuse
#        one half after the other has passed, so they must be runnable alone.
set -euo pipefail

IMAGE="${IMAGE:-estate-asr:dev}"
RIG="${RIG:-$HOME/projects/catenary/spike/r3-whisper}"
MODELS_DIR="${MODELS_DIR:-$HOME/tools/whisper.cpp/models}"
RENDER_NODE="${RENDER_NODE:-/dev/dri/renderD129}"
OUT_DIR="${OUT_DIR:-$PWD/asr-bench-results}"
MODELS="${MODELS:-base.en small.en medium.en large-v3}"
CLIP="${CLIP:-voice60}"
REPEATS="${REPEATS:-3}"
MAXLOAD="${MAXLOAD:-3.0}"
MODES="${MODES:-per-invocation resident}"

for req in "$RIG/bench.sh" "$RIG/audio/$CLIP.opus" "$MODELS_DIR" "$RENDER_NODE"; do
  [ -e "$req" ] || { echo "missing: $req" >&2; exit 1; }
done
docker image inspect "$IMAGE" >/dev/null 2>&1 || { echo "no image $IMAGE — build it first" >&2; exit 1; }

# Work on a copy. bench.sh writes its decoded WAV back into audio/ and its CSVs
# into results/, and the rig is Catenary's, not ours to litter.
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cp "$RIG/bench.sh" "$WORK/"
mkdir -p "$WORK/audio" "$WORK/results"
cp "$RIG/audio/$CLIP.opus" "$WORK/audio/"

mkdir -p "$OUT_DIR"

run() {                       # run <label> <clips_per_proc>
  local label="$1" cpp="$2"
  echo "=== $label (CLIPS_PER_PROC=$cpp) ==="
  docker run --rm \
    --device "$RENDER_NODE:$RENDER_NODE" \
    -v "$WORK:/bench" \
    -v "$MODELS_DIR:/opt/whisper/models:ro" \
    -e WHISPER=/opt/whisper \
    -e VULKAN_SDK=/opt/vulkan-sdk \
    -e MODELS="$MODELS" \
    -e BACKENDS=vulkan \
    -e CLIP="$CLIP" \
    -e REPEATS="$REPEATS" \
    -e MAXLOAD="$MAXLOAD" \
    -e CLIPS_PER_PROC="$cpp" \
    -e OUTFILE="/bench/results/$label.csv" \
    --entrypoint bash \
    "$IMAGE" /bench/bench.sh
  cp "$WORK/results/$label.csv" "$OUT_DIR/"
}

for mode in $MODES; do
  case "$mode" in
    per-invocation) run in-container-per-invocation 1 ;;
    resident)       run in-container-resident 4 ;;
    *) echo "unknown mode: $mode" >&2; exit 1 ;;
  esac
done

echo
echo "results in $OUT_DIR"
