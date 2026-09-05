# Review instructions

What a review of *this* repo is for (CHRN-72). The shared reviewer
(construct-server `docs/pr-reviewer.md`) supplies the procedure; this file
supplies the judgement.

`CLAUDE.md` describes how this repo works and states the invariants. Read it —
it is short, and the two invariants in it are what most of this file is about.
This file is higher priority than `CLAUDE.md` on anything to do with reviewing.

## Why this review carries more weight here than in a repo with human diff review

Chronicle's working agreement puts most tickets in **Mode A**: the human
reviewer reads the `Done when` claim and green CI, *not the diff*. That is a
sound trade only because something else read the diff, and that something is
you. On a Mode A ticket you are not a second opinion — you are the only read.

Three modes exist and the PR should tell you which one applies. The ticket
now **states** it in `review_mode`; `metadata.tier` is what that was derived
from, and is the fallback when `review_mode` is null:

| mode | tier | what it means for you |
|---|---|---|
| **A · evidence** | `sonnet` / `haiku` | nobody else reads this diff |
| **B · decision first** | `opus` | a written decision was approved before the code; check the code matches it |
| **C · full diff** | the five below | a human reads every line too |

Mode C is exactly **CHRN-22** (retention pruner), **CHRN-39** (revisions),
**CHRN-52** (tier isolation), **CHRN-65** (auth surface), **CHRN-67** (MCP write
scopes) — the tickets that can destroy authored data or hand an agent write
access to it.

## What CI already proves — and where it doesn't

Do not spend the review re-proving these. Do not assume more than they say.

| job | proves | reaches |
|---|---|---|
| `verify` / *build, vet, test* | `gofmt -l`, `go build`, `go vet`, `go test -race ./... -count=1` against a **real Postgres 16**, with **both** the `chronicle` and `chronicle_tier1` roles provisioned | every PR — `ci.yml` has no path filter, so unlike some estate repos there is no short-circuit-to-green case here |
| `lint` | golangci-lint v2, `standard` set (errcheck, govet, ineffassign, staticcheck, unused) | every PR |
| `schema` / *migration staleness guard* | `schema.sql` byte-matches what the migrations actually produce | every PR |
| `branch-name` | nothing. It **emits a `::warning::` and always exits 0** | every PR |

Two gaps worth holding on to:

- **`branch-name` is advisory.** A branch that does not match `chrn-NN-description`
  produces a warning in a green job. If per-ticket attribution matters to the PR,
  the check will not have enforced it.
- **`./verify.sh` is weaker than CI, and the working agreement leans on
  `verify.sh`.** It runs `go test ./...` **without `-race`**, and both
  database-backed suites — including the tier-isolation test — **skip silently**
  when their DSNs are unset, printing a NOTE rather than failing. So "verify.sh
  green" from a session is not the same claim as a green `verify` job. If a PR
  body rests an assertion on a local verify run, say which of the two it was.

What none of them prove: that a test asserts the thing the ticket asked for, or
any of the properties below. No Go test observes them.

## Ticket fidelity — check this first

When a Switchyard ticket is linked, read its description and `Done when` before
the diff, and answer explicitly in the summary:

- Does the implementation satisfy the stated `Done when`, or only the easy
  subset?
- Was a requirement silently dropped, narrowed, or deferred? Deliberate
  departures are fine **when stated** — this repo's PRs are expected to name them
  and say why. An unstated one is a finding.
- Does the PR claim something is verified that the diff does not demonstrate?

**Specific to this repo: a Mode B (`tier: opus`) ticket owes a written decision
before the code.** A PR implementing an opus-tier ticket with **no approved
decision at all**, or whose code contradicts the decision it does have, is a
**🔴 Important** finding — the working agreement's entire argument is that
discovering a wrong choice inside a 900-line diff is the most expensive possible
moment to find out.

**It may live in either of two places, and you have to look in both.**

