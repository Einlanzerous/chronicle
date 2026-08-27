# CHRN-75 — What makes a request trustworthy (decision)

Status: **proposed 2026-08-26 · amended 2026-08-26 after magos's review.**
Decision 1 is accepted with three amendments, marked **[rev]**, plus one
found during implementation review and marked **[rev 2]** — §3's third degraded
state, which the signed-off text missed. **The first
draft's Decision 2 — "charge for refusals, not requests" — is WITHDRAWN**: its
prose contradicted its own pseudocode, and §3 records why rather than deleting
the mistake. What replaces it is smaller and does the job the withdrawn half was
reaching for.
Ticket: CHRN-75 (Phase P1, parent CHRN-2). Tier `opus`, so Mode B: this document
is the review artefact and the PR that follows it is mechanical.
Decision owner: magos.
Read by: CHRN-20 (held on this), CHRN-65 (auth surface), and anything later that
asks "who is this request from".
Corrects: `docs/decisions/chrn-71-accounts-and-sessions.md` §7, whose closing
paragraph is false on this network. See §6.

## Context

`CHRONICLE_TRUSTED_PROXIES` defaults to `172.16.0.0/12`. `construct_net` is
`172.19.0.0/16`. The first contains the second, so **every one of the eighteen
containers on the shared network is a trusted proxy**, and `clientIP()` believes
the `X-Forwarded-For` any of them sends.

Verified against the live networks rather than taken from the ticket:

```
construct_net       172.19.0.0/16   gateway 172.19.0.1
construct_edge_net  172.31.240.0/24
traefik             172.19.0.16     ← DHCP, not reserved
chronicle           172.19.0.5
```

Eighteen containers hold an address in `172.19.0.0/16`; Traefik's is one of them
and is indistinguishable from the rest. So a neighbour reaching
`http://chronicle:4009/auth/session` directly — no Traefik in the path — picks
its own rate-limit bucket with a header it writes itself. Unbounded invite
guessing in one direction, and 429ing the owner off their own service in the
other.

**This is the exact threat the in-process limiter was written for.** CHRN-71 §7
justifies its existence as lateral rather than external. At this value it does
not cover the lateral path, and the edge limiter never did. Neither limiter
covers it.

The three options in the ticket are each wrong in a different way, and the reason
they are is worth naming before choosing between them: **options 1 and 3 both try
to make the peer address meaningful again, and the peer address is not the thing
that distinguishes Traefik from a neighbour on this network.** Nothing about
`172.19.0.16` is different in kind from `172.19.0.20`. Only *how a request got
here* differs, and an address cannot express that.

## Decision

> **1 · Trust a signal only Traefik can produce.** Traefik stamps a shared secret
> on every request it proxies to Chronicle. `clientIP()` believes
> `X-Forwarded-For` when and only when that header carries the configured secret.
> `CHRONICLE_TRUSTED_PROXIES` is retired: it cannot express the thing it needs to
> express here, and a knob that looks like it does security work and does not is
> the defect class `REVIEW.md` names.
>
> **2 · [rev] A secret that does not match is never silent.** A *wrong* secret and
> an *unset* one produce the same behaviour — one shared bucket — but only the
> unset case warned. A rate-limited warning on mismatch is what makes the
> degraded state findable, and it is the likeliest first-deploy state.

This is the ticket's option 2. Options 1 and 3 are rejected in §5, and option 3
is worth reopening later for a reason that has nothing to do with rate limiting.

## 1 · Why a shared secret, and what it is not

The question `clientIP()` needs answered is **"did this request come through the
estate's edge?"** — not "what address is the peer at". A secret answers exactly
that question and nothing else, which is why it is the right shape here rather
than merely the cheap one.

Traefik sets the header with `customRequestHeaders`, and the load-bearing fact is
that this **overwrites** whatever the caller sent. The estate already depends on
that semantic: `strip-identity-headers` deletes eleven spoofable identity headers
by setting them empty on both entrypoints, and `strip-cf-access` does the same
for `Cf-Access-*` on the public edge. This is the same mechanism used in the
same direction.

I did not take that from the documentation. In an isolated Traefik lab on its own
network (torn down afterwards, nothing production touched):

```
A · client FORGES the header, through Traefik
    sent:     X-Chronicle-Proxy-Secret: FORGED-BY-CLIENT
              X-Forwarded-For: 9.9.9.9
    arrived:  X-Chronicle-Proxy-Secret: lab-fixture-not-a-real-value   ← replaced
              X-Forwarded-For: 172.27.0.4                           ← replaced

C · neighbour goes DIRECT to the backend, forging both
    arrived:  X-Chronicle-Proxy-Secret: FORGED-BY-CLIENT            ← kept, and WRONG
              X-Forwarded-For: 9.9.9.9                              ← kept, and ignored
```

