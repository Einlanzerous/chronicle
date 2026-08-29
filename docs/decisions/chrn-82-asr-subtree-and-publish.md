# CHRN-82 — Where `asrd` lives, and how its image publishes (decision)

Status: **accepted 2026-08-29 by magos, at the recommendations on all three
rulings.** The settlement is recorded under *The rulings*. The two PRs that
follow this document are mechanical: the first is a rename, the second is a
workflow.
Ticket: CHRN-82 (Phase P2, standalone — E3 is closed and this is the half of
it that was never anyone's). Tier `opus`, so Mode B: this document is the
review artefact.
Decision owner: magos.
Read by: **SERV-156** (the compose half; §5 gives it the image name and the
`versions.env` key), **CHRN-83** (the runner; §5 decouples the build from it),
**SERV-154** (the `:latest` race; §5 inherits its fix), **CHRN-80** (remote
workers; §3 is where that protocol would go), and **Catenary**, whose spec URL
changes once and before it has generated anything (§3).

## Context

The question is older than the ticket and has never actually been argued.
CHRN-24 put the image in `deploy/asr/` with *revisit at CHRN-29*; CHRN-25's
decision listed the repo split under *What this does not decide* and called
CHRN-29 "the last cheap moment"; CHRN-29 closed on its own Done-when — the
contract doc and the Catenary handoff — and left the question unowned. So
"stays in the repo" is the standing default, not a decision. This is where it
becomes one.

Five facts were checked against the tree on 2026-08-29 rather than
remembered, and they set the weights:

- **The coupling is one generated package.** `go list -deps ./cmd/asrd`
  reaches `internal/asr`, `internal/asr/migrations`, `internal/asrclient` and
  nothing else in the module. No shared config, logging or store code. The one
  shared package is the client generated from `deploy/asr/openapi.yaml`, and
  the service imports it *on purpose* — for its wire types, so that a spec
  change the handlers have not caught up with is a compile error.
- **Catenary has generated nothing.** `~/projects/catenary` holds no reference
  to the spec, to `CLIENT.md`, or to the image. The coordinated two-service
  change CHRN-25 warned about has not started; the cheap moment is still now.
- **Cadence is coupled today and will not be later.** 15 of the 40 commits
  since E3 opened touched `asr` paths, and 1.2.0 and 1.3.0 both carried asr
  features. After E3 the remaining asr work is CHRN-80 and pin bumps — a
  service that changes a few times a year, inside a repository that will
  release weekly through E4–E9.
- **The runner is per-repository.** `Einlanzerous` is a personal account, so
  `chronicle-runner` (online, `self-hosted,Linux,X64`) cannot be shared at
  org level. A second repository is a second registration on the same box.
- **Estate image names follow the repository.** Every multi-image repo
  publishes `ghcr.io/einlanzerous/<repo>/<component>` — `switchyard/backend`,
  `interlock/worker`, `drydock/shell`. The ticket proposes
  `ghcr.io/einlanzerous/estate-asr`, which is the name for a repo that does
  not exist. The Dockerfile's OCI title label already says `estate-asr`.

## 1 · Three answers, and why the third

**Move to its own repository.** Honest version and cadence, honest ownership
of the contract and the pins, a clean cut while the cut is still one package.
Against it: a repo, a runner registration, `ci.yml` with the Postgres service,
lint, the branch-name guard, `pr-review.yml` with `sensitive_paths` and
`review-ignore`, `release.yml`, `verify.sh`, a `CLAUDE.md`, and either a new
Switchyard project or re-pointed tickets — roughly a day of scaffolding before
the first publish, against an afternoon for a job in this repo. And Chronicle's
`internal/asrclient` would still need generating from a spec it no longer
owns, which turns an in-process staleness guard into a cross-repo one — fetch
by pinned tag, or a vendored copy with a hash — which is exactly the kind of
guard that quietly stops guarding. It also serialises the actual goal: the
pump has been off since 1.2, and SERV-156 would wait on repo scaffolding
rather than on a workflow.

**Stay as it is.** Nothing new to build; the guard stays in-process; the
decision docs and benchmark tables stay where their tickets are; one
maintainer, so the "somebody eventually unpicks this" argument is about a
scale this estate does not have. Against it, two things that are real rather
than aesthetic: **one version number for two binaries** — `estate-asr:1.7.0`
would mean *whatever asrd was in Chronicle 1.7.0*, `asrd version` would report
Chronicle's number, and Catenary would pin an ASR version by reading
Chronicle's changelog — and **every Chronicle release republishes a 971 MB
image that almost never changed**, with an `ASR_TAG` in `versions.env` that
moves on every Chronicle release and says nothing about whether asr did.

**Stay, in a subtree with its own release component.** Everything the service
is moves under one directory, `asr/`; release-please gets a second package on
that path with its own tags and changelog; the publish workflow triggers on
that path and those tags. That buys the two real advantages of moving — an
honest version and an honest cadence — for the price of a `git mv` and an
import-path rewrite, with no new repository, runner, CI, or cross-repo guard.
And it makes "stay for now" *reversible* rather than nominally so: if the
split is ever worth doing, `git filter-repo --subdirectory-filter asr`
produces the new repository with its history intact and a build that already
works, because §2 is the invariant that makes it so.

Chosen: the third.

## 2 · The boundary — nothing under `asr/` imports anything outside it

This is the invariant that makes the subtree a subtree rather than a
directory, and it is worth stating in the same shape as `CLAUDE.md`'s tier
rule: **`asr/` depends on `go.mod` and the standard library, and on nothing
else in this module.** Two things violate it today, and each gets a specific
resolution rather than an exception.

### The wire types: generated into the subtree from the same spec

`internal/asr/api.go` imports `internal/asrclient` for the generated request
and response types, and `internal/asrclient/oapi-codegen.yaml` says why:
*"one definition of the wire format, held by both ends, so a change to the
spec that the service has not caught up with is a compile error rather than a
field that quietly stops being sent."* That property is worth keeping. The
import that carries it is not.

**Ruling 3 recommends:** the service gets its own generated types,
`asr/internal/wire`, from `asr/openapi.yaml`, with `models: true` and
`client: false` — the same generator, pinned by the same `tool` directive, run
by the same `scripts/gen-asrclient.sh`, under the same staleness check in
`verify.sh` and `ci.yml`. Two generated artefacts from one spec, both
regenerated by one script, both guarded. A spec change still recompiles both
ends against the same file; what changes is that the service's copy lives on
its side of the line. `internal/asrclient` stays exactly what it is:
Chronicle's client, `models` and `client`, unchanged.

The alternative — keep the import and write the invariant as "nothing outside
`asr/`, except `internal/asrclient`" — costs less today and loses on the day it
matters: an exception list on an invariant is where the second exception goes,
and on a split the new repository would need the generated types anyway. The
cost of doing it now is a second oapi-codegen config and a few lines of shell.

### The pump's integration test: one exported doorway, inward only

`internal/transcribe/integration_test.go` imports `internal/asr` to run the
real service in-process against the generated client — *"the only thing
standing between them is the spec."* That test exists **because** both halves
live in one repository; it is a benefit of staying, not a cost, and it keeps
running. But once the service is `asr/internal/asr`, Go's `internal` rule
stops `internal/transcribe` importing it, correctly.

So the subtree exports exactly one package, **`asr/asrtest`**: a harness that
connects, migrates, builds the router and a worker around a caller-supplied
`Transcriber`, and returns the `httptest.Server`. The types the stub has to
implement (`Transcriber`, `TranscribeRequest`, `Transcript`) are re-exported
from it as aliases. Everything else under `asr/` stays `internal`, so the
compiler — not a convention — limits what Chronicle can reach.

The direction matters and is asymmetric: Chronicle importing the service's
harness is client one testing against the service it lives beside; the
service importing Chronicle is the coupling §2 forbids. On a split, the
harness becomes a module dependency or the test moves to a binary; either is
a change to a test, not to a service.

### The guards, because a boundary with no guard is a directory

Two, and the first is free:

- **The image build copies only the subtree.** The `asrd` stage in the
  Dockerfile changes from `COPY . .` to `COPY go.mod go.sum ./` and
  `COPY asr/ asr/`. If anything under `asr/` imports a package outside it, the
  file is not in the build context and `go build ./asr/cmd/asrd` fails. The
  same change means a Chronicle-only commit no longer invalidates the `asrd`
  stage — the layer is rebuilt when asr changes and not otherwise.
- **`verify.sh` checks both directions.** Outward:
  `go list -deps -test ./asr/...` filtered to this module must contain nothing
  outside `asr/` (tests included — today the service's tests also import
  `internal/asrclient`, and that goes the same way as §2's first point).
  Inward: a direct import of `chronicle/asr/...` from a file outside the
  subtree is allowed only of `asr/asrtest`, checked with a grep. `ci.yml`
  runs `verify.sh`'s steps, so both checks are in CI by inheritance.

## 3 · The layout

Flat at the top of the subtree, because that is what a repository's root looks
like and the point of §1 is that this could be one:

| today | after | note |
|---|---|---|
| `cmd/asrd/` | `asr/cmd/asrd/` | binary; `go build ./asr/cmd/asrd` |
| `internal/asr/` | `asr/internal/asr/` | service, with `migrations/` inside it as now |
| — | `asr/internal/wire/` | **new**, generated (§2) |
| — | `asr/asrtest/` | **new**, the one exported package (§2) |
| `deploy/asr/openapi.yaml` | `asr/openapi.yaml` | the contract, at the top of its own tree |
| `deploy/asr/README.md`, `CLIENT.md` | `asr/README.md`, `asr/CLIENT.md` | the README already reads as the subtree's |
| `deploy/asr/Dockerfile` | `asr/Dockerfile` | build context stays the repo root (§2 needs `go.mod`) |
| `deploy/asr/compose.asr.yml`, `provision-db.sh` | `asr/deploy/` | the machine-shaped half |
| `deploy/asr/bench-in-container.sh`, `results/` | `asr/bench/` | the harness path changes; the harness does not |

What does **not** move, and why:

- **`internal/asrclient/`** — Chronicle's client, generated from
  `asr/openapi.yaml`. It is Chronicle's, so it lives with Chronicle.
- **`internal/transcribe/`** — the pump. Client one.
- **`docs/decisions/chrn-25-*.md`, `chrn-26-*.md`** — decisions on Chronicle
  tickets, cross-referencing Chronicle tickets. `asr/README.md` links to them
  as it does now. On a split they would be copied, not moved.
- **The `asr_test` database in `ci.yml` and `verify.sh`** — unchanged; the
  DSN is a fact about the tests, not their path.

Path references to fix in the same PR: `README.md` (3), `deploy/README.md` (2),
`asr/README.md` (5), `asr/CLIENT.md` (2), `scripts/gen-asrclient.sh`,
`.github/review-ignore`, and `pr-review.yml`'s `sensitive_paths` — where
`internal/asr/` and `cmd/asrd/` become `asr/`, which is broader and
correctly so: the whole subtree is the thing that can lose a memo's
transcription.

**IDEA-23 carries a comment pointing at `deploy/asr/CLIENT.md`.** That path
goes stale in the first PR; a follow-up comment with the new path is part of
that PR's evidence, not a later tidy-up. Catenary has generated nothing, so
the spec URL changing once costs it nothing now and would cost a coordinated
change later — the CHRN-25 warning, arriving on schedule.

## 4 · Its own version

release-please gets a second package:

```json
"packages": {
  ".":   {},
  "asr": { "component": "asr", "release-type": "simple",
           "bump-minor-pre-major": true, "bump-patch-for-minor-pre-major": true }
},
"separate-pull-requests": true
```

with `"asr"` seeded in `.release-please-manifest.json`. Tags are `asr-vX.Y.Z`;
the root package keeps `vX.Y.Z` and its existing tags. Commits are attributed
by path — a commit touching only `asr/**` reaches only the asr changelog —
which makes the `(asr)` scope in commit messages cosmetic from here on rather
than the thing that sorts them. **The attribution of a commit touching both
sides is checked on the first release PR rather than assumed**; a commit that
appears in both changelogs is correct, a commit that appears in neither is a
misconfiguration.

`bump-minor-pre-major` is there so that `1.0.0` is cut by decision (ruling 1)
and not by the first commit someone marks `!`.

`asrd version` reports the `asr-v` version, stamped by §5's workflow from the
tag; a main build stays blank → `dev`, for the reason `publish.yml` already
gives at length.

## 5 · The image

**Name: `ghcr.io/einlanzerous/estate-asr`** (ruling 2), as the ticket says and
the Dockerfile's title label already does. It breaks the estate's
repo/component naming on purpose: the name is what Catenary and SERV consume,
the repository is an implementation detail, and a name that says
`chronicle/asr` would have to change on the day the repository does. The
`versions.env` key is `ASR_TAG`, pinned at major.minor like `CHRONICLE_TAG`;
the compose fragment reads `image: ghcr.io/einlanzerous/estate-asr:${ASR_TAG:-latest}`
and drops its `build:` block — the estate pulls, it does not build.

**A sibling workflow, `publish-asr.yml`**, not a second job in `publish.yml`.
Its own `on:` — push to `main` filtered to `asr/**`, `go.mod`, `go.sum` and
itself; tags `asr-v*` (path filters are not applied to tag pushes, so a tag
always builds) — and its own `concurrency` group, so the two images in this
repository cannot queue behind or cancel each other. It differs from
`publish.yml` in four places, each recorded here so the diff reads as a
decision:

- **`type=match`, not `type=semver`.** metadata-action's semver type does not
  parse a prefixed tag; `type=match,pattern=asr-v(\d+\.\d+\.\d+),group=1`
  yields the bare version, and the same group feeds `VERSION` and gates
  `type=sha` off on a tag build.
- **`flavor: latest=false` from the first commit**, which is SERV-154's fix
  arriving before the bug rather than after. `:latest` is emitted only by the
  explicit `type=raw` entry, and its polarity follows what `publish.yml` does
  today (main's tip) until SERV-108 chooses for the estate — the two images in
  one repository should not disagree with each other.
- **Registry cache, not GHA cache:** `type=registry,ref=…/estate-asr:buildcache,mode=max`.
  The whisper.cpp stages compile from source against a pinned SDK and are the
  entire cost of this build; a cache that lives in the registry is warm for
  whichever runner builds next, and it is not subject to the 10 GB per-repo
  limit a 971 MB image would spend quickly.
- **`runs-on: self-hosted`** from day one, because that is where the cores
  are and the runner is online. But because of the point above the choice is
  not load-bearing — if the runner is down, the workflow runs on
  `ubuntu-latest` slowly rather than not at all, and CHRN-83 moves the rest of
  the workflows on its own schedule.

**What the image is does not change.** The ENTRYPOINT stays `whisper-cli`,
the pins stay the pins, the stages stay the stages, and
`asr/bench/bench-in-container.sh` runs the CHRN-12 harness through the
published image unmodified. The ticket is right that a publish which changed
any of those would invalidate the benchmark table in the same commit; the
test is that `bench-in-container.sh` against `estate-asr:asr-v0.1.0` reads
inside the tolerances `asr/README.md` already states.

## 6 · Two PRs, in that order, under this ticket

1. **`refactor(asr): move the service under asr/ and seal the boundary (CHRN-82)`**
   — the rename, the generated `wire` package, `asrtest`, the Dockerfile's
   narrowed `COPY`, the two `verify.sh` checks, the path fixes, and the
   `sensitive_paths` / `review-ignore` updates. No behaviour change. The
   review question is one question: does `git diff -M` show anything other
   than renames, import paths, and the additions this section names.
2. **`ci(asr): publish estate-asr on main and asr-v* tags (CHRN-82)`** — the
   release-please package, `publish-asr.yml`, the compose fragment on the
   published image, and the *"No GHCR publish"* line gone from the README.

Two rather than one because the second's path filters and package path depend
on the first, and because a 30-file rename mixed into a workflow PR hides the
workflow. One ticket rather than two because the ticket's Done-when names the
decision and the publish together, and the rename is the decision's first
half.

## What this does not decide

- **The compose half.** SERV-156: `ASR_TAG`, the token fan-out, the model
  mount, the render node. §5 gives it the name and the key and nothing else.
- **`:latest` polarity for the estate.** SERV-108. §5 follows the house
  until there is a house rule.
- **Moving `ci`, `publish` and `release` to the runner.** CHRN-83. §5 puts
  only the heavy build there.
- **Whether the split ever happens.** §1 makes it cheap and §2 keeps it
  cheap. The trigger, if there is one, is a second client contributing to the
  contract — at which point the contract belongs in neither client's
  repository, and this document's layout is the repository it moves into.
- **A second worker on a second device.** CHRN-80. It goes under `asr/`.

## The rulings, settled 2026-08-29

All three at the recommendation, by magos, on the same day the document was
written. Recorded here so the PRs that follow can cite a settled section
rather than a proposal.

**1 · Starting version.** *Recommendation: `asr-v0.1.0`*, and `1.0.0` is cut
when the service has transcribed a memo unattended in production — E3's exit
criterion, observable only after SERV-156. The contract is `/v1/` and that is
a fact about the wire format, not about whether the service has ever run
without a hand on it; it has not. The alternative is `1.0.0` now, on the
strength of the contract being published to Catenary. It loses because a
`1.0.0` that has never run unattended is the kind of number this rig has
learned not to publish.

**2 · Image name.** *Recommendation: `ghcr.io/einlanzerous/estate-asr`*, as
§5 argues. The alternative, `ghcr.io/einlanzerous/chronicle/asr`, follows the
estate convention exactly and would be the wrong name on the day the
repository changes, which §1 exists to keep possible. If the convention is
held to be a rule rather than a habit, the decision is the other way and the
compose fragment is the only thing that notices.

**3 · The service's wire types.** *Recommendation: generate `asr/internal/wire`
from the spec*, as §2 argues, and hold the boundary with no exceptions. The
alternative — keep importing `internal/asrclient` and write the guard with one
exemption — is a smaller first PR and a larger second one on the day the
exemption matters.

## Done when

The ticket's four, restated against this document:

- **The decision is written down and accepted** — this file, with the three
  rulings settled and the settlement recorded here.
- **The boundary holds and is guarded** — `asr/` imports nothing outside
  itself, the image builds from `go.mod`, `go.sum` and `asr/` alone, and
  `verify.sh` fails on a crossing in either direction. PR 1.
- **The image publishes** — `ghcr.io/einlanzerous/estate-asr` carries
  `latest`, a short sha, and `asr-v0.1.0` / `0.1` after the first release
  PR merges; `asrd version` inside the tagged image reports `0.1.0`; the
  buildcache tag exists. PR 2.
- **The pinned tags exist for SERV to reference**, `asr/README.md`'s *"No
  GHCR publish"* line is gone, IDEA-23 carries the corrected `CLIENT.md`
  path, and `bench-in-container.sh` against the published image reads inside
  the README's stated tolerances — with the load figures recorded at both
  ends, as the README requires.