- **The ticket's Switchyard PLAN**, approved before the work started.

  **Fetch it at `GET /v1/tickets/<key>/plan`. THE TICKET RESOURCE DOES NOT
  INCLUDE IT** — `GET /v1/tickets/<key>` carries `description`, `comments`,
  `metadata`, `review_mode` and `links`, and nothing plan-shaped, so a reviewer
  following the standard procedure and looking no further will conclude there is
  no decision and fire the false 🔴 this section exists to prevent. Check
  `status` on what comes back: a plan can be `draft` or in review, and only an
  **approved** one was settled before the code.

  This is the home going forward, for a reason that is not tidiness: the tracker
  can *enforce* it. A ticket whose `review_mode` is `decision` cannot enter
  `in_progress` until its plan is `approved` — a 422 with `guard:
  plan_required`. A file in a repo is checked by a reviewer after the fact; a
  plan is checked before the code exists, which is what the whole mode is trying
  to buy. **That guard is armed for CHRN as of 2026-09-03.** Every open ticket
  carrying a `metadata.tier` now has a stated `review_mode` — `evidence` for
  tier `sonnet`/`haiku`,
  `decision` for tier `opus`, `full` for the five Mode C tickets — so a
  `decision` ticket is now *blocked* from entering `in_progress` until its plan
  is approved, rather than merely expected to have one. Two gaps in that
  arming, both deliberate:

  - **A ticket whose decision is a `docs/decisions/` file was left unarmed.**
    `plan_required` only understands plans, so setting `decision` on one of the
    twelve would demand a plan for a decision that was already taken and settled
    in the repo. CHRN-36 is the live example: tier `opus`, decision on disk,
    `review_mode: null` on purpose.
  - **The seven epics, CHRN-74 and CHRN-85 stay `review_mode: null`** because
    they carry no `metadata.tier` to derive a mode from. Null reads as *not
    classified — ask*, and never as permission.

  You still check the approval yourself: the guard gates the *transition*, not
  the PR, and a ticket moved before the arming carries no trace of it. CHRN-33
  is the pattern (revision 4, 27 criteria).
- **`docs/decisions/<ticket>.md`**, one file per ticket
  (`docs/decisions/chrn-71-accounts-and-sessions.md` is the pattern). Twelve of
  these exist and they remain the decision for their tickets. Plans arrived
  after most of them, so their absence from Switchyard is history rather than a
  gap, and **they are not to be backfilled**.

So: an **approved** plan and no file is complete. A file and no plan is
complete. Neither is the 🔴. Check the code against whichever one exists, and if
both do, say so rather than picking.

A change that is clean code and wrong scope is **🔴 Important**. Quote the unmet
criterion.

When no ticket is linked, say so in one line and review the diff on its own
terms. Do not invent intent from the branch name.

## Severity

- **🔴 Important** — crosses the tier boundary, copies Switchyard or Amber state
  into a Chronicle table, destroys or risks authored data, leaks or logs
  credential material, widens who can reach an endpoint, stops the server
  booting, or does not do what the ticket asked.
- **🟡 Nit** — conventions, clarity, a comment that will mislead. Never blocking.
- **🟣 Pre-existing** — real, not introduced here. At most two per review.

Cap nits at five; beyond that say "plus N similar" in the summary. A review that
buries one Important finding under twelve nits has failed at its job.

## Always check

### 1. Which tier the change lands in

This is the repo's first invariant and the one most easily broken by a change
that looks correct. **Tier 1 is derived and disposable** — the generated estate
wiki, code, what is deployed, plus what Chronicle derives from its own corpus:
Scribe proposals, extracted entities, search indexes. All regenerable, never
hand-edited. **Tier 2 is authored and irreplaceable** — memos, transcripts,
notes, discussions, plans. Nothing rebuilds it.

The test CLAUDE.md gives: *talk about five things and build two.* The two that
got built are tier 1. The other three are tier 2.

For any new table, query, or handler, say which side it is on. Concretely:

- **Every table reference is schema-qualified.** `tier1.x` or `tier2.x`, never a
  bare name — `search_path` is never set in this codebase and 0001 creates both
  schemas empty precisely so there is no default to fall into. An unqualified
  table name in new SQL is a finding on its own, before you even work out which
  side it meant.
