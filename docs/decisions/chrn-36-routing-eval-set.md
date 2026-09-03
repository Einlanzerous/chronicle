# CHRN-36 — The routing eval set, and what a score is allowed to mean (decision)

Status: **accepted 2026-08-30**, all four rulings at their recommendations.
R3's stated reason was corrected before acceptance — see the ruling; its
conclusion is magos's and stands, the premise it originally rested on did not.
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
recomputed. The file is not the versioning mechanism; the hash is.

**[rev] With one correction the first draft missed: the hash identifies the
audio, not the transcript.** A memo can carry several transcripts — they are
unique on `(memo_id, model)`, and `chronicle retranscribe` makes that ordinary —
and §8's harness pulls through `TranscriptForScribe`, which returns the
best-ranked one *at query time*. A `large-v3` pass over the corpus would change
the text every memo is scored from while every hash still matched, and 0007's
`transcript_id` comment names this failure exactly: *CHRN-36 reads it as
nondeterminism when the input text simply changed.* So the label records the
transcript it was assigned against (`labelled_against`, §4), and §2's run log
records the transcript each run actually scored. The hash proves the memo did
not move; the transcript identity is what makes run N comparable to run N−1.

So, committed:

```
docs/eval/routing-v1.yaml     labels, reasons, ambiguity, strata, hashes, transcript pins
docs/eval/synthetic/*.md      the synthetic transcripts (see below)
internal/scribe/eval/         the harness
```

Not committed: real transcript text — whisper's or Google's, §6 — and audio.

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
date, proposer string, result, **[rev] and the transcript each memo was scored
from** — model and id, as `TranscriptForScribe` served them, since a
ranking-driven input change would otherwise read as prompt drift (§1). Not
ceremony; it is the only way to notice that the number stopped meaning what it
did on the first run.

Ruling R1 asks whether n=17 is even enough to hold out.

### The run log

One row per scoring run that touched `real`. `chronicle eval` ends its report
with the block to paste; what follows is the summary line plus the per-memo
transcript pins, which is what makes run N comparable to run N−1.

| # | date | proposer | labels sha | catalogue sha | strict | lenient | n |
|---|---|---|---|---|---|---|---|
| 1 | 2026-09-03 | `ollama/gemma4:31b@v1` | `3d3f32d4130d` | `57ea1c8619f7` | 14/18 (77.8%) | 15/18 (83.3%) | 18 scored, 3 unhandled |

**Run 1 — 2026-09-03.** The first scored run against the held-out stratum, and
the one that set `DefaultScribePreacceptMin`. Decoding was pinned by then
(`temperature: 0`, `seed: 20260831`), so §8's nondeterminism caveat no longer
applies and this run is reproducible rather than a sample.

Every memo was scored from the transcript its label was written against — no
moved pins, and every transcript `whisper.cpp/small.en`:

```
memo      transcript  labelled      proposed      conf  verdict
a7f69c42  c81ead0f    DISCARD       DISCARD       0.95  strict
0c9f3382  b14bdc63    DISCARD       DISCARD       0.95  strict
86dba197  efda5fe7    DISCARD       DISCARD       0.95  strict
2ec3cf5b  40215382    DISCUSSION    DISCUSSION    0.65  strict
cfdae2bb  05e2eabf    TICKET/task   TICKET/task   0.65  strict
9f58d44c  d7a3f1fb    TICKET/task   TICKET/task   0.65  strict
326bfea8  b3b9391b    NOTE          DISCUSSION    0.65  wrong
65b286f3  0eb55a6e    DISCUSSION    DISCUSSION    0.95  strict
1789ae0b  ea010dff    TICKET/task   TICKET/task   0.65  strict
9f94db49  f9aae3eb    TICKET/task   TICKET/task   0.65  strict
399de6bb  ffc32505    NOTE          NOTE          0.65  strict
a4f387e2  d810530b    DISCUSSION    DISCUSSION    0.65  strict
53a793d3  35692109    TICKET/spike  TICKET/spike  0.65  strict
138881e9  fe79e35c    NOTE          TICKET/spike  0.65  lenient
d5cdf80d  b70c1243    TICKET/task   TICKET/spike  0.65  strict
b2709faf  469aae7e    TICKET/task   DISCUSSION    0.65  wrong
ed9b9832  2e1f6038    TICKET/spike  TICKET/epic   0.65  unhandled
0cd7dc67  8a3cf284    TICKET/task   DISCUSSION    0.65  unhandled
6a47664c  c1e32daa    TICKET/spike  TICKET/spike  0.65  strict
dbefd14a  d38e1bbf    TICKET/task   TICKET/epic   0.65  unhandled
c74b8fe3  6b1033ed    TICKET/spike  DISCUSSION    0.65  wrong
```

