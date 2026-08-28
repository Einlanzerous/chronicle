# CHRN-25 — The transcription job contract (decision)

Status: **proposed — awaiting sign-off.**
Ticket: CHRN-25 (Phase P2, parent CHRN-3). Tier `opus`, so Mode B: this document
is the review artefact and the PR that follows it is mechanical.
Decision owner: magos.
Read by: **CHRN-26** (worker and lease), **CHRN-27** (Chronicle as client one),
**CHRN-28** (failure semantics), **CHRN-29** (Catenary handoff), and
**CHRN-22** (retention pruner, Mode C) for §5 — which is the predicate its whole
review turns on.

**Revised 2026-08-28 after review.** Four holes were found, all of them the kind
that would otherwise have been closed silently by a `sonnet`-tier implementer,
and one of them was in the predicate this ticket exists to settle. Changes are
marked **[rev]**; they close gaps rather than reverse positions.

## Context

This is the interface between two services, written before either client exists.
Catenary is client two, in a different language, by someone else, later — so
every shape here has to be right the first time or be wrong in two codebases.

CHRN-24 shipped the runner: a pinned container that transcribes a file on the
R9700 at a known rate, and decodes Opus, because the epic moved the decode here
on 2026-08-27. What it has no notion of is a job, a queue, a client, or a
result that outlives a process. That is this ticket.

`CLAUDE.md` names the failure this document exists to prevent, and names it with
this exact example:

> Discovering in a 900-line diff that the wrong idempotency key was chosen is the
> most expensive possible moment to find out; the decision costs one message to
> review before the code exists.

So §3 is the section to read hardest. §5 is the second, because CHRN-22 was moved
out of E2 specifically to wait for it.

## Decision

The ASR service is a separate service with **its own database and its own role**,
reached over HTTP at a versioned prefix, authenticated per client. A client
**submits audio inline** with a model and an `Idempotency-Key` it minted and
persisted before the request went out; **polls status** and **fetches the result
separately**; and may **cancel**. Job state lives in Postgres and a job leaves
the queue only by reaching a terminal state. The contract is described in
OpenAPI, the Go client is generated from it, and CI fails when the two disagree.

Nothing in this service is irreplaceable — every row and every byte it holds can
be recomputed from audio a client still has. That is what makes its whole store
safe to drop, and it is the tier invariant applied across a service boundary.

## 1 · Its own database and its own role, not a schema in Chronicle's

`CLAUDE.md` already states the estate pattern for Chronicle itself — *"Its own
database and its own role on the shared Postgres 16. Credentials live in Signet,
never in a compose file"* — and the ASR service is a service, so it gets the
same.

The alternative worth naming, because it is the convenient one: put the job table
in Chronicle's tier 1, next to `tier1.memo_uploads`. It is convenient for exactly
as long as Chronicle is the only client. The moment Catenary submits a job it
needs a credential on **Chronicle's** database, and the shared-service argument —
the whole reason E3 is an estate service rather than a Chronicle package —
collapses into Catenary depending on Chronicle's schema.

**This is not a tier-1/tier-2 question.** The tier split governs what lives in
*Chronicle's* two stores. Job rows are a third thing: another service's own
state, in another service's database, and the tier rule does not reach across
that boundary in either direction. Saying so explicitly because "the job table is
tier 1" is the plausible wrong summary of this section, and it would put the rows
in the wrong database.

Database `asr`, role `asr`, credentials in Signet. The provisioning cost is one
more run of the `deploy/provision-db.sh` pattern, which is already idempotent and
already keeps passwords out of compose files.

## 2 · Audio goes in the request body

Three ways the bytes could get here. Two of them are worse for reasons that only
show up later.