- **A new tier-2 table needs an explicit `REVOKE ALL … FROM chronicle_tier1`.**
  It is *redundant*, and stated anyway as **documentation of intent, at the
  point where the tier boundary is defined**. A new tier-2 table without one is
  a 🟡. One that *grants* anything to `chronicle_tier1` is 🔴.
  `0011_notes.up.sql` is the wording to match.

  **What makes it redundant is table privileges, not a schema wall — and you
  have to reason from the right one to reach the right verdict on that 🔴.**
  `0007:52` grants `USAGE ON SCHEMA tier2 TO chronicle_tier1`; the schema has
  not been a wall since E4, whatever older comments say. What holds instead:
  `0007` deliberately added **no `ALTER DEFAULT PRIVILEGES` on schema `tier2`**,
  so a tier-2 table created today is unreachable on table privileges alone
  until some migration grants it **by name**. Two have been:
  `GRANT SELECT ON tier2.memos, tier2.transcripts` at `0007:53`, so Scribe can
  read its input. Those two are the deliberate exceptions and the list is
  meant to stay that short.

  This is why the stale version of the argument is worth correcting rather
  than tolerating: reasoning from a schema wall, a table-level
  `GRANT … TO chronicle_tier1` looks harmless — the role supposedly cannot
  reach the schema anyway — and you would clear the exact thing the 🔴 exists
  to catch. It reaches. Judge the `GRANT` on its own terms.

  **Do not expect the `REVOKE` to be what makes a loosening visible.** This
  section claimed that until CHRN-79 and it was false: `pg_dump` emits only
  non-default ACLs, so revoking a privilege the role never held leaves nothing
  to emit — which is why `schema.sql` carries no `REVOKE` for any tier-2 table.
  What makes a loosening observable is the loosened **`GRANT`** itself, which
  *is* a non-default ACL — and the observing is split between you and CI, so do
  not go looking in the wrong half. **Yours is the migration**: `migrations/`
  is in `sensitive_paths`, so a `GRANT … TO chronicle_tier1` in a new `.up.sql`
  is in the diff you were given. **CI's is `schema.sql`, which you were not**
  (§6, `.github/review-ignore`) — the regenerated file gains an ACL stanza and
  the `schema` job (*migration staleness guard*) fails if the committed file
  disagrees. Measured on CHRN-79 rather than reasoned: a probe `GRANT SELECT ON
  tier2.memos TO chronicle_tier1` adds exactly one stanza, while all five
  tier-2 `REVOKE`s the migrations state — two on schema `tier2` (`0001:25`,
  `0001:52`), three covering its four tables (`0002:83-84`, `0003:201`) —
  render nothing at all. That visibility comes from `gen-schema.sh` rendering
  privileges instead of passing `--no-acl`, which is why the script is in
  `sensitive_paths` and why removing the ACLs there would break no test. **The
  requirement above is unchanged** — knowing the true reason is what keeps it
  from being argued away by the next person who checks the old one.
- **No tier-1 write path may reach a tier-2 table.** The enforcement is the
  Postgres grant; the proof is `TestTier1RoleCannotReachCredentials` in
  `internal/store/user_test.go`. That test **skips** without
  `CHRONICLE_TEST_TIER1_DATABASE_URL`, which CI sets — if a PR touches the CI
  env or the test's skip condition, check it still runs.

Note the two meanings of "disposable" and do not let a diff blur them. Tier 1 is
disposable *because it is regenerable*. Audio is pruned *by policy despite not
being regenerable*. See §3.

### 2. Linked, never copied

Switchyard tickets and Amber items **resolve at render time** and are never
written into Chronicle's tables. A column, struct field, or cached row holding
upstream *state* — `ticket_status`, `amber_state`, a title copied at write time
— is a third source of truth that goes stale silently, which is the exact
failure the tier split exists to prevent.

A cache is allowed. **A cache with no visible staleness is a copy that lies**:
cached state must carry its age, and an unreachable upstream must render as
unreachable rather than as a confident stale value. If a diff adds caching here,
find where the age is rendered; if you cannot, that is the finding.

Colour is an estate-wide rule, not a Chronicle choice: **coral is Switchyard,
gold is Amber**, wherever either resolves.

### 3. Audio deletion is gated on a durable transcript, not on the calendar

CLAUDE.md calls this "the single worst thing this system can do", and it is the
reason CHRN-22 is Mode C. Audio prunes at 30 days *by policy*, but it is not
regenerable — so the gate is **a transcript that exists and is durable**, never
elapsed time alone, and never a per-note pin alone.

