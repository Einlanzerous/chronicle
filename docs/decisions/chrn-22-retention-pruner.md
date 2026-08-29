# CHRN-22 — The retention pruner (decision)

Status: **accepted 2026-08-29 by magos, at the recommendations on every ruling,
and IMPLEMENTED the same day.**
Two amendments were owed at acceptance and are folded in, marked **[rev]**: §3's
status enum had no `pruned` case, and §1's fixture hazard needed a test rather
than a paragraph. The PR that follows is mechanical against this document, and
code that contradicts a ruling is a 🔴.

Ticket: CHRN-22 (Phase P1, parent CHRN-3). Tier `opus`, **and** one of
`CLAUDE.md`'s five Mode C tickets — so this is Mode B's artefact *and* the PR
that follows it is read line by line. Both, not either: the decision is where a
wrong predicate is cheap to find, and the diff is where a wrong `WHERE` clause
is.

Decision owner: magos.

Read by: **CHRN-28** (a failed transcription is a memo this job will not prune,
forever — §2), **CHRN-81** (the model floor is what stops a phone transcript
deleting audio — §1), **E8** (the label §3 defines), and whoever next touches
`internal/upload` (§5 deletes a warning it left behind).

## Context

This ticket was moved from E2 to E3 on 2026-08-27 **because the guard could not
be written**: there was no transcripts table anywhere in the repo, and *durable*
had no shape to test against. Building it then would have meant inventing the
predicate E3 would later have to match, in the one component whose bugs are
unrecoverable.

That blocker is gone, and most of what follows is **inheritance rather than
invention**:

- **CHRN-27** shipped `tier2.transcripts` with `partial`, `model` and `backend`.
- **CHRN-25 §5** settled what a completed run means, and **CHRN-26** proved it
  against a real one.
- **`store.HasDurableTranscript` already exists** (`internal/store/transcript.go:217`)
  and its docstring names this ticket: *"THE GATE, and CHRN-22 may use nothing
  else."*
- **CHRN-23** settled the path, the immutable clock and deletion-as-unlink.
- **CHRN-18 §6** settled that the pruner must not read `state`.

Four things are genuinely open. They are the rulings at the end; everything else
here is written down so the Mode C review has something to check the diff
against rather than re-deriving it from five documents.

### One thing that has changed since the move, and it starts a clock

The argument for moving this ticket was that **waiting cost nothing**, measured:
`CHRONICLE_AUDIO_DIR` was not set on the deployed container and `/data/chronicle`
did not exist. Both are now true the other way — SERV-151 gave Chronicle its
capture config, and `/data/chronicle/{audio,inbox}` exist on the box.

The corpus is still empty (`audio/` holds no author directory), so nothing is
accumulating *yet*. But the window is no longer open-ended: **it closes thirty
days after the first memo lands.** Not urgent today; not indefinite either.

## 1 · The gate is `HasDurableTranscript`, and the model floor goes INSIDE it

The predicate exists and is already correct about three things (`transcript.go:199-216`):
it reads Chronicle rather than the ASR service, whose results are gone at seven
days while this fires at thirty; it is `NOT partial` and never
`covered_ms >= audio_duration_ms`; and **empty text counts**, because a silent
memo has a true and complete answer.

What it does not yet have is a **model floor**, and CHRN-26's review found why it
needs one: a transcript produced on the phone (CHRN-81) would satisfy
`succeeded AND NOT partial` by the letter, so this pruner would delete the only
copy of a recording on the strength of a `base`-grade guess.

### It is one predicate, not two

**The floor goes into `HasDurableTranscript`. There is no second
`HasPrunableTranscript`.** The docstring already forbids the alternative in as
many words, and the reason is mechanical: **the transcription pump calls the same
function to decide whether to skip ASR** (`internal/transcribe/transcribe.go:212`
and `:249`). Two predicates that can disagree produce one of two failures, and
both are silent:

- the pump skips on a device transcript and the pruner refuses it → the audio is
  kept forever and **the server pass never runs**;
- the pruner deletes on a device transcript the pump would have replaced → the
  audio is gone and there is nothing left to transcribe properly.