**A shared filesystem path** — the client writes a path, the service opens it.
Both processes are on the same box and Chronicle's audio is already on that NVMe,
so this is nearly free to build. It is also wrong three ways: it makes the ASR
service able to read an author's recording directory, which it has no business
in; it breaks the moment either service moves, is containerised differently, or
gets a second host; and it does not work for Catenary at all, whose voice
messages are not in Chronicle's layout. A contract whose transport is "we happen
to share a disk" is not a contract.

**Pull-by-URL** — the client hands over a URL and the service fetches it. This
inverts the credential direction: the ASR service becomes an authenticated HTTP
client of both its clients, so each client must now issue *it* a credential, and
a submit becomes a two-party handshake that can half-fail.

**Inline, and that is the decision.** The sizes make this unremarkable: the
corpus averages ~5 MB per memo (4.1 GB over 812), and a 40-minute memo is about
7 MB at the bitrate the benchmark clip actually uses — IDEA-26 records its
`voice60` as *"Opus 24 kbps mono (realistic voice note, 182 KB)"*, and the file
on the rig is 182,191 bytes for 60 s, so 24 kbps is measured rather than assumed.
The hop is a container network, not mobile data.

`POST /v1/jobs` is `multipart/form-data` with two parts — `spec`
(`application/json`) and `audio`. **Not** a JSON body with the audio
base64-encoded inside it: that costs a third of the bytes and forces both ends to
hold the whole thing in memory to parse one field, in a service whose entire job
is to stream large binaries.

**[rev] The audio part declares its own `Content-Type`**, and the service accepts
a documented set — `audio/ogg`, `audio/webm`, `audio/mpeg`, `audio/mp4`,
`audio/wav` — answering **415** otherwise, with the accepted set advertised
alongside `GET /v1/models`. The first draft pinned the part to
`application/ogg`, which is both the wrong media type for audio-only Ogg (RFC
5334 gives `audio/ogg`) and too narrow to survive client two: browsers produce
`audio/webm;codecs=opus` and iOS produces `audio/mp4`. ffmpeg is in the image and
reads all of them, so the narrowness bought nothing and would have been
discovered by Catenary.

**No resumable upload here, deliberately.** CHRN-20 built one for the
phone→Chronicle hop because that hop is a bad mobile link and a 40-minute memo
is a real upload. Chronicle→ASR is localhost over a docker network; a failed
submit is retried whole, which is what the idempotency key in §3 makes safe.

## 3 · The idempotency key, and the three keys it must not be

The header is `Idempotency-Key`, matching CHRN-18 and the Switchyard call
vox-dictate made. Same name, same namespace, one meaning across the estate.
Scoped `(client_id, key)`, **never expires**, mismatch answers **409** — CHRN-18's
shape, adopted rather than re-derived.

**The key is minted per transcription attempt and persisted by the client before
the request is sent.** That is the load-bearing sentence. It must be stable
across HTTP retries of one attempt — otherwise the failure this prevents is not
prevented — and fresh for a deliberate re-transcription. CHRN-18 makes the same
distinction for recordings: *"a fresh random UUIDv4 minted per recording"*, per
recording and not per HTTP call, persisted so a retry reuses it.

The failure it prevents, concretely: Chronicle submits, the process dies before
it records the returned job id, it comes back and retries. Without a stable key
that is a second job — the GPU transcribes the memo twice and Chronicle now has
two results for one memo and no way to say which is the transcript.

### The three tempting keys, and why each is wrong

| candidate | why it fails |
|---|---|
| **the audio content hash** | the same audio must be re-transcribable. CHRN-28 retries a failure on the same bytes, and `medium.en` is a live upgrade path the benchmark explicitly leaves open. A hash-keyed job either refuses the second run or, worse, replays the first model's transcript to a request that asked for a different model |
| **`(content_hash, model)`** | closer, and still wrong: it refuses a deliberate re-run at the *same* model, which is exactly CHRN-28's move after a partial. It also makes "transcribe this again, I think it was wrong" unexpressible |
| **the client's own row id** (`memo_id`) | **Catenary has no memos.** The contract must not know its clients' domain models. This is the one that would look completely fine in a Chronicle-only PR and be discovered by client two |

