# CHRN-36 — The routing eval set, and what a score is allowed to mean (decision)

Status: **proposed 2026-08-30.** Four rulings open at the end.
Ticket: CHRN-36 (Phase P2, parent CHRN-4). Tier `opus`, so Mode B: this document
is the review artefact and the PR that follows it is mechanical.
Decision owner: magos.
Read by: **CHRN-30** (which it grades, and which may not look at half of it),
**CHRN-33** (the threshold gates ACCEPT ALL), **CHRN-31**, and later **CHRN-69**
(routing accuracy is one of its metrics).

## Context

This is the oracle for E4. Everything else in the epic — the confidence display,
ACCEPT ALL, the batch API — assumes proposals are usually right, and nothing
currently measures that.

**The corpus now exists**, which is new since the ticket was written and changes
the shape of this decision. Seventeen real memos were captured on 2026-08-30
through the CHRN-19 inbox, transcribed on the R9700, and are live in
`tier2.transcripts`: 31 minutes of audio, ~32 000 characters of transcript, all
`whisper.cpp/small.en`, zero failures. They are ordinary voice notes recorded
because magos wanted to capture something, not performed for a test.

Each also arrived with **an independent transcript from Google Recorder**, which
turns out to be worth more than a sanity check — see §6.

Two things about them that are not obvious and that this document has to absorb:

- They are **not evenly spread across destinations**. Eleven or so are work.
- They contain **two routing cases the contract does not handle** (§7), and
  scoring has to know that or it will mark the router wrong for them.

## Decision

1. **The labels live in the repo. The transcripts do not.** `content_hash` is
   what makes a run reproducible, not a copy of the corpus.
2. **Two strata, never averaged**: `real` (held out) and `synthetic`
   (development). CHRN-30 may look at one of them.
3. **A label carries its own ambiguity**, because a memo two people would label
   differently caps the accuracy anyone can achieve.
4. **Two scores, reported separately**: destination accuracy, and whether
   confidence predicts correctness. Only the second licenses ACCEPT ALL.
5. **The threshold is stated with its evidence**, including how weak that
   evidence is at n=17.
6. **Google's transcripts are a control, never an input.**
7. **Known-unhandled cases are labelled as such** and scored apart.

---

## 1 · The labels live in the repo; the transcripts live in Chronicle

The `Done when` says "the set exists in the repo". The natural reading is that
the transcripts are committed. They must not be.

**They are tier 2.** `CLAUDE.md`: *"Tier 2 is what a person said or wrote —
memos, transcripts, notes, discussions, plans. None of it is derivable from
anything and none of it can be rebuilt."* Their home is `tier2.transcripts`,
behind the role boundary migration 0007 exists to draw. Copying them into git
makes a second source of truth for authored content, which is the failure
invariant 2 exists to prevent — and it would be an odd thing to do in the same
epic that argues for linked-not-copied.

**And reproducibility does not need them.** What makes a run reproducible is
knowing the input did not change, and `tier2.memos.content_hash` already gives
that: immutable, computed over the bytes exactly as they arrived, never
recomputed. If the hash matches, the transcript scored is the transcript
labelled. The file is not the versioning mechanism; the hash is.

So, committed:

```
docs/eval/routing-v1.yaml     labels, reasons, ambiguity, strata, content hashes
docs/eval/synthetic/*.md      the synthetic transcripts (see below)
internal/scribe/eval/         the harness
```

Not committed: real transcript text, and audio.

**One exception, and it is principled rather than convenient: the synthetic
stratum is committed in full.** Nobody said those — they are fixtures written to
test a router, not authored corpus — so they are not tier 2 and have no other
home. The tier test settles it in one question: *did a person say this into a
recorder?*

### The cost, stated rather than discovered

A reviewer checking `memo abc123 → NOTE, because it argues a principle` cannot
verify that from the repo alone; they need database access. On this box that is
fine and CHRN-36 is graded by magos on a machine that has it, but it is a real
narrowing.

It also makes the eval set depend on the corpus surviving, which puts weight on
**CHRN-68**'s backup and restore drill. That ticket already says the drill
happens *before* the store holds anything irreplaceable. These 17 memos are the
first genuinely irreplaceable content, so its timing is now a real question
rather than a theoretical one.

## 2 · Two strata, and CHRN-30 may only look at one

| stratum | n | what it is | who may look |
|---|---|---|---|
| `real` | 17 (+4 recommended, §3) | memos magos recorded, transcribed by whisper | **CHRN-36 only** |
| `synthetic` | ~25 | transcripts written as fixtures, from estate material | CHRN-30 freely |

**Accuracy is reported per stratum and never averaged.** A single blended number
hides the only comparison that matters: whether the router does worse on real
speech than on text somebody wrote to be routed. Real memos carry disfluencies,
self-interruption, ASR errors and sentences that never resolve; synthetic ones
carry whatever the author unconsciously made easy.

