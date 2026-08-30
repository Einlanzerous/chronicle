# CHRN-32 — The proposal contract (decision)

Status: **proposed 2026-08-29.** Four rulings open at the end of this document.
Ticket: CHRN-32 (Phase P2, parent CHRN-4). Tier `opus`, so Mode B: this document
is the review artefact and the PR that follows it is mechanical.
Decision owner: magos.
Read by: **CHRN-30** (the prompt must emit exactly this), **CHRN-31** (the
catalogue is what §5 validates against), **CHRN-33** (the batch API's unit),
**CHRN-34** (HOLD and DISCARD), **CHRN-35** (the TICKET route's inputs),
**CHRN-36** (the eval set grades this shape), **CHRN-52** (§1.1 hands it a
decision), and later **CHRN-55** (the triage screen) and **CHRN-67** (the MCP
write tool).

**Revised twice on 2026-08-29 after review.** `[rev 2]` folds in three
at-acceptance amendments from the second pass, in CHRN-22's manner: who connects
as the tier-1 role in E4 (§1.1), `raw_output`'s pairing with `payload` (§7), and
`generation` moving on every payload mutation rather than only on a supersede
(§2). Plus the transcript-ranking correction in §2. None of them moved a
position.

**Revised 2026-08-29 after review.** One blocking finding and five smaller ones,
marked **[rev]**. The blocking one was in §1, and it was the good kind: the
section's argument survives intact, but the sentence carrying its enforcement
mechanism described a role that cannot read Scribe's input and that nothing runs
as today. That is now §1.1 and ruling **R4**. None of the four load-bearing
positions moved, and no ruling changed direction.

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
**E5**) and threads (CHRN-43, **E6**). Two of this contract's four proposable
destinations therefore have nowhere to land yet. §6 settles what the contract
does about that, because the alternative is discovering it inside CHRN-33's
diff.

`CLAUDE.md` names the thing this document must not get wrong, and names it in
the sentence that lists what tier 1 is:

> Tier 1 is the estate's account of what exists … plus whatever Chronicle
> derives from its own corpus: **Scribe proposals**, extracted entities, search
> indexes.

A proposal is derived. That one word decides §1, and §1 decides most of the
rest.

## Decision

1. A proposal is a **tier-1 row**, not a tier-2 row and not a transient value.
2. **[rev]** Its identity is `(memo_id, proposer)`, on `tier2.transcripts`'s
   pattern, and it carries a **`generation`** and the **`transcript_id`** it was
   derived from.
3. The payload is a **discriminated union on `destination`**, validated in two
   stages that fail differently.
4. **`reason` is required, non-empty, and schema-level.**
5. **Scribe may propose `DISCARD`. Scribe may never propose `HOLD`.** **[rev]**
   And `DISCARD` is never pre-acceptable, by contract rather than by threshold.
6. **`nearest_page` ships now, nullable, validated against a catalogue whose
   page half is empty until E5** — the shape is final, the constraint tightens.
7. A proposal that will not validate becomes a **recorded failure**, never a
   silent absence — and **[rev]** never displaces a payload that was valid.
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
> different code paths reaching different schemas.**

Scribe writes proposals all day and never marks a memo `triaged` — that
transition belongs to the acceptance path, and it is driven by a person. A
router that could route *and* commit would be a tier-1 process authoring tier-2
state, which is the fabrication the tier split exists to prevent.

It also disposes of a question CHRN-33 would otherwise have to answer: what
happens if Scribe re-runs while an operator has the triage screen open. The
proposal is regenerable and the memo's state is not, so a re-run rewrites tier 1
and touches no memo state the operator has decided. (It can still change a
proposal out from under them, which is §2's `generation`.)

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

## 1.1 · [rev] Which role Scribe runs as — and the grant that has to move

**The first draft of §1 said "Scribe runs as `chronicle_tier1` and writes
proposals all day." That sentence was false twice, and review caught it.**

`migrations/0001_init.up.sql:36` is `REVOKE ALL ON SCHEMA tier2 FROM
chronicle_tier1`, and the comment above it says the role "cannot see tier 2 at
all." Scribe's *input* is `tier2.transcripts`, joined to `tier2.memos` to know
which memos are `transcribed` and untriaged. **A role with no `USAGE` on schema
tier2 cannot `SELECT` either one.** As designed, the role cannot run Scribe.

And nothing runs as it today. `internal/config/config.go` carries exactly one
DSN, `CHRONICLE_DATABASE_URL`; `CHRONICLE_TEST_TIER1_DATABASE_URL` exists only
so `verify.sh` can run the isolation test. E3's worker writes `tier1.memo_jobs`
*and* `tier2.transcripts`, so it necessarily connects as `chronicle`. The role
is a grant target with no process attached, and **CHRN-52 — P3, a different
epic — is the ticket that decides how a process gets those credentials.**

So this was an enforcement mechanism asserted rather than described. It is
exactly the cross-epic edge §6 exists to surface, and §1 had missed its own.

### The deeper problem: 0001's grant is stricter than the doctrine it enforces

This is not a Scribe-specific inconvenience to be worked around. `CLAUDE.md`
defines tier 1 as the estate's account of what exists **"plus whatever
Chronicle derives from its own corpus: Scribe proposals, extracted entities,
search indexes."**

Every one of those three derives *from tier 2*. Proposals derive from
transcripts. Extracted entities derive from notes and transcripts. CHRN-41's
search index covers "notes and transcripts" by its own title. **You cannot
derive from a corpus you cannot read.** A tier-1 role with no read on tier 2
makes the entire second half of the tier-1 definition unimplementable.

E4 is simply the first ticket to touch it. **[rev 2]** CHRN-41 hits it harder
and hits it in E5: its title is "full-text search over notes **and
transcripts**", so its index cannot be built at all without reading tier 2.
(The first draft named CHRN-42 alongside it. Review is right that it does not
belong — backlinks and tags are *authored*, which its own ticket calls "the one
kind of link that is stored". CHRN-41 carries the argument alone, and it is
enough.)

Note also what the invariant actually requires. `CLAUDE.md`: *"a test proving no
tier-1 **write path** can reach a tier-2 table is the proof."* The doctrine is
about writes. 0001's comment — "cannot see tier 2 at all" — is strictly stronger
than the invariant it cites, and it is the stronger half that breaks.

### The two options

**(a) Grant the tier-1 role read on exactly its inputs.**

```sql
GRANT USAGE ON SCHEMA tier2 TO chronicle_tier1;
GRANT SELECT ON tier2.memos, tier2.transcripts TO chronicle_tier1;
```

`SELECT` only, on two named tables, never `ALTER DEFAULT PRIVILEGES` — so a
tier-2 table added later is unreadable until someone grants it deliberately.
Notably **not** `tier2.users` or `tier2.user_tokens`, which 0002 revokes by name
and which are the tables whose exposure would matter most.

The invariant survives verbatim: no tier-1 **write** path reaches a tier-2
table, and CHRN-52's test is unchanged in what it must prove. What changes is
0001's comment, which has to stop claiming the role cannot see tier 2 and start
saying which two tables it can read and why.

**One correction to the review here, because it changes this option's risk.**
The review warned that a loosened grant would not surface in `schema.sql`,
citing 0002's comment. That comment says the opposite, and says so because
CHRN-78 corrected exactly this claim: *"pg_dump emits only non-default ACLs, so
revoking a privilege the role never held leaves nothing to emit … **A loosened
GRANT is a non-default ACL and shows up in the diff on its own.**"* So CHRN-77's
generated-schema guard **does** catch a later widening from `SELECT` to
`INSERT`. That is the difference between a deliberate narrow grant and an
un-guarded hole, and it is why (a) is safe to take.

**(b) Scribe runs as `chronicle`; the boundary is a code-path guarantee for
now.** `internal/scribe` is given a store interface with no method that writes
any `tier2.*` table, proven by test, and CHRN-52 later moves it onto the role
once it decides how credentials are issued.

Honest about what exists and requires no migration. Weaker: a code-path
guarantee is the thing the doctrine explicitly says is *not* the enforcement
mechanism ("A separate database and role is the enforcement mechanism").

### Recommendation

**(a)**, recorded as a deliberate amendment to 0001 in a new migration rather
than an edit to it, with 0001's comment corrected in the same change, and
flagged to CHRN-52 so its test is written against the grant that exists rather
than the one 0001 describes.

**[rev 2] (a) includes a second DSN and a second pool, in E4.**
`CHRONICLE_TIER1_DATABASE_URL`, and `internal/scribe` gets its own pool opened
as `chronicle_tier1` — Scribe does **not** keep running as `chronicle` until
CHRN-52 issues credentials. Review is right that the alternative would ship E4
with the enforcement decorative, which is the precise argument §1.1 makes
against option (b): a role nothing connects as is a role that enforces nothing.

This is a config-surface change and so it is stated rather than implied. It
falls back to `CHRONICLE_DATABASE_URL` when unset, which keeps a single-DSN
deployment working and makes the tier-1 pool an addition rather than a
prerequisite. **CHRN-52 owns how deploy actually issues the credential** —
Signet, compose, and whether the fallback is allowed to survive in production.

The reason to prefer it over (b) is not Scribe's convenience. It is that (b)
leaves a known-wrong grant in place for another epic and a half, during which
CHRN-41 will have to route around it, and will be tempted to route around it by
running as `chronicle` — at which point the role has no users at all and the
enforcement mechanism is decorative.

**Ruling R4.** This is a change outside E4, so under the working agreement it is
magos's to take and not mine.

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

### [rev] The third axis: which transcript it was derived from

The identity argument above names two inputs, model and prompt. **There is a
third, and review caught it: which transcript.**

`tier2.transcripts` is unique on `(memo_id, model)`, so a memo can carry several
— and the retranscribe path that produces them is real, not hypothetical
(`chronicle retranscribe`, CHRN-27). A `medium.en` transcript arriving for a
memo that already has `small.en`, followed by a Scribe re-run under the *same*
proposer, produces a superseded payload that CHRN-36 cannot attribute: the model
did not change and the prompt did not change, so the diff looks like
nondeterminism when in fact the input text changed.

So the row records **`transcript_id`**. Not in the key — one proposal per memo
per proposer is still the right identity, and adding the transcript to the key
would let two transcripts of one memo each carry their own proposal, which is a
triage screen showing the same memo twice.

**Which transcript Scribe reads**, when there are several: the same predicate
the pump and pruner already share — `HasDurableTranscript` and CHRN-22's
two-axis model floor — and, among those that qualify, the highest-quality model.
Stating it because it was unstated, and because a router silently reading the
`small.en` transcript of a memo that has a `large-v3` one is a routing error
nobody would ever trace to its cause.

**[rev 2] The predicate is shared; the ranking is new, and it diverges from the
one helper that exists.** Review is right to make the distinction.
`store.SufficientModels` is an unranked *set* — `{small.en, medium.en,
large-v3}` — and says nothing about which is better. `store.GetTranscript`
orders by `partial ASC, transcribed_at DESC`: **most recent complete, not
highest quality.** So Scribe cannot reuse it, and a memo re-transcribed *down*
to `small.en` after a `large-v3` run would hand Scribe the worse text.

The rank is therefore a new thing this contract introduces, and it lives
**hard-coded beside `SufficientModels`, never in configuration** — that
constant's own comment gives the reason and it applies unchanged: the file is in
`sensitive_paths`, so a constant is reviewed at the expensive tier and an
environment variable is reviewed by nobody. Scribe's selector is
`HasDurableTranscript`'s clause plus that rank, and it is a distinct helper from
`GetTranscript` rather than a change to it — `GetTranscript` serves display,
where most-recent is the right answer.

### [rev] `generation`, and the drift §1 argued against

§1 rejects transient proposals partly because they would drift under the
operator's cursor. Review's finding: supersede-in-place reintroduces exactly
that. An operator who taps ACCEPT on the proposal they are looking at, while a
re-run has just replaced it, accepts a proposal **they never saw**.

The row carries a **`generation`** — a counter, incremented on **every payload
mutation**, not only on a supersede. `GET` hands it to the client; an accept
must send it back; and §5's stage 2 at acceptance **refuses a mismatch** rather
than accepting silently. The memo goes back to the operator with the new
proposal shown.

**[rev 2] "Every mutation" and not "every re-run", because a supersede is not
the only door.** Review found the second one: stage 2 at acceptance can itself
change the payload — a project archived between Tuesday and Thursday clears
`project_key` and sets `needs_input` — and no Scribe run was involved. Under
"increments on supersede" the counter would not move, and the client's next
accept would echo a generation that still matched a payload that had changed.
Same drift, different door. So the rule is the payload's, not the run's: if the
bytes an operator could be looking at are not the bytes on the row, the
generation has moved.

The column is this contract's. The 409 and how the batch reports it are
CHRN-33's.

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
| `title` | string | yes, **[rev]** except on `DISCARD` | Imperative, ≤ 100 chars. The salvaged prompt's rule, unchanged. |
| `nearest_page` | string \| null | yes, may be null | Advisory. A page path. §6, and §5 for what a bad one does. |

**[rev] `title` is optional on `DISCARD`.** Review's point: the prompt would
otherwise have to invent an imperative title for something being thrown away,
which is a fabrication the operator has no use for. The batch row labels a
discarded memo with its transcript's first line instead — CHRN-33's display
choice, noted here so it is not read as a missing field.

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
| `page_path` | string \| null | yes, may be null | Where the note should live. **[rev]** May name a page that does not exist; its nearest existing ancestor must be live. Null until E5 — §6. |
| `body` | string | yes | The cleaned-up note text. |

**[rev] `page_path` may propose a new page.** Review found that requiring
`page_path` to be live would pre-decide that a note can only ever land on a page
that already exists — a real constraint on E5, decided here by accident, and one
that would make `page_path` and `nearest_page` redundant for NOTE.

They are different fields doing different jobs. `nearest_page` is **advisory**:
it feeds the reason line the canvas shows (*"Nearest existing page is
`storage/amber`"*) and is never a landing target. `page_path` is the **target**,
and it may name a page that does not exist yet, provided its nearest existing
ancestor is in the catalogue — which is what stops `a/b/c/d/e` being invented
whole.

Today the behaviour is identical either way, because no page exists and no
ancestor can be live, so every non-null `page_path` is cleared. The shape is
fixed now because §6's whole argument is that the shape gets fixed now; E5 keeps
the right to tighten the ancestor rule.

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

### [rev] DISCARD is never pre-acceptable, and that is a contract rule

Review's corollary to R1, and it is right.

`discarded` is **terminal** in the memo state machine, written down where it can
be read: `0003_memos.up.sql` lists it only ever as a transition *target* and
never as a source, and the comment says that is how terminal is expressed. There
is no `discarded>` anything.

§8's single `CHRONICLE_SCRIBE_PREACCEPT_MIN` cannot express "this destination is
never in ACCEPT ALL", because a confident wrong DISCARD is exactly the case that
clears any threshold. A `DISCARD` at 0.92 swept up by ACCEPT ALL is a memo that
can never be re-routed.

It is not data destruction — the transcript is tier 2 and survives, and CHRN-22
prunes only audio, only behind a durable transcript. But it is **the one accept
that cannot be undone**, and it should cost a deliberate tap.

So, by contract and not by threshold: **a `DISCARD` proposal is never
pre-accepted, at any confidence.** It is displayed, with its reason, and it
requires an explicit per-item action. CHRN-33 and CHRN-34 inherit this.

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
- `page_path` is non-null ⇒ its nearest existing ancestor must be **live**.
- `nearest_page` is non-null ⇒ it must be a **live** page path.

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
catalogue is already being assembled for the batch. **[rev]** The acceptance
check also verifies §2's `generation`.

### [rev] A bad target blocks; a bad advisory field does not

The first draft cleared the offending field and marked the proposal
`needs_input` uniformly. Review found that this makes the primary surface worse
for nothing, and the fix is to split the fields by what they do.

**Targets — `project_key`, `page_path`.** Clearing one leaves the proposal
unable to land. Status becomes `needs_input`; the operator must supply the
missing target. `destination`, `confidence` and `reason` are kept, because they
are still the model's honest answer: a proposal whose project was archived needs
a new project, not a re-route from scratch.

**Advisory — `nearest_page`.** It feeds the reason line and nothing else. A
hallucinated one is **cleared, and the status does not change** — an otherwise
perfectly acceptable TICKET does not become an item the operator has to touch
because the model invented a page name in a sentence.

The `Done when` still holds strictly: a cleared field cannot be accepted, so a
hallucinated page path cannot be accepted.

**Every clearing is recorded** on the row, not silently dropped, so CHRN-36 can
report a hallucination rate per proposer. A model that invents a page in one
proposal out of three is a fact about the prompt, and it is only visible if the
clearing leaves a trace.

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
cleared by stage 2, because no page and no ancestor is in the catalogue. The
`Done when`'s "a hallucinated page path cannot be accepted" is satisfied —
strictly, and by the general mechanism rather than a special case. As CHRN-37
lands, the catalogue fills and the same check starts admitting real paths with
no code change and no migration. The constraint tightens; the contract does not
move.

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
with `status = 'invalid'`, the validation error, and the **raw model output kept
verbatim** for CHRN-30 to read. It is tier 1 and disposable; a truncated field
here would hide the only evidence of why the prompt failed.

**The memo stays untriaged and stays visible.** It appears in the triage batch
carrying its error instead of a proposal, which is a memo the operator can route
by hand. It is not skipped, not filtered, not silently deferred.

`status` on the proposal row: `valid` | `needs_input` | `invalid`.
`needs_input` is §3's empty project key and §5's cleared target.

### [rev] A failed re-run never displaces a valid payload

Review's finding, and it is the same class as §2's drift: a memo that had a
perfectly good proposal on Monday, re-run on Tuesday under a bumped prompt
version that fails validation three times, would have had its payload replaced
by nothing. The operator loses a usable proposal to a prompt regression.

So a re-run that ends `invalid` **keeps the existing payload and its status**,
and records the failure alongside it — `generation` does not advance, and the
proposal the operator can see is still the one that validated. The row carries
the fact that the latest attempt failed; it does not carry the failure *instead
of* the proposal.

An `invalid` row with no payload therefore means one thing only: no run has ever
produced a valid proposal for this memo under this proposer.

### [rev 2] `raw_output` pairs with `payload`; a failed attempt goes elsewhere

Review caught the consequence: if the failed attempt's raw output overwrote
`raw_output` while the previous payload stayed, then `raw_output` would be
attempt N's junk beside attempt N−1's proposal — and CHRN-36, which the Surface
section keeps `raw_output` on every row *precisely* to diff what the model
literally said, would read the wrong pair.

**`raw_output` always holds the output that produced the payload sitting beside
it.** A failed attempt writes `error` and **`last_attempt_raw`**, and touches
neither `payload` nor `raw_output`. The invariant is one line and worth stating
as one: *`raw_output` is the text `payload` was parsed from, always.*

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

**[rev]** The threshold is a floor and not the only gate: §4's DISCARD exclusion
sits above it, and no value of `CHRONICLE_SCRIBE_PREACCEPT_MIN` admits a DISCARD
to ACCEPT ALL.

Corollary, and the reason the epic cares: `ACCEPT ALL` is licensed by
calibration, not by accuracy. This contract's job is to make sure the number is
present, in range, and attributable to a named proposer so that CHRN-36 can
decide what it is worth.

## Surface

Nothing in E4 is public yet; these are the shapes CHRN-33 builds on. **[rev]**
`generation`, `transcript_id` and the cleared-field record are new; `raw_output`
is now kept on every row.

```
tier1.memo_proposals
    id              UUID PRIMARY KEY
    memo_id         UUID NOT NULL          -- NO FOREIGN KEY into tier2.memos
    transcript_id   UUID NOT NULL          -- NO FOREIGN KEY either; §2
    proposer        TEXT NOT NULL          -- ollama/gemma4:31b@v1
    generation      INTEGER NOT NULL       -- §2; an accept must echo it
    status          TEXT NOT NULL          -- valid | needs_input | invalid
    payload         JSONB
    superseded_payload JSONB
    cleared_fields  JSONB                  -- §5; what stage 2 removed, and why
    error           TEXT                   -- the latest attempt's failure, if any
    raw_output      TEXT                   -- ALWAYS the text `payload` was parsed from
    last_attempt_raw TEXT                  -- §7; a failed re-run's output, kept apart
    created_at, updated_at
    UNIQUE (memo_id, proposer)
```

**[rev] No foreign key into tier 2, on either column, and that is not an
oversight.** 0004 established the rule, 0005 repeated it and 0006 states it on
`tier1.memo_jobs` in as many words: *a tier-1 table referencing tier 2 would be
the cross-schema path the doctrine exists to forbid.* The consequence is the
same one `memo_jobs` carries — a proposal row can outlive its memo — and the
same answer applies: a sweep collects the orphans. Stated here because §1 is
where CHRN-52's line is being drawn, and a reader who finds no FK should find
the reason next to it.

**[rev] `raw_output` on every row, not only on `invalid`.** Review's point: it
is tier 1, it is cheap, and it is what lets CHRN-36 diff two runs of the same
prompt. Keeping it only on failures means the successful runs — the ones the
eval set actually scores — are the ones with no evidence of what the model
literally said.

`internal/scribe/` — the package. Contract types, the two validators, and the
proposer string. Not `internal/api/`: CHRN-67 exposes routing as an MCP tool,
and a contract that only exists inside HTTP handlers is one that gets
reimplemented for the agent surface.

## Configuration

| var | meaning |
|---|---|
| `CHRONICLE_TIER1_DATABASE_URL` | **[rev 2]** §1.1. The tier-1 pool Scribe connects on. Falls back to `CHRONICLE_DATABASE_URL`; CHRN-52 owns how deploy issues it. |
| `CHRONICLE_SCRIBE_MODEL` | e.g. `gemma4:31b`. Qualified to `ollama/…@vN` for the proposer string. |
| `CHRONICLE_SCRIBE_OLLAMA_URL` | Ollama's base URL. |
| `CHRONICLE_SCRIBE_PREACCEPT_MIN` | §8. Set by CHRN-36, not by this ticket. |
| `CHRONICLE_SCRIBE_MAX_ATTEMPTS` | §7's retry ceiling. Default 3. |

## What each ticket inherits

- **CHRN-30** — emit exactly §3, including `reason`. Never `HOLD` (§4). No
  `title` needed on `DISCARD` (§3). The prompt version is part of the proposer
  string (§2), so a prompt change is a version bump and not a silent overwrite.
- **CHRN-31** — the catalogue is what §5 stage 2 validates against, and its page
  half is legitimately empty until E5 (§6). Live per run, never cached across.
- **CHRN-33** — the unit is a proposal row; `needs_input` is not `invalid`
  (§3, §7); stage 2 runs again at acceptance and **an accept must echo §2's
  `generation`, with a mismatch reported as re-show rather than accepted**;
  **DISCARD is never in ACCEPT ALL** (§4); and §6 names the NOTE / DISCUSSION
  question it must settle in writing.
- **CHRN-34** — `HOLD` is an operator action on any item, not a proposable
  destination (§4). `DISCARD` marks; CHRN-22's pruner deletes. **[rev]** Two
  state-machine facts it should not have to rediscover: `held` exits only to
  `queued` or `discarded` — there is no `held>transcribed`, so a memo released
  from hold re-enters E3's worker, which skips ASR because it already has a
  durable transcript; and `discarded` is genuinely terminal, so the ticket's
  "reversible for a window" is **the pruner's window on the audio, not a memo
  state that can be walked back**. That tension is CHRN-34's to resolve, and
  this is the line it will be resolved against.
- **CHRN-35** — inherits `project_key`, `ticket_type` and `description` from
  §3, and inherits the rule that an empty project key is completed by a person.
- **CHRN-36** — grades §3's shape; sets §8's threshold; depends on §2's
  proposer string to attribute a regression to a prompt or to a model, and on
  §2's `transcript_id` to attribute one to a re-transcription; and can report a
  hallucination rate from §5's `cleared_fields`.
- **CHRN-52** — **[rev]** §1.1 is a decision it inherits. Its isolation test
  should be written against the grant R4 settles, not against 0001's comment.
  **[rev 2]** E4 adds `CHRONICLE_TIER1_DATABASE_URL` with a fallback to the main
  DSN; CHRN-52 owns how deploy issues the credential and whether that fallback
  may survive in production.

## What this does not decide

- **The prompt.** CHRN-30.
- **How the catalogue is assembled or cached within a run.** CHRN-31.
- **Batch semantics, partial failure, idempotent accepts, the generation 409.**
  CHRN-33.
- **The threshold's value.** CHRN-36.
- **Where a NOTE or DISCUSSION actually lands.** E5 and E6; §6 flags it for
  CHRN-33.
- **How a process obtains tier-1 credentials.** CHRN-52; §1.1 hands it the
  grant question and R4's answer.
- **The eval corpus.** Decided separately with magos on 2026-08-29: hybrid and
  stratified, ~15 real memos held out for scoring plus ~25 synthesised for
  destination coverage, reported per stratum.

## The four rulings

**R1 · May Scribe propose `DISCARD`, and is `HOLD` correctly withheld?**
Recommendation: **yes to DISCARD, and yes, HOLD is withheld** (§4), **with
DISCARD excluded from pre-acceptance by contract** (§4, [rev]). DISCARD is a
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

**R4 · [rev] Does `chronicle_tier1` gain `SELECT` on `tier2.memos` and
`tier2.transcripts`?** Recommendation: **yes — option (a) in §1.1**, in a new
migration that also corrects 0001's comment, flagged to CHRN-52 — and **[rev 2]
including `CHRONICLE_TIER1_DATABASE_URL` and a second pool in E4**, so Scribe
actually connects as the role being granted to rather than leaving the
enforcement decorative for another epic and a half.

This is the blocking finding from review and it is outside E4, so it is yours to
take. The short version: 0001 grants the tier-1 role *less* than the doctrine
requires it to have. `CLAUDE.md` defines tier 1 to include "whatever Chronicle
derives from its own corpus", and every example it gives — proposals, extracted
entities, search indexes — derives from tier 2. A role that cannot read tier 2
cannot derive anything from it. E4 is the first ticket to hit this; CHRN-41 hits it
harder and hits it in E5, since its index covers notes *and transcripts*.

The invariant is untouched, because the invariant is about writes: *"no tier-1
write path can reach a tier-2 table."* `SELECT` on two named tables is not a
write path, `tier2.users` and `tier2.user_tokens` stay revoked, and CHRN-77's
schema guard does catch a later widening — a loosened `GRANT` is a non-default
ACL and appears in the diff on its own, which is the correction CHRN-78 already
made to 0002's comment.

If you would rather not move a grant this epic, option (b) is honest and
cheap: Scribe runs as `chronicle`, the boundary is a code-path guarantee proven
by test, and CHRN-52 moves it later. The cost is that the role stays unused
through E5, where CHRN-41 will have to route around it the same way.

## Done when

The `Done when` on the ticket, mapped to this document:

- *every proposal that reaches the API is schema-valid* — §5 stage 1, with §7
  guaranteeing that a proposal which cannot be made valid is recorded as invalid
  rather than dropped, and never at the cost of a payload that was valid.
- *a destination outside the live catalogue is rejected rather than displayed* —
  §5 stage 2, run at write **and** at acceptance.
- *a hallucinated page path cannot be accepted* — §5 stage 2 against §6's
  catalogue, which today admits no page at all; a cleared field cannot be
  accepted whether or not it moved the status.