### What a replay and a mismatch do

- **Same key, same `spec`, same audio hash** → **200** with the existing job:
  same job id, current status. Not a new job, not an error.
- **Same key, different audio hash or different model** → **409**. The client
  mints a fresh key and retries, producing a second job — which is the correct
  outcome, because it asked for a different thing.

The audio hash is carried in `spec` so the mismatch check does not depend on
having buffered the body. A client that lies about it gets a job that transcribes
bytes the contract believes are something else, which is a client bug the service
cannot and should not defend against.

**[rev] The uniqueness is an index, and the write is insert-then-compare.**
`UNIQUE (client_id, key)`, and a submit inserts and handles the conflict rather
than checking first. CHRN-18's Done-when #3 exists because a check-then-insert
*"fails against a design that passes"* its plain concurrency test — the race is
between two retries of one attempt, which is precisely the situation this key is
for.

## 4 · The job state machine, and what survives `kill -9`

```
queued ──► leased ──► running ──► succeeded
   │          │          │      └► failed
   │          │          └────────► cancelled
   │          └───────────────────► queued        (lease expired, attempts += 1)
   └──────────────────────────────► cancelled
```

Six stored states. `leased` is separate from `running` because they answer
different questions: a worker has claimed the job, versus inference has actually
started. Collapsing them makes a worker that dies between claim and start
indistinguishable from one that died mid-inference, and those need different
`attempts` accounting. Cancellation is **not** a state — see §8.

**A job leaves the queue only by reaching a terminal state.** The ticket is
explicit about why: dropping is indistinguishable from a memo that was never
captured, which is the failure the whole system exists to avoid.

**`kill -9` survival is a lease expiry, not a shutdown hook.** The worker holds
`lease_expires_at` and renews it while it works; a reaper returns any job whose
lease has passed to `queued` and increments `attempts`. A hook that runs on
shutdown is not a mechanism — `kill -9` does not run it, and that is the case the
`Done when` names.

**Claiming is a compare-and-swap, and this is inherited rather than invented.**
CHRN-18's review found that removing the `from` predicate from `AdvanceMemoState`
let **six of six workers win the same claim**, measured. The same shape applies
here: claiming is `UPDATE ... WHERE id = $1 AND status = 'queued'` and a claim
that updates zero rows lost the race. Never read-then-write.

`attempts` is a column on this table. **How many retries, and with what backoff,
is CHRN-28's policy** — the counter lives here because the reaper increments it,
the ceiling does not.

### [rev] What CHRN-25 ships, because the `Done when` requires a runner

The first draft handed the worker wholly to CHRN-26 and then asked for `kill -9`
survival *"on the runner"* — with no runner to kill. The reaper was described and
assigned to nobody. That is a scope hole, not a wording one.

**CHRN-25 ships the reaper and a minimal per-invocation loop**: claim a job,
decode and transcribe through the CHRN-24 image, write the result. It is
deliberately the shape CHRN-12 measured as the slow one — 43.2× rather than
59.6× — because it is a placeholder that CHRN-26 replaces with the resident
worker, per-client fairness and the real lease.

Two reasons to prefer this over proving lease expiry against a synthetic row and
moving the `kill -9` clause to CHRN-26: the property under test is that **a
crashed process releases its work**, and a hand-expired lease tests the reaper
rather than the thing; and it gives CHRN-27 an end-to-end path to build against
instead of blocking it behind CHRN-26.

**The interim loop is single-flight by construction** — one worker process, one
job at a time. The epic's exit criterion is that the R9700 is never running two
inferences at once, and a placeholder is not exempt from it.

## 5 · The result, and the durable-transcript predicate CHRN-22 turns on

