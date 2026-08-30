# CHRN-32 — The proposal contract (decision)

Status: **proposed 2026-08-30.** Three rulings open at the end of this document.
Ticket: CHRN-32 (Phase P2, parent CHRN-4). Tier `opus`, so Mode B: this document
is the review artefact and the PR that follows it is mechanical.
Decision owner: magos.
Read by: **CHRN-30** (the prompt must emit exactly this), **CHRN-31** (the
catalogue is what §5 validates against), **CHRN-33** (the batch API's unit),
**CHRN-34** (HOLD and DISCARD), **CHRN-35** (the TICKET route's inputs),
**CHRN-36** (the eval set grades this shape), and later **CHRN-55** (the triage
screen) and **CHRN-67** (the MCP write tool).

## Context

CHRN-32 is the keystone of E4. Its own ticket says the contract is consumed by
three surfaces and an MCP tool, which is the whole argument for spending the
expensive tier on it: a field that is wrong here is wrong in a Vue screen, an
Android screen, a Go handler and an agent tool, and it is wrong in the eval set
that was supposed to catch it.

What exists already: memos (CHRN-18) with a state machine that *already*
contains `triaged`, `held` and `discarded`; transcripts (CHRN-27) in
`tier2.transcripts`, one per memo per model; and a salvaged routing prompt
(`docs/salvage/vox-dictate/routing-prompt.md`) that worked against real memos
with one destination and no confidence.

What does not exist: the page tree (CHRN-37, **E5**), the note model (CHRN-38,
**E5**) and threads (CHRN-43, **E6**). Two of this contract's five destinations
therefore have nowhere to land yet. §6 settles what the contract does about
that, because the alternative is discovering it inside CHRN-33's diff.

`CLAUDE.md` names the thing this document must not get wrong, and names it in
the sentence that lists what tier 1 is:

> Tier 1 is the estate's account of what exists … plus whatever Chronicle
> derives from its own corpus: **Scribe proposals**, extracted entities, search
> indexes.

A proposal is derived. That one word decides §1, and §1 decides most of the
rest.

## Decision

1. A proposal is a **tier-1 row**, not a tier-2 row and not a transient value.
2. Its identity is `(memo_id, proposer)`, on `tier2.transcripts`'s pattern.
3. The payload is a **discriminated union on `destination`**, validated in two
   stages that fail differently.
4. **`reason` is required, non-empty, and schema-level.**
5. **Scribe may propose `DISCARD`. Scribe may never propose `HOLD`.**
6. **`nearest_page` ships now, nullable, validated against a catalogue whose
   page half is empty until E5** — the shape is final, the constraint tightens.
7. A proposal that will not validate becomes a **recorded failure**, never a
   silent absence.
8. The contract **carries confidence and does not interpret it**. The threshold
   is CHRN-36's and lives in configuration.

---

## 1 · A proposal is tier 1, and the acceptance is the tier-2 write

`tier1.memo_proposals`. Not `tier2`, and the reason is not filing tidiness.

`CLAUDE.md` lists Scribe proposals among the things tier 1 holds, and the test
it gives holds: delete every proposal in the database and re-run Scribe over the
transcripts, and you have them back. Nothing is lost that a person authored.
Contrast the transcript, which is tier 2 and irreplaceable once the audio prunes
at 30 days — the pruner's whole design (CHRN-22) turns on that asymmetry.

This has a consequence worth stating plainly, because it is the line CHRN-52's
isolation test will be drawn along:

> **The proposal is a tier-1 write. The acceptance is a tier-2 write. They are
> different code paths reaching different schemas through different roles.**

Scribe runs as `chronicle_tier1` and writes proposals all day. It cannot write
`tier2.memos`, so it cannot mark a memo `triaged` — that transition belongs to
the acceptance path, which runs as `chronicle` and is driven by a person. A
router that could route *and* commit would be a tier-1 process authoring tier-2
state, which is the fabrication the tier split exists to prevent.

It also disposes of a question CHRN-33 would otherwise have to answer: what
happens if Scribe re-runs while an operator has the triage screen open. The
proposal is regenerable and the memo's state is not, so a re-run rewrites tier 1
and touches nothing the operator has decided.

### Why a row and not a transient value

The tempting alternative is to compute proposals on demand: `GET /triage`
transcribes nothing, reads N transcripts, calls Gemma N times, returns. No
table, no migration, nothing to keep consistent.

It is wrong for three reasons, in increasing order of severity:

- **Gemma 4 at 31B is not free.** Forty memos is forty completions on the same
  box that runs whisper and Ollama for everything else. Every screen refresh
  would pay it again.
- **The proposal would drift under the operator's cursor.** Re-running the model
  is not idempotent. An operator who scrolls, hesitates and scrolls back sees a
  different destination with a different confidence, and has no way to tell
  whether the model changed its mind or they misread it the first time.
- **CHRN-36 could not exist.** The eval set grades proposals against labels, and
  compares runs against each other. A proposal that was never written down is a
  proposal that cannot be scored, diffed, or blamed for a regression.

## 2 · Identity is `(memo_id, proposer)`, and a re-run supersedes rather than mutates

`tier2.transcripts` already solved this problem and the answer carries over
directly. It is unique on `(memo_id, model)` — one transcript per memo per model
— with a trigger that refuses to re-attribute a row to another memo or model.
The comment says why: keeping `small.en` and `medium.en` over the same memo is
worth doing, and it is what makes the model column mean anything.

Proposals get the same shape:

```
UNIQUE (memo_id, proposer)
```

where `proposer` is **runner-qualified**, exactly as
`tier2.transcripts.model` holds `whisper.cpp/small.en` and not `small.en`:

```
ollama/gemma4:31b@v1        -- runner / model : prompt version
ollama/gemma4:e4b@v1
```

The lesson is one this repo has already paid for once. The ASR model column
carries its runner because a bare `small.en` says nothing about *what ran it*,
and CHRN-22's model floor had to be two-axis — quality *and* runner — for
precisely that reason. A proposal is the output of a model **and a prompt**, and
a prompt revision changes the output as surely as a model swap does. `v1` in the
proposer string is CHRN-30's prompt version, and it is not optional: without it,
CHRN-36 cannot tell a prompt regression from a model regression, which is the
one comparison the eval set exists to make.

A re-run with the same proposer **supersedes**: the row is replaced, and the
previous payload is kept in `superseded_payload` for one generation. Not an
append-only history — tier 1 is disposable and a full history of every
proposal the model ever made is a table that grows without a reader. One
generation is enough to answer "did the prompt change do what I thought", which
is the only question anyone asks.

## 3 · The payload, as a discriminated union

One JSONB column validated against a schema, rather than fifteen nullable
columns. The destination-conditional fields genuinely differ in kind — a TICKET
proposal carries a project key and a Switchyard type, a NOTE proposal carries a
page path — and modelling that as columns produces a table where two thirds of
every row is NULL and no constraint can say which two thirds.

### Common fields, on every proposal

| field | type | required | notes |
|---|---|---|---|
| `destination` | enum | yes | `NOTE` \| `TICKET` \| `DISCUSSION` \| `DISCARD`. **Not `HOLD`** — see §4. |
| `confidence` | number | yes | `0.0 … 1.0` inclusive. Carried, not interpreted — §8. |
| `reason` | string | yes | Non-empty after trimming, ≤ 400 chars. See §4. |
| `title` | string | yes | Imperative, ≤ 100 chars. The salvaged prompt's rule, unchanged. |
| `nearest_page` | string \| null | yes, may be null | A page path. §6. |

### `destination: "TICKET"` additionally

| field | type | required | notes |
|---|---|---|---|
| `project_key` | string | yes, may be `""` | Uppercase, or empty. Empty is a real answer — see below. |
| `ticket_type` | enum | yes | `spike` \| `task` \| `bug` \| `epic`. Switchyard's enum, from the salvaged prompt. |
| `description` | string | yes | The `## Summary / ## Goals / ## Approach` markdown the salvaged prompt already produces. |

**`project_key: ""` is valid and is not pre-acceptable.** This is the salvaged
prompt's best rule and it carries over verbatim: *"If you cannot tell which
project applies, return empty string — DO NOT default to one."* The reason it
was written is that `project_key` is immutable after ticket creation in
Switchyard, so a guessed project is a permanent wrong answer. The recovery lives
in code and in the operator, never in the model.

So an empty project key does not make the proposal invalid. It makes it a
proposal the operator must complete before it can be accepted, which the batch
API (CHRN-33) must surface as *needs input* rather than as *failed*. Ruling R2.

### `destination: "NOTE"` additionally

| field | type | required | notes |
|---|---|---|---|
| `page_path` | string \| null | yes, may be null | Where the note should live. Null until E5 — §6. |
| `body` | string | yes | The cleaned-up note text. |

### `destination: "DISCUSSION"` additionally

| field | type | required | notes |
|---|---|---|---|
| `opening_post` | string | yes | The first turn of the thread. |

### `destination: "DISCARD"` additionally

Nothing. `reason` is doing the work, and it is the field the operator reads
before agreeing to throw something away.

## 4 · `reason` is required, and `HOLD` is not the model's to propose

### The reason

The ticket is explicit that `reason` belongs in the schema and not in prose, and
the epic gives the shape:

> *argues a principle, names no owner and no due date — reads as doctrine, not
> work. Nearest existing page is `storage/amber`.*

Required, non-empty after trimming. A proposal that arrives with
`"reason": ""` is invalid and takes §7's path, not a shrug and a display.

The argument is in the epic and it is worth restating because it is the thing
that makes the whole triage screen work: a destination without a reason is
something the operator has to verify from scratch, which is **slower than
deciding unaided**. A router that makes triage slower has negative value no
matter how accurate it is.

### Why the model may not propose HOLD

`HOLD` and `DISCARD` are both called escapes in the epic, and it would be
natural to give the model both. They are not the same kind of thing.

**`DISCARD` is a judgement about content.** "This is a voice memo of someone
testing their microphone" is a claim about the transcript, the model is
positioned to make it, and `reason` makes it checkable at a glance. Scribe may
propose it.

**`HOLD` is a statement about the operator's own state of mind** — *I will
decide this later*. A model cannot hold that belief, and a model that emits
`HOLD` is not routing, it is declining to route.

That distinction matters because of the failure mode the epic warns about. A
model that can answer "I'd rather not" will learn to, and `HOLD` would become
the safe default that swallows the ambiguous half of every batch. The operator
then reads forty proposals of which eighteen say `HOLD`, which is the screen
they would have had with no router at all — except that it cost forty
completions and it looks like it did something.

**Low confidence is already how the model says it does not know**, and it is
strictly better than `HOLD`: it still commits to a best guess, which CHRN-36 can
score, and the guess is often right even at 0.4. `HOLD` produces nothing to
grade. A router that abstains cannot be measured, and E4's exit criterion is a
measurement.

So: `HOLD` is an **operator action** on the batch API (CHRN-34), available on
every item regardless of what was proposed, and it is not a value the model can
emit. Ruling R1.

## 5 · Two-stage validation, and the two stages fail differently

The `Done when` names two distinct checks and they are worth keeping distinct,
because one is about shape and the other is about the world.

### Stage 1 — shape

Schema validation against the union in §3. Enum membership, required fields,
`confidence` in range, `reason` non-empty, string lengths.

This is deterministic, needs nothing external, and runs before anything touches
a database. Ollama is already asked for `format: "json"`, which — as the salvage
note records — constrains decoding to valid JSON and is why the downstream
parser needs one fenced-block fallback rather than a general repair pass. Shape
validation is what catches *valid JSON that is the wrong shape*, which is the
much more common failure.

### Stage 2 — referential, against the live catalogue

CHRN-31 assembles what Scribe is allowed to route to, freshly, per run. Stage 2
checks the proposal's targets against it:

- `project_key` is non-empty ⇒ it must be a **live** Switchyard project key.
- `page_path` or `nearest_page` is non-null ⇒ it must be a **live** page path.

This is what the `Done when`'s "a destination outside the live catalogue is
rejected rather than displayed" and "a hallucinated page path cannot be
accepted" are asking for, and it is why CHRN-31 insists the catalogue is read
live and never cached across runs. A stale catalogue produces the worst kind of
error: the destination it picked *does exist*, it is merely the wrong one, and
nothing in the proposal looks wrong.

**Stage 2 runs twice** — once when the proposal is written, and again at
acceptance. Not belt-and-braces: a proposal generated on Tuesday evening and
accepted on Thursday morning has had two days for a project to be archived or a
page to be renamed. Validating only at write time would let CHRN-33 create a
ticket in a project that no longer takes them. The second check is cheap and the
catalogue is already being assembled for the batch.

### What a stage-2 failure does, and does not do

It **does not** discard the proposal. It clears the offending field and marks
the proposal as needing input, keeping `destination`, `confidence` and `reason`
— which are still the model's honest answer and still useful. A proposal whose
project was archived is a proposal that needs a new project, not a memo that has
to be re-routed from scratch.

## 6 · `nearest_page` ships now, empty, and tightens when E5 lands

This is the cross-epic edge, and it is the reason this section exists rather
than a comment in a diff three weeks from now.

`nearest_page` and `page_path` refer to the Chronicle page tree, which is
**CHRN-37, in E5**. `NOTE` needs the note model, **CHRN-38, in E5**.
`DISCUSSION` needs threads, **CHRN-43, in E6**. Two of four proposable
destinations have nowhere to land when E4 ships in isolation.

Three options, and the third is the decision:

**(a) Drop the page fields from the contract and add them in E5.** Rejected. The
contract is consumed by three surfaces and an MCP tool; adding a required-ish
field later means a migration, a schema revision, a re-generated client, and a
prompt change — in the epic that is supposed to be *using* this contract, not
revising it. The epic's own canvas example names a nearest page, so the field is
known-needed. Deferring a known-needed field to buy nothing now is how contracts
acquire versions.

**(b) Pull CHRN-37 and CHRN-38 forward into E4.** Rejected, and it is the option
the working agreement explicitly tells me to stop and ask about rather than
take: it is a change that wants to touch something outside its own epic. It also
inverts the epic order for no gain, since E4's exit is *measured routing
accuracy*, and accuracy is measured against labels, not against landed notes.

**(c) The field ships now, nullable, and §5's stage-2 check validates it against
the live catalogue — whose page half is simply empty until CHRN-37 fills it.**

Under (c) the behaviour today is: any non-null `page_path` or `nearest_page` is
rejected by stage 2, because it is not in the catalogue. The `Done when`'s "a
hallucinated page path cannot be accepted" is satisfied — strictly, and by the
general mechanism rather than a special case. As CHRN-37 lands, the catalogue
fills and the same check starts admitting real paths with no code change and no
migration. The constraint tightens; the contract does not move.

CHRN-30's prompt is told the page list is empty in the same way it is told the
project list — the catalogue is a runtime slot, and an empty slot is a valid
one. A model given no pages to choose from returns null, which is the correct
answer.

### What this leaves for CHRN-33

Accepting a `NOTE` or `DISCUSSION` proposal has no destination table before E5
and E6. **That is CHRN-33's decision to make, not this one**, and it is flagged
here so it is made deliberately: the options are to defer those two accept paths
behind the same catalogue emptiness, or to record a *decision* on the memo
without materialising a note. This document takes no position beyond insisting
CHRN-33 answer it in writing before it writes code.

## 7 · An invalid proposal is recorded, never absent

The ticket's sharpest sentence: *"A malformed proposal is a proposal that
silently disappears from a batch, and the operator will not notice which memo
went missing."*

So there is no path where a memo enters a Scribe run and produces nothing.

**Retry, with the error fed back.** Up to **three** attempts. The second and
third include the validation error in the prompt — a model told `confidence must
be between 0 and 1, got 1.5` usually fixes it, and this costs one completion
rather than an operator's attention.

**Then record the failure.** After three attempts the proposal row is written
with `status = 'invalid'`, no payload, the validation error, and the **raw model
output kept verbatim** for CHRN-30 to read. It is tier 1 and disposable; a
truncated field here would hide the only evidence of why the prompt failed.

**The memo stays untriaged and stays visible.** It appears in the triage batch
carrying its error instead of a proposal, which is a memo the operator can route
by hand. It is not skipped, not filtered, not silently deferred.

`status` on the proposal row: `valid` | `needs_input` | `invalid`.
`needs_input` is §3's empty project key and §5's cleared field.

## 8 · Confidence is carried, not interpreted

The contract validates that `confidence` is a number in `[0, 1]`. It attaches no
meaning to any particular value.

The threshold below which a proposal is shown but never pre-accepted is
**CHRN-36's to set**, because CHRN-36 is the only thing that will ever know it:
it is the ticket that measures whether confidence predicts correctness, and the
epic is explicit that a well-calibrated 0.86 is worth more than an uncalibrated
0.95.

It lives in configuration — `CHRONICLE_SCRIBE_PREACCEPT_MIN`, env-only and
`CHRONICLE_`-prefixed like everything else — and **never as a literal in the
contract, the prompt or a handler.** A threshold compiled into three surfaces is
a threshold that will be changed in two of them.

Corollary, and the reason the epic cares: `ACCEPT ALL` is licensed by
calibration, not by accuracy. This contract's job is to make sure the number is
present, in range, and attributable to a named proposer so that CHRN-36 can
decide what it is worth.

## Surface

Nothing in E4 is public yet; these are the shapes CHRN-33 builds on.

```
tier1.memo_proposals
    id, memo_id, proposer, status, payload JSONB,
    superseded_payload JSONB, error TEXT, raw_output TEXT,
    created_at, updated_at
    UNIQUE (memo_id, proposer)
```

`internal/scribe/` — the package. Contract types, the two validators, and the
proposer string. Not `internal/api/`: CHRN-67 exposes routing as an MCP tool,
and a contract that only exists inside HTTP handlers is one that gets
reimplemented for the agent surface.

## Configuration

| var | meaning |
|---|---|
| `CHRONICLE_SCRIBE_MODEL` | e.g. `gemma4:31b`. Qualified to `ollama/…@vN` for the proposer string. |
| `CHRONICLE_SCRIBE_OLLAMA_URL` | Ollama's base URL. |
| `CHRONICLE_SCRIBE_PREACCEPT_MIN` | §8. Set by CHRN-36, not by this ticket. |
| `CHRONICLE_SCRIBE_MAX_ATTEMPTS` | §7's retry ceiling. Default 3. |

## What each ticket inherits

- **CHRN-30** — emit exactly §3, including `reason`. Never `HOLD` (§4). The
  prompt version is part of the proposer string (§2), so a prompt change is a
  version bump and not a silent overwrite.
- **CHRN-31** — the catalogue is what §5 stage 2 validates against, and its page
  half is legitimately empty until E5 (§6). Live per run, never cached across.
- **CHRN-33** — the unit is a proposal row; `needs_input` is not `invalid`
  (§3, §7); stage 2 runs again at acceptance (§5); and §6 names the NOTE /
  DISCUSSION question it must settle in writing.
- **CHRN-34** — `HOLD` is an operator action on any item, not a proposable
  destination (§4). `DISCARD` marks; CHRN-22's pruner deletes.
- **CHRN-35** — inherits `project_key`, `ticket_type` and `description` from
  §3, and inherits the rule that an empty project key is completed by a person.
- **CHRN-36** — grades §3's shape; sets §8's threshold; and depends on §2's
  proposer string to attribute a regression to a prompt or to a model.

## What this does not decide

- **The prompt.** CHRN-30.
- **How the catalogue is assembled or cached within a run.** CHRN-31.
- **Batch semantics, partial failure, idempotent accepts.** CHRN-33.
- **The threshold's value.** CHRN-36.
- **Where a NOTE or DISCUSSION actually lands.** E5 and E6; §6 flags it for
  CHRN-33.
- **The eval corpus.** Decided separately with magos on 2026-08-30: hybrid and
  stratified, ~15 real memos held out for scoring plus ~25 synthesised for
  destination coverage, reported per stratum.

## The three rulings

**R1 · May Scribe propose `DISCARD`, and is `HOLD` correctly withheld?**
Recommendation: **yes to DISCARD, and yes, HOLD is withheld** (§4). DISCARD is a
judgement about content that `reason` makes checkable; HOLD is a claim about the
operator's intent that a model cannot hold, and that would become the abstention
default that makes the router unmeasurable. If you would rather Scribe never
propose throwing anything away, DISCARD becomes an operator-only action too and
§3's fourth variant disappears — the contract is unharmed, and the cost is that
the microphone-test memos have to be recognised by hand.

**R2 · Is a TICKET proposal with `project_key: ""` valid-but-incomplete, or
invalid?** Recommendation: **valid, with `status = 'needs_input'`** (§3). It is
the salvaged prompt's rule that an unguessable project must come back empty
rather than defaulted, because the key is immutable once the ticket exists. That
rule is only worth having if an empty key survives to a screen where a person
can fill it in. Marking it invalid would teach the model to guess.

**R3 · How long do superseded proposals live?** Recommendation: **one
generation**, in `superseded_payload` (§2). Enough to answer "did that prompt
change help", which is the only question asked of it. The alternative — full
append-only history — is a table that grows for every re-run with no reader, and
tier 1 is explicitly disposable. If CHRN-36 turns out to want run-over-run
history, it should own that table and its retention rather than growing this
one.

## Done when

The `Done when` on the ticket, mapped to this document:

- *every proposal that reaches the API is schema-valid* — §5 stage 1, with §7
  guaranteeing that a proposal which cannot be made valid is recorded as invalid
  rather than dropped.
- *a destination outside the live catalogue is rejected rather than displayed* —
  §5 stage 2, run at write **and** at acceptance.
- *a hallucinated page path cannot be accepted* — §5 stage 2 against §6's
  catalogue, which today admits no page at all.