**[rev] The lab ran the same image production does — not a nearby version.**
`traefik:v3.3` resolves to `sha256:2cd5cc75…`, byte-identical to the digest
`docker-compose.yml:1144` pins, and `traefik version` reports **3.3.7** in both.

Both properties hold. Through Traefik the secret is honest no matter what the
client sends; around Traefik the client keeps its forgery and the forgery does
not match. Note case C carefully: **the header is present and wrong.** The check
is a comparison against the configured value, never a presence test.

**What this is not:** it is not authentication, and it must never be read as any.
It decides one thing — whether `X-Forwarded-For` is believed for rate-limit
keying. Nothing else in Chronicle may consult it. A secret that starts as a
keying hint and ends up gating a route is how this kind of header becomes a
credential nobody rotated.

Two alternatives inside option 2, both considered and dropped:

- **A header naming the entrypoint** (`X-Chronicle-Entrypoint: public`). A
  neighbour forges it as easily as `X-Forwarded-For`; it carries no secret, so it
  distinguishes nothing.
- **mTLS between Traefik and Chronicle.** Correct, and the honest end-state of
  this line of thinking. It needs a CA, a rotation story and a client-cert
  configuration on a shared Traefik — machinery an estate this size has not
  built, for one keying decision.

## 2 · The rule, exactly

```
clientIP(r):
    if proxySecret is configured
       AND subtle.ConstantTimeCompare(header, proxySecret) == 1
       AND X-Forwarded-For is present and its rightmost entry parses:
        → that address
    otherwise:
        → RemoteAddr
```

Unchanged from today: **rightmost** entry, because with `trustedIPs: []` Traefik
writes a single address, and where a chain exists the last hop is the only one
our trusted peer actually observed. Reading the leftmost takes the caller's word.

Constant-time comparison. The value is low-stakes but comparing a secret with
`==` in the credential path is not a habit worth keeping, and `crypto/subtle`
costs one import.

**[rev] The header is never logged, and the reason is structural rather than a
list.** The first draft said it "goes in whatever redaction list the log setup
already has." There is no redaction list. `requestLogger`
(`internal/api/router.go:209`) emits method, path, status and duration and **no
headers at all** — its own comment says *"Note what is absent: no query string,
no Authorization header, no request body."* So the requirement on the
implementing PR is to keep that true, not to add an entry somewhere: the secret
must not appear in the mismatch warning of §3 either, which logs the peer and a
hint and neither value.

### When the secret is unset

`clientIP` returns `RemoteAddr` always, and boot warns — the same shape CHRN-71
§7 chose, and for the same reason: the coarse behaviour is safe-but-blunt and
should announce itself rather than be discovered. Every request through Traefik
then shares one bucket.

Note the direction that fails in: **a missing middleware over-limits rather than
under-limits.** The failure mode is a coarse bucket, not a spoofable one. That is
the right way round, and it is a property of putting the secret on the trusted
side of the boundary rather than the untrusted one.

### Retiring `CHRONICLE_TRUSTED_PROXIES`

Removed from config, from `deploy/compose.chronicle.yml`, and from
construct-server's `docker-compose.yml`.

**[rev] Set, it warns and is ignored. It does NOT error.** The first draft had
`config.Load` refuse to start, on the half-configured-Access precedent. That
precedent does not transfer, and the review is right about why:
`deploy/compose.chronicle.yml` pins `${CHRONICLE_TAG:-latest}` and
construct-server's compose sets the variable **today**, so pulling the new image
before the SERV compose change lands turns a retired knob into a **crash loop**.
The Access pair errors because a half-configured pair is a *silent security
failure*; a retired variable that affects nothing is not, so the hard error would
buy tidiness at the price of an outage window.

So: warn, name the replacement, ignore the value. It becomes an error one release
later, once no deployed compose file still sets it — a follow-up line, not a
condition on this ticket.

## 3 · [rev] A mismatched secret must not be silent

**This replaces the withdrawn Decision 2, and it is the amendment that matters
most.**

Boot warns when the secret is *unset*. Nothing warned when it was *wrong* — and
wrong is the likelier state, because it is what a typo, a rotation half-applied,
or an image deployed ahead of the SERV change all produce. A wrong secret means
every WAN request quietly shares one bucket with no signal anywhere. That is
`CLAUDE.md`'s *"a cache with no visible staleness is a copy that lies"* in a
different costume: the system is in a degraded state it is not reporting.