This section is the one CHRN-22 was moved out of E2 to wait for. Its move comment
is direct about the problem: *"building the pruner now means inventing the
predicate E3 will later have to match, in the one component whose bugs are
unrecoverable data loss."* Here is the predicate, so it is reviewed here rather
than inside a Mode C diff.

A result carries:

| field | |
|---|---|
| `status` | `succeeded` \| `failed` \| `cancelled` |
| `partial` | **boolean, set by the service about its own run** |
| `text` | the transcript, timestamps stripped |
| `segments[]` | `{start_ms, end_ms, text}` |
| `model`, `backend` | `whisper.cpp/small.en`, `vulkan` — what produced it |
| `audio_duration_ms` | as decoded |
| `covered_ms` | end of the last segment. **Evidence, not a predicate** — see below |
| `failure` | `{code, message}` when `status` is not `succeeded` |

`model` and `backend` are on the result and not merely in the service's logs
because CHRN-27 stores them with the transcript: *"a corpus transcribed by two
different models over time is one whose quality varies invisibly."*

### The predicate

> **A transcript is durable iff its job reached `succeeded` and `partial` is
> false.**

That is the whole of it, and it is deliberately not a calculation. `REVIEW.md`
§3 asks a reviewer to trace three cases; here is what each does.

| case | durable? | |
|---|---|---|
| transcription **failed** | **no** | no `succeeded`, so the audio is the only copy of that thought and stays |
| still **pending** | **no** | no terminal state at all |
| **succeeded but wrote nothing** | **yes** | and this is the one worth arguing |
| **partial** | **no** | CHRN-28: *"never let a partial satisfy the retention pruner's durable transcript gate"* |

**Empty text with a completed run is a durable transcript.** A memo that is forty
seconds of silence, or of traffic noise, has a true and complete answer and the
answer is "no speech". Treating it as not-durable means every such memo keeps its
audio forever — and the corpus accumulates exactly the recordings least worth
keeping, while the UI's `PRUNES 2026-09-20` label quietly becomes a lie for them.
The gate is *did we get a complete answer*, not *did we like it*.

### The trap: do not derive `partial` from the timestamps

`partial` is a fact the **service** records about whether its own run completed.
It is never inferred by a client from `covered_ms < audio_duration_ms`.

That inference looks obviously right and is wrong on ordinary memos: whisper
emits segments only where there is speech, so a memo with five seconds of
trailing silence has `covered_ms` short of the duration on a perfectly complete
run. A pruner using that as its gate would mark most of the corpus not-durable
and never fire. That is a safer failure than the other one — but it is still a
failure, it is silent, and it makes the retention promise in the UI false in the
direction nobody checks.

`covered_ms` is recorded because it is genuinely useful evidence when a transcript
looks short. It is not the gate.

### [rev] Where the pruner actually reads this

The predicate above is stated in the *service's* vocabulary, and **the pruner
cannot evaluate it there.** Job rows live in database `asr` (§1), result payloads
are purged at 7 days and the whole store is droppable (§9) — and CHRN-22 fires at
**30 days**, from inside Chronicle. At the moment the pruner runs, the job it
would need to consult is routinely gone. The first draft stated a predicate
against a store its only consumer cannot read.

So the fact is **projected** into Chronicle's own tier-2 transcript row when the
result is collected, and the pruner reads Chronicle and nothing else. CHRN-18
already framed the gate as a durable transcript read from E3's table; this names
that table as Chronicle-side.

Three requirements on **CHRN-27**, which is `sonnet`/Mode A and gets no decision
document of its own, so they bind from here:

1. **`tier2.transcripts` carries `partial`**, alongside `model` and `backend`.
2. **A row is written for every `succeeded` result, empty text included.** This is
   the one that will be got wrong. `if text == "" { return }` is an entirely
   innocent-looking line that silently inverts the ruling above — in the *safe*
   direction, so nothing ever complains — and the memos it strands are exactly
   the ones this section argued should prune.
