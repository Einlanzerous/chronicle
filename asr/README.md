# Shared estate ASR — whisper.cpp on the R9700, and the service over it

**CHRN-24 + CHRN-25 + CHRN-26 · E3.** One whisper.cpp build, on one GPU, behind
one image. Chronicle is client one; Catenary is client two and its handoff is
CHRN-29.

Three things live here, and it is worth keeping them apart:

- **The runner (CHRN-24)** — a pinned, reproducible container that transcribes a
  file on the R9700 and hits a known number. Everything from here to *Measured,
  in this container* is about that, and about why each pin is load-bearing.
- **The service (CHRN-25)** — `asrd`, the job contract over it: submit, poll,
  fetch, cancel, with a job table and a lease. See **The service** below.
  **Writing a client? Read `CLIENT.md`**, which is the same service from the
  outside: the numbers, what the queue does when two clients are busy, and the
  three things that surprise people.
- **The resident worker (CHRN-26)** — one `whisper-server` process holding the
  model, a single-flight GPU lease, and round-robin fairness between clients.
  See **The resident worker** below.

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
docker build -f asr/Dockerfile -t estate-asr:dev .
```

**From the repository root**, not from `asr/` — it was the latter until
CHRN-25 put `asrd` in this image. `asrd` is a Go binary in this repository, and
a build context that cannot see `go.mod` cannot build it. The whisper.cpp
stages read nothing from the context either way, and the `asrd` stage copies
**only `go.mod`, `go.sum` and `asr/`** — which is the subtree boundary from
CHRN-82 §2 enforced for free: anything under `asr/` that imported a package
outside it would not be in the context, and the build would fail here.

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
OUT_DIR=asr/bench/results ./asr/bench/bench-in-container.sh
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

**Model resident — and since CHRN-26 this is the operative column.** The
service holds the model across jobs, so these are the numbers a memo is
transcribed at. Measured through this image rather than host-native, which is
what makes them comparable to what `asrd` actually does.

**Every figure in both tables was taken under BEAM SEARCH** — `whisper-cli`
defaults to `--beam-size 5 --best-of 5` and `bench.sh` passes neither, so that
is what the harness measured. `whisper-server` does **not**: it defaults to
`-bs -1` (greedy) and `-bo 2`, which is *faster* and a different transcript.
`asrd` therefore passes `-bs 5 -bo 5` explicitly. **A number compared against
this table must name the decode it was taken with.**

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

## The service — `asrd` (CHRN-25)

The image carries a second binary: `asrd`, the transcription job service. The
contract it implements is `asr/openapi.yaml`, and the argument behind
every shape in it is `docs/decisions/chrn-25-job-contract.md`.

It is **in this image** because it shells out to `ffmpeg` and supervises
`whisper-server` as siblings. The alternative — a separate container running `docker run` against
this one — needs the daemon socket mounted, which hands a network service the
ability to start anything on the host.

```
POST /v1/jobs                submit audio inline, multipart, Idempotency-Key required
GET  /v1/jobs/{id}           status, cheap, safe to poll; carries retry_after_ms
GET  /v1/jobs/{id}/result    the transcript, fetched once; 410 once the payload ages out
POST /v1/jobs/{id}/cancel    idempotent
GET  /v1/models              what this deployment runs, and the audio it accepts
GET  /healthz  /readyz       open; /readyz pings the database and reports the queue and the device
```

Its own database and its own role, provisioned separately:

```bash
signet exec --secret construct-server/ASR_DB_PASSWORD -- asr/deploy/provision-db.sh
```

Not a schema inside Chronicle's, and this is the call worth understanding rather
than copying: the moment Catenary submits a job it would need a credential on
**Chronicle's** database, and the reason E3 is an estate service rather than a
Chronicle package collapses into Catenary depending on Chronicle's schema. It is
also **not** a tier-1/tier-2 question — those govern Chronicle's two stores, and
job rows are a third thing.

**Nothing this service holds is irreplaceable.** The audio still exists on the
client side, so every row and every byte can be recomputed: drop the `asr`
database and the estate loses queue position and nothing else. The corollary a
reviewer should check on any change here is that nothing in it has become the
only copy of anything.

### The resident worker (CHRN-26)

`asrd serve` supervises **one `whisper-server` process** from the same pinned
tree, bound to loopback, holding the model across jobs. The argument for every
choice below is `docs/decisions/chrn-26-resident-worker.md`; what follows is
what it concluded.

**Why residency is worth having** is not the 28% it saves on a 60-second memo.
R3 isolated a **fixed 388 ms per process** on Vulkan, so the tax is a constant
rather than a share: a 5-second voice note is **5.6× slower** per-invocation, a
40-minute one 1.01×. It dominates exactly the memos there are most of.

**Three different things are called a lease here**, and they are worth keeping
apart:

| | says | held for |
|---|---|---|
| the **job lease** (CHRN-25) | this worker owns this job | the whole job, decode included — a timestamp, so it depends on no connection |
| the **device lock** | this process owns the card | the process's lifetime — a Postgres advisory lock on `hash(ASR_DEVICE_ID)`, on a connection of its own |
| the **GPU semaphore** | an inference is running now | the inference only — so ffmpeg is not serialised behind the device |

A process that cannot take the device lock is a **standby**: it serves the API,
claims no jobs, loads no model and holds no VRAM, and `/readyz` says `standby`
rather than looking broken. That is what makes a rolling redeploy safe — the
lock goes with its connection, so the departing process releases it by dying,
with nothing to run in it.

**What it does NOT guarantee is the device.** Ollama also lives on the R9700 and
is reached directly by whoever needs a completion, so `asrd` cannot admit it.
The claim this service makes is **single-flight transcription**; contention with
Ollama is real, is a slowdown rather than a failure, and is made visible instead
of arbitrated — a job running past **2× its expected rate** logs a warning
naming contention as the likely cause.

**Fairness is round-robin by client**, which is a promise to client two: the
claim takes the oldest queued job of the client least recently served, so a
Catenary backfill of eight hundred voice messages cannot put a Chronicle memo
behind it. The honest limitation, because it is otherwise discovered as a bug:
that is **half the device under contention, not priority**. A memo waits for at
most one backfill job — about a second at `small.en`, and
`ASR_MODEL_SWITCH_MAX_WAIT` **plus** one job if the backfill names a different
model.

**One model is resident at a time, and it drains before it switches.** A job for
another model waits until the queue holds nothing for the resident one, or until
it has waited `ASR_MODEL_SWITCH_MAX_WAIT`, whichever comes first. A model that
fails to initialise takes the child process with it — `/load` frees the old
model before loading the new one and `exit(1)`s if that fails, which is
upstream's own TODO — so the supervisor restarts on the **last known good**
model and refuses the one that failed. That is the difference between a service
and a restart loop.

**Every job carries an inference deadline**, `max(30s, 5 × audio_ms ÷ expected
rate)`. It is the only thing that can see a **hung** child, as opposed to a
crashed one: a `whisper-server` that stops answering without exiting leaves
every lease reporting healthy while nothing moves, forever. On breach the child
is killed and the job released.

**Each of the three blocking calls on the job path has a wall clock**, for the
same reason: the inference above, a fixed 60 s on a model load, and a fixed
5 minutes on the decode — the last of which is fifty times what a forty-minute
memo takes. None of them is a performance budget. They are the difference
between a hang and forever, and a wedged ffmpeg is the quietest of the three
because the resident process stays up and healthy throughout it.

**A dying child releases its job rather than failing it.** One crash must not
permanently fail a memo that nothing was wrong with, so `attempts` goes up and
the job returns to the queue — bounded by CHRN-28's retry ceiling, which is
load-bearing rather than a nicety: without it the requeue path is an unmetered
loop. `Release` records **why** the job came back, and the ceiling prices the
reasons apart — five ordinary attempts, two for a job killed by a deadline,
because that one spent five times its expected run getting nowhere. At the
ceiling the job is dead-lettered to `failed` with `retries_exhausted`, logged
at ERROR, and nothing picks it up again.

**Cancelling actually stops the work.** Dropping the HTTP request would not:
`/inference` holds the server's mutex for the whole synchronous call, so it
would run to completion holding the device with the queue blocked behind it. The
child is killed and restarted, which costs about 2.3 s.

### Measured, through `asrd`

Taken on an idle box — 1-minute load average **0.63 at the start and 0.69 at
the end**, against the harness's own refusal threshold of 3.0. The first run is
discarded and the figure is the median of the three after it.

| model | resident worker, through `asrd` | CHRN-24 resident column | delta |
|---|---|---|---|
| **`small.en`** | **56.3×** (1.066 s) | 57.9× (1.036 s) | **−2.8%** |

Runs: 1.967 s (discarded), then 1.204 / 1.066 / 1.011 s on `voice60`, 60.01 s of
audio. Decoded with **`-bs 5 -bo 5 -nlp`** and `response_format=verbose_json`,
which is what makes the comparison mean anything — at the server's own defaults
the decode is greedy, which is *faster*, so an unpinned run would beat this
table for a reason that is not residency.

**What the 1.066 s covers**, because a throughput number without its boundaries
is the drift this file exists to prevent: the ffmpeg decode (as the reference
column also counts it), the wait for the device, the `leased`→`running` write,
the multipart upload of the decoded WAV to the child over loopback, the
inference, and the parse. It does not count the submit, the result write, or
the poll. It is the worker's own measurement of one job — the `took` field on
the `transcribed` log line — so it is the number an operator can reproduce from
the logs rather than one only a benchmark can see.

**2.8% behind a raw resident bench is the expected shape of answer**, not a
finding: the gap is a job service's overhead — a database round trip and an HTTP
upload — around an inference that is doing the same work. What the number rules
out is the failure this epic keeps meeting, where something is quietly off the
pace with nothing in the output to say so.

### Configuration

| variable | | |
|---|---|---|
| `ASR_DATABASE_URL` | required | boot fails if unset |
| `ASR_CLIENT_TOKENS` | required | `name:token` pairs out of Signet. Empty is a **boot error**, never "open" |
| `ASR_DEFAULT_MODEL` | `small.en` | CHRN-12's default, and its reasoning |
| `ASR_RESULT_TTL` | 7 days | matches `upload.DefaultTTL`; the **payload** ages out, the job row does not |
| `ASR_LEASE_TTL` | 30s | held for the whole job, decode included; renewed at a third of it. CHRN-26 §7 checked it against a 40-minute memo and left it alone |
| `ASR_MODEL_DIR` | `/opt/whisper/models` | absolute; the mount above |
| `ASR_BACKEND` | `vulkan` | recorded on every result, so a corpus does not vary invisibly |
| `ASR_WORKER` | true | off runs the HTTP surface with no worker: it serves submit and status, owns no device, and answers `/readyz` for its database alone |
| `ASR_DEVICE_ID` | `r9700` | what the advisory lock names and what lands in `leased_by`. **Per device, never per deployment** — a second card is a second value |
| `ASR_WHISPER_SERVER_BIN` | `whisper-server` | the supervised child, from the same pinned tree |
| `ASR_WHISPER_SERVER_ADDR` | `127.0.0.1:8081` | **loopback only.** It has no authentication of any kind; a listener on `construct_net` would make `ASR_CLIENT_TOKENS` decorative |
| `ASR_MODEL_SWITCH_MAX_WAIT` | 60s | how long a job for a non-resident model waits before forcing a switch — **and the fairness bound under mixed models** |
| `ASR_INFERENCE_DEADLINE_FACTOR` | 5 | multiplier on the expected inference time before a job is treated as wedged; floored at 30s |
| `ASR_MAX_ATTEMPTS` | 5 | how many times a job may lose its claim before it is **dead-lettered** to `failed` with `retries_exhausted` rather than requeued. Zero is refused, not read as "no ceiling" |
| `ASR_MAX_ATTEMPTS_WEDGED` | 2 | the lower ceiling for `inference_deadline` and `decode_deadline` — a job killed by a deadline spent five times its expected run getting nowhere, and the third attempt costs what the first two did |
| `ASR_EXPECTED_RATES` | CHRN-24's resident column | `model=realtime_x` pairs for **this worker's device**. An unnamed model uses 18.3×, the slowest measured, so it errs wide |

`client_id` comes from the **token** and from nothing else. It is never read
from a body, header or query parameter: CHRN-26 queues per client for fairness,
so a client-asserted identity would let either service submit as the other and
jump its queue.

---

## What is deliberately not here

- **No arbitration of the R9700 against Ollama.** CHRN-26 §3, at length.
  `asrd` admits one *transcription* at a time on the device it owns; Ollama is
  a separate container reached directly, and making it participate would mean
  an admission proxy with its own availability, or a lock every future caller
  has to remember. If it is ever wanted it is an estate admission service and
  its own ticket.
- **No second worker on a second device.** CHRN-80, deliberately standalone.
  The lock key, the fairness bookkeeping and the deadline rates are all
  per-device already, so what is missing is a worker-facing protocol — and the
  trust question that comes with it, since audio is tier-2 authored content and
  a worker outside the estate sees every recording it claims.
- **No callback or webhook.** Deliberately not now: it needs the service to hold
  its clients' credentials, which is the same objection that ruled out
  pull-by-URL for the audio.
- **No GHCR publish — yet.** The repository question that was tied to it is
  decided: `docs/decisions/chrn-82-asr-subtree-and-publish.md` keeps the
  service here, in this subtree, with its own release. The publish itself is
  that ticket's second PR.
- **No HIP build.** IDEA-26 measured it slower in every cell, and a second
  backend in the image is a second thing that can be selected by accident.
- **No generated `schema.sql` for the `asr` database.** Chronicle has one
  because the guard's whole point is that a generated artefact with no guard is
  one somebody hand-edits — and here there is no artefact to hand-edit. One
  table, one migration, and the migration is the shorter and more authoritative
  statement of it. Recorded so the absence reads as a decision rather than an
  omission.
