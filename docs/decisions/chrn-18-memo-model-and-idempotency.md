# CHRN-18 — The memo model and the idempotency rule (decision)

Status: **proposed 2026-08-25 — awaiting sign-off. No code until this is accepted.**
Amendments from Fable's review of the first draft are marked **[rev]**. They close
two holes in the two properties this ticket exists for — a hold that became
un-releasable after thirty days, and concurrent same-key retries erroring instead
of collapsing — and settle five interactions with CHRN-19/20/21/22/71 that the
first draft left for the implementing PR to discover. None of them changes the
identity rule or the shape of the schema.
Ticket: CHRN-18 (Phase P1, parent CHRN-2). Tier `opus`, so Mode B: this document
is the review artefact and the PR that follows it is mechanical.
Decision owner: magos.
Read by: CHRN-19, CHRN-20, CHRN-21, CHRN-22, CHRN-23, and every later epic — the
ticket's own words are *"every later epic reads this."*

## Context

Two ingest paths converge on one table. Copyparty drops a file into a watched
folder on the NVMe; the Chronicle app uploads to an authenticated, resumable
endpoint. A retrying mobile queue and a sync client that rescans a folder mean
**the same audio arrives twice in normal operation, not as an edge case.**

Get the rule wrong and the corpus fills with near-duplicates, each transcribed,
routed and proposed separately, and the operator triages the same thought twice —
which is the tax this system exists to remove. There is no oracle for this: it
fails silently and accumulates. That is the whole reason the ticket is `opus`.

There is a precedent in the repo, and it is a cautionary one. `docs/salvage/vox-dictate/create-ticket.md`
records that vox-dictate keyed on *(source file, routing decision)* — so the same
Drive file re-parsed to a different project created a second ticket. The salvage
note is explicit about the fix: *"the drive file id alone is the key and the
fingerprint has to go."* The general lesson is the one this decision turns on:
**do not fold a derived, revisable decision into an identity.**

## Decision

**A memo is identified by the SHA-256 of the bytes that arrived, scoped to its
author.** The client-supplied idempotency key is not a second identity — it
identifies an *arrival attempt*, which is a different thing that a content hash
cannot do at the moment the question is asked. Arrivals are their own table, so
*"the same file delivered by both paths, twice each, produces one row"* is a
sentence the schema can state and a test can read back: one memo, four arrivals.

The state machine is enforced by a `BEFORE INSERT OR UPDATE` trigger, so an
illegal edge is impossible from `psql`, from a future MCP write scope, and from
a Go bug — not only from the repo layer.

## 1 · Why the content hash is the identity, and what exactly is hashed

**The hash is taken over the bytes exactly as they arrived, before normalisation,
and never recomputed.**

Hashing the normalised WAV instead would make identity a function of CHRN-21's
ffmpeg flags: change a flag and every memo in the corpus is a different memo.
That is the vox-dictate mistake with a different derived artefact in the slot.
The bytes the person's device produced are the authored fact; everything
downstream is derived from them.

It is also the only identity **the Copyparty path can compute at all.** A file in
a watched folder carries no session, no key and no client intent — only bytes.
Since the cross-path duplicate (a memo both synced to Copyparty *and* uploaded
by the app) is the exact case the ticket names, the identity has to be something
both paths can produce, and the hash is the only candidate.

Cost is not a consideration: streaming SHA-256 over a 5 MB memo is single-digit
milliseconds, and over the entire 4.1 GB salvaged corpus is about fifteen
seconds, once.

**Scoped to the author, not global.** `UNIQUE (author_id, content_hash)`. If two
accounts deliver identical bytes — one person forwards another's recording — a
global constraint would collapse them and attribute one person's memo to
somebody else. Tier 2 exists to record what a person said; silently
re-attributing it is the worst kind of corruption because it looks like data.

**`author_id` is `NOT NULL`.** There is no unattributed memo. This puts a
requirement on CHRN-19 that its description does not settle: the watched root
gets **one subdirectory per account**, the watcher resolves the author from the
subdirectory, and a file dropped at the root belongs to the owner. CHRN-19 owns
the mechanism; this decision owns the constraint that there must be one.

**Known and accepted gap.** Bytes are not audio. A recorder that re-containers
the same recording with a fresh timestamp in the Ogg header produces different
bytes and therefore a second memo. Closing that would mean decoding to PCM
before we can even name the row — inverting the dependency on CHRN-21, spending
its measured 152 ms of decode on every arrival, and pulling a ~100 MB non-Go
dependency into an identity path. Not worth it. Erring toward two rows is
recoverable by merging; erring toward one row is not.

## 2 · Why the idempotency key is not redundant — it does a job the hash cannot

The hash identifies **completed** audio. CHRN-20 is a resumable upload from a
phone on mobile data: at the moment the server is asked *"have you already got
this?"*, the last byte has not arrived and the server cannot have computed a
hash. Only a key the client chose before it started can answer that.

