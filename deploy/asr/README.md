# Shared estate ASR runner — whisper.cpp on the R9700

**CHRN-24 · E3.** One whisper.cpp build, on one GPU, behind one image. Chronicle
is client one; Catenary is client two and its handoff is CHRN-29.

This directory is the *runner*: a pinned, reproducible container that transcribes
a file on the R9700 and hits a known number. It is deliberately not a service —
the HTTP job contract is CHRN-25 and the resident worker with its GPU lease is
CHRN-26. What is settled here is everything that is a property of the machine and
the build rather than of the service.

---

## Why every version below is pinned

IDEA-26 could not promise that a later whisper.cpp keeps this backend fast, and
this rig has now produced a confidently wrong number **twice**:

1. **A backend that worked and was 5× off the pace.** ROCm/HIP: identical
   transcripts, genuine GPU use, 89% utilisation, zero build errors — and slow.
2. **A decode flag that read as formatting.** `-nt` suppresses timestamp tokens
   *during decoding*, so every latency figure IDEA-26 published is optimistic by
   20–71%, and on one clip it silently dropped 45% of the transcript.

Neither looked like a failure. That is the whole argument for pinning: a float in
any of these four values moves a published latency figure with nothing in the
output to say so.

| pin | value | why this one |
|---|---|---|
| `WHISPER_REF` | `1fe009ca` (2026-08-14), ggml 0.20.0 | the exact tree IDEA-26 built and CHRN-12 re-measured. A SHA, not a tag — tags move |
| `VULKAN_SDK_VERSION` | `1.4.357.1` | **required, not preference** — see below |
| `MESA_VERSION` | `25.2.8-0ubuntu0.24.04.2` | the RADV the reference numbers were taken on |
| `LIBVULKAN_VERSION` | `1.3.275.0-1build1` | the loader they were taken on — see below |
| `FFMPEG_VERSION` | `7:6.1.1-3ubuntu5` | the decode is *inside* the measurement, not next to it |

Mesa is pinned **exactly**, so when that version rotates out of `noble-updates`
the build fails loudly rather than quietly measuring a different driver. Bump it
as a decision, then re-measure and update the table at the bottom of this file.

**`ubuntu:24.04` is deliberately not on that list.** It is a moving tag, rebuilt
regularly, and pinning it by digest would buy little that the five rows above do
not already buy. That is only defensible because the one thing it supplies that
reaches a published figure — ffmpeg — is pinned on its own line. Every figure
here counts an Opus decode, so a base bump that changed ffmpeg would move
`decode_ms` against a gate whose whole margin is a couple of percent, which is
precisely the silent drift the rest of this file exists to prevent.

### The SDK is required, and the reason is invisible

Noble ships `glslc` **2023.8**, three years behind the SDK's. It cannot compile
ggml's `KHR_cooperative_matrix` shaders — the path that drives RDNA4's matrix
cores. Build against distro shaderc and you get a binary that compiles, runs and
transcribes correctly, just slowly: failure mode #1 above, reproduced exactly.

The build log is where you check this. It must say:

```
-- Enabling coopmat glslc support
-- Enabling coopmat2 glslc support
```

and the runtime banner must say `matrix cores: KHR_coopmat`.

### Two corrections to IDEA-26's write-up, found while building this

**The SDK ships its loader under `lib/VulkanLoader/lib/`, not `lib/`.** So
`$VULKAN_SDK/lib` contains no `libvulkan.so` of any kind. Two consequences:

- The recipe's hand-made `libvulkan.so` symlink is not just a no-sudo
  workaround — there is nothing in the SDK to link against either way. It is
  reproduced here against the distro loader for exactly that reason.
- More importantly: `bench.sh` puts `$VULKAN_SDK/lib` on `LD_LIBRARY_PATH`, and
  that directory holds no loader. **Every reference number in CHRN-12 was
  therefore measured against the distro loader, 1.3.275.0**, with the SDK
  supplying headers and `glslc` only. That is what this image pins, because it is
  what was measured. The SDK is a build-time dependency and is not in the runtime
  stage at all.

### renderD129 is the R9700, and the numbering reads the wrong way round

Confirmed through `/sys/class/drm/*/device/uevent`, not inferred:

```
renderD128 -> i915    0000:00:02.0   CometLake iGPU
renderD129 -> amdgpu  0000:03:00.0   Navi 48 / R9700
```

Only the R9700's node is passed in, so the iGPU is not in the container to be
enumerated at all and `GGML_VK_VISIBLE_DEVICES=0` is a second belt rather than
the only thing between this and a far slower device.

---

## Build

```bash
docker build -f deploy/asr/Dockerfile -t estate-asr:dev deploy/asr
```

Models are **not** baked in. `large-v3` alone is 2.9 GB and the four together are
5.0 GB, against a 466 MB default — and a model is data the service is pointed at,
not code it ships. Mount them at `/opt/whisper/models`.

## Smoke test — is the GPU actually being used?

The first thing to check, always, because a silent CPU fallback reports success:

```bash
docker run --rm --device /dev/dri/renderD129 \
  -v ~/tools/whisper.cpp/models:/opt/whisper/models:ro \
  -v ~/projects/catenary/spike/r3-whisper/audio:/audio:ro \
  estate-asr:dev -m /opt/whisper/models/ggml-base.en.bin -f /audio/tiny1s.wav
```