One predicate, both callers, and CHRN-81's *"server pass becomes primary"* then
works for free: a memo whose only transcript is below the floor stops being
skipped and is submitted for a proper run.

**That is a behaviour change to CHRN-27's pump, shipped under a CHRN-22 diff, and
the Mode C reviewer should expect it rather than find it.** It is inert today —
no device transcript exists — but it is deliberate and it is the mechanism, not a
side effect.

### Quality, not provenance — and the string is not what you would guess

The CHRN-22 comment said *"which models count as server-grade"*; CHRN-81 said
*"good enough to read but not good enough to be the only copy."* Those are
different tests and the second one is right.

**Provenance alone is wrong**, and this epic supplies its own counterexample:
CHRN-3's CPU fallback is `base.en` and only `base.en`. Under a "ran on the
server" rule that transcript prunes, and it is precisely the one that should not.
A model is a property of the model, not of the machine.

**But quality alone is not sufficient either, and the reason is in the data.**
`tier2.transcripts.model` does not hold `small.en`. It holds what the ASR result
carried, which the worker builds as `"whisper.cpp/" + job.Model`
(`internal/asr/worker.go:217`) — so the stored value is **`whisper.cpp/small.en`**,
runner-qualified. Two consequences, and the first is the kind of bug this
ticket's whole review posture exists to catch:

1. **`model = ANY(ARRAY['small.en', …])` matches nothing.** The pruner would
   never fire, forever, and every test written against a hand-inserted
   `small.en` fixture would pass. Correct against its fixtures, wrong against the
   corpus — the exact failure the E2→E3 move was made to avoid, arriving through
   a different door.
2. **The string already carries the runner**, so the floor can be honest about
   both axes instead of pretending there is one.

So the gate is **two-part**:

```
model quality   the name after the runner: small.en, medium.en, large-v3.
                Never base, base.en, tiny, tiny.en — at or above the default.
runner known    the name before it: whisper.cpp today.
```

A runner this deployment has never measured is not trusted to have produced what
its model name claims — a quantised CoreML `small.en` is the same *model* and not
necessarily the same *transcript*. What that buys, checked case by case:

| transcript | prunes? | |
|---|---|---|
| `whisper.cpp/small.en` on the R9700 | **yes** | the ordinary case |
| `whisper.cpp/base.en` (CPU fallback) | **no** | fable's case — provenance alone would have deleted it |
| `whisper.cpp/small.en` on the desktop (CHRN-80) | **yes** | a second device is not a lesser one |
| `whisperkit/small.en` on the phone (CHRN-81) | **no** | which is CHRN-81's own recommendation |

Adding a runner is a deliberate act with a Mode C review attached, which is the
correct amount of friction for widening the set of transcripts that may delete
audio.

### [rev] The prefix is a cross-binary string, so a literal fixture cannot prove the floor

The hazard this section found does not stop at "write the predicate correctly".
`whisper.cpp/` crosses the HTTP contract **from another binary**: `asrd` builds
it, Chronicle stores whatever arrives. If `asrd` ever changes that string —
renames the runner, drops the prefix, adds a version — the pruner silently stops
firing, and **every test written against a hand-inserted `whisper.cpp/small.en`
literal keeps passing.** That is the same failure one level up: a fixture that
agrees with the code and with nothing else.

So the floor's positive case is proven by a transcript that **arrived through the
real pump path** — `internal/transcribe`'s integration test, or the fake
`whisper-server` in `internal/asr` — being handed to `HasDurableTranscript` and
satisfying it. Literals are fine for the six refusal cases, because a refusal
that is wrong keeps audio. A literal proving the *acceptance* case is the bug
this section describes, shipped as a test.

### Where the list lives

**In Go, beside the predicate, as a constant. Never in a `CHRONICLE_*`
variable.** A config knob that widens the set of transcripts sufficient to delete
audio is a change to the worst thing this system can do that no review ever sees.
`internal/store/` is already in `sensitive_paths`, so a change to it is reviewed
at the expensive tier — which is the whole point of putting it there rather than
in the environment.

## 2 · The pruner does not read `state`, and that costs something worth naming