### Why the real stratum is held out, and what that costs

If CHRN-30 tunes the prompt against the set that scores it, the score measures
memorisation. So the real stratum is the held-out half and CHRN-30 develops
against the synthetic one.

The cost is real and worth naming: **the honest distribution is the one CHRN-30
may not see.** Prompt work therefore happens against material that is
systematically easier than production, and the real stratum's job is to reveal
the gap rather than to close it.

**And a held-out set degrades every time it is looked at.** Score against it,
change the prompt because of what you saw, score again, and it has quietly
become a development set — the tuning just happened through a human instead of a
loop. So: **every scoring run against `real` is recorded in the doc** with its
date, proposer string and result. Not ceremony; it is the only way to notice
that the number stopped meaning what it did on the first run.

Ruling R1 asks whether n=17 is even enough to hold out.

## 3 · What the corpus actually contains, and what it is missing

Proposed labels for the 17, for magos to correct — the labels are his judgement,
not mine, and this table is the thing to argue with rather than the schema
around it.

| memo | s | proposed | confident? | note |
|---|---|---|---|---|
| Aug 30 at 1-42 PM | 9 | `DISCARD` | yes | talks himself out of it in nine seconds |
| Skills or MCP? | 34 | `DISCUSSION` | yes | open question, leans one way, unresolved |
| Wiki cleanup | 43 | `TICKET` task | yes | concrete, scoped |
| Chronicle Work Mode | 48 | `TICKET` task | yes | carries a real deadline |
| System Autonomy | 59 | `NOTE` | **no** | direction, not work — or an epic |
| Interlock Improvements | 66 | `TICKET` task | yes | add MCP support |
| Sextant Improvements | 69 | `TICKET` task | yes | personal content, see R3 |
| Server Doctor | 83 | `NOTE` | yes | "not a plan, go build tomorrow"; personal content, R3 |
| City Maps | 105 | `TICKET` spike | **no** | "curious about the viability" — spike or note |
| Gemini at Home | 119 | `NOTE` | **no** | reflection on his own tool use |
| Smart Calendar | 119 | `TICKET` | yes | new integration |
| Portfolio | 120 | `TICKET` | yes | new project |
| Home SRE Agent | 141 | `TICKET` spike | yes | **dedup case**, §7 |
| Cafe Passport | 159 | `TICKET` | yes | **cross-reference**, §7 |
| Opportunistic Agent Cycling | 167 | `TICKET` spike | yes | "we need to investigate" |
| At a Glance | 217 | `TICKET` | **no** | **cross-reference**, §7; addendum to Smart Calendar |
| Automatic Improvements | 317 | `TICKET` spike | **no** | capability idea, could be doctrine |

Roughly **11 TICKET, 3 NOTE, 1 DISCUSSION, 1 DISCARD**, with five where a second
labeller could reasonably disagree.

### What is missing, and why it is worth four more recordings

`DISCUSSION` and `DISCARD` are **n=1 each**. At that count a correct answer is
indistinguishable from a lucky one, and DISCARD is the destination CHRN-32
excluded from pre-acceptance precisely because it cannot be undone. Grading the
one irreversible accept entirely on synthetic data is the weakest part of this
set.

Recommended before scoring: **two more DISCUSSION** (an open question with the
tension stated and no decision), **two more DISCARD** (a false start, a
mis-tap). One more clear `NOTE` would help but is less urgent — there are three.

This is not padding. TICKET is the path vox-dictate already proved end to end;
what E4 adds is that TICKET is one destination among several, and a corpus that
is two-thirds TICKET grades mostly the half that already worked.

## 4 · A label carries its own ambiguity

```yaml
- hash: 2ec3cf5b…          # tier2.memos.content_hash — the identity
  stratum: real
  destination: DISCUSSION
  ticket_type: null
  confident: true
  reason: >
    An open question with the tension stated and no decision reached. Leans
    toward MCP but says explicitly it is worth investigating.
  also_defensible: []
  unhandled: []
```

**`confident` and `also_defensible` are the load-bearing fields**, and they exist
because of what §3's table shows: five of seventeen are genuinely arguable.

Where two labels are both defensible, the router picking the other one **is not
a router failure** — it is the ceiling on what any router can score. Without
recording that, CHRN-30 tunes the prompt against noise and the number drifts
upward while nothing improves.

So scoring reports **two accuracies**: strict (the label) and lenient (label or
any `also_defensible`). The gap between them is the ambiguity tax, and it is a
property of the corpus rather than of the prompt. A prompt change that moves
strict accuracy without moving lenient accuracy has learned the labeller's
preferences, not the task.