| | answers | available |
|---|---|---|
| `content_hash` | *is this the same memo?* | **[rev]** only once the bytes are complete **server-side** |
| `idempotency_key` | *is this the same attempt?* | from the first byte, on the upload path only |

Matching the estate: the header is `Idempotency-Key`, as in the Switchyard call
vox-dictate made. Same name, same namespace, one meaning.

**[rev] Two deliberate departures from Switchyard's implementation**
(`switchyard/server/src/lib/idempotency.ts`), stated so they read as choices
rather than as a half-copied pattern. Switchyard scopes a key to
`(user_id, method, path, key)`, expires it after 24 hours, and on replay returns
the original status and body verbatim without comparing the new request to the
old one. Chronicle scopes to `(author, key)`, **never expires**, and answers a
mismatch with **409**.

Both follow from what is being made idempotent. Switchyard is deduplicating an
*HTTP call*, where a day is longer than any sane retry window and the useful
answer is the response that was already sent. Chronicle is deduplicating a
*recording*, whose identity has to outlive a phone that was offline for a week
— an expiring key would silently start creating second memos at exactly the
moment the queue was longest. And there is no cached response worth replaying
here: the answer is the memo row, always recomputable, so the bytes are
available to compare and comparing them is strictly safer than not.

### [rev] The probe takes a key *or* a hash

"Server-side" is the load-bearing qualifier, and the first draft dropped it. The
*phone* holds the whole file and can SHA-256 5 MB in milliseconds. So CHRN-20's
probe accepts either handle:

- **by key** — "is attempt `K` already complete?" The retry case.
- **by hash** — "do you already hold these bytes, from anywhere?" This is what
  makes the Copyparty-then-app case free. The first draft named that case as
  *the* case and then left the app to upload 40 MB and discover the collapse at
  the end, which is correct and not free.

### [rev] The key's shape, and what a client does with a 409

The key is a **fresh random UUIDv4 minted per recording**, and is never derived
from anything recyclable — not a filename, not a device id plus a counter, not a
timestamp bucket. The unique index is permanent and key reuse is refused
(below), so a derived key that can ever recur is a permanent brick: that
recording can never be uploaded.

A client that receives 409 mints a fresh key and retries. A 409 is only ever
raised when the bytes differ, so that produces a second memo — the accepted gap
in §1, and the correct outcome: two rows for two recordings.

### Resolution order, and the one case that is an error

An arrival carries *(author, source, key?, hash)*. In one transaction:

0. **[rev] When there is a key, take `pg_advisory_xact_lock` on
   `(author_id, key)` first.** See §9 — without it two concurrent retries of the
   same key both miss step 1 and the loser gets a unique violation instead of the
   memo, which breaks *"retries are free"* in exactly the case the phrase exists
   for.
1. **Key.** An existing arrival for `(author, key)` short-circuits: return its
   memo, touch nothing.
2. **Then the hash.** `INSERT … ON CONFLICT (author_id, content_hash) DO UPDATE
   … RETURNING id` — one statement, so two concurrent arrivals cannot both
   insert. **[rev]** `DO UPDATE` rather than `DO NOTHING` because the ratchet
   (§5) needs somewhere to run: it is a conditional `DO UPDATE … WHERE` that
   fires only when an arrival's retention opinion actually outranks the row's.
   When it does not fire, `RETURNING` is empty and the id comes from a plain
   `SELECT` — which §9 explains is safe, and which the first draft wrongly gave
   as the *reason* for choosing `DO UPDATE`. Either form waits out a competing
   transaction, so neither choice is about the race.
3. **Record the arrival** against that memo, with its source and key.

The consequence worth stating out loud: a memo ingested from Copyparty with no
key, then uploaded by the app with key `K`, is found by hash — and `K` is
recorded on the new arrival row. Every subsequent retry of `K` now short-circuits
at step 1. The two paths converge without either knowing about the other.

**Key reuse with different bytes is a 409, never a second row and never an
overwrite.** If `(author, K)` exists against hash `H1` and an arrival presents
`K` with `H2`, the client has re-used a key that is supposed to name one
recording. That is a client bug, and the only safe answer is to report it: a new
row would defeat the rule, and an overwrite would destroy authored audio on the
strength of a mistake.

## 3 · Arrivals are their own table

The ticket says *"the `memos` table"*, singular. This proposes two, and the
reason is that the ticket's own success criterion is a statement about arrivals:
four deliveries, one memo.

- A `source` column on the memo would have to pick a winner for a memo that came
  by both paths — a column that is wrong half the time it matters most.
- When a duplicate shows up in production, the arrival rows say which path
  delivered it and when. Without them the failure this ticket exists to prevent
  is diagnosed by guessing.
- `(author_id, idempotency_key)` belongs on the attempt, not on the memo, because
  a memo can legitimately accumulate several.
- It gives `captured_at` an unambiguous definition: **the first arrival.**

`memo_arrivals` is **tier 2**. It is not regenerable — once a delivery has
happened nothing rebuilds the record of it — and a tier-1 table holding a foreign
key into `tier2.memos` would be the cross-schema write path the CHRN-71 decision
already ruled out.

