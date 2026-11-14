# CLAUDE.md — Chronicle

Voice-note ingestion into a notes and discussion wiki, layered over the estate.
A memo is captured, transcribed on the shared ASR service, routed by the Scribe
into a **note**, a **ticket** or a **discussion**, and lands in a corpus that
links out to Switchyard and Amber rather than absorbing them. Single static Go
binary, sibling to the other construct-server Go services.

Tracked in Switchyard under the **CHRN** project.

## Layout

The tree is derivable; these are the parts it does not tell you.

- `asr/` is the shared estate ASR service: a **sealed subtree** with its own
  database, role, migrator and release (`asr-v*` tags). Nothing under it imports
  anything outside it; `asr/asrtest` is the one exported doorway, for
  Chronicle's integration test (`docs/decisions/chrn-82-asr-subtree-and-publish.md`).
- `internal/mcp/` is a **client of the HTTP API**: it reaches Chronicle only
  through `internal/apiclient/` (generated from `openapi.yaml` by
  `scripts/gen-apiclient.sh`) and may import neither `internal/api` nor
  `internal/store`. `verify.sh`'s `mcp boundary` step and its `ci.yml` mirror
  enforce it.
- `web/` and `mobile/chronicle/` generate their API types from `openapi.yaml`
  (`scripts/gen-webapi.sh`, `scripts/gen-dartapi.sh`). Mobile checks run in
  `.github/workflows/mobile.yml`, not `verify.sh`, which must not need a JDK.
- `deploy/` holds the Dockerfile, database provisioning and deploy decisions.
  The service block and routers live in `construct-server`, not here.
- `docs/salvage/` and `docs/benchmarks/` are recovered and measured inputs the
  plan rests on; read them before re-deriving anything.

## Conventions

- Go, `pgx/v5`, no ORM, and an in-process migrator over embedded SQL.
- Config is env-only, `CHRONICLE_`-prefixed. No config files.
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
Chronicle's tables. Copy either one here and there is a third source of truth that goes stale
silently.

A reference renders as a live card carrying **upstream** state under the label
`LINKED · NOT COPIED`. Colour is estate-wide: **coral is Switchyard, gold is
Amber**, anywhere either resolves.

A cache is not a copy — but **a cache with no visible staleness is a copy that
lies.** Cached state carries its age, and when the upstream is unreachable the
card says so rather than showing a confident stale value.

## Working agreement

Review mode is chosen per ticket from `metadata.tier`.

| mode | what the reviewer sees | tickets |
|---|---|---|
| **A · evidence** | the `Done when` claim and green CI — not the diff | tier `sonnet` / `haiku` |
| **B · decision first** | a written decision *before* any code; the PR is then mechanical | tier `opus` |
| **C · full diff** | every line | the six below |

**Mode C is exactly six tickets, and the list does not grow by habit:**
CHRN-22 (retention pruner), CHRN-39 (revisions), CHRN-52 (tier isolation),
CHRN-65 (MCP transport), CHRN-67 (MCP write scopes), CHRN-120 (the phone's own
prune of its local audio). The rule that generates that
list: *anything that can destroy authored data, or hand an agent write access to
it.* CHRN-68's restore drill is reviewed as a result rather than as a diff.

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