## 5 · Two scores, and only one licenses ACCEPT ALL

The ticket is explicit that these are separate, and it is right.

**Destination accuracy** — proportion correct, per stratum, strict and lenient,
with a per-destination breakdown. The breakdown matters more than the headline:
at n=17 an overall 88% could be one router that never gets DISCARD right.

**Calibration** — does confidence predict correctness. This is what licenses
ACCEPT ALL, and the epic is explicit that a well-calibrated 0.86 beats an
uncalibrated 0.95.

At n≈42 the usual instruments are noise. Expected calibration error over ten
bins on forty items measures binning, not calibration. So:

- **Three buckets**: `< 0.5`, `0.5 – 0.8`, `≥ 0.8`. Report n and accuracy in
  each.
- The claim to check is monotonicity — **does accuracy rise with confidence at
  all?** A router whose high-confidence answers are no better than its
  low-confidence ones has a confidence field that is decoration, and no
  threshold can rescue it.
- Report the **count of confident-and-wrong** items explicitly, since those are
  what ACCEPT ALL would actually ship.

A single summary number is deliberately not specified. At this n it would invite
comparisons it cannot support.

## 6 · Google's transcripts are a control, never an input

Every real memo has a second transcript from Google Recorder, produced by an
independent ASR from the same audio. That is an unusual thing to have and it
buys a measurement E4 otherwise has no way to make.

**Never as router input.** Production reads `tier2.transcripts`, which is whisper
output. Scoring the router on Google's text measures a router that will never
exist — the same distribution error as the synthetic stratum, better dressed.

**As a control: run Scribe on both transcripts of the same memo and measure how
often the destination changes.** That tests an assumption this repo has already
committed to in code but never checked: `store.TranscriptForScribe` ranks
transcripts by model quality — `large-v3` over `medium.en` over `small.en` — on
the reasoning that a worse transcript is a worse routing input. Nobody has
verified that routing is sensitive to transcript quality at all.

- If destinations barely move, the ranking is harmless but idle, and the model
  floor conversation gets simpler.
- If they move, the size of the move is the first real evidence about how much
  transcript quality the router needs — which feeds CHRN-22's floor and CHRN-85.

An early observation supporting the second: on *Skills or MCP?* whisper captured
a closing clause Google's rendering does not surface — *"I tend to lean towards
the MCP side but it's definitely worth investigating"* — and that clause is
exactly what separates an unresolved DISCUSSION from a decided TICKET. Two
transcripts of one memo can carry different amounts of the deciding signal.

**Google's text is not stored in `tier2.transcripts`.** It would be rejected by
the durable floor as an unmeasured runner — correctly — and those memos' audio
would then never prune while the pump kept retrying them. It lives beside the
eval set as reference data.

## 7 · Two cases the contract does not handle, labelled as such

Found by reading the corpus, and both would otherwise be scored as router
failures for doing the only thing they can.

**An idea arriving in more than one pass.** At least four memos refer back to
earlier thinking — *"this is probably an add-on to the smart calendar"*,
*"building off the city Maps thing"*, *"I reference that in that calendar note
too"*.

The first draft of this section called these "cross-references" and said nothing
resolves *"the smart calendar one"* to a **prior memo**. That framing was wrong,
and magos corrected it: **the target is a page in the corpus, not a memo.** Memos
are captures; notes are the durable thing. So the question is never "which memo
did you mean" — it is *which page does this belong to, and what should it do to
what is already there?*

And the shape that matters is not linking. It is **an idea getting richer over
time**: a vague high-level thought, then a trade-show conversation, a video, a
session with Claude, and a second memo carrying a far more concrete version of
the same idea. What should happen to the first note is a real question with more
than one defensible answer — append, supersede, or stand beside it as something
related.

Which means the missing piece is a **verb**, not a reference. CHRN-32's contract
already carries `page_path`, and §6 of that decision already allows it to name a
page that does not exist yet. What it cannot say is *create* versus *append*
versus *supersede*.

**That verb is not this ticket's to define, and deliberately not CHRN-32's
either.** It belongs to **CHRN-39** (append-only revisions and soft delete),
which is Mode C precisely because it governs what may happen to authored text —
its own description already names the hazard: *"the revision log is what makes
that recoverable when the model does something confidently wrong at 3 a.m."* An
auto-append to the wrong note is that failure exactly. Resolving a memo to a
candidate page additionally needs retrieval over the corpus, which is
**CHRN-41**.

The safety rule follows from the epic's own thesis and should not wait for
either ticket: **Scribe proposes a target and a verb; a person confirms. Nothing
appends to authored text unattended.**