Pruning audio for a memo whose transcription never succeeded is unrecoverable
loss with no user-visible warning. In any diff touching retention, deletion, or
the transcription result path, trace what happens when transcription **failed**,
**is still pending**, or **succeeded but wrote nothing**. A pruner whose
predicate is a timestamp comparison and nothing else is 🔴 regardless of how the
surrounding code reads.

**One deletion path in this repo is deliberately not that, and reading it as
the pruner would produce a false 🔴 on every PR that touches it.**
`internal/upload/sweep.go` (CHRN-20) collects **abandoned partial uploads**, and
its predicate *is* a timestamp comparison and nothing else — correctly, because a
partial upload is regenerable: the phone still holds the recording until the
memo is acknowledged, which is the same argument that makes
`tier1.memo_uploads` a tier-1 table. The two are told apart by what the code can
name, not by how it reads:

| | sweep | pruner (CHRN-22) |
|---|---|---|
| deletes | partial uploads in `audio.StagingDir` | recordings under an author's directory |
| regenerable | **yes** | **no** |
| gate | idle for a TTL | a durable transcript |

So the check on a sweep diff is narrower and just as firm: **can it name
anything outside `tier1.memo_uploads` and `StagingDir`?** If a change lets it
walk an author's directory, read a memo row, or take a path from a caller, that
is 🔴 — it has stopped being the sweep. `TestSweepNeverReachesAFinishedRecording`
is the assertion; a diff that weakens or deletes it is the finding.

### 4. Credentials

Sign-in is invite-based: a single-use invite redeems into a durable per-device
session. `internal/store/user.go` stores only `token_hash`; the plaintext is
`chr_`-prefixed and returned exactly once, by the call that mints it.

- **A token is returned once.** Anything that puts token material into a second
  response, a log line, an error string, or a URL that outlives its redemption
  is 🔴.
- **The invite URL carries a live credential.** `internal/invite/url.go`
  deliberately refuses a base URL with a query, forced query, or fragment.
  Preserve that direction: a malformed base is *dropped* rather than stored.
- **`DATABASE_URL` carries a password.** Anything that prints config, echoes
  flags, or wraps a DSN into an error message is a candidate. LYCM-119 is the
  estate's instance of this exact bug.
- **Cookie flags come from config, not from the request.** `setSessionCookie`
  uses `a.secureCookies`, not `r.TLS != nil` — the service terminates TLS at
  Traefik, so the request is plain HTTP and the request-derived version silently
  ships a non-`Secure` cookie. If a diff reintroduces request-derived flags,
  that is 🔴.
- **`clientIP` trusts `X-Forwarded-For` only when the request carries the secret
  Traefik stamps** (`X-Chronicle-Proxy-Secret`, compared with `crypto/subtle`).
  Reading the leftmost hop instead of the rightmost, or **treating the header's
  presence as trust rather than comparing it**, lets any caller pick its own
  rate-limit bucket. The presence-not-comparison version is the specific shape
  CHRN-75 was: a neighbour going direct arrives with the header set and wrong.
  This replaced `TrustedProxies`, which could not express the question — on
  `construct_net` no CIDR distinguishes Traefik from a neighbour.

### 5. Guards are applied in the route table

Routes are wired in `internal/api/router.go`, and the wrapper is applied *at the
call site* — `mux.HandleFunc("POST /auth/session", a.limitSignIn(a.handleAuthSession))`.
So a handler is not guarded by having a guard-shaped name; it is guarded by the
line that registers it. A new route added without its wrapper compiles, passes
its handler test, and is open. Read the route table, not the handler.

There are three wrappers and the difference between them is who, not whether:
`a.limitSignIn` (throttle, no identity — the two credential endpoints),
`a.requireUser` (a valid session), `a.requireOwner` (`requireUser` plus the
owner check — the `/admin/` surface). A `/admin/` route on `requireUser` is a
privilege widening that nothing else catches.

Two routes are deliberately unauthenticated because they are how a client gets a
credential: `POST /auth/session` and `POST /auth/sso/cloudflare`. A third one
needs an argument in the diff.

Cloudflare Access and Chronicle's own auth are **complementary, not
alternatives** — Access decides whether the request is served at all, Chronicle
decides who it is served as. A change that treats one as a substitute for the
other is a finding, not a simplification.

