# ASR model choice — measured, not extrapolated

**CHRN-12 · measured 2026-08-22 · supersedes the design canvas.**

The canvas asserts `whisper.cpp · large-v3 · 6.4× realtime`. Nobody had measured it. IDEA-26 (R3)
measured the models below `large-v3` but never `large-v3` itself, so 6.4× was an extrapolation
that every queue-depth and offline-UX decision downstream was resting on.

It has now been measured on the R9700, and so has everything else, because getting the number
required fixing the harness first — see [§ Why the R3 numbers moved](#why-the-r3-numbers-moved).

---

## The numbers

Vulkan/RADV on `gfx1201`, 60 s Opus voice note, ffmpeg decode counted (149 ms), median of 3 after
a discarded warm-up, on an idle box. Full methodology and the raw CSVs live with the rig at
`~/projects/catenary/spike/r3-whisper/` (`FINDINGS.md` § 8).

**Model resident — the number E3 should size against,** because the ASR service holds the model
between clips rather than re-loading per file:

| model | 60 s note takes | × realtime |
|---|---|---|
| `base.en` | 0.79 s | **76.4×** |
| **`small.en`** | **1.01 s** | **59.6×** |
| `medium.en` | 1.63 s | **36.8×** |
| `large-v3` | 3.28 s | **18.3×** |

**Per invocation**, if anything ever shells out once per clip instead:

| model | 60 s note takes | × realtime |
|---|---|---|
| `base.en` | 0.94 s | 63.9× |
| `small.en` | 1.39 s | 43.2× |
| `medium.en` | 2.34 s | 25.6× |
| `large-v3` | 4.77 s | 12.6× |

**CPU, no GPU available:** `base.en` 4.2 s (14.3×), `small.en` 12.1 s (5.0×).

---

## What E3 should do

**Default to `small.en`.** 1.0 s for a 60 s note. This is R3's recommendation and it survives
re-measurement — but note that the *reason* has changed: it is no longer "small.en is nearly free
next to base.en" (the gap is 30%, not 5%), it is that nothing above it earns its cost.

**Do not default to `large-v3`, and drop it from the canvas.** It costs **3.3×** `small.en`
resident. The ticket anticipated a 10× penalty, so on latency alone it is more affordable than
feared — but it buys nothing measurable. On the one hard clip available, across four decodes per
model, `large-v3` was the *least* consistent on proper nouns and produced the worst single miss
(`Frédéric` for `Fradique` — a French substitution, plausibly its multilingual training asserting
itself on a Portuguese name). The `.en` models are tuned for English speech; `large-v3` is not.

**`medium.en` is the upgrade path, not `large-v3`.** 1.63 s / 36.8× resident, consistent output,
and it is an `.en` model. If transcript quality ever disappoints, go here.

**CPU fallback is `base.en`,** at 14.3×. Do not fall back to the same model more slowly — the
CPU path collapses above `small.en`: 5.0× for `small.en`, **1.4×** for `medium.en`, and **0.6×**
for `large-v3`, which is 101 seconds to transcribe a 60-second clip. Above `small.en` the CPU is
not a degraded fallback, it stops being a fallback.

**Keep the model resident.** It is worth 45% of `large-v3`'s wall clock and 16% of `base.en`'s;
model load alone is 1.9 s for `large-v3`'s 3.1 GB against 145 ms for `base.en`. This is an argument
for the shared ASR service being a long-lived process, which is the shape CHRN E3 already
assumes.

**Record the model on the transcript.** `engine: "whisper.cpp/small.en"` — the model is a live
knob and a transcript should say what produced it.

### Sizing, concretely

At 59.6× a day's memos transcribe while you put the phone down: **twenty 3-minute notes take
about a minute of GPU**. The ticket framed the risk as "at 59× a day's memos transcribe while you
put the phone down, at 6× they do not" — even `large-v3`, the slowest option measured, clears that
bar at 18.3×. **Transcription is not a latency risk at any model on this list.** The queue exists
for isolation and retry, not because the work is slow.

---

## Backend: Vulkan, but the margin is not what E3 was told

R3 concluded Vulkan over ROCm/HIP and that conclusion holds — **Vulkan is ahead in every cell of
the matrix.** What changed is the size of the lead, and it matters because the figure E3 inherited
was measured in a shape E3 does not use.

| model | Vulkan resident | HIP resident | Vulkan's lead |
|---|---|---|---|
| `base.en` | 76.4× | 36.7× | 2.08× |
| `small.en` | 59.6× | 30.6× | 1.95× |
| `medium.en` | 36.8× | 20.2× | 1.82× |
| `large-v3` | 18.3× | 15.2× | **1.20×** |

Most of HIP's famous 4.8× deficit was a **fixed ~3.6 s per-process rocBLAS init tax**. A resident
worker pays that once at startup instead of once per clip, so the gap collapses to 1.2×–2.1× — and
narrows as the model grows, because the fixed cost is amortised against more compute.

**Practical consequence:** "4.8× faster than HIP" is a per-invocation number. Do not quote it at a
service that holds its model resident; the honest figure there is about **2×** on `small.en`.
Still take Vulkan — 2× is 2×, and it needs no ROCm at runtime — but the backend is no longer the
dominant lever it looked like. Keeping the model resident is worth more than the backend choice is
for the larger models.

One row survives correct measurement unchanged, and it is still the strangest in the set: **on
`base.en`, the CPU (14.3×) beats HIP (13.4×).**

---

## The quality claim is thin, deliberately

One 60 s clip of encyclopedic read-aloud speech, four decodes per model, scored by eye on a handful
of proper nouns. That is enough to **refuse** an unjustified 3.3× cost. It is not a quality
evaluation and must not be cited as one.

E4 already calls for a labelled eval set for routing accuracy. Transcription quality wants the same
treatment, on **actual voice notes** rather than Spoken Wikipedia — a memo from someone you know is
much easier material than an encyclopedia article, so these results probably understate every
model. If that set ever shows `small.en` dropping content on real memos, `medium.en` is 0.6 s away.

---

## Why the R3 numbers moved

If you remember `small.en` at 59.6× per invocation from IDEA-26 and this page says 43.2×, this is
why.

R3's harness timed `whisper-cli -np -nt`. **`-nt` / `--no-timestamps` is not a formatting flag** —
it suppresses timestamp tokens during decoding, which changes what the model emits. Two effects:

- **Everything looks faster.** Fewer tokens to emit: `base.en` understated 38%, `small.en` 44%,
  `medium.en` 20%, `large-v3` **71%**.
- **Audio can go missing.** On the R3 test clip, `-nt` costs `small.en` **45% of its transcript** —
  the whole middle of the recording. On the second clip the same flag costs nothing. Real, but
  clip-dependent and unpredictable.

It is a decode change and not a printing bug: the `-nt` output is not a subset of the timestamped
output — it capitalises differently, so different tokens were generated.

Worth internalising as a pattern rather than a footnote, because it is the second time this rig has
produced a confidently wrong number: **exit 0, plausible transcript, faster than expected, and
entirely wrong.** R3's first pass recommended ROCm/HIP the same way. Neither failure looked like a
failure. Anything Chronicle builds on top of an ASR benchmark should re-check the flags before
trusting the figure.

The harness now leaves timestamps on and strips them afterwards, and refuses to run when the box is
busy — a CI runner at load 12.8 read `large-v3` as 9.2× against 12.6× idle, a 27% error with
nothing in the output to indicate a problem.
