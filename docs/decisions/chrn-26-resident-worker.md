# CHRN-26 — The resident worker and the GPU lease (decision)

Status: **accepted 2026-08-28.** Proposed, revised twice after review, revised a
third time to fold in the second review pass, and accepted by magos the same day
**at the recommendations** — the three rulings at the end are settled and the
settlement is recorded there. The PR that follows this document is mechanical.

**Third revision, marked [rev 3].** Seven items from the second review pass,
all small, all of the kind an implementer closes silently: the advisory lock's
connection (§3), a sentence that still described the per-inference draft (§3),
a `/load` deadline (§7), `Release` against the cancel constraint (§8), what a
deadline breach costs CHRN-28 (§8), the contention warning coupled to the
deadline factor (ruling 2), and what a standby reports (§9).

**Second revision, marked [rev 2].** magos asked what §3 means for a second
worker — the desktop, and possibly a machine outside the estate — and for a
transcript produced on the phone. Neither is built here, and both got a ticket
(**CHRN-80**, standalone; **CHRN-81**, under E9). What changed in this document
is that three choices which would have hardcoded *one worker* are now made the
other way, at no cost: the lock is keyed per device, fairness lives in the
query, and the deadline uses the worker's own rate. See §3, §5, §7 and *What
this does not decide*.

**Revised after review.** Seven findings, all accepted, and **four of them are
decisions this document left open that an implementer would have closed
silently** — the shape CHRN-25's revision was for. Changes are marked **[rev]**;
they close gaps rather than reverse positions, and the one that matters is §7's:
a *hung* child, as opposed to a crashed one, deadlocked the queue with every
lease reporting healthy.

Every claim this document makes about `whisper-server` is now **checked against
the pinned tree** at `1fe009ca` rather than inferred from its `--help`, and the
reading turned up two things the review did not have — both of which would have
produced a passing Done-when over broken behaviour:

- **§1**: the server's decode defaults are **greedy** where every reference
  number was measured under beam search. The review had these matching. An
  unpinned run beats 57.9× for a reason that is not residency.
- **§4**: `response_format` defaults to `json`, which returns text and **no
  segments** — and CHRN-25's contract makes an empty segment list *valid*, so
  nothing downstream would ever complain.