3. **The gate is a Chronicle-side query**, never an HTTP call to the ASR service,
   whose answer at 30 days is a 410:
   `EXISTS (SELECT 1 FROM tier2.transcripts WHERE memo_id = $1 AND NOT partial)`.

## 6 · Identity comes from the credential, never from a field

Each client holds a bearer token issued out of Signet. **`client_id` is derived
from the token** and is never read from a request body, header or query
parameter.

This is CHRN-75's principle one layer down — *"trust a signal only Traefik can
produce"* — and it is not theoretical here, because CHRN-26 queues **per client**
for fairness. A client-asserted `client_id` would let either service submit as
the other and jump its queue, and the symptom would be Catenary's backlog
starving a memo someone is waiting on: the exact behaviour the lease exists to
prevent, reachable by setting a string.

## 7 · Poll and result are separate calls

- `GET /v1/jobs/{id}` — status, cheap, safe to poll.
- `GET /v1/jobs/{id}/result` — the transcript, fetched once.

A 40-minute transcript is not something to re-send every two seconds, and the
ticket gives the other reason: a client can crash and come back, and the result
must still be there to collect.

**The status response carries `retry_after_ms`.** The server sets poll pressure,
not the client, so a deep queue can back clients off without a contract change.
At 59.6× resident a three-minute memo is about three seconds of GPU, so the
honest default is short — but a fixed client-side interval is the thing that
becomes wrong the first time the queue is long.

## 8 · Cancel is a POST, and cancellation is a column [rev]

`cancel_requested_at` is a **column**, not a state. The first draft made
`cancelling` a state, which left `leased` with no cancel edge and left one
interaction undefined: a `running` job that was cancelled and whose worker then
died would be reaped back to `queued` and **run again** — the one outcome cancel
exists to prevent.

- `queued` / `leased` → `cancelled` immediately, by CAS.
- `running` → set `cancel_requested_at`; the worker observes it and stops
  renewing its lease.
- **the reaper sends an expired lease whose `cancel_requested_at` is set to
  `cancelled`, never back to `queued`.** That is the interaction the state
  version got wrong.
- already terminal → **no-op, 200, returning the terminal state.** Not an error.
  A client that crashed after cancelling and retries must not receive a 409 for
  having succeeded.

On the wire the client still sees **`cancelling`** — `running` with the column
set. It is a *derived* status and it **is in the OpenAPI enum**, because it is
observable by two codebases and a status a client can see but cannot find in the
schema is a status one of them will fail to handle.

`POST` and not `DELETE` because the job row survives cancellation — it is a
record that work was asked for and stopped, which `DELETE` would misdescribe.

## 9 · The service holds nothing irreplaceable

Job rows, the submitted audio, the decoded WAV and stored results are all
**derived**: the audio still exists on the client side, so anything here can be
recomputed. Therefore the entire `asr` database can be dropped and the estate
loses nothing but queue position.

**The corollary a reviewer should check on any PR here: nothing in this service
may become the only copy of anything.** The moment it does, its store stops being
disposable and it has quietly acquired the properties of tier 2 without any of
the protections.

### [rev] Where the submitted bytes live

The first draft covered the decoded WAV and said nothing about the Opus the
client sent — and a lease-expired job returns to `queued` needing those bytes
again.

They live in the `asr` database as `bytea`, deleted at terminal alongside the
WAV. At ~5 MB average this is unremarkable, and it keeps the claim above
literally true: drop the database, lose queue position and nothing else. A spool
directory would reintroduce a filesystem dependency, a volume to mount, and a
second cleanup path that can disagree with the first — and the decoded WAV is
already deleted at terminal because CHRN-21 is emphatic that a derived artefact
must never be written beside authored bytes.

### [rev] What "purged at 7 days" purges

The **result payload**, not the job row. The row survives, which is what lets
`GET /v1/jobs/{id}/result` answer **410 Gone** rather than 404 — a client that
comes back late learns its result expired, not that its job never existed. §3's
`Idempotency-Key` uniqueness therefore still holds forever, because the row it is
unique against is still there. There is no tension between "never expires" and
this purge; they are about different things.

