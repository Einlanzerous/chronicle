# Chronicle

Voice-note ingestion into a notes and discussion wiki, layered over the estate.

Talk into a phone. The memo is captured, transcribed locally on the shared ASR
service, and the Scribe proposes where it belongs — a **note**, a **ticket**, or
a **discussion** — with a confidence and a reason you can reject at a glance.
Nothing is filed without a tap.

---

## Two rules this project does not bend

Everything else here is an implementation detail. These two are not.

### 1 · Tier 1 is derived and disposable. Tier 2 is authored and irreplaceable. They do not share a store.

|  | **Tier 1** | **Tier 2** |
|---|---|---|
| what it is | the estate's account of **what exists** | **what a person said** about it |
| examples | the generated wiki, the code, what is deployed, Scribe proposals, entities, search indexes | memos, transcripts, notes, discussions, plans |
| where truth lives | outside Chronicle | here, and nowhere else |
| lose it and | it rebuilds | it is gone |
| editable | **never** — the pane is stamped `READ ONLY` | yes, that is the point |

The test: **talk about five things and build two.** The two that got built are
real — they exist, and tier 1 documents them *because* they exist. The other
three are ideas, discussions and plans. They stay tier 2.

This is why hand-editing tier 1 is the cardinal sin rather than a style
violation: **editing tier 1 fabricates existence.** It is also why the two tiers
do not share a store. Tier 1 regenerates itself; if a regeneration could reach
tier-2 tables, one bad run eats the authored corpus. A separate database and
role enforces it, and a test proves it.

> **Careful with the word "disposable".** Tier 1 is disposable *because it is
> regenerable*. Audio is pruned at 30 days *by policy despite not being
> regenerable* — which is why deletion is gated on a durable transcript rather
> than on the calendar. Same word, opposite reasoning.

### 2 · Switchyard and Amber are linked, never copied.

A Switchyard ticket or an Amber item **resolves at render time.** It is never
written into Chronicle's tables.

```
┌─────────────────────────────┐
│ SY-412 · IN PROGRESS      ↗ │   coral = Switchyard
│ LINKED · NOT COPIED         │   gold  = Amber
└─────────────────────────────┘   (estate-wide, not a Chronicle choice)
```

Amber holds the durable archive as its source of truth; Switchyard owns the work
ledger. Copy either one here and there is a third source of truth that goes
stale silently — the exact failure the tier split exists to prevent,
reintroduced one convenience at a time.

**A cache is not a copy — but a cache with no visible staleness is a copy that
lies.** Cached state carries its age, and when the upstream is unreachable the
card says so instead of showing a confident stale value.

---

## How a memo becomes something

```
capture ──▶ transcribe ──▶ Scribe proposes ──▶ you tap ──▶ note
Copyparty     whisper.cpp    NOTE · TICKET                  ticket
or the app    small.en       DISCUSSION                     discussion
              on the R9700   + HOLD / DISCARD
```

- **Capture** — a watched Copyparty folder, or a direct upload from the
  Chronicle Android app. Both converge on one memo row with the same idempotency
  rules, so a memo delivered twice by two routes is one memo.
- **Transcribe** — the shared estate ASR service, `whisper.cpp` on Vulkan on the
  R9700. A 60-second note takes about a second. Audio prunes at 30 days unless
  pinned; the transcript is permanent.
- **Route** — the Scribe, on a local model, descended from the one salvaged out
  of `vox-dictate`. It *proposes*; a tap confirms. Triage is batch-first,
  because the real pattern is an evening pass over a day's memos.

## Layout

| path | |
|---|---|
| `cmd/chronicle/` | entrypoint and subcommands |
| `internal/` | config, model, store, api |
| `migrations/` | embedded SQL, applied on boot |
| `deploy/` | compose fragment and Traefik labels |
| `docs/decisions/` | written decisions for the tickets that get one before code |
| `docs/salvage/` | the Scribe, recovered from `imperium-loop` before it was decommissioned |
| `docs/benchmarks/` | measured ASR model choice — read this before picking a model |
| `CLAUDE.md` | conventions, invariants, and the working agreement |

## Getting in

No passwords. A one-time invite redeems into a durable per-device session, and
the same account is reachable two ways: through Cloudflare Access in a browser,
and with a redeemed invite on the direct host that the app and MCP use. The two
are complementary — Access decides whether a request is served at all, Chronicle
decides who it is served as.

There is no mode that serves an unauthenticated caller anything but a health
probe. The first boot logs an invite; `chronicle mint-invite` issues another.
See `docs/decisions/chrn-71-accounts-and-sessions.md`.

## Status

Tracked in Switchyard under **CHRN** — 10 epics, 60 tickets, graduated from
`IDEA-21`. The board is the status surface; this file does not duplicate it.