So: when `X-Chronicle-Proxy-Secret` is **present and does not match**, log a
warning — at most once per minute, guarded by a timestamp under the limiter's
existing mutex, because the lateral path lets a neighbour trip it deliberately.

```
WARN  msg="X-Chronicle-Proxy-Secret did not match; X-Forwarded-For is being ignored"
      peer=172.19.0.20
      hint="Traefik and Chronicle disagree about the secret, or a neighbour is forging it"
```

**Neither value is logged** — not the received one, not the configured one. The
peer address is the whole diagnostic: `172.19.0.16` means the deploy is broken,
anything else means a neighbour is probing.

Through Traefik this appears in Dozzle on the first request after a bad deploy.
That is what makes the rotation story in *What this does not decide* honest: a
mismatch degrades to the coarse bucket **and says so**, which is a different
thing from degrading silently.

### [rev 2] The third degraded state, which this section originally missed

Found by the reviewer on PR #13, **after sign-off**, and it is a gap in this
document rather than in the code that implemented it. There are **three** ways
to end up in one shared bucket, not two:

| | boot warns? | §3 warns? |
|---|---|---|
| secret unset | yes | — |
| secret set, **wrong** | no | yes |
| secret set, **never presented** | **no** | **no** ← |

The third is a WAN request arriving through Traefik with the **middleware not
attached**, and at the request level it is indistinguishable from a neighbour
talking to the container directly — both simply have no header. So the secret is
set (boot is quiet), the header is absent (the mismatch path is quiet), every
sign-in shares one bucket, and **a WAN stranger locks the owner out with twenty
requests while nothing says a word.** That is this section's own argument
applied to a state this section left out.

It is also the *likeliest* way SERV-148 half-lands: that ticket attaches the
middleware to three routers and its own text warns *"all three, not only the
`/auth/` one"*. Miss `chronicle-public-auth` and it is the sign-in path that
degrades.

**So an absent header is reported until one request proves the middleware is
attached, and silent for the life of the process afterwards.** A neighbour
cannot produce a match, so it cannot switch the signal off; and the warning
stops the moment the deploy is actually correct, which is what keeps it from
becoming noise on a LAN install. With no secret configured at all it stays
quiet — boot already covers that case, and doubling it would train someone to
ignore both.

### The withdrawn half, and why it was wrong

The first draft's Decision 2 proposed charging the limiter only for refusals
(401/403) rather than for requests, and claimed *"a valid credential is never
refused, however full the bucket is"* and that this *"is what actually kills
owner lockout."* **Both claims are false, and its own pseudocode shows it:**

```go
if !permit(key) { 429 }        // ← refuses on a full window, BEFORE the handler runs
next(rec, r)
if 401/403 { charge(key) }
```

In the degraded shared bucket, a stranger sends twenty bad tokens, gets twenty
401s, charges twenty times, fills the window — and the owner's *valid* redemption
then hits `permit` and gets a 429. Lockout, exactly as today. **An attacker's
traffic is entirely refusals, so counting refusals instead of requests changes
nothing at all about the threat named.**

What it actually bought was that *legitimate successful* sign-ins stop consuming
budget. Real, and small at household scale — and paid for with a status recorder,
a non-atomic `permit`/`charge`, and unbounded 400s on the lateral path.

And the Done-when it proposed — *"a valid redemption succeeds against a full
bucket"* — **is unmeetable in principle.** You cannot know a credential is valid
without running the handler, and running the handler on a full bucket makes the
limiter decorative: the guess gets tested either way, and a correct guess returns
200. That is the entire thing the limiter exists to prevent.

Recorded rather than deleted, because the idea is attractive enough to be
proposed again.

## 4 · The test that fails before the fix

The ticket asks for one, and the existing `clientIP` tests do not catch this
because they use a narrow made-up proxy prefix. **The defect only appears at the
deployed value**, which is the whole lesson: the unit tests were right and the
configuration was wrong.

```
trustedProxies = 172.16.0.0/12          ← the shipped default
peer           = 172.19.0.5             ← a neighbour on construct_net
no proxy secret presented
21 × POST /auth/session, each with a different X-Forwarded-For

before: 21 × 200/401  — a fresh bucket per spoofed value
after:  the 21st is 429 — all of them keyed on 172.19.0.5
```

**[rev] What survives in the tree is narrower than that, and the first draft
oversold it.** `trustedProxies` is being deleted, so the post-fix test cannot
reference the field the pre-fix behaviour turned on. The failing-before run is a
fact about this branch's history, reproducible by checking out the parent commit;
what the merged suite asserts is the *behaviour*: a neighbour presenting no
secret, or a wrong one, is keyed on its peer address however many distinct
`X-Forwarded-For` values it sends. The PR body carries the before/after run.