Inherited, not decided here. CHRN-18 §6 hands it over in one line:

> **And the rule this hands to CHRN-22: the pruner must not read `state`.** Its
> gate is a durable transcript, full stop. A destructive job must not rest on a
> second, softer fact, and a bug in the state machine must not be able to become
> data loss.

So `discarded` is not special. A discarded memo's audio goes at thirty days if it
has a durable transcript, and its transcript stays — which is intended rather
than an oversight: CHRN-18 §6 makes discarding survivable precisely because *"the
row stays, the transcript stays, the memo is inert… recoverable by a human with
SQL, and by no automated path."*

**The gap that follows is real and this document accepts it.** A memo discarded
*before* transcription is never picked up by the pump —
`MemosAwaitingTranscription` selects `state IN ('captured','queued')`
(`internal/store/memo.go:561`) — so its gate can never be satisfied and **its
audio is kept forever, under the one setting where the person explicitly said to
throw it away.**

Accepted, for three reasons. It errs in the safe direction, which is the only
direction this job is allowed to err in. The non-optional rule has no exception
clause, and writing the first one into the destructive job is how exceptions
start. And the volume is trivial: a discarded memo is one somebody tapped away in
the seconds after recording it.

**What it owes instead is visibility.** The dry run reports these as their own
count — `held back · no durable transcript` — so the number is a thing an
operator can see rather than a silence. A human-driven "purge discarded" is a
different job with a different safety argument and belongs to its own ticket.

## 3 · Retention is a STATUS, not a date

The Done-when says *the date the UI shows is the date the job uses*. For a memo
with no durable transcript **there is no date the job will use** — the label
`PRUNES 2026-09-20` passes and nothing happens.

"Eligible from" is not an acceptable reading of that label. CHRN-25 §5 already
used the PRUNES label as the argument for the empty-text ruling — a silent memo
must prune, or the label lies. It cannot be allowed to lie in the other direction
to save a sentence here.

**One function computes a memo's retention status, and the pruner is that
function evaluated over the table.** It returns a status, not a date:

```
scheduled(at)          a durable transcript exists and retention is days_30
pinned                 retention is forever
awaiting_transcript    no durable transcript yet — prunes when transcribed
discard_pending        retention is discard_now and the gate is satisfied
pruned(at)             audio_pruned_at is set; the audio is already gone
```

**`pruned` is not optional and it is checked first.** [rev] It is the state the
pruner itself produces, so a status function without it has an undefined case on
exactly the rows this job creates — and the memo whose audio is gone is the one a
person is most likely to be looking at when they wonder what happened to it.

`discard_pending` rather than `discard_now`: the retention *value* is
`discard_now`, and a status sharing its name would make "which one does this
mean" a question at every call site.

The UI renders `PRUNES <date>` only for `scheduled`, `PRUNED <date>` for
`pruned`, and `PRUNES WHEN TRANSCRIBED` for `awaiting_transcript` — which also
tells a person that pinning ahead of time is available and useful. Then *"the
date the UI shows is the date the job uses"* holds **by construction** rather
than by a test that compares two implementations, in the same way
`audio.ProjectionWindow` already makes the interval one number.

E8 is unbuilt, so this ticket's "UI" is a field on the memo's JSON plus the dry
run's output. The shape is what matters; the rendering is E8's.

**[impl] The JSON half is deferred, and this records it rather than leaving it
to be noticed.** `store.RetentionStatus` ships and is tested — it is the shape,
and it is the same clause the sweep evaluates. What does not ship is a field on
a response, because **there is no `GET /memos/{id}`**: the only memo JSON today
is the upload path's, where a memo has just been captured and its status is
always `awaiting_transcript`. A per-upload query to render a constant is
ceremony. The field is one line on the endpoint that will carry it, and E8 is
the ticket that adds both.

### A late transcript prunes with no date ever shown, and that is accepted

A memo transcribed on day 40 — held, then retranscribed — is eligible the moment
the transcript lands. Under the conditional label it never showed `PRUNES` at
all, and then the audio goes at the next sweep.

