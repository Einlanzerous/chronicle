# CHRN-26 — The resident worker and the GPU lease (decision)

Status: **proposed 2026-08-28.** Three rulings at the end need magos before any
code is written.
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

## 2 · What "resident" is worth, and the framing that makes it obvious

The honest number for a 60-second memo is **28%**: 1.39 s per invocation against
1.01 s resident on `small.en` (CHRN-12; 43.6× and 57.9× re-measured through the
container in CHRN-24). Stated that way this ticket looks like a modest
optimisation, and it is worth saying why that framing is wrong.

R3 isolated the cost by transcribing a **1-second** clip: **388 ms on Vulkan**,
essentially all of it fixed per-process setup. It is a constant, not a share.
So on the memos this system actually receives:

| memo | inference at 59.6× | + 388 ms tax | per-invocation is |
|---|---|---|---|
| 5 s | 84 ms | 472 ms | **5.6× slower** |
| 30 s | 503 ms | 891 ms | 1.8× slower |
| 60 s | 1.01 s | 1.39 s | 1.28× slower |
| 40 min | 40.3 s | 40.7 s | 1.01× slower |

**The tax is paid per job, so it dominates exactly the memos there are most
of.** A voice note is far more often ten seconds than forty minutes. The 28%
figure is the number for the longest jobs, which is the case that needed this
least.

There is a second cost the table does not show. Model load alone is **145 ms for
`base.en` and 1.9 s for `large-v3`'s 3.1 GB** — so per-invocation, the upgrade
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
   34,208,743,424 bytes — 31.9 GB — and `small.en` is 466 MB resident against
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
automatically when its connection dies, which is the same property CHRN-25's
job lease relies on and for the same reason — a crashed process must not hold
the device.

It is deliberately **not** the job lease from CHRN-25. Those are different
things: the job lease says *this worker owns this job*, and the GPU lease says
*this process may run inference now*. A worker holds a job lease for the whole
of a job including the decode, and holds the GPU lease only for the inference.
Collapsing them would serialise ffmpeg behind the GPU, which is a decode that
could have happened while the previous job was still on the device.

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
`build-vk/bin/whisper-server` at `1fe009ca` — so this is a checked fact rather
than an assumption about upstream. A restart is the fallback if it turns out not
to do what its name says, and it is not painful: the same model load plus
process startup, paid only at a switch.

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

## 6 · What happens to CHRN-25's placeholder

Deleted, not extended. `internal/asr/worker.go` is one file, it is labelled a
placeholder in three places, and the shape it has — claim, shell out, write —
is not the shape of a worker that holds a model and a device lease.

**What survives unchanged, and must**: the job table, the five stored states,
the compare-and-swap claim, `RenewLease`, the reaper, and every `Store` method.
CHRN-25 built those to be the durable half precisely so this ticket could
replace the worker without touching them, and a PR here that alters the job
table is a PR that has gone wrong.

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

## 8 · When the resident process is not there

Four failures, and they want different answers.

| | |
|---|---|
| **`whisper-server` exits** | restart with backoff; jobs in flight lose their lease and are reaped back to `queued` with `attempts` incremented, exactly as CHRN-25's kill -9 path already does. Nothing new is needed |
| **it will not start at all** (no model, no device) | `/readyz` **unready**, naming the check. Jobs are still ACCEPTED — the queue is the right place for work a service cannot do yet, and rejecting submissions would push the retry into two clients |
| **the GPU is absent and it falls back to CPU** | this is CHRN-24's named nightmare, and CHRN-25 already logs the device once per process and warns on a software rasteriser. That warning moves here, to the supervised child's startup |
| **an individual job fails** | unchanged from CHRN-25: `failed` with a code, the client sees it, CHRN-28 decides about retries |

**Accepting work a service cannot currently do is deliberate.** CHRN-25's
contract has no "try later" answer, and inventing one would mean two clients
implementing a backoff against a state that is usually transient.

## 9 · Readiness reports the device, not just the database

`/readyz` currently pings Postgres and reports queue depth. It gains: whether
the resident process is up, which model it holds, and how long the current
inference has been running. A service whose GPU has gone but whose database is
fine currently reports **ready** and accepts work forever.

`/healthz` stays dependency-free. A dead `whisper-server` is not a reason to
restart `asrd` — `asrd` is the thing that restarts `whisper-server`.

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
| `ASR_MODEL_SWITCH_MAX_WAIT` | ruling 1 | how long a job for a non-resident model waits before forcing a switch |
| `ASR_GPU_LOCK_KEY` | fixed | the advisory-lock key. Named so a second deployment on one Postgres can differ |

## What each ticket inherits

- **CHRN-28** — `attempts` is incremented by the reaper, unchanged. §8's table is
  the set of failures you are writing policy for. A model that will not load is
  a deployment fault and not a job to retry; a job that failed to decode is.
- **CHRN-29** — §5's round-robin is a **promise to client two**, and the honest
  limitation is in that section: half the device under contention, not priority.
  Publish it with the numbers, because a client that expects priority will read
  fairness as a bug.
- **CHRN-22** — nothing. This ticket does not touch the durable-transcript
  predicate, and if a PR here appears to, that is the signal to stop.

## What this does not decide

- **Estate-wide GPU arbitration including Ollama.** §3, at length. If it is ever
  wanted, it is an admission service and its own ticket, and this document is
  the argument for why it was not smuggled in here.
- **Batching several memos into one inference.** whisper.cpp accepts repeated
  `-f`, and CHRN-12's harness used it to amortise startup — which is exactly the
  cost residency already removes. Nothing left to buy.
- **A second worker process or a second GPU.** The lease is built so that adding
  one is a configuration change rather than a redesign, and there is no second
  device to put it on.
- **CPU fallback.** CHRN-12 is clear that above `small.en` the CPU stops being a
  fallback (1.4× for `medium.en`, 0.6× for `large-v3`). A queue that waits is
  better than a queue that takes 101 seconds per minute of audio.

## The three rulings this needs

1. **`ASR_MODEL_SWITCH_MAX_WAIT`: what number?** The recommendation is **60 s**.
   Long enough that a burst of same-model jobs is not interrupted for one
   outlier; short enough that nobody waits on a memo without an explanation. The
   switch costs at most 1.9 s, so the knob is bounding a wait, not a cost.

2. **Should contention be detected and logged?** The recommendation is **yes,
   cheaply**: record inference wall-clock per job against the model's CHRN-24
   reference and log once, at warn, when a rolling median drifts past a
   threshold — naming Ollama contention as the likely cause. §3 gives up
   arbitration; giving up *visibility* as well would mean the first symptom is a
   timeout somewhere else. The cost is a few lines and one number per job.

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
- Measured throughput on `small.en` lands within a few percent of CHRN-24's
  **resident** column (57.9× in-container), and the number goes in
  `deploy/asr/README.md` beside the others.
- `/readyz` reports the resident model and refuses readiness when it is absent.