Plus, in the same table: through-Traefik traffic (correct secret, distinct
`X-Forwarded-For`s) still gets a bucket each, so the fix does not regress to one
shared bucket; a *wrong* secret from a neighbour is keyed on the peer, which is
lab case C as a unit test; and the §3 warning fires once and not twenty-one
times.

## 5 · Why not the other two

**Option 1 — pin Traefik an `ipv4_address` on `construct_net`.** It would work,
and it is what `construct_edge_net` already does. The problem is not tidiness: on
`construct_net` there is **no reserved range**. IPAM is default, DHCP allocates
across the whole `/16`, and Traefik currently sits at `172.19.0.16` by allocation
order alone. Pin that address and any container that comes up first can hold it,
at which point **Traefik fails to start and every tunneled service on the estate
goes dark.** Reserving a band means setting `ip_range` on `construct_net` — which
is `external:`, so changing its IPAM means recreating the network and every
container attached to it. A SERV change, and a large one, to fix a Chronicle
keying bug.

**Option 3 — put Chronicle on its own network with only Traefik.** Conceptually
the strongest of the three, and there is an estate precedent: SERV-107 bound
Traefik's `internal` entrypoint to its single `construct_edge_net` address
precisely so the other ~30 containers could not reach it. The same move works
here — a two-member network, Chronicle binds its listener to that address only,
and the peer address is meaningful again because nobody else can connect.

It is rejected **for now**, not on principle:

- Chronicle must stay on `construct_net` regardless, because Postgres is there.
  So it is dual-homed, and Docker DNS would hand Traefik two addresses for
  `chronicle` with no guarantee which it dials. Making that deterministic means
  addressing the backend by literal IP in `routers.yml`, which is a footgun of
  its own.
- It is a two-repo change to shared infrastructure to fix a keying bug that one
  header fixes.

**It is worth reopening on its own merits, and CHRN-65 is the moment.** What it
buys is not better rate limiting: it removes the lateral path entirely, so no
neighbour can reach `/auth/session` at all. That closes a class rather than an
instance — and it is the honest end-state of the sentence CHRN-71 §7 opens with.

## 6 · What was already corrected, and what this corrects

Corrected in construct-server#162, before this decision — the comments asserting
the opposite of the truth. `docker-compose.yml` claimed *"A neighbour container
on construct_net is not a trusted peer and stays keyed on its real address"*;
`routers.yml` built on that claim to argue the two limiters covered complementary
paths. Both now state the gap. Also corrected there: *"Both entrypoints set
`forwardedHeaders.trustedIPs: []`"* is literally true only of `public` —
`internal` has no `forwardedHeaders` block and relies on Traefik's default. Same
effect today, and it was checkable and wrong.

**Still false, and this ticket owns it.** Three places in *this* repo repeat the
claim:

| file | what it says |
|---|---|
| `internal/api/ratelimit.go`, `clientIP` doc comment | *"A neighbour container connecting directly is not a trusted peer, so its own X-Forwarded-For is ignored"* |
| `deploy/compose.chronicle.yml` | *"which makes this trustworthy from Traefik and from nowhere else"* |
| `docs/decisions/chrn-71-accounts-and-sessions.md` §7 | the same sentence, as the justification for the design |

The implementing PR fixes all three. That decision doc gets a `[rev]` note rather
than an edit — CHRN-18 set that pattern, and a decision record that quietly
changes its own reasoning is worth less than one that shows the correction. Which
is also why §3 above keeps the withdrawn half rather than deleting it.

A reader who believes a false comment does not look again. That is why this is
listed as work rather than left as tidying.

## 7 · What CHRN-20 inherits

CHRN-20 is held on this ticket because *"the choice constrains CHRN-20's upload
path"*. What it actually constrains, now that the choice is made:

- **`clientIP()` stays the single answer to "who is this request from."** CHRN-20
  uses it and does not grow a second notion of client identity.
- **The upload path must not key anything durable on an IP address.** A phone
  moving between wifi and mobile data changes address mid-upload; a resumable
  upload keyed on IP breaks in exactly the case resumability exists for. Key on
  the session and the account — which CHRN-71 already provides and which
  CHRN-18's idempotency key identifies an arrival attempt within.
- **The edge rate limit stays off the upload path**, as `routers.yml` already
  documents: a 5 req/s bucket across a chunked 40-minute upload throttles ingest
  rather than attackers.