The job table is unbounded by design: one row per transcription attempt, kept.
At this scale that is nothing, and saying so is cheaper than leaving it to be
found as an omission.

7 days matches `upload.DefaultTTL`, the CHRN-20 sweep's window. Nothing depends
on the number; matching an existing one beats inventing a second.

**The client-side rule that follows: "result expired" is not "transcription
failed".** A client that comes back after 7 days re-submits with a fresh key.

## 10 · OpenAPI is the source, and CI proves it

`deploy/asr/openapi.yaml` is the contract. Chronicle's client is generated from
it; Catenary generates its own, which is the entire point.

The precedent is Catenary's own D3, which split its client across Vue and Flutter
and so bought two hand-written implementations of one wire format. This contract
has two clients for a different reason — two services rather than two front-ends
— and lands in the same place if nobody generates: two implementations, drifting,
the divergence surfacing as a bug in whichever was written second.

**A generated artefact with no guard is a generated artefact someone
hand-edits** — `CLAUDE.md`'s words about the schema/migration check, and the same
guard applies here: CI regenerates the client and fails if the committed one
differs. That check needs no hardware, so it belongs in `verify.sh` alongside the
others.

**[rev] The generator is pinned by name and version** — `oapi-codegen`, declared
as a `tool` directive in `go.mod` so `verify.sh` and CI run the same one. An
unpinned generator makes the staleness guard a coin flip.

**[rev] Directories, because the first draft's collided.** The generated
Chronicle client goes in `internal/asrclient/`. The service itself is
`cmd/asrd/`, with **its own migrations package and its own migrator**:
`migrations/migrations.go` is `//go:embed *.sql` over *Chronicle's* migrations,
so a second service reusing it would apply Chronicle's schema to the `asr`
database. The first draft said `internal/asr/` for the client, which is where the
service would naturally have gone.

**[rev] Two repo-level registrations, in the same PR as the spec.**
`deploy/asr/openapi.yaml` joins `sensitive_paths`; the generated client joins
`.github/review-ignore`. That is exactly the test `.github/review-ignore` states
for itself — the fix for a wrong generated client is to re-run the generator, so
a review comment on its diff has nowhere to land.

`/v1/` is in the path so Catenary can pin a version it was generated against.

## Surface

| route | auth | |
|---|---|---|
| `GET /healthz`, `GET /readyz` | open | `/readyz` reports the GPU lease and queue depth; `/healthz` stays dependency-free |
| `POST /v1/jobs` | client token | multipart `spec` + `audio`. `Idempotency-Key` **required**. 201 new, 200 replay, 409 mismatch, **415 unsupported media type** |
| `GET /v1/jobs/{id}` | client token | status + `retry_after_ms`. Status enum includes the derived `cancelling`. Another client's job is **404, not 403** — CHRN-71's precedent |
| `GET /v1/jobs/{id}/result` | client token | 200 with the result; 409 while not terminal; **410 once the payload is purged** |
| `POST /v1/jobs/{id}/cancel` | client token | idempotent |
| `GET /v1/models` | client token | what this deployment can run, and the accepted audio media types, so a client discovers rather than hardcodes |

## Configuration

| variable | | |
|---|---|---|
| `ASR_DATABASE_URL` | required | boot fails if unset |
| `ASR_CLIENT_TOKENS` | required | `name:token` pairs out of Signet. Empty → boot error, never "open" |
| `ASR_DEFAULT_MODEL` | no | `small.en` — CHRN-12's default and its reasoning |
| `ASR_RESULT_TTL` | no | 7 days |
| `ASR_LEASE_TTL` | no | shorter than the shortest plausible inference. CHRN-26's to tune |

## What each ticket inherits