**One consequence for §3's labels.** The proposed destinations for these four
are provisional in a deeper way than the others. *At a Glance* is labelled
`TICKET`, but if the corpus can express "append to the Smart Calendar page" it
may well be a `NOTE` with a verb — and the honest answer is that its correct
label depends on a capability that does not exist. That is what `unhandled:`
records, and it is why these four are scored apart rather than counted wrong.

**Dedup against existing work.** *Home SRE Agent* opens *"I think I've talked
about it, but I'm pretty sure I don't have a ticket for it."* That is a memo
asking the router to check Switchyard for a duplicate before creating one.
Nothing in the contract does that.

Both are recorded in `unhandled:` on the label, **excluded from the headline
accuracy, and reported as their own line**. They are not defects in CHRN-30 and
tuning a prompt against them would be tuning against a missing feature. The
count is the useful output: *four of seventeen real memos want a capability the
contract does not have* is a finding for E4 and E5, not a score.

## 8 · The harness

`internal/scribe/eval`, driven by a `chronicle eval` subcommand. It reads the
labels, resolves each `hash` to a memo, pulls the transcript through
`TranscriptForScribe`, runs Scribe, and writes a scored report.

**It cannot run in CI today, and that is not a gap to close here.** A scored run
needs Ollama and `gemma4:31b`, which exist only on this box — a GitHub-hosted
runner could never execute it whatever is committed. **CHRN-83**, which moves ci
onto `chronicle-runner`, is what would make it possible at all.

Two things to know before anyone tries:

- **GPU contention.** A routing eval and memo transcription both want the R9700,
  and nothing arbitrates. CHRN-26's decision named this exact gap: `asrd` can
  single-flight *transcription* but not the device, because Ollama does not go
  through it. A 42-item eval on every PR would contend with the pump.
- **Nondeterminism.** Ollama's default temperature is 0.8 and the salvaged
  prompt sets `format: json` but no temperature, so the same memo can route
  differently run to run. An accuracy gate would fail PRs on noise. **CHRN-30
  should pin decoding regardless of whether CI ever runs this** — an
  uncalibrated router that is also irreproducible cannot be debugged either.

## What this does not decide

- **The prompt.** CHRN-30.
- **The threshold's value.** Produced by the first scored run, not chosen here.
- **Eval in CI.** §8 records what it would need; CHRN-83 is the prerequisite.
- **Whether the contract should handle §7's two cases.** E4 and E5.
- **Retention of the corpus audio.** Currently `days_30`, so these prune around
  **2026-09-29**; and once pruned, `internal/watch/ingest.go:146` refuses
  re-ingestion, so §6's control and any re-transcription become impossible after
  that date. There is no supported way to pin a memo after capture — the
  per-memo permanent pin IDEA-21 locked in has no implementation. Flagged here
  because this ticket is the thing that would lose by it.

## The four rulings

**R1 · Is n=17 enough to hold out, or should the real stratum be split?**
Recommendation: **hold all 17 out, do not split.** Eight-and-nine would give a
development half too small to learn anything from and a scoring half too small
to trust. Better to develop entirely on synthetic and accept that the real
stratum is a single honest measurement taken rarely, with §2's log of every run.

**R2 · Record four more memos before the first scored run?** Recommendation:
**yes — two DISCUSSION and two DISCARD.** At n=1 those destinations are
unmeasured rather than measured, and DISCARD is the irreversible one. It is
maybe fifteen minutes of recording and it is the difference between a number
that covers four destinations and one that covers two.

**R3 · What happens to the two memos with personal content?** *Server Doctor*
(a local medical model for his own health data) and *Sextant Improvements*
(resume and job-hunting). Both are legitimate corpus and neither is a secret.
Recommendation: **keep them as memos, exclude them from the committed labels**,
and mark them `stratum: real, committed: false` so the count stays honest. The
label file is the only part that lands in git; excluding two costs the scored
set very little and does not require deciding anything about the corpus.

**R4 · Does the threshold ship at all if calibration is flat?** Recommendation:
**no — leave `CHRONICLE_SCRIBE_PREACCEPT_MIN` at its 1.01 default, which admits
nothing.** If accuracy does not rise with confidence, there is no value at which
pre-accepting is better than not, and shipping a number anyway would spend trust
the router has not earned — the precise failure CHRN-4 opens by naming. ACCEPT
ALL then waits for a prompt that calibrates, which is a CHRN-30 problem and not
a threshold problem.

## Done when

- *the set exists in the repo* — §1: the labels, reasons, ambiguity, hashes and
  harness, with the transcripts left where they belong.
- *a scored run is reproducible* — §1's `content_hash` identity plus §8's
  harness; and §2's run log is what keeps the second run comparable to the first.
- *a stated threshold below which proposals are shown but never pre-accepted* —
  §5 produces it, §4's ambiguity tax bounds what it can claim, and R4 settles
  what happens when the evidence does not support one.