### [rev] An arrival row means a delivery, not a scan

The first draft said the watcher *"will see that file on every rescan forever"*
and then wrote an arrival row plus a memo `UPDATE` for every sighting. Arrivals
would grow as files × scans, `updated_at` would stop meaning "last authored
change", and E2's exit criterion — *"re-delivery is a no-op"* — would be false in
the ordinary case. Three things fix it, at three layers:

1. **Keyless arrivals are unique on `(memo_id, source, source_ref)`** (§8). A
   repeat sighting of the same path for the same bytes cannot create a row.
2. **The memo upsert only updates when something actually changes** (§9), so no
   trigger fires and `updated_at` does not move.
3. **CHRN-19 must not re-read a file it has already ingested.** A durable
   seen-ledger keyed on `(path, size, mtime)`, so a rescan does not re-hash the
   corpus every poll.

That ledger is **tier 1** — genuinely derived and genuinely disposable: delete it
and the next scan rebuilds it by re-hashing, which §1 measures at about fifteen
seconds for the whole corpus. It records observed file identity → content hash
and holds no foreign key into `tier2.memos`, so the cross-schema rule is intact.

**[rev] And the decision under it: the watcher observes, it never consumes.**
The obvious alternative — atomically move each file out of the watched folder
into CHRN-23's layout — makes re-delivery structurally impossible and is wrong
here. The folder is fed by a sync client the estate does not control. Moving a
file out from under a two-way sync either causes it to be re-pushed (a loop) or
propagates the removal back to the phone, which deletes the person's own
recording. That is the failure mode this whole system is built to avoid, arrived
at from a different direction.

**[rev] `source_ref` is therefore load-bearing, not merely diagnostic.** It is
the watcher's handle for a keyless arrival: the watched path for Copyparty, the
upload session id for the app. It is still never used to derive a storage path —
CHRN-23's layout stays a pure function of `content_hash` — but it is now inside a
unique index, so it must be stable and non-null for the Copyparty path, and §8
constrains it accordingly.

## 4 · `captured_at` is immutable, and the trigger enforces it

CHRN-22 renders `PRUNES 2026-09-20` beside the audio. That label is a promise,
and **a promise computed from a mutable column is a promise that quietly
changes.** A sync client that re-delivers the same file weekly would otherwise
walk the prune date forward forever if the clock ran from the latest arrival, or
from `updated_at`.

So `captured_at` is set once, from the first arrival, and the trigger rejects any
`UPDATE` that moves it — alongside `author_id`, `content_hash` and `byte_size`.
CHRN-22 gets a clock it cannot accidentally be wrong about, from the schema
rather than from a convention.

**[rev] It is `DEFAULT now()` and is never supplied by the caller.** The first
draft defined it as "the first arrival" and then passed it in as an ingest
parameter, which would have let the watcher pass a file's mtime while the upload
path passed `now()` — the unambiguous definition evaporating in the first PR.
Server time, assigned by the database, monotone, not a client's opinion.

**[rev] One thing that follows, and should not surprise anyone later.** The epic
uses "capture" for the moment of *recording*. These are the same instant for a
live upload and are not for a memo that sat in an offline queue for a week: that
memo gets a week more audio life than `PRUNES <date>` implies. This is the safe
direction and it is deliberate. If the UI ever wants to show recording time, that
is a nullable client-asserted `recorded_at`, display-only, added later — never
this column, and never the prune clock.

## 5 · The retention ratchet: a re-delivery may only lengthen the life of audio

`retention` is `discard_now` | `days_30` (default) | `forever`, carried from
capture per CHRN-20 so the decision made at the moment of recording survives the
trip.

Two arrivals of the same bytes can disagree. **The more conservative value wins,
and an arrival may only ratchet toward keeping.** A re-delivery must never be
able to shorten the life of audio that is not regenerable.

**[rev] Only a real opinion may ratchet — a default may not.** The first draft
gave every Copyparty arrival the `days_30` default, so this happened: a person
picks DISCARD NOW in the app, the same file also syncs into the watched folder,
the folder's *default* outranks the person's *choice*, and the audio silently
gets thirty days it was told not to have. Harmless in direction and wrong in
principle — this document treats an authored choice as sacred everywhere else.

So a Copyparty arrival carries **no retention opinion at all** (`NULL`), the
column keeps its `NOT NULL DEFAULT 'days_30'` for the insert path, and the
ratchet is a no-op when the arrival has nothing to say. Only real opinions
compete.

**[rev] And the ratchet does not run on a discarded memo.** §6 says a re-arrival
matching a discarded memo *"changes nothing"*; the first draft's `CASE` had no
`state` guard, so an arrival carrying `forever` would have changed its retention
— contradicting the prose and failing Done-when #6 as written.