The alternative is a second clock, `max(captured_at + 30d, transcribed_at +
grace)`. **Rejected.** CHRN-18 and CHRN-23 spent real effort making the clock one
immutable column precisely so that no later event can move a deadline onto today,
and a grace period is a second input to a function that currently has three.
`awaiting_transcript` reading *"prunes when transcribed"* is honest about it, and
a person who cares can pin.

## 4 · Mark, then unlink — and the mark re-evaluates the whole predicate

**Order.** Mark the row, then unlink the file. Never the reverse. A crash after
the mark leaves a file with no memo pointing at it, which is an orphan: the
storage report already counts it (`internal/api/storage.go:76-78`), it is the
only actionable list CHRN-23 kept, and the pruner may simply retry. A crash after
an unlink whose mark never landed leaves a memo claiming audio that is gone —
which is indistinguishable, from every side, from data loss.

**The mark is a compare-and-swap over the entire predicate**, not an update
following a check:

```sql
UPDATE tier2.memos
   SET audio_pruned_at = now()
 WHERE id = $1
   AND audio_pruned_at IS NULL
   AND retention <> 'forever'
   AND <the window clause for this row's retention>
   AND EXISTS (SELECT 1 FROM tier2.transcripts
                WHERE memo_id = $1 AND NOT partial AND <the model floor>)
RETURNING id
```

Unlink **only if a row came back**. That closes the read-then-delete window where
a person pins a memo between the check and the unlink — the interval in which the
audio is still there and the decision to keep it has already been made. It is the
same shape `AdvanceMemoState` already uses (`internal/store/memo.go:374-379`):
the predicate is in the `WHERE`, and `RETURNING` empty means somebody else got
there first.

**The dry run is that identical `WHERE` under a `SELECT`, from one SQL constant
shared by both.** Then *"a dry run lists exactly what a real run would delete"*
is met by construction rather than asserted by a test comparing two queries that
can drift apart in the next PR.

## 5 · Re-upload of a pruned memo is refused, and `audio_pruned_at` is monotone

CHRN-20 §6 handed this over unresolved: the file lands, the row still carries
`audio_pruned_at`, and the storage report counts the recording as an orphan.

The tempting fix — clear the column, let the audio come back — does not survive
contact with the clock. `captured_at` is immutable (`CH002`), so a memo captured
sixty days ago that is re-uploaded today is **already past its deadline**: the
column clears, the file lands, and the very next sweep deletes it again. That is
not a bug in the sweep. It is the correct answer to *"is this memo's audio past
its retention?"*, and a re-upload is not new information about `captured_at`,
`retention` or the transcript.

**So: a memo's audio is delivered once. Once pruned, pruned.** Adding "unless
re-uploaded" would make the re-upload a fourth input to a function CHRN-20 §6
deliberately closed against exactly this: *"`captured_at` is the clock, and it is
immutable so that a re-delivery cannot move a prune deadline onto today."*

Three concrete changes, and the first is smaller than it sounds:

- **The upload shortcut splits on `audio_pruned_at`, not on a new concept.**
  `POST /memos/uploads` already shortcuts when the author holds those bytes
  **and the file is on disk** — the gate is on the FILE, not the memo row, and
  there is a test named for that (`internal/upload/upload_test.go:519-544`). The
  file-absent branch is also the crash-recovery path (`:914`), so it splits
  rather than moves: `audio_pruned_at IS NULL` → accept and heal, exactly as
  today; `NOT NULL` → answer as a duplicate and transfer nothing.
- **The watcher gains a row lookup before the copy.** It writes file-then-row
  (`internal/watch/ingest.go:108,149,153`), so today a rescan of a pruned memo's
  file re-creates the audio on disk before `IngestMemo` ever runs. A read is
  cheap; the only race is against a concurrent prune, which leaves an orphan the
  report already catches.
- **CHRN-20 §6's warning becomes unreachable and is deleted** in the same PR
  rather than left as a comment describing a state that can no longer occur.

This posture is already half-shipped and worth noticing rather than re-deciding:
`MemosAwaitingTranscription` carries `audio_pruned_at IS NULL`, so a pruned memo
never re-enters the pump either.