- **The proxy secret is available to CHRN-20 and it must not use it for
  authorisation.** §1's constraint, restated where it is most likely to be
  violated: the upload endpoint is internet-facing, and "came through Traefik" is
  tempting to read as "trusted". It is not. `requireUser` is.

CHRN-20 is unblocked the moment this is signed off.

## Configuration

| variable | | |
|---|---|---|
| `CHRONICLE_PROXY_SECRET` | new | Shared with Traefik. Lives in Signet, rendered into the env like every other estate secret. Unset → `X-Forwarded-For` is never believed, and boot warns. Wrong → §3's warning. |
| `CHRONICLE_TRUSTED_PROXIES` | **removed** | Set → warn naming the replacement, and ignore. Not an error; see §2. |

Header: `X-Chronicle-Proxy-Secret`.

### [rev] The construct-server side is three parts, not one

The first draft's snippet was **wrong and would have failed silently**, which is
the combination §3 exists to prevent:

```yaml
# WRONG — the file provider does not expand ${...}. Traefik would stamp the
# literal string "${CHRONICLE_PROXY_SECRET}", which never matches the configured
# value, and before §3 nobody would have found out.
X-Chronicle-Proxy-Secret: "${CHRONICLE_PROXY_SECRET}"
```

The estate precedent is Go-templating in the dynamic file, and it is already load
bearing for the CrowdSec bouncer at
`config/traefik/dynamic/routers.yml:137` — whose neighbouring comment on the
Traefik service says so in as many words: *"Referenced from dynamic/routers.yml
via file-provider go-templating."* So the SERV ticket has three parts and each is
required:

1. **Signet** — mint `CHRONICLE_PROXY_SECRET`, target `.env` on both hosts.
2. **`docker-compose.yml`** — an env line on the **traefik** service, beside
   `CROWDSEC_BOUNCER_KEY` at `:1162`:
   `- CHRONICLE_PROXY_SECRET=${CHRONICLE_PROXY_SECRET:-}`
3. **`config/traefik/dynamic/routers.yml`** — the middleware, templated:
   ```yaml
   chronicle-proxy-secret:
     headers:
       customRequestHeaders:
         X-Chronicle-Proxy-Secret: '{{ env "CHRONICLE_PROXY_SECRET" }}'
   ```
   attached to `chronicle`, `chronicle-public` and `chronicle-public-auth`.
   **All three**, not only the `/auth/` one — `clientIP` is a general answer and
   CHRN-20 will want it on the upload router.

**This is the one part untestable from this repo**, which is why it is spelled
out here: a Chronicle-side merge alone leaves production in the coarse-bucket
case, now visibly rather than silently.

## What this does not decide

- **Whether the lateral path should exist at all.** Option 3 closes it; this
  closes the keying bug. Raised for CHRN-65.
- **Rotation of `CHRONICLE_PROXY_SECRET`.** Signet's ordinary rotation applies,
  and a mismatch degrades to the coarse bucket rather than to an outage — **and
  logs, per §3**, which is what makes that acceptable rather than merely quiet.
- **The burst and window** (20/minute). Untouched.
- **Anything about CHRN-16's edge limiter.** It covers WAN volume; that is
  unchanged and still complementary — which is what `routers.yml` will be able
  to say truthfully again once this lands.

## Done when

- A neighbour on `construct_net` cannot choose its own bucket — a neighbour
  presenting no secret, or a wrong one, is keyed on its peer address however many
  distinct `X-Forwarded-For` values it sends. The failing-before run against
  `172.16.0.0/12` goes in the PR body (§4).
- A WAN client through Traefik still gets its own bucket — no regression to one
  shared bucket.
- A wrong secret from a neighbour is keyed on its peer address (lab case C, as a
  unit test).
- **A mismatched secret logs once per minute, carrying the peer and neither
  value.**
- **[rev 2] A configured secret that no request has ever carried logs too**,
  until one does — the half-applied-middleware state, which was silent in the
  first two drafts. It goes quiet permanently on the first match, cannot be
  silenced by a forged value, and does not fire when no secret is configured.
- `CHRONICLE_TRUSTED_PROXIES`, if set, **warns and is ignored**. It does not
  refuse to boot.
- The three false comments in §6 are corrected, and CHRN-71 §7 carries a `[rev]`.
- `deploy/compose.chronicle.yml` and construct-server's `docker-compose.yml` /
  `routers.yml` describe what is true, and stop pointing here.
- A SERV ticket exists carrying **all three** parts above — Signet key, env line
  on the traefik service, Go-templated middleware on three routers — and this
  ticket does not close before it lands.
- `./verify.sh` green.