```sql
ON CONFLICT (author_id, content_hash) DO UPDATE
   SET retention = $4
 WHERE $4 IS NOT NULL                        -- a default is not an opinion
   AND tier2.memos.state <> 'discarded'      -- discarded memos are inert
   AND tier2.retention_rank($4) > tier2.retention_rank(tier2.memos.retention)
```

The `WHERE` is what makes a no-op re-delivery write nothing (§3). It references
`$4` rather than `EXCLUDED.retention` deliberately: `EXCLUDED` carries the
coalesced insert value, so a NULL opinion would arrive as `days_30` and ratchet
after all — the bug this amendment exists to remove.

The ratchet lives in the ingest statement, not in the trigger, because it is true
of *arrivals* and not of every writer: a person changing their mind to DISCARD
NOW in the UI is an authored act on the memo and is allowed to move it in either
direction.

Note also the distinction the epic makes and this schema keeps apart: `retention
= 'discard_now'` governs the **audio file** and still waits for a durable
transcript — CHRN-22's non-optional rule — whereas `state = 'discarded'` means
the person threw the **memo** away. Blurring the two is how audio gets deleted
before it was ever transcribed.

## 6 · The state machine, and two places this reads the ticket rather than quotes it

Seven states, one entry point, and every legal edge in one function.

```
                    ┌──────────────────────────────────────────┐
captured ──▶ queued ──▶ transcribing ──▶ transcribed ──▶ triaged │
                ▲            │                                  │
                └── retry ───┘                                  │
   any of the five above ──▶ held ──▶ queued                    │
   any of the six above  ──▶ discarded  (no edge out)  ◀────────┘
```

**Departure 1 — `held` is terminal for the automated pipeline, not absolutely
terminal.** The ticket lists `held` and `discarded` as terminals. A hold nothing
can leave is a memo that can never be rescued, which would make E4's `HOLD` worse
than the vox-dictate behaviour it improves on — the salvage note's whole argument
for `HOLD` is that an uncertain memo waits *instead of* a wrong ticket existing.
So `held → queued` is legal, by a person's action. Nothing automated makes it.

`held → transcribed` is deliberately **not** legal. Allowing it would let an
operator declare a never-transcribed memo transcribed, and the only way to police
that in a trigger is a cross-table check against a table E3 has not built.

### [rev] The hold that could not be released — and what closes it

The first draft justified the single exit by saying re-entering at `queued` costs
one second of re-transcription (CHRN-12: 1.0 s for a 60-second note, model
resident). That reasoning assumed the audio was still there. It need not be:
`transcribed → held` is legal, so a memo held at the *routing* stage has a
durable transcript, and this document's own §6 rule tells the pruner not to read
`state` — so at thirty days that memo's audio is deleted, correctly. Its only
non-discard exit then required an input that no longer exists. **A memo held
longer than the prune window could only ever be discarded**, which is precisely
the outcome `HOLD` exists to prevent.

The first draft also contradicted itself on the way past: *"a memo held at the
routing stage has a transcript already"* against *"a `held` memo has no
transcript"* two paragraphs later. The second sentence was reasoning about the
transcription-failure hold only. It is wrong as a general claim and is withdrawn:
**a held memo may or may not have a transcript, and which one it is depends on
where it was held.**

The fix keeps every edge as it is and puts one requirement on E3 instead:

> **A transcription worker that claims a memo already carrying a durable
> transcript does not re-run ASR.** It advances the memo to `transcribed` without
> reading audio.

So `held → queued → transcribing → transcribed → triaged` works whether or not
the audio survived: a routing-stage hold resumes through the same path it always
would, and touches nothing that has been pruned. E3 needs this behaviour anyway —
it is what makes its own retries idempotent — so this is a requirement recorded,
not a cost imposed.

**Departure 2 — `discarded` is genuinely terminal, with no un-discard edge, and
the mitigation is that it destroys nothing.** DISCARD is offered at capture, on
the device, on a memo the person just made and is looking at. Making it
reversible means the pipeline and the pruner both have to reason about
resurrection, and a re-delivery of discarded bytes becomes ambiguous — which
matters, because the Copyparty watcher will see that file on every rescan.
So: **a re-arrival matching a discarded memo is a no-op.** It changes nothing —
not the state, not the retention (§5), and under §3 not even a row. A sync client
cannot undo a person's decision by rescanning a folder.

What makes that safe is that discarding is not a delete: the row stays, the
transcript stays, the memo is inert and invisible. A mis-tap is recoverable by a
human with SQL, and by no automated path at all. That is the right side to put
the friction on.

**Re-routing does not move a memo backwards.** Scribe proposals are tier 1 and
regenerable; re-running the Scribe on a `triaged` memo produces a new proposal
without touching the memo's state. `triaged` has no edge back to `transcribed`
and does not need one.

**And the rule this hands to CHRN-22: the pruner must not read `state`.** Its
gate is a durable transcript, full stop. A destructive job must not rest on a
second, softer fact, and a bug in the state machine must not be able to become
data loss.

## 7 · What the database enforces and what Go enforces

They are different jobs and the PR should not collapse them.

