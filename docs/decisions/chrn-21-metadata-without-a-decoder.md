# CHRN-21 — audio metadata without a decoder

**Status:** implemented · **Decided by:** magos, 2026-08-27 · **Tier:** `haiku`

CHRN-21 is tier `haiku`, so Mode A. This document exists because the ticket was
**stopped and asked** rather than built: its original scope needed a ~100 MB
non-Go dependency, and `CLAUDE.md`'s working agreement makes that a stop rather
than an implementation detail whatever the tier says. §5 is what E3 inherits.

---

## 1 · What was asked, and why it stopped

CHRN-21 was *"Audio normalisation: Opus to 16 kHz mono WAV, plus duration and
codec metadata."* The E2 run order carried a hypothesis that it would collapse —
CHRN-12 measured a 152 ms decode *inside* the transcription budget, which
suggests the shared ASR service already decodes.

**That reading did not survive contact with the source.** Three facts, checked
rather than inferred:

1. **whisper.cpp does not accept Opus.** The benchmark harness says so in its own
   docstring and its code is unambiguous — `bench.sh:88` runs
   `ffmpeg -ac 1 -ar 16000 -c:a pcm_s16le`.
2. **The 152 ms was the harness's own ffmpeg call**, not a service's. The tell is
   that `decode ms` is *identical in every row* of CHRN-12's matrix — across
   vulkan/hip/cpu and all four models. One fixed CPU-side step.
3. **There was no ASR service to accept anything.** CHRN-3 is Backlog; it *is*
   the epic that builds the runner. The question had no subject.

So a decoder was genuinely needed somewhere, and ffmpeg is ~100 MB against a
file whose first line is *"Single static Go binary"*.

## 2 · Four options, and the one taken

| | |
|---|---|
| 1 · `apk add ffmpeg` in the image | ~100 MB onto `alpine:3.20 + ca-certificates`; the first line of `CLAUDE.md` stops being true |
| 2 · sidecar container | keeps the binary static; adds a service and a contract to a system whose point was fewer moving parts |
| 3 · shell out to the host's ffmpeg | needs a bind mount; the deploy stops being reproducible from the image |
| **4 · decode moves to E3** | **taken** |

**Option 4 was taken on 2026-08-27.** Chronicle ships the metadata parse and
holds no decoder at all; the decode becomes part of CHRN-3's job contract, which
now states that it accepts Opus.

### Why E3 and not here

CHRN-3 already makes this argument for itself, about the inference:

> Building the runner once with a client contract costs very little more than
> building it inside Chronicle, and it is the only version of this that Catenary
> can adopt without a refactor.

It applies to the decode unchanged. **Catenary is the second client and its
voice messages are not WAV either.** Decode in Chronicle means decode in Catenary
too — twice — to feed one GPU service that could have accepted compressed audio
once. And that service is already a long-lived process holding a resident model,
which is where a decode step costs least.

### The independent second argument, which is the one that settles it

Supplied by CHRN-23's review rather than by this ticket, and it holds even if the
dependency question went the other way:

`byte_size` is immutable (`CH002`) and CHRN-23's layout gives a memo exactly one
path. **So if Chronicle ever rewrites audio in place, every successfully
normalised memo becomes a permanent `mismatched`** — the reconciliation state
meaning *"something corrupted your audio"* becomes the steady state, and the
three-way split that exists to make that alarming stops working.

Of the exits — a second column for the normalised size, excluding normalised
memos from the comparison, or not decoding here at all — only the third leaves
the design intact. Options 1–3 all require picking one of the first two.

## 3 · What is left needs no decoder, and beats the obvious tool

Opus counts granule positions in samples at a fixed 48 kHz **regardless of the
source rate**, so the duration is arithmetic over two numbers already in the
file: the final page's granule, and the pre-skip in `OpusHead`.

```
duration = (final_granule − pre_skip) / 48000
```

The pre-skip is encoder delay — samples the decoder emits before the first real
one. Subtracting it is the entire difference between this and `ffprobe`, which
divides the granule and stops. Measured across seven files:

| file | `Probe` | `ffprobe` | delta |
|---|---|---|---|
| `gen_0.25s` | **250 ms** | 256.5 ms | +6.5 |
| `gen_3.5s` | **3500 ms** | 3506.5 ms | +6.5 |
| `gen_12.0s` | **12000 ms** | 12006.5 ms | +6.5 |
| `gen_37.5s` | **37500 ms** | 37506.5 ms | +6.5 |
| `jfk60` | **60000 ms** | 60006.5 ms | +6.5 |

**Exactly +6.5 ms every time** — 312 samples at 48 kHz, which is what libopus
writes. The generated files hit their requested duration to the millisecond, so
the error against the ticket's one-frame (20 ms) tolerance is **zero**, and
`ffprobe` is the one that is out.

### One honest caveat, found by measuring

`OpusHead`'s input sample rate is **not** "what this was recorded at", whatever
RFC 7845 intends. An encoder writes the rate it was *fed*, and encoders that
resample first record the post-resample rate: ffmpeg's libopus writes **48000
for every file**, including sources generated at 8 kHz, 16 kHz and 44.1 kHz.

So a corpus of `sample_rate_hz = 48000` is the expected reading, not a bug. It is
recorded because it is what the file says, not because it is informative — and
the comment on the field says exactly that, so nobody investigates it later as a
defect.

## 4 · A probe failure never rejects a memo

The `Done when` says a corrupt file must *"fail loudly rather than producing
silence"*. **Loudly means visible, not fatal**, and the difference matters
enough to be a design rule rather than an implementation choice.

By the time the probe runs the bytes are durable and the memo exists. A file
whose headers cannot be read is **undescribed, not broken** — the three columns
stay NULL, a warning is logged, and the memo stands. Refusing a recording because
Chronicle could not parse its container would be Chronicle deciding somebody's
memo does not count, which is a far worse failure than an unknown duration.

`ErrNotOpus` is kept distinct from a corruption error for the same reason:
"somebody uploaded an m4a" and "this Opus file is damaged" are different facts
and only the second is alarming.

**The retry story is free.** Both call sites guard on `DurationMS == nil`, so a
described memo is never re-probed, and a memo whose probe *failed* gets another
attempt the next time its bytes arrive by either path. One guard buys
idempotence and retry together.

## 5 · What E3 inherits

Recorded on CHRN-3 as well, because it is a change to that epic's scope made
from outside it.

- **The job contract takes compressed audio.** A client submits the bytes as
  recorded; Opus-in-Ogg is what Chronicle will send. Chronicle has no decoder and
  will not grow one.
- **The decoded WAV is derived and disposable**, and belongs to the ASR service
  rather than beside the authored bytes in Chronicle's tier-2 store.
- **The numbers in CHRN-3 do not change.** The 152 ms is already inside every
  figure in that table; it just now happens on the service's side of the contract
  instead of in a client.
- **Do not decode in place, anywhere.** §2's second argument is not specific to
  Chronicle: any design where a decoded artefact replaces the original inherits
  the same problem.

## 6 · Implementation notes worth keeping

- **Two small reads, not a pass.** The header is at the start and the granule is
  at the end, so a 40-minute memo costs one read at each end. That matters
  because this runs on the ingest path, immediately after the bytes were written
  once already.
- **A granule of −1 is legal** — it means no packet completes on that page — so
  the backwards scan walks past it rather than taking the last `OggS` it finds.
  `TestProbeWalksPastPagesWithNoGranule` pins it.
- **`OggS` is required at offset zero**, not searched for. Scanning deeper would
  accept a file with arbitrary junk in front of a valid stream.
- **The unit tests build their own Ogg pages** rather than using a checked-in
  fixture: a granule of −1, a truncated tail and a bad `OpusHead` version cannot
  be produced by an encoder on demand, and a binary blob would be unreviewable.
  `TestProbeAgainstRealOpusFiles` covers what a real encoder emits and skips
  without `CHRONICLE_TEST_OPUS_DIR`, the way the database suites skip without a
  DSN.
- **No migration.** `duration_ms`, `codec` and `sample_rate_hz` have existed and
  been nullable since 0003, which is why this ticket blocked nothing and why
  CHRN-19 and CHRN-20 shipped ahead of it.