- **CHRN-26** — the table, the lease column, the CAS claim and the reaper are
  here, along with a placeholder per-invocation loop you delete. Yours: the
  resident worker, the single-flight GPU lease, per-client fairness. `leased` and
  `running` are separate states for your benefit.
- **CHRN-27** — **§5's three requirements are binding**: `partial` on the tier-2
  transcript row, a row for every `succeeded` result including empty text, and a
  Chronicle-side gate query. Persist the `Idempotency-Key` **before** submitting,
  or §3 buys nothing. Store `model` and `backend` with the transcript. A tier-1
  row correlates memo → job (the `tier1.memo_uploads` precedent); the transcript
  itself is tier 2.
- **CHRN-28** — `attempts` exists; the ceiling and the backoff are yours.
  `partial` is a field the service sets, and your policy decides when.
- **CHRN-22** — §5 is your predicate, read from Chronicle: **`succeeded` and not
  `partial`**. Empty text with a completed run is durable. Never compute it from
  `covered_ms`, and never ask the ASR service.
- **CHRN-18's Done-when #7, second half** — *"that E3's worker skips ASR when a
  durable transcript already exists"* was explicitly deferred to E3 as needing
  E3's worker and E3's table. Both now exist, so that test lands here.
- **CHRN-29** — publish §§2, 3, 5, 7 with the numbers from CHRN-24.

## What this does not decide

- **Whether this service gets its own repo.** CHRN-24 put the image in
  `deploy/asr/` here and deferred the split to CHRN-29; this follows that. Worth
  saying plainly that **CHRN-29 is the last cheap moment** — once Catenary
  generates a client against a spec in Chronicle's repo, moving it is a
  coordinated change across two services.
- **Retry counts, backoff, dead-lettering.** CHRN-28.
- **Fairness policy between clients.** CHRN-26.
- **Whether a re-upload un-prunes a memo.** Still CHRN-22's, per CHRN-20 §6.
- **A callback/webhook instead of polling.** Deliberately not now: it needs the
  service to hold client credentials, which §2 rejected for the same reason.

## Three things I would like ruled on rather than assumed

Reviewed independently; the recommendation on each came back the same way, and
the second one came back with an improvement worth taking.

1. **§1, the separate database.** Recommended **yes** — the provisioning cost is
   one `provision-db.sh` run, against a coordinated migration across two services
   the day Catenary arrives.
2. **§5's empty-transcript ruling.** Recommended **yes, with the guard moved out
   of the predicate and into CHRN-22.** The reasoning is worth stating because it
   is the one way this ruling becomes corpus loss: an empty result is the correct
   answer to silence *and* the exact symptom of a decoder emitting silence — a
   broken ffmpeg pin, a wrong sample rate — across the whole corpus at once. So
   keep the predicate as stated, and give **CHRN-22's dry run a rate ceiling**:
   if the share of empty transcripts in a prune set exceeds a threshold, refuse
   the run and say so. Cheap, and it closes the only path from this ruling to
   irreversible loss.
3. **§9's 7-day result TTL.** Recommended **fine**; it matches `upload.DefaultTTL`
   and nothing depends on the exact number.

## Done when

- `deploy/asr/openapi.yaml` describes the surface above, and `verify.sh` fails
  when the committed client and the spec disagree, using a pinned generator.
- A Go client is generated from it, not hand-written.
- **Job state survives `kill -9` of the worker**: the job returns to `queued` with
  `attempts` incremented, and no job is ever dropped. Testable because §4 ships a
  worker to kill.
- A replayed `Idempotency-Key` returns the original job; a mismatched one is 409.
- **N concurrent submits sharing one key produce one job and no error** — its own
  test, because CHRN-18 found this case *"fails against a design that passes"* the
  plain concurrency test.
- **A cancelled `running` job whose worker then dies ends `cancelled`, not
  `queued`**, and is never re-run.
- An unsupported audio media type is 415, and the accepted set is discoverable.