| | mechanism | what it buys |
|---|---|---|
| the set of states | `CHECK` | a typo is rejected on `INSERT`, where a trigger comparing `OLD` cannot help |
| the legal edges | `BEFORE UPDATE` trigger | an illegal edge is impossible from any client, including `psql` and CHRN-67's MCP write scope |
| identity + `captured_at` immutability | same trigger | see §4 |
| one memo per (author, hash) | unique index + `ON CONFLICT` | correct under concurrency, in one statement |
| one memo per in-flight key | **[rev]** advisory xact lock | §9 — the index alone raises on the loser instead of collapsing it |
| **claiming** a memo for work | `UPDATE … WHERE state = 'queued'` in Go | compare-and-set, so two workers cannot both claim the same memo |

The last row is the one that is easy to mistake for the second. The trigger says
the edge `queued → transcribing` is *permitted*; the conditional `WHERE` is what
makes exactly one worker win it.

**[rev] Each trigger rejection gets its own SQLSTATE.** The first draft raised
`check_violation` for both, which in Go is indistinguishable from a `CHECK` firing
on a typo'd enum value — so an illegal transition would surface as a generic 400
and the store could not tell the two apart. `CH001` illegal transition (mapped to
a typed `ErrIllegalTransition`), `CH002` immutable column, `CH003` created in a
state other than `captured`.

**[rev] Who writes `state`: only the `chronicle` role, ever.** Worth stating
because CHRN-52 and CHRN-67 read this document. Nothing running as
`chronicle_tier1` can move a memo — 0001 leaves that role no `USAGE` on schema
`tier2`, so a memo row is not reachable from it at all. A proposal being
*accepted* or *held* is a person's act, made through the ordinary `chronicle`
role.

**Which role the Scribe itself runs as is E4's question, and this document
should not have answered it.** The first draft asserted `chronicle_tier1`, which
cannot be right as stated: proposing means reading a transcript, transcripts are
tier 2, and that is precisely what the role is guaranteed not to see. The
invariant above does not depend on the answer. CHRN-52 should take the Scribe's
role from E4 rather than from a premise stated here in passing.

### [rev] Two consequences of review that E3 and E4 inherit

**`AdvanceMemoState` is a compare-and-swap, not a setter.** The caller states the
state it believes the memo is in and the update applies only if that still holds.
This is not belt-and-braces over the trigger: the guard consults its edge list
only when `NEW.state IS DISTINCT FROM OLD.state`, so a same-state write is
invisible to it, and without the `from` predicate two workers could both claim
one memo — measured, with the predicate removed, as six of six workers winning
the same claim. §7's table already assigned claiming this shape; the first draft
of the code did not implement it.

**`state_reason` is replaced on every transition, not merged.** A memo released
from a hold must not keep explaining why it was held. The consequence, which is
easy to trip over: **a reason has to be re-supplied on any transition that should
keep one**, `held → discarded` included. Passing `""` clears it.

## 8 · The schema

Both tables are **tier 2** — authored, irreplaceable, not derivable. Migration
`0003_memos`, up and down, every reference schema-qualified.

```sql
CREATE TABLE tier2.memos (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- ON DELETE RESTRICT, deliberately: see the note below on CHRN-71.
    author_id         UUID NOT NULL REFERENCES tier2.users(id) ON DELETE RESTRICT,

    -- Identity: SHA-256, lowercase hex, over the bytes exactly as they
    -- arrived. Before normalisation, before anything. Never recomputed.
    content_hash      TEXT NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    byte_size         BIGINT NOT NULL CHECK (byte_size > 0),

    -- First arrival, assigned by the database. Never supplied by a caller,
    -- immutable once set, and the only clock CHRN-22 may run from.
    captured_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    state             TEXT NOT NULL DEFAULT 'captured'
                        CHECK (state IN ('captured', 'queued', 'transcribing',
                                         'transcribed', 'triaged', 'held',
                                         'discarded')),
    state_reason      TEXT,                 -- why it is held, why a decode failed

    retention         TEXT NOT NULL DEFAULT 'days_30'
                        CHECK (retention IN ('discard_now', 'days_30', 'forever')),
    audio_pruned_at   TIMESTAMPTZ,          -- set by CHRN-22; the transcript never goes

    -- CHRN-21 fills these; NULL until it has run.
    duration_ms       INTEGER CHECK (duration_ms IS NULL OR duration_ms > 0),
    codec             TEXT,
    sample_rate_hz    INTEGER,

    -- Authored, display-only. Never used to derive a path.
    original_filename TEXT,

    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX memos_author_content ON tier2.memos (author_id, content_hash);

CREATE TABLE tier2.memo_arrivals (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    memo_id           UUID NOT NULL REFERENCES tier2.memos(id) ON DELETE CASCADE,
    author_id         UUID NOT NULL REFERENCES tier2.users(id) ON DELETE RESTRICT,
    source            TEXT NOT NULL CHECK (source IN ('copyparty', 'upload')),
    idempotency_key   TEXT CHECK (idempotency_key IS NULL
                                  OR length(idempotency_key) BETWEEN 16 AND 200),
    source_ref        TEXT,                 -- watched path, or upload session id
    arrived_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Every arrival carries at least one handle, or a rescan cannot recognise
    -- it as one it has already recorded.
    CONSTRAINT memo_arrivals_has_handle
        CHECK (idempotency_key IS NOT NULL OR source_ref IS NOT NULL),
    -- The keyless path's handle is its path, and a partial unique index over a
    -- NULL column would not dedupe anything.
    CONSTRAINT memo_arrivals_watched_path
        CHECK (source <> 'copyparty' OR source_ref IS NOT NULL)
);

-- Author-scoped, not global: a global unique on a client-chosen string lets one
-- account's key collide with another's and deny it an upload, through a
-- namespace the two share for no reason.
CREATE UNIQUE INDEX memo_arrivals_key ON tier2.memo_arrivals (author_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- [rev] A repeated sighting of the same file is not a second delivery.
CREATE UNIQUE INDEX memo_arrivals_sighting
    ON tier2.memo_arrivals (memo_id, source, source_ref)
    WHERE idempotency_key IS NULL;

CREATE INDEX memo_arrivals_memo ON tier2.memo_arrivals (memo_id);
```