**All three misses went the same way: TICKET or NOTE answered as DISCUSSION**,
and all three at 0.65. The router's own reasons show why, and they are not
careless — each names a genuinely unsettled sentence at the end of the memo
("I need to figure out just sort of the best way to ensure that"; "I don't know
how we would do around ownership"). The model is reading late hedging as
*unsettled*, where the labeller read the deliverable named earlier as *decided*.
That is one failure mode with one cause, which makes it a CHRN-30 prompt
question rather than three separate errors.

The synthetic stratum was scored in the same session for the pooled calibration
below: **21/23 strict (91.3%), 23/23 lenient**, catalogue `4d870db6d1cd` — a
different hash to the real run's, which is the point of §2's split and not a
discrepancy.

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
| **[rev 3] — R2's four, recorded 2026-08-30** | | | | |
| Testing testing | 3 | `DISCARD` | yes | two words |
| Never mind | 11 | `DISCARD` | yes | *"actually, no, never mind. No, cancel."* — **no Google text**, §6 |
| Home lessons at work | 60 | `DISCUSSION` | yes | *"I want to have like a general discussion"*; explicitly not to build yet |
| How far with Sextant | 93 | `DISCUSSION` | yes | opens *"a good thing to discuss"*, ends on an open question; personal, R3 |

**[rev 3] 12 TICKET, 3 NOTE, 3 DISCUSSION, 3 DISCARD — twenty-one**, with five
where a second labeller could reasonably disagree. (Seventeen at first
acceptance: 12/3/1/1, counted rather than "roughly" because the first draft's
eleven did not sum.) **R2 is discharged**: DISCUSSION and DISCARD are n=3
apiece, so neither is graded on a single example any more, and the destination
that cannot be undone is no longer the thinnest.

### What is missing, and why it is worth four more recordings

`DISCUSSION` and `DISCARD` are **n=1 each**. At that count a correct answer is
indistinguishable from a lucky one, and DISCARD is the destination CHRN-32
excluded from pre-acceptance precisely because it cannot be undone. Grading the
one irreversible accept entirely on synthetic data is the weakest part of this
set.

**[rev 3] Done — recorded 2026-08-30 and in the table above.** Both DISCUSSION
memos state their tension and reach no decision; both DISCARD memos are genuine
rather than staged. One more clear `NOTE` would still help but remains the least
urgent — there are three, and none of them is the irreversible destination.

This is not padding. TICKET is the path vox-dictate already proved end to end;
what E4 adds is that TICKET is one destination among several, and a corpus that
is two-thirds TICKET grades mostly the half that already worked.

## 4 · A label carries its own ambiguity

```yaml
- hash: 2ec3cf5b…          # tier2.memos.content_hash — the identity
  stratum: real
  labelled_against: whisper.cpp/small.en   # [rev] the transcript the labeller read — §1
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

**[rev] Calibration pools both strata to reach its n, and that is said here
because §2 forbids exactly this for accuracy.** What is pooled is the
confidence–correctness relationship, not a score — no blended accuracy falls out
of it — and each bucket reports its per-stratum n beside the total, so the
pooling stays inspectable rather than becoming §2's averaging through a side
door.

A single summary number is deliberately not specified. At this n it would invite
comparisons it cannot support.

### The result, 2026-09-03 — and the threshold it sets

**Pooled calibration, both strata, n=41.** Correctness is lenient, as §5
specifies, and each band carries its per-stratum n so the pooling stays
inspectable:

| confidence | n | correct | accuracy | by stratum |
|---|---|---|---|---|
| `< 0.50` | 0 | 0 | — | — |
| `0.50 – 0.80` | 23 | 20 | 87.0% | real 14, synthetic 9 |
| `>= 0.80` | 18 | 18 | **100.0%** | real 4, synthetic 14 |

Monotonic and rising, and **zero confident-and-wrong items in either stratum**.
So R4 does not fire: the evidence supports a threshold, and
`DefaultScribePreacceptMin` moves from 1.01 to **0.80**.

**Why 0.80 and not some other number.** The router emits three confidence values
and no others — 0.65, 0.85, 0.95 — so every candidate in (0.65, 0.85] partitions
the corpus identically. 0.80 is this section's own band boundary and it lands in
the empty gap between the two clusters, which is what keeps it from being fitted
to a data point. The precision is spurious and saying so is part of the claim.

**What it buys, stated as the sweep measured it rather than as a rate:**

| stratum | pre-accepted at 0.80 | of those wrong | held for a tap |
|---|---|---|---|
| synthetic | 9 of 23 | 0 | 14 |
| real | **1 of 18** | 0 | 17 |

**On real speech it is very nearly inert, and that is the finding, not the
threshold.** Seventeen of twenty-one real memos come back at 0.65. Of the four
that clear 0.80, three are DISCARD, which §4 excludes from pre-acceptance at any
confidence — so ACCEPT ALL selects exactly one card in a twenty-one-memo batch.
The honest summary of this run is *the confidence field carries real signal and
almost never uses it.*

**Widening it is the wrong repair, and the sweep says so numerically.** At 0.65
the real stratum pre-accepts thirteen and **three of them are wrong** — a 23%
error rate on precisely the items ACCEPT ALL would ship unread. The cliff
between one-and-zero-wrong and thirteen-and-three-wrong is the whole argument
for the band boundary. Making ACCEPT ALL worth having means a prompt whose
confidence spreads on real speech, which is CHRN-30's problem and not a
threshold's.

**Three caveats that the numbers above do not carry on their own:**

- **100% on n=18 is not "never wrong".** Zero errors in eighteen is consistent
  with a true error rate as high as ~15% at 95% confidence. The top band is
  clean; it is not proven safe.
- **The labeller probe fails.** Median confidence is 0.65 where the labeller was
  unsure against 0.85 where they were sure, which looks right — but one
  labeller-unsure synthetic item (`talked-myself-out-of-it`) sits in the top
  band, and on the real stratum alone **both medians are 0.65**, so there the
  router does not distinguish hard from easy at all. Confidence tracks
  correctness without tracking difficulty, and those are not the same property.
- **The pooled table above was computed by hand from two reports, because the
  harness can no longer produce it.** CHRN-31's follow-up made `--stratum`
  mandatory for a scored run — one router holds one catalogue — so each run's
  `Calibration` block pools over a single stratum and reaches its own verdict on
  R4. The two runs disagreed for exactly that reason: synthetic alone printed
  **"No threshold ships (R4)"** (its lenient accuracy is 100% in every band, so
  `Rising` is false), real alone printed **"accuracy rises"**. Neither is §5's
  number. The arithmetic is small and fully derivable from the two committed
  reports, but a decision this size should not rest on a reader redoing it —
  filed as **CHRN-86**.

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

### [rev] Where Google's text lives — which the first draft got wrong

Not `tier2.transcripts`: the durable floor rejects an unmeasured runner,
correctly. And not the repo, which is what the first draft's "beside the eval
set" meant — §1's own test forbids it. Google's transcript renders exactly the
same speech as whisper's, and *did a person say this into a recorder* does not
care which ASR listened. Committing it would carry the full text of every real
memo into git through a side door, the two R3 memos included, and §1's argument
would be over.

So it lives with the other irreplaceable things: a flat directory inside
Chronicle's data volume — `eval/reference/<content_hash>.google-recorder.txt` —
referenced from the label file by hash, inside the boundary **CHRN-68**'s drill
covers. The drill must name it, or a restore quietly drops the control. (That is
a requirement CHRN-68 inherits if this section is accepted, and is recorded
there at that point, not before.)

### [rev 3] The control covers twenty of twenty-one, and the gap is itself a finding

*Never mind* has **no Google transcript at all** — the service returned nothing
for it — while whisper produced a clean one: *"Oh, actually, no, never mind. No,
cancel. I don't want this. Just go away."* So the paired comparison runs on 20
of 21.

The miss is worth more than the pair would have been. This section was framed
around *wording* differences between two ASRs; the sharper failure is one ASR
returning **nothing** where the other returns usable text. Had Chronicle been
built on Google's output, that memo would have entered the corpus silently
empty — no transcript, no error, and CHRN-22's durable gate would have held its
audio forever without anyone knowing why.

And the miss is not random. It landed on an **11-second, low-content memo** —
which is exactly the `DISCARD` class. If short throwaway recordings are
systematically what a second ASR drops, then the control has a blind spot
precisely where the eval set is thinnest and where the one irreversible accept
lives. Recorded so that a future comparison reports coverage per destination
rather than a single 20/21.

Capturing that text out of Google Recorder — an external service that owes it
nothing — was this section's time-sensitive act, and **it is done**: all
seventeen were exported with the audio on 2026-08-30 and placed in the home
above, keyed by `content_hash`, 80 KB in total.

It outranked the prune deadline and no longer competes with it: the control
needs the two *texts*, not the audio, and whisper's half is tier 2 and
permanent. So **the control now survives 2026-09-29 outright.** What does not
survive it is re-transcription — see *What this does not decide*.

## 7 · Two cases the contract does not handle, labelled as such

Found by reading the corpus, and both would otherwise be scored as router
failures for doing the only thing they can.

**An idea arriving in more than one pass.** Two memos refer back to earlier
thinking: *At a Glance* — *"this is probably an add-on to the smart calendar"*
and *"I referenced that in that calendar note too"*, two references in one memo,
both to the Smart Calendar note — and *Cafe Passport*, *"building off the city
Maps thing"*. **[rev] The first draft said "at least four", and the corpus says
otherwise:** a phrase sweep over the whisper transcripts finds refer-back
language in exactly these two, which is what §3's table marked all along; the
four came from reading one memo's references as separate memos. With *Home SRE
Agent* below, the unhandled count is **three of seventeen**.

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
appends to authored text unattended — not from the batch API, and not from an
MCP write tool.** [rev] The two exclusions are load-bearing rather than
examples, and the batch one is stated as policy now: an `append` or `supersede`
is never in ACCEPT ALL, exactly as CHRN-32 §4 already holds for DISCARD.
CHRN-39's revision log makes an errant append recoverable where a DISCARD is
not — but recoverable-by-forensics is not a bar ACCEPT ALL gets to clear. The
CHRN-39 comment of 2026-08-30 carries this same rule with the same exclusions;
they are one rule, not two drifting copies.

**One consequence for §3's labels.** The proposed destinations for these three
are provisional in a deeper way than the others. *At a Glance* is labelled
`TICKET`, but if the corpus can express "append to the Smart Calendar page" it
may well be a `NOTE` with a verb — and the honest answer is that its correct
label depends on a capability that does not exist. That is what `unhandled:`
records, and it is why these three are scored apart rather than counted wrong.

**Dedup against existing work.** *Home SRE Agent* opens *"I think I've talked
about it, but I'm pretty sure I don't have a ticket for it."* That is a memo
asking the router to check Switchyard for a duplicate before creating one.
Nothing in the contract does that.

Both are recorded in `unhandled:` on the label, **excluded from the headline
accuracy, and reported as their own line**. They are not defects in CHRN-30 and
tuning a prompt against them would be tuning against a missing feature. The
count is the useful output: *three of seventeen real memos want a capability the
contract does not have* is a finding for E4 and E5, not a score. [rev] A sixth
of the corpus rather than the first draft's quarter — smaller, and the finding
stands.

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
  re-ingestion. [rev] The first draft said this kills §6's control too, and it
  does not: the control needs only the two texts, and survives the prune once
  Google's is captured to the home §6 names. What dies with the audio is
  re-transcription — a `large-v3` pass over the corpus, or a third transcript
  for §6's comparison. There is no supported way to pin a memo after capture —
  the per-memo permanent pin IDEA-21 locked in has no implementation. Flagged
  here because this ticket is the thing that would lose by it.

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
They stay memos under either ruling — nothing here touches the corpus.

[rev] The first draft recommended excluding their labels from git while naming
both memos and describing their content in its own first sentence — a
half-measure that discloses more than the label lines it withholds. The real
question is whether personal *topics* may appear in this repo at all, and either
answer is coherent where the middle is not:

- **Yes** — commit their labels like the other fifteen, with reasons as terse as
  the table's. The set stays at seventeen, and nothing lands in git that this
  document has not already said.
- **No** — mark them `stratum: real, committed: false`, and scrub this document
  to match: §3's two rows and this ruling's own parentheticals reduce to their
  hashes.

Recommendation: **the first — commit the labels.**

**[rev 2] The first version of this recommendation opened "The repo is private."
It is not.** `Einlanzerous/chronicle` is public, so the descriptive sentences
were already world-readable in this PR's diff before the ruling was settled, and
the branch's history keeps them reachable by SHA whatever is edited later — a
scrub decides future exposure, not past.

**The conclusion survives the correction and is magos's, taken 2026-08-30 with
the true premise in front of him:** the exposure is topic-level — no health
detail, no dates, no identifiers — and not worth the asymmetry the alternative
buys. Recorded this way round because a decision document is read for its
reasons, and the next reader must not inherit "the repo is private" as an
estate fact.

The rest of the original reasoning is unaffected and still holds: the
descriptive sentence already exists in this document either way, §6's correction
keeps the actual transcript text out of git under both rulings, and the scrub is
two rows and two parentheticals in both worlds.

**One thing it does change, and in this document's favour.** §1 argued the
transcripts stay out of git from the tier doctrine alone. A world-readable repo
is a second, independent argument for the same line — and it makes §6's
correction load-bearing rather than tidy, since "beside the eval set" would have
carried the full text of every real memo into a public repository.

**R4 · Does the threshold ship at all if calibration is flat?** Recommendation:
**no — leave `CHRONICLE_SCRIBE_PREACCEPT_MIN` at its 1.01 default, which admits
nothing.** If accuracy does not rise with confidence, there is no value at which
pre-accepting is better than not, and shipping a number anyway would spend trust
the router has not earned — the precise failure CHRN-4 opens by naming. ACCEPT
ALL then waits for a prompt that calibrates, which is a CHRN-30 problem and not
a threshold problem.

**R4 did not fire on the first scored run (2026-09-03).** Pooled calibration
rose — 87.0% below 0.80 against 100% at or above it, n=41 — so the threshold
shipped at 0.80. Recorded here because the ruling reads as a prediction
otherwise, and the interesting half is that it came within one stratum of
firing: scored alone, `synthetic` reports flat and refuses a threshold. See
§5's result.

## Done when

- *the set exists in the repo* — §1: the labels, reasons, ambiguity, hashes and
  harness, with the transcripts left where they belong.
- *a scored run is reproducible* — §1's `content_hash` identity plus §8's
  harness; and §2's run log is what keeps the second run comparable to the first.
- *a stated threshold below which proposals are shown but never pre-accepted* —
  §5 produces it, §4's ambiguity tax bounds what it can claim, and R4 settles
  what happens when the evidence does not support one.
