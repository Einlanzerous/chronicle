# CLAUDE.md — Chronicle

Voice-note ingestion into a notes and discussion wiki, layered over the estate.
A memo is captured, transcribed on the shared ASR service, routed by the Scribe
into a **note**, a **ticket** or a **discussion**, and lands in a corpus that
links out to Switchyard and Amber rather than absorbing them. Single static Go
binary, sibling to the other construct-server Go services.

Tracked in Switchyard under the **CHRN** project — 10 epics (`CHRN-1`…`CHRN-10`),
60 tickets (`CHRN-11`…`CHRN-70`). It graduated there from `IDEA-21`. The key is
`CHRN` and not `CHR` because the estate wiki's own note IDs are `CHR-####` and
the two namespaces would collide.

## Layout

- `cmd/chronicle/` — entrypoint + subcommands (`serve`, `migrate`, `version`).
  Composition root: `setup()` wires store + services + router.
- `internal/config/` — env-only config, `CHRONICLE_`-prefixed.
- `internal/store/` — pgx pool, embedded migrator, repo queries, and the
  domain types themselves. Types sit beside the queries that return them
  rather than in a separate `internal/model/`: CHRN-71 put `User` in
  `user.go` and CHRN-18 put `Memo` in `memo.go`, and a types-only package
  with two files in it earns nothing.
- `internal/api/` — HTTP surface.
- `migrations/` — `NNNN_name.up.sql` / `.down.sql`, embedded, auto-applied on boot.
- `deploy/` — compose fragment and Traefik labels.
- `docs/` — architecture, plus `docs/salvage/` and `docs/benchmarks/`, which are
  the recovered and measured inputs the plan rests on. Read them before
  re-deriving anything they already answer.

## Conventions (match the construct-server house style)

- Go 1.26, `pgx/v5`, `google/uuid`. No ORM. No external migration tool — an
  in-process migrator applies embedded SQL.
- Config is env-only, `CHRONICLE_`-prefixed, with a `DATABASE_URL` fallback. No
  config files.
- Logs: structured, to stdout, in a shape Dozzle and Datadog can read.
  Health: `GET /healthz` and `GET /readyz`.
- Release-please + GHCR image `ghcr.io/einlanzerous/chronicle`. Conventional
  commits.
- Its own database and its own role on the shared Postgres 16. Credentials live
  in Signet, never in a compose file.

## Invariants — don't break these

### 1 · Tier 1 is derived and disposable. Tier 2 is authored and irreplaceable. They do not share a store.

**Tier 1 is the estate's account of what exists** — the generated wiki, the
code, what is actually deployed — plus whatever Chronicle derives from its own
corpus: Scribe proposals, extracted entities, search indexes. All of it is
regenerable from a source of truth that lives outside Chronicle. None of it is
ever hand-edited. The tier-1 pane is stamped `READ ONLY` because editing it
would fabricate existence.

**Tier 2 is what a person said or wrote** — memos, transcripts, notes,
discussions, plans. None of it is derivable from anything and none of it can be
rebuilt.

The test: *talk about five things and build two.* The two that got built are
real, and tier 1 documents them because they exist. The other three are ideas,
discussions and plans — tier 2, with or without an IDEA ticket.

They live in different tables reached by different code paths. A separate
database and role is the enforcement mechanism; a test proving no tier-1 write
path can reach a tier-2 table is the proof.

**"Disposable" means two different things — do not blur them.** Tier 1 is
disposable *because it is regenerable*: delete it and it rebuilds. Audio is
pruned at 30 days *by policy despite not being regenerable*. That is why
deletion is gated on a durable transcript rather than on the calendar. Pruning
audio for a memo whose transcription never succeeded is unrecoverable loss with
no user-visible warning, and it is the single worst thing this system can do.

### 2 · Switchyard and Amber are linked, never copied.

A ticket or an Amber item **resolves at render time** and is never written into
Chronicle's tables. Amber holds the durable archive as its source of truth;
Switchyard owns the work ledger. Copy either one here and there is a third
source of truth that goes stale silently — the exact failure the tier split
exists to prevent, reintroduced one convenience at a time.

A reference renders as a live card carrying **upstream** state — `SY-412 · IN
PROGRESS`, `AMB-2291 · SEALED` — under the label `LINKED · NOT COPIED`, with an
outbound arrow. Colour is an estate-wide rule, not a Chronicle choice: **coral
is Switchyard, gold is Amber**, anywhere either resolves.

A cache is not a copy — but **a cache with no visible staleness is a copy that
lies.** Cached state carries its age, and when the upstream is unreachable the
card says so rather than showing a confident stale value.

## Working agreement

Reviewing sixty diffs does not scale, and long autonomous runs accumulate
unreviewed *decisions* rather than unreviewed lines — by the time a PR appears,
three of them are load-bearing. So review mode is chosen per ticket from
`metadata.tier`, and decisions are written down at ticket boundaries as they
happen, never batched to the end of an epic.

| mode | what the reviewer sees | tickets |
|---|---|---|
| **A · evidence** | the `Done when` claim and green CI — not the diff | tier `sonnet` / `haiku` |
| **B · decision first** | a written decision *before* any code; the PR is then mechanical | tier `opus` |
| **C · full diff** | every line | the five below |

**Mode C is exactly five tickets, and the list does not grow by habit:**
CHRN-22 (retention pruner), CHRN-39 (revisions), CHRN-52 (tier isolation),
CHRN-65 (auth surface), CHRN-67 (MCP write scopes). The rule that generates that
list: *anything that can destroy authored data, or hand an agent write access to
it.* CHRN-68's restore drill is reviewed as a result rather than as a diff.

> CHRN-16 was Mode C in the original six and was moved to Mode A by decision on
> 2026-08-23. Recorded here so the change reads as a decision rather than drift.

Mode B is where the leverage is. Discovering in a 900-line diff that the wrong
idempotency key was chosen is the most expensive possible moment to find out;
the decision costs one message to review before the code exists.

### Mechanics

- One worktree per epic. Branch `chrn-NN-description` per ticket, so per-ticket
  spend attributes correctly. Release-please reads the commit message, so the
  `feat:` / `fix:` prefix belongs there and not in the branch name.
- `./verify.sh` green before anything is handed over: every check that does not
  need hardware, in one command.
- Every ticket closes with a Switchyard transition **and** a comment carrying its
  evidence. **The board is the status surface** — nobody should have to scroll a
  session transcript to learn where things stand. That is also why this file has
  no status section: it is the shared prefix every agent inherits, and it stays
  byte-identical so caching hits.

### Stop and ask when

- a `Done when` cannot be met as written,
- a decision surfaces that the ticket does not settle,
- or the change wants to touch something outside its own epic.

Everything else runs to completion and is reviewed afterwards.

## Testing

`go test ./...`, plus `./verify.sh` for the full non-hardware suite. CI builds
the binary, runs tests and lint, and fails when the schema and the migrations
disagree — a generated artefact with no guard is a generated artefact someone
hand-edits.