There is no `audio_path` column. CHRN-23 asks that *"every audio file's path is
derivable from its memo row alone"*, and `content_hash` is immutable, so the path
is a pure function of it. Storing it too would be a second source of truth for
the same fact. Pruned is `audio_pruned_at IS NOT NULL`, not a nulled path.

### [rev] `ON DELETE RESTRICT`, and the change it makes to CHRN-71

`DeleteUser` (`internal/store/user.go:305`) is a plain `DELETE FROM tier2.users`
with no foreign-key error handling. The moment an account owns memos, that
delete raises `23503`, which the current code wraps as a generic error and the
admin surface answers as a 500.

Refusing the delete is the right behaviour — deleting an author would either
orphan or cascade authored, irreplaceable rows — but it is a change to another
ticket's contract, so this document decides it rather than leaving the PR to
discover it: **`ON DELETE RESTRICT` is written explicitly**, and `DeleteUser`
maps `23503` to a typed error the handler turns into **409**, alongside its
existing `ErrOwnerImmutable` path. That is two small edits in `internal/store`
and `internal/api`, in this PR, named here so they read as intended rather than
as scope creep. Removing a person who has recorded memos is a conversation about
their corpus, not a button.

### The REVOKE, and a pre-existing false comment

```sql
REVOKE ALL ON tier2.memos, tier2.memo_arrivals FROM chronicle_tier1;
```

It is redundant — `chronicle_tier1` holds no `USAGE` on schema `tier2` — and is
stated anyway as documentation of intent, per the pattern `0002_accounts.up.sql`
established.

**[rev] But the reason 0002 gives for it is not true, and this document does not
repeat it.** 0002 claims the explicit `REVOKE` makes a later loosened grant *"show
up as a `schema.sql` diff rather than as an absence nobody notices."* `pg_dump`
emits only non-default ACLs, and revoking a privilege the role never held leaves
nothing to emit — which is why `schema.sql` today contains no `REVOKE` for
`tier2.users` at all. A loosened `GRANT` would appear in the diff with or without
the `REVOKE`. Keep the statement, drop the claim. Correcting 0002's comment is a
separate one-line change and not this ticket's — `REVIEW.md` is explicit that a
false comment in this repo is expensive, and this one has been sitting in the
migration that defines the tier boundary.

`[rev]` Both follow-ups have landed. **CHRN-78** corrected `0002` (`4ea9875`),
and **CHRN-79** corrected `REVIEW.md` §1, which turned out to be the last place
in the repo still stating the claim — and worse, it cited the very comment that
refutes it. Closing the loop here so the trail does not end at a forward
reference.

### The functions