**The case this loses**, stated because it is the honest cost: a person who kept
the original file, wants the audio back, and re-uploads it carrying `forever`.
Under "refuse", the retention ratchets to `forever` on audio that is already gone
— honest, just late — and recovery is SQL plus a file copy, the same posture as
un-discard. The compromise ("refuse unless the arrival carries `forever`") is
recorded here and **not recommended**: a special case on the one path whose
failure mode is deleting authored bytes buys back a case a human can already fix.

## 6 · Where it runs, and how often

**In-process, on a ticker, in the `serve` process** — the shape the reaper
(`internal/asr/reaper.go`) and the upload sweep already use. A cron entry outside
the binary is a second thing that can be missing on a box where the service is
running fine.

**`chronicle prune -dry-run` is the same code path with the `SELECT`.** It is the
operator's surface and the Done-when's, and it must be a subcommand rather than
an endpoint: a destructive job's rehearsal should not be reachable over HTTP.

**A new package, `internal/retention/`.** `REVIEW.md` §7 requires this PR to add
its own package to `sensitive_paths` in `.github/workflows/pr-review.yml`, and
its absence is a 🔴 finding — every later PR touching the pruner would otherwise
be reviewed at the cheap tier, silently. A new package is also the honest place:
`internal/audio/` computes where a file lives, and the code that deletes it
should be nameable on its own line.

**DISCARD NOW means "at the next sweep", and the interval is what makes that
true.** One hour by default. The ticket says *immediately*; a sweep interval is
the honest reading of immediately in a service that does not act on a write, and
an hour is short enough to mean it and long enough that the query is free.

## What this does not decide

- **Deleting transcripts.** Never, at any age, under any setting. The asymmetry
  is the design and there is nothing here to configure.
- **Purging a discarded memo's audio** — §2. It needs a different safety
  argument, because it is the one case with no durable transcript to gate on. Its
  own ticket.
- **Anything about `audio.StagingDir`.** CHRN-20 §6 is explicit that the upload
  sweep is not this pruner and that staging is not corpus.
- **A refcount, of any kind.** CHRN-23 §2 settled the layout so that this job is
  an unlink and never an arithmetic problem. If a diff here computes how many
  memos share a file, that diff has gone wrong.
- **Whether CHRN-81's runner is trusted.** §1 refuses it by default; admitting it
  is that ticket's decision, taken with a Mode C review, and it is a one-line
  change here by construction.

## The rulings, settled 2026-08-29

All accepted at the recommendations below.

1. **The model floor set.** Recommendation: quality `small.en`, `medium.en`,
   `large-v3` — at or above the default, never `base`/`tiny` — and runner
   `whisper.cpp` only. §1 has the case-by-case table.
2. **The discarded-before-transcription memo keeps its audio forever.**
   Recommendation: **accept**, and report the count in the dry run. The
   alternative is the first exception clause in the one job that cannot be
   undone.
3. **A late transcript prunes without ever having shown a date.**
   Recommendation: **accept**, with `awaiting_transcript` reading "prunes when
   transcribed". The alternative is a second clock over an immutable column.
4. **Re-upload of a pruned memo is refused.** Recommendation: **refuse**, and do
   not take the `forever`-carrying compromise. §5 has both.

## Done when

- **A dry run lists exactly what a real run would delete**, because both read one
  SQL constant — not because a test compares two queries.
- **A memo with no durable transcript is never in that list**, and a memo whose
  only transcript is below the model floor is not either.
- **A pinned memo is never in that list**, and pinning between the check and the
  unlink is not a race the audio loses — the mark is a compare-and-swap over the
  whole predicate.
- **The date the UI shows is the date the job uses**, because the same function
  produced both, and a memo with no date shows a status instead of a lie.
- **The transcript survives every path**, including `discarded`, including
  `discard_now`, including a pruned memo that is re-uploaded.
- **A crash between the mark and the unlink leaves an orphan and never a memo
  that claims audio it does not have** — asserted by a test that kills between
  the two.
- **Re-uploading a pruned memo transfers nothing** and does not clear
  `audio_pruned_at`, by either path — upload or watcher.
- **`internal/retention/` is in `sensitive_paths` in the same PR.**
