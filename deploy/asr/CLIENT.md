# The estate ASR service — a client's guide

**CHRN-29.** This document is for **client two**. Chronicle is client one and it
is already on this service; if you are writing Catenary's transcription, this is
what you build against instead of a second whisper queue.

The machine-readable contract is `deploy/asr/openapi.yaml` and it is the source
of truth — generate from it rather than hand-writing a client. What is here is
everything the spec cannot tell you: the numbers, what the queue does when both
clients are busy, and the three things that will surprise you.

**The whole argument for this service is that you adopt it.** Two services
queueing work to one GPU with two independent schedulers is a resource
contention bug, and the second one written is the one that discovers it — in
production, as timeouts. If E8 builds its own queue, this service is a Chronicle
package with extra ceremony.

---

## The shape

```
POST /v1/jobs                submit audio, multipart, Idempotency-Key REQUIRED
GET  /v1/jobs/{id}           status; cheap, safe to poll; carries retry_after_ms
GET  /v1/jobs/{id}/result    the transcript; 410 once the payload ages out
POST /v1/jobs/{id}/cancel    idempotent
GET  /v1/models              what this deployment runs, and the audio it accepts
```

`Authorization: Bearer <token>`, on everything but the two probes. **Your
`client_id` is derived from that token and from nothing else** — there is no
field to set it, and that is deliberate: fairness is per client, so a
client-asserted identity would let either service jump the other's queue.

**Submit compressed audio as recorded.** Do not decode first. The service runs
ffmpeg in front of the model, because whisper.cpp does not read Opus and doing
the decode once here beats doing it in every client. `GET /v1/models` lists the
media types; today that is Ogg, WebM, MPEG, MP4 and WAV.

---

## The three things that will surprise you

**1 · `Idempotency-Key` is required and it names an ATTEMPT, not a memo.** Mint
it and persist it *before* you send the request. A retry of one attempt reuses
one key and is a replay — 200 with the existing job, nothing queued twice. A
*different* attempt at the same audio is a fresh key and a second job, which is
the correct answer because you asked for a different thing. Same key with
different audio or a different model is a **409**, and your move is a new key.

**2 · An empty transcript is a success, not a failure.** Forty seconds of
silence has a true and complete answer, and the answer is "no speech". A result
with `"text": ""`, `"segments": []` and `"partial": false` is durable. If you
gate anything on non-empty text you will keep audio forever for exactly the
recordings least worth keeping.

**3 · The result expires and the job does not.** Result payloads are purged
after seven days; the job row survives. So a late fetch answers **410 Gone**,
which means *"the answer aged out"* and never *"transcription failed"*. Collect
results promptly, and treat 410 as "re-submit" rather than "this memo is
broken".

---

## The numbers, and what they were measured with

All on the R9700, in-container, on a 60-second voice note with the Opus decode
counted. `deploy/asr/README.md` carries the full tables and the pins.

| model | resident, in container |
|---|---|
| `base.en` | 76.7× realtime |
| **`small.en`** (default) | **57.9×** |
| `medium.en` | 35.9× |
| `large-v3` | 18.3× |

Through `asrd` end to end, `small.en` measures **56.3×** — the gap is a database
round trip and an HTTP upload around an identical inference.

**Every figure above was taken under beam search** (`-bs 5 -bo 5`). If you
benchmark this service yourself, say which decode you used or the number means
nothing: `whisper-server`'s own defaults are greedy, which is *faster* and a
different transcript.

**`small.en` is the default and `medium.en` is the upgrade path — not
`large-v3`.** CHRN-12 measured `large-v3` at 3.3× the cost with no quality gain
to show for it, and it was the least consistent of the four on proper nouns.
Transcription is not a latency risk at any model on that list; even the slowest
clears 18× realtime. The queue exists for isolation and for serialising the GPU,
not because the work is slow.

---

## What happens when we are both busy

**One inference runs on the card at a time.** That is the guarantee this service
exists to make, and it is enforced inside the process that owns the device.

**Queueing is round-robin by client, not one global FIFO.** The claim takes the
oldest queued job of the client *least recently served*. So a backfill of eight
hundred voice messages from you cannot put a Chronicle memo behind it, and a
Chronicle burst cannot bury your backfill.

**The honest limitation, because it is otherwise discovered as a bug report:
round-robin gives the quiet client HALF the device, not priority.** A memo
submitted while your backfill is running waits for at most one of your jobs —
about a second at `small.en`.

**Under mixed models it is longer, and this is the number to plan against.** The
service holds one model resident and switches only when the queue holds nothing
for it, bounded by `ASR_MODEL_SWITCH_MAX_WAIT` — **60 seconds** on this
deployment. So if your backfill names a different model from the other client's
work, their wait is *that bound plus one job*, not the one-second figure. **Plan
against 60 s, not 1 s**, and prefer the resident model for bulk work if you care
about the other client's latency.

**Ollama also lives on this GPU and this service does not arbitrate it.** It is
a separate container reached directly, so it cannot be admitted here. Contention
with it is a slowdown and never a failure — you will see inference times drift,
not errors — and the service logs a warning naming it as the likely cause when a
job runs past twice its expected rate.

---

## What a failure means

Jobs are retried inside the service: a worker that crashes or wedges releases
its job and it is claimed again, bounded by a retry ceiling. **You do not need
to implement that.** What reaches you:

- **`failed` with a code.** Branch on the code, not the message. `decode_failed`
  means the audio did not decode and will not next time. `retries_exhausted`
  means the service gave up — starting a fresh job runs the same file into the
  same wall with a new counter.
- **`partial: true`.** The run did not complete. Keep the text if it is useful,
  and do not treat it as the final answer.
- **`cancelling`** is a wire status you will see between your cancel and the
  job stopping. It is derived, not stored.

**Nothing here is the only copy of anything.** Every job row and every byte in
this service is regenerable from audio you still hold, and submitted audio is
deleted the moment a job reaches a terminal state. If this database were
dropped, the estate would lose queue position and nothing else. Hold your own
audio accordingly — this service is not storage.

---

## Getting a credential

`ASR_CLIENT_TOKENS` is `name:token` pairs out of Signet, set on the service. Ask
magos for one; the name you are given is your `client_id` and it is what
fairness is computed against. There is no anonymous mode and no self-service:
an unauthenticated transcription service is one any container on `construct_net`
can queue four gigabytes of audio into.