Ticket: CHRN-26 (Phase P2, parent CHRN-3). Tier `opus`, so Mode B: this document
is the review artefact and the PR that follows it should be mechanical.
Decision owner: magos.
Read by: **CHRN-28** (failure semantics — §7 and §8 are the states it acts on),
**CHRN-29** (Catenary handoff — §5's fairness rule is a promise to client two),
and whichever ticket eventually gives Scribe a GPU credential, because §3 is
where the estate's answer to "who is allowed to run on the R9700" gets written
down or deferred.

## Context

CHRN-24 pinned the runner. CHRN-25 settled the contract and shipped a
**placeholder** worker: one process, one job at a time, `whisper-cli` invoked per
job. It is deliberately the shape CHRN-12 measured as the slow one, and it says
so in three places. This ticket replaces it.

The ticket names two deliverables and they are not equally hard:

> One worker process holding the model, and a lease that guarantees exactly one
> inference runs on the R9700 at a time.

The first is an engineering choice with a measured answer. The second is the
reason this epic exists at all, and it turns out to have a boundary that the
ticket's framing does not survive contact with — see §3, which is the section to
read hardest.

## Decision

The resident worker is **`whisper-server`**, from the same pinned whisper.cpp
tree CHRN-24 already builds, supervised by `asrd` as a child process and spoken
to over loopback. Admission to the GPU is a **single in-process semaphore of one**
in `asrd`, backed by a **Postgres advisory lock** so that two `asrd` processes
overlapping during a redeploy cannot both run. Queueing is **round-robin by
client**, and the model is **switched only when the queue holds nothing for the
resident one**.

`asrd` is the single point of admission **for transcription**. It is not, and on
the evidence below cannot cheaply become, the single point of admission for the
device — §3.

## 1 · The resident process is `whisper-server`, not cgo and not a wrapper

Three ways to hold a model in VRAM across jobs.

**cgo against `libwhisper`.** The image already builds `libwhisper.so.1.9.2`, so
this is available today. It is rejected on two counts, and the second is the
one that decides it. It requires `CGO_ENABLED=1`, which ends the "single static
Go binary" property `CLAUDE.md` states for every service in this estate and
makes `asrd` unbuildable outside the whisper image. And it puts a C library's
lifetime inside the process that serves HTTP: a decoder segfault on a malformed
file stops taking jobs, stops answering `/readyz`, and takes the queue's only
reader down with it. A crash in a child process is a restart; a crash in the
server is an outage.

**A bespoke long-lived wrapper** — a small C++ or Go program holding the model
and reading jobs off a pipe. This is inventing a protocol nobody else will ever
maintain, in a tree that already ships one.

**`whisper-server`, and that is the decision.** It is built by the cmake
invocation CHRN-24 already runs, from the pinned SHA, into the same
`build-vk/bin` the benchmark harness uses — verified present in the image:
`whisper-server` sits beside `whisper-cli`, `whisper-bench` and the `.so`s. It
holds the model, exposes `/inference`, takes `--host`, `--port`, `-m`, `-dev`
and `-t`, and needs **no new pin, no new build stage, and no change to what
CHRN-24 measured.**

Bound to `127.0.0.1` inside the container, never to the container's interface.
It has no authentication of any kind, and the credential surface of this service
is `ASR_CLIENT_TOKENS` on `asrd` — a second listener on `construct_net` that
transcribes anything sent to it would make that surface decorative.

**Supervised, not assumed.** `asrd` starts it, waits for it to answer, restarts
it with backoff if it exits, and reports it in `/readyz`. A resident process
nobody supervises is a resident process that dies once and turns the service
into a queue that fills forever — which looks exactly like a busy service.

**[rev] It has a `/health` endpoint, and that is what to wait on.**
`server.cpp:1205-1213` answers `{"status":"ok"}` when READY and **503** with
`{"status":"loading model"}` while a model is loading — so "started" and "ready
to take work" are distinguishable without guessing at a sleep. The supervisor
polls it at startup and after every `/load`.

**[rev] It carries its own single-flight line, and that is worth knowing rather
than relying on.** `whisper_mutex` (`server.cpp:638`) is held for the whole of
`/inference` (`819`) and the whole of `/load` (`1164`), so a second concurrent
request **blocks** rather than running. That is a second guarantee under §3's
semaphore, not a replacement for it: it is per-process, so it says nothing about
two `asrd` processes, and blocking is not admission control — a queue of
requests parked on a mutex is invisible to everything that would want to see a
queue. Belt and braces, with the semaphore as the belt.

### [rev] The decode parameters must be pinned, and the review had this backwards

The review's read was that `whisper-server`'s defaults match `whisper-cli`'s, so
this ticket's throughput Done-when is a fair comparison. **They do not match**,
checked in the pinned tree's `--help` for both binaries:

| | `whisper-cli` | `whisper-server` |
|---|---|---|
| `--beam-size` | **5** | **-1** (greedy) |
| `--best-of` | **5** | **2** |
| `--threads` | 4 | 4 |
| `--flash-attn` | true | true |

And `bench.sh` passes only `-m` and `-f`, so **every CHRN-12 and CHRN-24
reference number was measured at beam search with beam size 5.**

A `whisper-server` left on its defaults therefore decodes **greedily**, and that
is worse than a small discrepancy in two ways at once. It is *faster*, so this
ticket's own Done-when — "within a few percent of the resident column" — would
be met by a different decode rather than by residency working, and would look
like a pass. And it is a different transcript: CHRN-12's model comparison,
including the `Frédéric`/`Fradique` observation that ruled out `large-v3`, was
made under beam search, so a corpus decoded greedily is not the corpus that
benchmark describes.

**`asrd` passes `-bs 5 -bo 5` explicitly**, and the throughput claim is only
meaningful with the decode parameters matched. This is the **third** time in
this epic that an unmatched knob would have invalidated a comparison — after
`-nt` being a decode change rather than a formatting flag, and the load guard
being a start-of-run check only. The pattern is consistent enough to state as a
rule: **a number compared against CHRN-24 must name the decode it was taken
with.**

## 2 · What "resident" is worth, and the framing that makes it obvious

The honest number for a 60-second memo is **28%**: 1.39 s per invocation against
1.01 s resident on `small.en` (CHRN-12; 43.6× and 57.9× re-measured through the
container in CHRN-24). Stated that way this ticket looks like a modest
optimisation, and it is worth saying why that framing is wrong.

R3 isolated the cost by transcribing a **1-second** clip: **388 ms on Vulkan**,
essentially all of it fixed per-process setup (`~/projects/catenary/spike/r3-whisper/FINDINGS.md`
§8; the same table reads 3611 ms for HIP). It is a constant, not a share. So on
the memos this system actually receives:

| memo | inference at 59.6× | + 388 ms tax | per-invocation is |
|---|---|---|---|
| 5 s | 84 ms | 472 ms | **5.6× slower** |
| 30 s | 503 ms | 891 ms | 1.8× slower |
| 60 s | 1.01 s | 1.39 s | 1.38× slower |
| 40 min | 40.3 s | 40.7 s | 1.01× slower |

**The tax is paid per job, so it dominates exactly the memos there are most
of.** A voice note is far more often ten seconds than forty minutes. The 28%
figure is the number for the longest jobs, which is the case that needed this
least.

**[rev] The two framings are one fact, and the table said so wrongly.** The 60 s
row read 1.28×, which is the 28% figure leaking into a column of time ratios;
1.39 s against 1.01 s is **1.38×**. Both describe the same thing — per-invocation
takes 38% longer, resident saves 27% of the wall clock — and every other row was
already a time ratio. Corrected above.

There is a second cost the table does not show. Model load alone is **145 ms for
`base.en` and 1.9 s for `large-v3`'s 3.1 GB** (`docs/benchmarks/whisper-model-choice.md`)
— so per-invocation, the upgrade
path CHRN-12 recommends (`medium.en`) and the model somebody will eventually try
(`large-v3`) are the ones the tax punishes hardest, and residency is what keeps
that door open.

## 3 · The lease guarantees single-flight transcription. It does not guarantee the device.

This is the section that changes the ticket, so it is stated plainly before the
argument: **the ticket's framing does not survive the deployment, and pretending
otherwise would ship a guarantee that is not one.**

The ticket says:

> The R9700 also hosts Ollama, which Scribe needs, and Catenary wants the same
> GPU for its own transcription. Without a single point of admission, three
> consumers discover contention independently, in production, as timeouts that
> look like bugs in three different services.

Two of those three go through this service. Chronicle submits jobs; Catenary
will submit jobs; both arrive at `asrd`, and `asrd` can trivially admit one at a
time. **Ollama does not.** It is a separate container with its own model
residency and its own HTTP surface, reached directly by whoever needs a
completion. Nothing `asrd` does can stop it starting work on the R9700, and
making it participate would mean either putting Ollama behind an admission
proxy — a new estate component, with its own availability — or requiring every
Ollama *caller* to take a lock first, which is a promise enforced by convention
across services written at different times by different people. That is the
shape of promise that holds until the first one that forgets.

So the scope this decision claims is the one it can actually keep:

> **`asrd` admits exactly one transcription at a time on the R9700.** Contention
> with Ollama is real, is not prevented here, and is bounded rather than
> arbitrated — see below.

Three things make that an acceptable place to stop, and they should be checked
rather than taken on trust:

1. **Concurrency with Ollama is a slowdown, not a failure.** The R9700 reports
   **34,208,743,424 bytes — 31.9 GB** — read from
   `/sys/class/drm/renderD129/device/mem_info_vram_total` on this box, which is
   the `amdgpu` card and therefore the right one (CHRN-24: the render-node
   numbering reads the wrong way round). `small.en` is 466 MB resident against
   `large-v3`'s 3.1 GB. Two consumers
   contend for compute and memory bandwidth, and both get slower. Neither gets a
   wrong answer, and neither is evicted.
2. **The single-flight rule that matters most is the one within transcription.**
   Two whisper processes on one device is the case that doubles VRAM for the
   models *and* halves throughput for both, and it is the case CHRN-24's exit
   criterion names.
3. **A bounded queue is a better signal than an arbitrated one.** `/readyz`
   already reports queue depth (CHRN-25). A transcription backlog caused by
   Ollama contention shows up there as a growing number, in one place, rather
   than as timeouts in three services.

What this ticket owes instead of arbitration is **visibility**: if inference
time for a known model drifts materially above the CHRN-24 reference, that is
worth a log line naming contention as the likely cause, because otherwise the
first explanation anyone reaches for is that whisper got slower. See ruling 2.

### Where the lease actually lives

**An in-process semaphore of one**, because within `asrd` that is the whole of
the mechanism and anything else is ceremony.

**Plus a Postgres advisory lock on the `asr` database**, because the case the
semaphore cannot see is two `asrd` processes: a rolling redeploy overlapping old
and new, or somebody running a second one by hand. An advisory lock is released
automatically when its connection dies, so a crashed process cannot hold the
device.

**[rev 3] That is a different property from CHRN-25's lease, and the first
draft said otherwise.** The job lease is a **timestamp**, chosen precisely so
that it depends on no connection; the advisory lock is **connection-scoped**,
and everything below follows from that.

- **It is held on a dedicated `pgx.Conn`, outside the pool.** A pooled
  connection is the pool's to close, and the lock goes with it.
- **Loss of that connection is loss of ownership.** A Postgres restart drops
  every session, and the lock with it. `asrd` must notice — the connection
  errors on its next use, and a periodic `SELECT 1` on it is the cheap way to
  make "next use" soon — **stop claiming**, and re-acquire before claiming
  again. The inference in flight may finish: its job lease is time-based and
  renewal resumes on reconnect, and an outage longer than the 30 s TTL reaps
  it, which is CHRN-25's existing behaviour and not this ticket's problem.
- **A standby polls `pg_try_advisory_lock`.** Never the blocking form: a query
  that blocks forever on one connection is invisible to everything, including
  `/readyz`.
- **A process exit sends FIN, so container stop and `kill -9` both release
  cleanly.** What does not is a host crash, which leaves the session — and the
  lock — until Postgres's TCP keepalive notices, and the OS default for that is
  **7200 s**. One statement on the lock connection bounds it to about a
  minute: `SET tcp_keepalives_idle = 30, tcp_keepalives_interval = 10,
  tcp_keepalives_count = 3`. Session-settable, no change to the shared
  Postgres.

**[rev] It is taken ONCE, FOR THE PROCESS'S LIFETIME, and not per inference.**
The first draft implied per-inference and that is worse in three ways.

A session advisory lock lives on **one connection**, so a per-inference lock
means pinning a pool connection for the length of every job and unpinning it
after — and if that connection drops mid-inference the lock silently vanishes
while the work continues, which is precisely the "lease lost, work still
running" class this document is careful about everywhere else.

Taken at startup instead, it says something more useful: **this `asrd` owns the
device.** A process that cannot get it is a standby — it claims no jobs and
loads no model, so it holds no VRAM either, which per-inference locking does not
give you. And it makes finding 3's fairness bookkeeping safe to keep in memory
rather than in a query, because there is exactly one process doing the claiming.

**[rev] `ASR_GPU_LOCK_KEY` is dropped, because its rationale was backwards.**
Advisory locks are scoped to the **database**. Two deployments on one Postgres
have two `asr` databases and therefore cannot collide however they are keyed, so
a distinguishing knob buys nothing; and two deployments genuinely sharing one
GPU would need a *shared* lock, which separate databases cannot give them. A
knob that cannot do either job is a knob somebody will one day set.

**[rev 2] What the lock names is a device, and that is a knob with a reason.**
magos raised a second worker — the desktop first, later possibly a machine
outside the estate (CHRN-80) — and a single key per database means **one worker
per Postgres, full stop**: the standby above would be the desktop, forever. So
the key is `hash(ASR_DEVICE_ID)`. One R9700 today; a second device is a second
key rather than a redesign, and two `asrd` processes naming the *same* device
still exclude each other, which is the redeploy case this lock exists for. That
is the rationale the first draft's knob should have had — per **device**, never
per deployment — and it is why it comes back under a name that says what it
locks. `ASR_DEVICE_ID` also lands in `leased_by`, so `GET /admin/transcription`
can say which device transcribed what once there is more than one.

Neither half is the job lease from CHRN-25, and **[rev 3]** the three are
worth naming apart because the first draft's sentence here still described the
per-inference design. The **job lease** says *this worker owns this job* and is
held for the whole of a job including the decode. The **advisory lock** says
*this process owns this device* and is held for the process's lifetime. The
**semaphore** says *inference is running now* and is held only for the
inference. A worker decodes under the job lease alone, and takes the semaphore
after — collapsing the last two would serialise ffmpeg behind the GPU, which is
a decode that could have happened while the previous job was still on the
device.

## 4 · One resident model, switched only when the queue is empty of it

A resident model is only resident until somebody asks for a different one. The
options are the usual three and two of them are wrong here.

**Switch per job** — load whatever the job asks for. This gives back the entire
benefit of §2 the first time a queue holds a mix, and worse than that: it turns
a mixed queue into an alternating sequence of 1.9 s model loads, which is slower
than the placeholder this ticket replaces.

**Refuse anything but the configured model** — simple, and it makes
`ASR_DEFAULT_MODEL` a deployment-wide constraint rather than a default. CHRN-12
explicitly leaves `medium.en` open as an upgrade path and CHRN-25's contract
lets a client name a model; taking that away in the implementation would make
the contract's `model` field a lie.

**Drain, then switch, and that is the decision.** The worker keeps the resident
model while the queue holds any job for it, and switches only when it does not.
A job for another model waits for the current model's queue to empty.

The obvious objection is starvation, and it is a real one: a steady trickle of
`small.en` jobs would keep a `medium.en` job waiting forever. So the rule has a
bound: **a job that has waited longer than `ASR_MODEL_SWITCH_MAX_WAIT` forces a
switch on the next job boundary.** One knob, defaulting to something short — see
ruling 1 — and the switch itself costs at most 1.9 s, which is under two seconds
of a queue nobody is watching.

**Model switching is a `/load` on the resident server, not a restart.** The
endpoint is present in the pinned binary — `/load` and `/inference` are both in
`build-vk/bin/whisper-server` at `1fe009ca`.

### [rev] What `/load` actually does, read rather than assumed

The first draft verified `/load` by **string presence in the binary**, which is
not the same as verifying what it does. Read at
`examples/server/server.cpp:1184-1194` of the pinned tree, it does this:

```c
        // clean up
        whisper_free(ctx);
        ctx = whisper_init_from_file_with_params(model.c_str(), cparams);

        // TODO perhaps load prior model here instead of exit
        if (ctx == nullptr) {
            exit(1);
        }
```

**It frees the old model first, and calls `exit(1)` if the new one fails to
initialise.** That comment is upstream's, verbatim. So a failed switch is not an
error response — it is the resident process terminating, and the supervisor from
§1 restarting it. With which model?

Three rules follow, and none of them are optional:

1. **Restart with the last-known-good model, never the one that just failed.**
   Restarting with the failure reproduces it, and §1's backoff then produces a
   restart loop rather than a service.
2. **A model that fails to initialise is marked UNLOADABLE**, and jobs naming it
   are `failed` with a code rather than queued. This is consistent with what
   CHRN-28 inherits: a model that will not load is a deployment fault, not a job
   to retry.
3. **A model that is merely ABSENT is a different answer.** `/load` returns
   **400** for a path that does not exist and does not exit
   (`server.cpp:1175-1182`) — so "not installed" and "installed and broken" are
   distinguishable, and only the second costs the resident process.

**A correction to the review, which is worth having right because code would be
written for it.** `/inference` does **not** answer 503 during a load. Both
handlers take the same `whisper_mutex` (`server.cpp:638`, locked at `819` for
inference and `1164` for load), so an inference arriving mid-switch **blocks on
the mutex until the load finishes**. The 503 comes from `/health` only
(`server.cpp:1206-1213`), which reports `{"status":"loading model"}` while the
state is `SERVER_STATE_LOADING_MODEL`. The practical rule the review wanted is
right — asrd must read a switch as *wait*, not as job failure — but the
mechanism is a blocking call and a `/health` probe, not a 503 on the inference
path. Handling a 503 there would be handling a response that never arrives.

### [rev] The response format is a trap, and it is silent

`response_format` defaults to `json` (`server.cpp:117`), and that branch emits
**`{"text": ...}` and nothing else** (`server.cpp:1153-1160`). No segments, no
timestamps, no duration.

That is the dangerous shape: CHRN-25's contract makes an empty `segments` list
**valid**, because a memo with no speech legitimately has one. So a worker that
forgot to ask for `verbose_json` would produce transcripts that pass every
check, satisfy the durable-transcript predicate, and quietly carry no timing at
all — across the whole corpus, with nothing to say so. **`verbose_json` is
required, and a job whose response carries text but no segments for audio that
is not silent should be loud about it.**

Two consequences of that format, both checked:

- **Its timestamps are float SECONDS**, `t0 * 0.01` (`server.cpp:1089-1090`),
  where the placeholder reads integer milliseconds from `whisper-cli -oj`. The
  conversion is mechanical; leaving it out is a corpus of transcripts timed a
  thousand times short.
- **It computes language probabilities by default**, which the source itself
  calls an "expensive operation" (`server.cpp:1066-1078`). Pass `-nlp`. Nothing
  in Chronicle reads them, and CHRN-24's reference numbers were taken with
  `whisper-cli -oj`, which does no such detection — leaving it on would make
  this ticket's own throughput Done-when unfair to itself.
- It also reports `duration` in float seconds, so the WAV-header arithmetic the
  placeholder does can go.

A restart is still the fallback if `/load` misbehaves in some way the source
does not show, and it is not painful: the same model load plus process startup,
paid only at a switch.

## 5 · Fairness is round-robin by client, and it is a promise to client two

CHRN-25 §6 already established that `client_id` comes from the token and never
from a field, and said why in terms of this ticket: a client-asserted identity
would let either service jump the other's queue. This is the section that makes
that matter.

**The claim picks the oldest queued job of the client least recently served.**
Not a global FIFO: a Catenary backfill of eight hundred voice messages would put
every Chronicle memo behind it, and the symptom is a memo somebody is waiting on
that never arrives while the service looks perfectly healthy.

Expressible as a claim query and therefore keeping CHRN-25's compare-and-swap
shape intact — the `WHERE status = 'queued'` predicate is untouched; only the
ordering changes. Concretely, order by *(the most recent `started_at` among that
client's jobs, nulls first)*, then by `created_at`.

**Fairness is between clients, not within one.** Within a client the order stays
oldest-first, because a client's own jobs have no ranking this service could
justify inventing.

The honest limitation, recorded because it will otherwise be discovered as a
bug: with two clients and a deep backlog on one, round-robin gives the quiet
client **half** the device, not priority. A memo submitted while Catenary is
mid-backfill waits for at most one Catenary job, which at `small.en` is about a
second. That is the intended behaviour and it is worth writing down so that
"Chronicle should always win" is a decision somebody takes deliberately rather
than a bug report.

### [rev] …and that "about a second" only holds under one model

§4 and this section compose, and the first draft did not say in which order.
Worse, the reassuring number above is **only true when both clients are asking
for the resident model.** If Catenary's backfill names a different one, a
Chronicle memo waits `ASR_MODEL_SWITCH_MAX_WAIT` **plus** one job — so ruling 1
is not only a starvation knob, it **is the fairness bound under mixed models**,
and CHRN-29 must publish that number rather than the one-second figure.

The claim order, stated so it cannot be composed two ways:

1. **A job for a non-resident model that has waited longer than
   `ASR_MODEL_SWITCH_MAX_WAIT`** → switch to its model and take it. Starvation
   beats residency; it is the only rule with an unbounded downside.
2. **Otherwise, round-robin among jobs for the RESIDENT model.** This is the
   common case and the one the benchmark describes. **[rev 2]** *Resident* is
   per worker: the claim takes the caller's resident model as a parameter, so
   two workers holding different models drain different halves of a mixed
   queue without either switching — which is half of what a second device
   would be for.
3. **Otherwise** — nothing queued for the resident model — round-robin among
   everything and switch to whatever wins.

**[rev] Adding an index for this is allowed, and so is a new store method.**
§6 says a PR that alters the job table has gone wrong, and taken literally that
would stop an implementer adding the index this ordering needs — a
`MAX(started_at) GROUP BY client_id` over a table CHRN-25 calls "unbounded by
design" is a sequential scan per claim. What §6 forbids is changing what the
table *means*: its states, its columns' semantics, the idempotency uniqueness.
**An index migration is fine. A new `Store` method is fine.**

**[rev 2] The bookkeeping lives in the query, not in memory — decided, not
left open.** The first revision allowed either because exactly one process
claims, and that stops being true the day a second device is added (§3
[rev 2]). In-memory last-served is not round-robin at all with two workers:
each alternates on its own view and the pair can serve one client twice in a
row while the other waits. `MAX(started_at) GROUP BY client_id` is right for
any number of workers, the index makes it cheap, and settling it now means the
mechanical PR is not undone later.

## 6 · What happens to CHRN-25's placeholder

Deleted, not extended. `internal/asr/worker.go` is one file, it is labelled a
placeholder in three places, and the shape it has — claim, shell out, write —
is not the shape of a worker that holds a model and a device lease.

**What survives unchanged, and must**: the five stored states, the
compare-and-swap claim, `RenewLease`, the reaper, and the meaning of every
column. CHRN-25 built those to be the durable half precisely so this ticket
could replace the worker without touching them, and a PR here that changes what
the job table *means* is a PR that has gone wrong.

**[rev] That is not the same as "touch nothing".** The first draft said "the job
table … and every `Store` method", which reads as a freeze and would stop two
changes this ticket needs: **an index migration** for §5's claim ordering, and
**a new `Store` method** for §8's child-death release. Both add; neither
redefines. The test is whether an existing caller's behaviour changes.

**`leased` and `running` earn their separation here.** CHRN-25 kept them apart
for this ticket's benefit and this is what that benefit is: a job is `leased`
while it decodes and waits for the GPU lease, and `running` only once inference
has actually started. The gap between them is the queue for the device, and it
is now a thing an operator can see rather than infer.

## 7 · The lease TTL, and the forty-minute memo

CHRN-25 set `ASR_LEASE_TTL` to 30 s and left the tuning here. The renewal
interval is a third of it, which is correct and stays.

The case that breaks a naive setting is a long memo. At 59.6× a forty-minute
recording is about 40 s of inference — comfortably renewable. At `large-v3`'s
18.3× it is 131 s, and a model load ahead of it adds two more. None of that is
near a 30 s lease **provided the renewal keeps running**, and the renewal is a
goroutine that ticks independently of inference, so it does.

What must be checked rather than assumed: **the renewal must not be starved by
the GPU semaphore.** If the renewal goroutine ever waits on the same lock the
inference holds, a long job expires its own lease and is reaped out from under
itself — and the reaper then hands it to the worker that is still running it.
The rule is that **nothing on the renewal path may acquire the GPU lease.**

### [rev] And that rule, alone, deadlocks the queue on a hung child

This is the finding that changes the ticket's own Done-when, so it is stated
before the remedy: **decoupling the renewal from the GPU is correct for a long
job and catastrophic for a wedged one.**

The Done-when says *"a crashed worker releases its lease rather than deadlocking
the queue"*, and every mechanism in this document is built for a worker that
**dies**. A `whisper-server` that **hangs** — a GPU stall, a wedged driver, an
inference that never returns — does not die. `asrd` is alive, its renewal
goroutine is ticking exactly as designed, the job lease never expires, the
reaper never fires, the GPU semaphore is held, and the advisory lock is held.
**Every lease reports healthy and nothing moves, forever.** §9's "how long the
current inference has been running" was offered as the answer and is not one:
that is a number on a readiness probe, not a bound.

**Every job gets an inference deadline, derived from the audio.** A wall clock
is the only thing that can tell a long job from a stuck one, because from
outside they are identical:

> `deadline = max(30 s, 5 × audio_duration_ms ÷ expected_rate(model))`

Five times the expected inference time, floored at 30 s so short memos are not
tripped by a cold cache. On `small.en` that is 30 s for a 60-second memo and
about 200 s for a forty-minute one; on `large-v3`, about 11 minutes for the
same forty minutes. Wide enough that contention with Ollama — which §3 accepts
and does not prevent — cannot trip it, and finite.

**On breach: kill the child, restart it, release the job.** Killing is what
distinguishes this from the lease, which cannot help: the process holding
everything is the healthy one.

**[rev 3] A `/load` gets a deadline too, and it is fixed.** A model load that
never returns — a driver wedge during pipeline compile — is the same finding
one step earlier: every lease healthy, nothing moving. The measured cost is
1.9 s for the largest model, so **60 s** is wide by thirty times and finite.
On breach: kill, restart on last-known-good, and treat the model as if its init
had failed — §4's three rules apply unchanged, because from outside a load that
hangs and a load that exits are the same fault.

`audio_duration_ms` is known before inference starts — the decode produced it,
and `verbose_json` reports it too — so this needs nothing new to compute. And
the per-job inference wall-clock it requires **is the same number ruling 2 wants
for contention detection**, so the two findings cost one measurement between
them.

**[rev 2] `expected_rate(model)` is the WORKER's rate, not the table's.**
CHRN-24's numbers describe the R9700. A worker on another device (§3 [rev 2])
has its own, and a deadline computed from somebody else's GPU is either a false
kill or no bound at all. So the rates are configuration per worker —
`ASR_EXPECTED_RATES`, `model=realtime_x` pairs, defaulting on this deployment
to CHRN-24's resident column — and **a model the worker does not name uses
18.3×**, the slowest CHRN-24 measured, so an unknown model errs wide rather
than killing a healthy job. The contention check in ruling 2 reads the same
table, for the same reason.

## 8 · When the resident process is not there

Four failures, and they want different answers.

| | |
|---|---|
| **`whisper-server` exits** | restart with backoff, and **release the job explicitly** — see the [rev] below, because "the reaper handles it" was wrong |
| **`whisper-server` hangs** | the inference deadline in §7 kills and restarts it. This is the case no lease can see |
| **it will not start at all** (no model, no device) | `/readyz` **unready**, naming the check. Jobs are still ACCEPTED — the queue is the right place for work a service cannot do yet, and rejecting submissions would push the retry into two clients |
| **the GPU is absent and it falls back to CPU** | this is CHRN-24's named nightmare, and CHRN-25 already logs the device once per process and warns on a software rasteriser. That warning moves here, to the supervised child's startup |
| **an individual job fails** | unchanged from CHRN-25: `failed` with a code, the client sees it, CHRN-28 decides about retries |

**Accepting work a service cannot currently do is deliberate.** CHRN-25's
contract has no "try later" answer, and inventing one would mean two clients
implementing a backoff against a state that is usually transient.

### [rev] "Nothing new is needed" was wrong, and it would have failed jobs

The first draft said a dying child needs nothing because the reaper already
handles it. **That is the path for `asrd` dying.** When the CHILD dies, `asrd`
is alive: it sees a connection reset from a request it made, and the
placeholder's handling of a failed transcriber run turns a `FailureError` into
a **`failed` job** — so one child crash would permanently fail a memo that
nothing was wrong with. Waiting for the lease instead is not much better: it
costs the TTL plus the reap interval, about 35 s of an idle GPU with a live
worker sitting next to it.

**A new `Store.Release` method**: `running` → `queued`, `attempts + 1`, lease
cleared. That edge is **already legal** in CHRN-25's trigger — `'running>queued'`
is in the transition array, put there for the reaper — so this is a new method
over an existing edge and not a schema change. Per §6's [rev], that is allowed.

**[rev 3] `Release` mirrors the reaper's cancel clause, or the database
refuses it.** `running>queued` with `cancel_requested_at` set raises `AS004` —
*a cancelled job may not return to the queue* — and a child that dies while
running a job somebody cancelled is exactly that row. `Release` sends such a
job to `cancelled` with the reaper's terminal payload, never to `queued`. The
constraint would catch the other answer, but catching it in the one path
nobody tests is how a mechanical PR stops being mechanical.

**[rev 3] `Release` says why.** A crash and a deadline breach both increment
`attempts`, and the counter cannot tell them apart — but they cost differently
by a factor of five (§7's deadline is 5× the expected run), and a file that
wedges the GPU stalls the queue for up to 200 s per attempt on `small.en`, 11
minutes on `large-v3`. So `Release` takes a reason, logs it at warn with the
job and the elapsed time, and CHRN-28 is told below that a breach deserves a
lower ceiling than a crash.

And the case §1 used to argue against cgo deserves finishing: **a malformed file
that segfaults the decoder now loops** — crash, release, re-claim, crash — until
CHRN-28's ceiling stops it. That is the correct behaviour for this ticket (the
alternative is failing jobs on transient crashes) and it is exactly why CHRN-28
needs a ceiling rather than wanting one.

### [rev] Cancelling a running job, which was undecided

CHRN-25 §8 says a `running` job is marked and *"the worker observes it and stops
renewing its lease."* Against the placeholder that worked: `exec.CommandContext`
killed `whisper-cli`. Against a resident server it does not. `/inference` holds
`whisper_mutex` for the whole synchronous call (`server.cpp:819`), so **dropping
the HTTP request does not stop the inference** — it runs to completion holding
the device, and the mutex blocks every job behind it.

Two options. Let it finish and discard the result: simple, and it spends up to
131 s of GPU on `large-v3` work somebody explicitly cancelled, with the queue
stalled behind it. Or **kill the child and restart it**, which is the decision:
it costs 388 ms of process startup plus a model load, at most about 2.3 s, and
it is the only one that actually stops the work.

The job then reaches `cancelled` exactly as CHRN-25 §8 describes — the worker
stops renewing, the reaper reads `cancel_requested_at`, and it is never
requeued. Nothing about that contract changes.

## 9 · Readiness reports the device, not just the database

`/readyz` currently pings Postgres and reports queue depth. It gains: whether
the resident process is up, which model it holds, and how long the current
inference has been running. A service whose GPU has gone but whose database is
fine currently reports **ready** and accepts work forever.

`/healthz` stays dependency-free. A dead `whisper-server` is not a reason to
restart `asrd` — `asrd` is the thing that restarts `whisper-server`.

**[rev 3] A standby is unready, and says so.** A process that did not get the
device lock (§3) holds no model, so the Done-when's rule — *refuses readiness
when the resident model is absent* — already makes it unready. That is the
right answer stated deliberately rather than by accident: it serves the API
correctly (submit and status are database-only) but it cannot transcribe, and
"ready" here means the latter. The body names the check as `standby`, so a
second `asrd` that came up during a redeploy is not mistaken for a broken one.

**[rev] This also settles a debt CHRN-25's review recorded against this
ticket.** CHRN-25's *Surface* table promised `/readyz` would report *"the GPU
lease and queue depth"*, and the shipped handler reports queue depth only —
correctly, because there was no GPU lease to report until now. The reviewer
flagged it as deferred rather than dropped and named this ticket. §3's lease is
what makes it reportable, so it is paid here rather than amended away.

## Surface

No change to `deploy/asr/openapi.yaml` except the readiness body, which gains
optional fields. That is the intended outcome: **CHRN-25 wrote the contract so
this ticket would not need to touch it**, and a PR here that changes a request
or response shape is one that should be questioned.

## Configuration

| variable | | |
|---|---|---|
| `ASR_WHISPER_SERVER_BIN` | `whisper-server` | the supervised child |
| `ASR_WHISPER_SERVER_ADDR` | `127.0.0.1:8081` | loopback only, never the container interface |
| `ASR_LEASE_TTL` | 30 s | unchanged from CHRN-25; §7 says why it holds |
| `ASR_MODEL_SWITCH_MAX_WAIT` | ruling 1 | how long a job for a non-resident model waits before forcing a switch. **[rev]** Also the fairness bound under mixed models — §5 |
| `ASR_INFERENCE_DEADLINE_FACTOR` | 5 | **[rev]** multiplier on expected inference time before a job is treated as wedged; floored at 30 s. §7 |
| `ASR_DEVICE_ID` | `r9700` | **[rev 2]** what the advisory lock names and what `leased_by` records. A second device is a second value. §3 |
| `ASR_EXPECTED_RATES` | CHRN-24's resident column | **[rev 2]** `model=realtime_x` pairs for THIS worker's device; unknown models use 18.3×. §7 |

**[rev] `ASR_GPU_LOCK_KEY` is gone, and [rev 2] `ASR_DEVICE_ID` is not it
back.** The old knob's rationale — "so a second deployment on one Postgres can
differ" — was backwards: advisory locks are database-scoped, so two deployments
have two `asr` databases and cannot collide however they are keyed, while two
sharing one GPU would need a lock separate databases cannot give them. The new
one names a **device**, which is the thing the lock actually protects, and
exists so a second worker on a second GPU is a value rather than a redesign.
§3 has both arguments.

## What each ticket inherits

- **CHRN-28** — `attempts` is incremented by the reaper, and now also by §8's
  `Release`. §8's table is the set of failures you are writing policy for. A
  model that will not load is a deployment fault and not a job to retry; a job
  that failed to decode is. **[rev] Your ceiling is load-bearing, not a
  nicety**: §8 establishes that a file which crashes the decoder loops until
  something stops it, and that something is yours. **[rev 3] And a deadline
  breach costs five times a crash** — up to 200 s of a stalled queue per
  attempt on `small.en`, 11 minutes on `large-v3`. `Release` logs which it
  was; a breach deserves a lower ceiling than a crash, and that number is
  yours too.
- **CHRN-29** — §5's round-robin is a **promise to client two**, and the honest
  limitation is in that section: half the device under contention, not priority.
  **[rev] Publish `ASR_MODEL_SWITCH_MAX_WAIT` as part of it.** Under mixed
  models the wait is that value plus one job, not the one second the
  single-model case gives — and a client told the smaller number will read the
  real behaviour as a bug.
- **CHRN-22** — nothing. This ticket does not touch the durable-transcript
  predicate, and if a PR here appears to, that is the signal to stop.

## What this does not decide

- **Estate-wide GPU arbitration including Ollama.** §3, at length. If it is ever
  wanted, it is an admission service and its own ticket, and this document is
  the argument for why it was not smuggled in here.
- **Batching several memos into one inference.** whisper.cpp accepts repeated
  `-f`, and CHRN-12's harness used it to amortise startup — which is exactly the
  cost residency already removes. Nothing left to buy.
- **A second worker on a second device — CHRN-80.** Raised by magos on
  2026-08-28 (the desktop, and possibly a machine outside the estate) and
  deliberately kept open rather than built. §3, §5 and §7 now make the
  single-worker assumption explicit and cheap to lift — per-device lock key,
  fairness in the query, worker-local rates — and what remains is a
  **worker-facing protocol**: claim, renew and result over HTTP, with a worker
  credential distinct from `ASR_CLIENT_TOKENS`, so a machine that is not on
  `construct_net` never holds the `asr` database credential. That changes
  CHRN-25's contract, which this ticket promised not to touch, so it is a
  standalone ticket outside the epic. The trust question it carries — audio is
  tier-2 authored content, and a worker outside the estate sees every
  recording it claims — is that ticket's to settle, not this one's.
- **On-device transcription — CHRN-81, under E9.** A transcript produced on
  the phone or in the browser does not pass through `asrd` at all: it arrives
  *with* the memo and is written by Chronicle's upload path, so the surface is
  Chronicle's. The ideal path is live text while recording, with the estate
  service as the quality pass. The consequence that is **not** E9's: by the
  letter of CHRN-25 §5 a phone-grade transcript is durable, so CHRN-22 would
  prune audio on the strength of a `base` transcript. Recorded on CHRN-22 as a
  model floor its predicate needs before any device transcript lands.
- **CPU fallback.** CHRN-12 is clear that above `small.en` the CPU stops being a
  fallback (1.4× for `medium.en`, 0.6× for `large-v3`). A queue that waits is
  better than a queue that takes 101 seconds per minute of audio.

## The three rulings this needs

1. **`ASR_MODEL_SWITCH_MAX_WAIT`: what number?** The recommendation is **60 s**.
   Long enough that a burst of same-model jobs is not interrupted for one
   outlier; short enough that nobody waits on a memo without an explanation. The
   switch costs at most 1.9 s, so the knob is bounding a wait, not a cost.

   **[rev] Rule on it knowing it is two things.** §5's revision found that this
   is also the **fairness bound under mixed models**: a Chronicle memo behind a
   Catenary backfill on another model waits this long plus one job, not the
   ~1 second the single-model case gives. 60 s is still the recommendation, but
   it is now a number CHRN-29 publishes to client two rather than an internal
   tuning knob.

2. **Should contention be detected and logged?** The recommendation is **yes,
   cheaply**: record inference wall-clock per job against the model's CHRN-24
   reference and log once, at warn, when a rolling median drifts past a
   threshold — naming Ollama contention as the likely cause. §3 gives up
   arbitration; giving up *visibility* as well would mean the first symptom is a
   timeout somewhere else.

   **[rev] It is now free.** §7's inference deadline needs the same per-job
   wall-clock, so the measurement is being taken either way and this ruling only
   decides whether anything reads it.

   **[rev 3] The warning fires well before the deadline does.** §7's factor of
   5 is asserted to be wider than any contention with Ollama, and that is an
   assumption rather than a measurement. So the contention warning fires at
   **2×** the expected run — under half the kill threshold — which means a
   deadline kill under contention is never the first symptom, and if the log
   ever shows 2× drift routinely, the factor is the number to revisit rather
   than the jobs.

3. **Is §3's scope acceptable?** This is the ruling that matters, because it
   narrows what the ticket said it would deliver. Restated: `asrd` guarantees
   single-flight *transcription*, and does not arbitrate the device against
   Ollama. If the answer is no, the work is an estate admission service and this
   ticket should be split before any code is written rather than after.

## Done when

- One `whisper-server` process holds the model across jobs, supervised by
  `asrd`, bound to loopback, and restarted with backoff when it exits.
- **Two clients submitting concurrently both complete and neither starves** —
  the ticket's own Done-when, tested with a backlog on one client and a single
  job on the other.
- **Exactly one inference runs at a time**, proved by instrumenting the worker
  rather than by inspection: overlapping inference windows are a test failure.
- **A crashed worker releases its lease rather than deadlocking the queue** —
  CHRN-25's kill -9 test, still passing, plus the GPU lock released with its
  connection.
- **[rev] A HUNG worker does too.** A child that stops responding without
  exiting is killed by §7's inference deadline and its job released — the case
  where every lease reports healthy and nothing moves. Testable with a stub that
  accepts a request and never answers.
- **[rev] A dying child releases its job rather than failing it.** One crash
  must not permanently fail a memo that nothing is wrong with.
- **[rev] A failed model switch does not become a restart loop** — the
  supervisor comes back on the last-known-good model, and the model that failed
  is refused rather than retried.
- **[rev] A cancelled `running` job actually stops the inference**, rather than
  finishing and discarding the result while the queue waits behind it.
- **[rev] Segments survive.** A transcript for audio that is not silent arrives
  with timestamps, in milliseconds. `response_format` defaulting to `json`
  yields text and no segments, and CHRN-25's contract makes that shape *valid* —
  so nothing else in the system would ever complain.
- Measured throughput on `small.en` lands within a few percent of CHRN-24's
  **resident** column (57.9× in-container), and the number goes in
  `deploy/asr/README.md` beside the others — **[rev] measured with `-bs 5 -bo 5`
  and recorded with those parameters named.** At the server's own defaults the
  decode is greedy, which is faster, so an unpinned run would pass this
  Done-when without residency having worked.
- `/readyz` reports the resident model and refuses readiness when it is absent —
  **[rev 3]** including a standby, which names `standby` as the check.

## Rulings, settled 2026-08-28

All three accepted by magos at the recommendations, after two review passes
and the second-device discussion that produced CHRN-80 and CHRN-81.

1. **`ASR_MODEL_SWITCH_MAX_WAIT` = 60 s** — accepted knowing it is two things:
   the starvation bound for a non-resident model, and the fairness bound under
   mixed models that CHRN-29 publishes to client two.
2. **Contention is detected and logged** — per-job wall-clock against the
   worker's expected rate, warn at 2× (well under the 5× deadline), naming
   Ollama contention as the likely cause.
3. **§3's narrowed scope stands.** `asrd` guarantees single-flight
   *transcription* on the device it owns; it does not arbitrate the R9700
   against Ollama. Estate-wide admission, if ever wanted, is its own ticket.

What the accepting discussion added rather than changed: the lock names a
device, not a deployment, so a second worker is CHRN-80's protocol and not a
redesign here; and a transcript produced on the phone (CHRN-81) never reaches
this service, but does reach CHRN-22's predicate, which now owes a model floor.