```sql
CREATE FUNCTION tier2.retention_rank(r TEXT) RETURNS INT
    LANGUAGE sql IMMUTABLE STRICT AS $fn$
    SELECT CASE r WHEN 'discard_now' THEN 0
                  WHEN 'days_30'     THEN 1
                  WHEN 'forever'     THEN 2 END
$fn$;

CREATE FUNCTION tier2.memos_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
BEGIN
    -- One entry point. A memo exists only once its audio is complete and
    -- durable, so there is no state meaning "maybe there are bytes".
    IF TG_OP = 'INSERT' THEN
        IF NEW.state <> 'captured' THEN
            RAISE EXCEPTION 'memo must be created in state captured, got %', NEW.state
                USING ERRCODE = 'CH003';
        END IF;
        RETURN NEW;
    END IF;

    IF NEW.author_id    IS DISTINCT FROM OLD.author_id
    OR NEW.content_hash IS DISTINCT FROM OLD.content_hash
    OR NEW.byte_size    IS DISTINCT FROM OLD.byte_size
    OR NEW.captured_at  IS DISTINCT FROM OLD.captured_at THEN
        RAISE EXCEPTION 'memo identity and captured_at are immutable'
            USING ERRCODE = 'CH002';
    END IF;

    -- 'discarded' appears only as a target and never as a source: that is how
    -- terminal is written down, where it can be read.
    IF NEW.state IS DISTINCT FROM OLD.state
       AND (OLD.state || '>' || NEW.state) <> ALL (ARRAY[
             'captured>queued',          'captured>held',      'captured>discarded',
             'queued>transcribing',      'queued>held',        'queued>discarded',
             'transcribing>transcribed', 'transcribing>queued',
             'transcribing>held',        'transcribing>discarded',
             'transcribed>triaged',      'transcribed>held',   'transcribed>discarded',
             'triaged>held',             'triaged>discarded',
             'held>queued',              'held>discarded'
       ]) THEN
        RAISE EXCEPTION 'illegal memo state transition % -> %', OLD.state, NEW.state
            USING ERRCODE = 'CH001';
    END IF;

    NEW.updated_at := now();
    RETURN NEW;
END
$fn$;

CREATE TRIGGER memos_guard BEFORE INSERT OR UPDATE ON tier2.memos
    FOR EACH ROW EXECUTE FUNCTION tier2.memos_guard();
```

**Nothing in this schema holds Switchyard or Amber state.** No ticket key, no
upstream status, no cached title. The second invariant is untouched by this
ticket, and stays that way when E4 links proposals out.

## 9 · Ingest, in one transaction

```
BEGIN
  if key present:
      -- [rev] Serialise same-key arrivals BEFORE the read. Without this, two
      -- concurrent retries of one key both miss the SELECT below, both reach
      -- the upsert (correctly getting the same memo), and then the loser's
      -- arrival insert hits memo_arrivals_key -> 23505. The retry gets an
      -- error instead of its memo, which is the failure "retries are free"
      -- names. This is the ordinary mobile-queue shape, not an exotic race.
      SELECT pg_advisory_xact_lock(hashtext($author_id::text || ':' || $key)::bigint)

      SELECT memo_id FROM tier2.memo_arrivals
       WHERE author_id = $1 AND idempotency_key = $2
      -- hit and hash matches  -> return that memo, write nothing
      -- hit and hash differs  -> 409 key_reused, roll back

  INSERT INTO tier2.memos (author_id, content_hash, byte_size,
                           retention, original_filename)
  VALUES ($1, $2, $3, COALESCE($4, 'days_30'), $5)
  ON CONFLICT (author_id, content_hash) DO UPDATE
     SET retention = $4
   WHERE $4 IS NOT NULL
     AND tier2.memos.state <> 'discarded'
     AND tier2.retention_rank($4) > tier2.retention_rank(tier2.memos.retention)
  RETURNING id
  -- [rev] RETURNING is empty when the conflict resolved to no update, which is
  -- the common re-delivery case and the point: no write, no trigger, no
  -- updated_at bump. Fall back to a plain SELECT for the id — safe, because
  -- ON CONFLICT has already waited out any competing transaction, so the row
  -- is committed and visible.

  INSERT INTO tier2.memo_arrivals (memo_id, author_id, source, idempotency_key, source_ref)
  VALUES (...)
  ON CONFLICT (memo_id, source, source_ref) WHERE idempotency_key IS NULL DO NOTHING
  -- [rev] Targeted at the sighting index only. A key collision still raises
  -- rather than being swallowed: with the advisory lock held, one reaching
  -- here means a bug, not a race.

  SELECT count(*) FROM tier2.memo_arrivals WHERE memo_id = $1   -- >1 => duplicate delivery
COMMIT
```

The count is how the log line below knows it collapsed a duplicate. It is the
definition rather than a proxy, which is why it is preferred to the `xmax = 0`
upsert trick — that reports "did this statement insert", which is not the same
question and is not documented API.

## 10 · The oracle problem, and the answer to it

The ticket's stated reason for `opus` is *"no oracle, and the failure is silent
and cumulative."* Half of that is fixable: **make it not silent.**

Every collapsed duplicate emits one structured line —
`msg="duplicate arrival"`, with `memo_id`, `source`, `arrival_count`. Then the
rule has telemetry, and both failure directions are visible in Dozzle rather than
discovered in the corpus months later:

- the rule working looks like collapses appearing when a file is re-delivered;
- the rule broken looks like **zero** collapses where four were expected, or
  memo count climbing in step with arrival count.

No transcript text, no filename beyond the memo id, nothing authored, per
`REVIEW.md` §8.

### [rev] Who emits it, and the trap found in review

**CHRN-18 provides the signal; CHRN-19 and CHRN-20 emit the line.** `store` holds
no logger and gaining one would drag the composition root into a model ticket, so
the ingest paths log it at the point they already have a request or scan context.
Naming the owner here because the first draft deferred this without saying so,
and an observability commitment nobody owns is one that quietly does not happen.