Expected, and all three lines matter:

```
ggml_vulkan: Found 1 Vulkan devices:
ggml_vulkan: 0 = AMD Radeon Graphics (RADV GFX1201) (radv) | ... | matrix cores: KHR_coopmat
whisper_backend_init_gpu: using Vulkan0 backend
```

`WARNING: radv is not a conformant Vulkan implementation` is Mesa's standard
conformance notice, not a defect. Transcripts are identical to ROCm's.

That invocation takes a WAV. **An actual memo is Opus, and `whisper-cli` cannot
read it** — ffmpeg is in the image for exactly that, so the shape is two steps:

```bash
docker run --rm --device /dev/dri/renderD129 \
  -v ~/tools/whisper.cpp/models:/opt/whisper/models:ro \
  -v ~/projects/catenary/spike/r3-whisper/audio:/audio:ro \
  --entrypoint bash estate-asr:dev -c '
    ffmpeg -y -hide_banner -loglevel error -i /audio/voice60.opus \
      -ac 1 -ar 16000 -c:a pcm_s16le /tmp/voice60.wav
    whisper-cli -m /opt/whisper/models/ggml-small.en.bin -f /tmp/voice60.wav'
```

Joining those two steps behind the job contract is CHRN-25's work, not this
image's. Shown here only so the documented path is one the image can actually
run on the format the estate records in.

## Benchmark

```bash
OUT_DIR=deploy/asr/results ./deploy/asr/bench-in-container.sh
```

The harness is **not** rewritten for the container. The image lays itself out as
`$WHISPER/build-vk/bin` and `$WHISPER/models` precisely so `bench.sh` from the
rig runs inside it unmodified — a container benchmark written fresh would measure
something nobody has a baseline for.

It refuses to run above a 1-minute load average of 3.0. **Do not raise
`MAXLOAD` to get a number out of a busy box**: a CI runner at load 12.8 read
`large-v3` as 9.2× against 12.6× idle, a 27% error with nothing in the output to
suggest a problem. Wait for the box.

---

## Measured, in this container

Same box, same clip (`voice60`), same harness, decode counted, median of 3 after
a discarded warm-up, both halves started on an idle box (load 0.78 and 0.64).
CSVs in `results/`, each carrying the pin set that produced it.

**Per invocation — CHRN-24's gate.**

| model | in container | CHRN-12 reference | delta |
|---|---|---|---|
| `base.en` | 61.8× | 63.9× | −3.3% |
| **`small.en`** | **43.6×** (1377 ms) | **43.2×** (1390 ms) | **+0.9%** |
| `medium.en` | 25.7× | 25.6× | +0.4% |
| `large-v3` | 12.6× | 12.6× | 0.0% |

**Model resident — recorded, not gated on.** CHRN-26 is what makes this the
operative column; it is measured here so that ticket starts from a number taken
through this image rather than a host-native one.

| model | in container | CHRN-12 reference | delta |
|---|---|---|---|
| `base.en` | 76.7× | 76.4× | +0.4% |
| `small.en` | 57.9× | 59.6× | −2.9% |
| `medium.en` | 35.9× | 36.8× | −2.4% |
| `large-v3` | 18.3× | 18.3× | 0.0% |

Worst cell 3.3%, four of eight inside 1%, two landing on the reference exactly,
and the deviations run in both directions. **Containerising this build costs
nothing measurable** — a container that came out uniformly *faster* would have
meant the two runs were not measuring the same work.

The decode is the one systematic difference: 154 ms here against 149 ms
host-native, which is the ffmpeg build differing. The number that makes it
ignorable is the **difference**, not the share — **5 ms on the gated row, 0.36%
of its 1377 ms total**. The share is no use as a single figure because it moves
by an order of magnitude across the table: decode is 11.2% of `small.en` per
invocation and 3.2% of `large-v3`.

### One measurement was thrown away, and why it is worth knowing

An earlier sweep read `large-v3` at **11.5×** per invocation — 8.7% under
reference, while every other cell sat inside 3%. It was not a real result. The
harness's load guard is a **start-of-run** check: it refuses to begin on a busy
box and then cannot see load arriving during the minutes that follow. Models run
in order, so late load lands on the last rows, and `large-v3` is last.

Re-run from idle, it reads 12.6× — the reference figure exactly. That is why the
CSVs now record load at **both ends** of each run rather than one. Read the end
figure with its caveat, though: the sweep drives load itself, so a rise from idle
is partly self-inflicted and is a flag to check a suspect row against the
reference, not a verdict on one.

---

## What is deliberately not here

- **No HTTP surface, no job table, no queue.** CHRN-25.
- **No resident worker and no GPU lease.** CHRN-26. The image runs `whisper-cli`
  once per invocation, which is why CHRN-24's gate is the per-invocation column.
- **No GHCR publish.** The image name and its release workflow belong with the
  service, and that is CHRN-25/CHRN-29 territory.
- **No HIP build.** IDEA-26 measured it slower in every cell, and a second
  backend in the image is a second thing that can be selected by accident.