### 6. Migrations

Applied in order at boot by `store.Migrate`, and a failure **returns before the
server starts** — a bad migration does not fail a test, it stops the service
booting. Every `.up.sql` needs its `.down.sql`. Prefer additive.

`schema.sql` is generated from the migrations and is excluded from your diff
(`.github/review-ignore`) — the migration is the authoritative and shorter
statement of the same fact, and the `schema` job is what stops the two
disagreeing. Do not ask for a hand-edit of `schema.sql`; ask for the migration
to change and be regenerated.

### 7. A Mode C ticket must register its own package

The five Mode C tickets are the ones that can destroy authored data or hand an
agent write access to it, so their code must live somewhere `sensitive_paths`
in `.github/workflows/pr-review.yml` names. **One of the five still has no path
in this repo — CHRN-67 (MCP write scopes) and CHRN-65 (the MCP server itself:
Streamable HTTP behind Access).** Neither has a package, and an `internal/mcp/`
or `internal/api/mcp/` would match nothing in the pattern — the
`internal/api/(session|cfaccess|ratelimit|router|storage|upload|transcription)`
alternation is exhaustive. `sensitive_paths` cannot list a package that does not
exist, so the entry has to arrive with the code.

So: **if this PR implements CHRN-65 or CHRN-67, check that it adds its new
package to `sensitive_paths` in the same PR.** If it does not, that is a 🔴
Important finding — every subsequent PR touching Chronicle's MCP surface, the
Access-facing transport included, would otherwise be reviewed at the cheap tier,
silently, which is the whole cost of the omission.

CLAUDE.md's Mode C list calls CHRN-65 *"auth surface"*, which reads like the
`internal/api/session` and `internal/invite/` code that already exists. It is
not: the ticket is the MCP transport. Do not let the label talk you out of the
check.

**Two tickets this section used to name are now covered, and finding them
uncovered is a false 🔴** — check the pattern before raising one:

| ticket | path today | covered by |
|---|---|---|
| CHRN-22 (retention pruner) | `internal/retention/` | `internal/(invite\|config\|audio\|watch\|upload\|retention)/` |
| CHRN-39 (revisions) | `internal/store/` — CHRN-38 put revisions there | `internal/store/` |

For any other Mode C ticket, resolve the package against `sensitive_paths`
before writing a finding; the rule is about a package the pattern misses, not
about a ticket appearing on a list.

### 8. Logs

Structured `log/slog` to stdout, in a shape Dozzle and Datadog can read. A new
log line that interpolates a token, a DSN, an email, or a transcript body is a
finding. Config that changes behaviour when unset should log which branch it
took at boot — the estate's cautionary tale is an integration that was a silent
no-op in production for weeks and looked like a working feature.

## Verification bar

Report a finding only when you can point at the line that causes it and name the
concrete failure — the input, state, or sequence that produces the wrong
outcome. "This could be risky" is not a finding.

Behaviour inferred from a name is not evidence. If you find yourself writing
"this may not handle…", go read the implementation or drop it. For anything
turning on a claim in a comment or PR body, verify the claim — this repo's
comments are unusually load-bearing and carry reasoning not recoverable from the
code, which is exactly why a false one is expensive.

Where a probe is cheap, run it. This repo builds fast, and the tier and grant
questions above are decidable from `migrations/` and `schema.sql` by reading,
not by reasoning about what a grant probably does.

## Re-reviews

Round three should be shorter than round one. After the first review of a PR:
report **new Important findings only**. No new nits, no restating open findings,
no re-raising something the author explicitly declined. Note in one line what got
fixed, then move on.

Check that a fix was applied **at the layer that owns the invariant**, not at the
call site that happened to be reported. If a guard, a normalization, or a
required step was added, ask whether every path through it is covered.

## Summary shape

Open with a one-line tally — `2 important, 1 nit` — or **No blocking issues**.
Then ticket fidelity in a sentence, naming the review mode. Then findings, most
severe first, each with the file, the concrete failure, and what would fix it.

Close with what you checked and could not fault — on a Mode A ticket you are the
only diff read, so a review listing only problems does not tell the human what
was examined.

If the diff is clean, say so in one line and stop. Do not pad.