The signal is `IngestResult.Collapsed`, **not** `Deliveries > 1`. The reviewer
caught the first draft inferring one from the other, and the inference is wrong
in the worst available direction: the two commonest collapses — a same-key retry
and a repeated sighting — deliberately write *no arrival row*, so the count stays
at 1 through eight retries and `Deliveries > 1` reports **false** for exactly the
cases this section exists to make visible. The alarm above is "zero collapses
where four were expected", so a duplicate-detector that under-reports duplicates
would have made that alarm fire on healthy traffic and stay silent on the failure
— worse than no telemetry, because it would have been believed.

`Collapsed` is set where the collapse actually happens: on the same-key early
return, when the arrival insert affects zero rows, and when the memo already
carried an arrival.

## 11 · [rev] `internal/model/` — and fixing the sentence, not working around it

`CLAUDE.md` describes `internal/model/` as domain types 1:1 with the schema. That
package **does not exist**: CHRN-71 put `User` and `Session` in `internal/store`
beside their queries, and that is the only precedent in the repo.

`Memo` and `MemoArrival` go in `internal/store`, following the code. **[rev] And
this PR corrects the sentence in `CLAUDE.md`** rather than leaving it. The first
draft recommended leaving it alone to protect the cached shared prefix, which
undervalues the cost on the other side: a known-false line in the prefix *every
agent inherits* will keep generating `internal/model` packages and split the
convention, and that is worth more than a single cache miss. One line, changed
once, in the PR that made it demonstrably false.

## What this does not decide

- **Where transcripts live** (E3 / CHRN-3). This ticket fixes two seams into it:
  the pruner's gate is a durable transcript read from E3's table rather than from
  `memos.state`, and **[rev]** a worker claiming a memo that already has one does
  not re-run ASR (§6).
- **How CHRN-19 detects a completed upload.** But note the dependency runs the
  other way from how it reads: a hash over a half-written file is a *different*
  hash, so the next scan produces a **second memo**. CHRN-19's atomic-handoff
  requirement is load-bearing for CHRN-18's `Done when`, not merely tidy.
- **CHRN-21's ffmpeg placement** — image, sidecar or host. Still open from the
  E2 brief. It does not block this ticket: `duration_ms`, `codec` and
  `sample_rate_hz` are nullable and filled in afterwards.
- **CHRN-23's layout function**, constrained here only to be a pure function of
  immutable columns.
- **Whether CHRN-22 runs before or after E3.** The brief's open question, and
  unaffected by anything above.

## Done when

The ticket's three criteria, one test each, plus the tests that cover the rules
this decision adds beyond it.

1. **One row, four arrivals.** The same bytes delivered twice by Copyparty and
   twice by upload produce one `tier2.memos` row and four `tier2.memo_arrivals`
   rows.
2. **Under concurrency.** The same, from N goroutines at once under `-race` — the
   test that catches a read-then-insert wearing an `ON CONFLICT` costume.
3. **[rev] N goroutines, one key.** Concurrent retries sharing an idempotency key
   all return the same memo id and none returns an error. Its own test, because
   it fails against a design that passes #2.
4. **Retries are free.** A repeated upload with the same key returns the same
   memo id and leaves `updated_at` unchanged.
5. **[rev] A re-delivery writes nothing.** Re-ingesting an unchanged Copyparty
   file adds no arrival row and does not move `updated_at`.
6. **The state machine is the database's.** An illegal transition issued as raw
   SQL, bypassing the repo layer entirely, is rejected — **[rev]** with SQLSTATE
   `CH001`, distinguishable from a `CHECK` violation. So is an `UPDATE` that
   moves `captured_at` (`CH002`), and an `INSERT` in any state but `captured`
   (`CH003`).
7. **[rev] A hold survives its audio — the half that can run here.** A memo with
   `audio_pruned_at` set is walked `held → queued → transcribing → transcribed →
   triaged`, and the trigger accepts every edge. The other half — that E3's
   worker skips ASR when a durable transcript already exists — needs E3's worker
   and E3's table, so it is E3's test, named under *What this does not decide*.
   The first draft put the whole assertion here, where it cannot execute.
8. **Key reuse with different bytes is 409**, and creates no row.
9. **The ratchet holds, and only for real opinions.** An arrival carrying
   `days_30` onto a `forever` memo leaves it `forever`; **[rev]** a Copyparty
   arrival does not lift an authored `discard_now`; **[rev]** an arrival carrying
   `forever` onto a `discarded` memo changes nothing.
10. **Cross-author.** Identical bytes from two accounts are two memos.
11. **[rev] An author with memos cannot be deleted.** `DeleteUser` on an account
    holding memos returns a typed error and the admin route answers 409, not 500.
12. **Tier.** `chronicle_tier1` cannot read `tier2.memos` — the CHRN-71 assertion
    extended to the table that actually holds the corpus, scanning into `any` and
    requiring a *permission* error, so "the migration never ran" cannot pass as
    "the role is locked out".
