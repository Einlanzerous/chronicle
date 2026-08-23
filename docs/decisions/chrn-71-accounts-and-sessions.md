# CHRN-71 — Accounts and per-device sessions (decision)

Status: **accepted 2026-08-23 — implemented, then amended after review**.
Amendments from the CHRN-71 code review are marked **[rev]** below; they change
four of the original positions and none of the shape.
Ticket: CHRN-71 (Phase P1, parent CHRN-1). Tier `opus`, so Mode B: this
document is the review artefact and the PR that follows it is mechanical.
Decision owner: magos.
Blocks: CHRN-16, CHRN-20, CHRN-59, CHRN-65.

## Context

Chronicle's plan has no credential surface. Four tickets presuppose an
authenticated client and none of them creates one, which is how CHRN-16 came to
look blocked on a judgement call when it was really blocked on a missing
prerequisite.

`~/projects/lyceum` already solved this on the same estate, against the same
edge, and is live with `LYCEUM_AUTH=true`. The governing principle is stated in
the comment above its tunneled router:

> this one decides whether the request is served at all, Lyceum's decides who it
> is served as

Cloudflare Access and application auth are complementary, not alternatives.
Chronicle adopts Lyceum's model rather than inventing a second one: two auth
models on one estate means two sets of edge cases to debug at 1 a.m.

## Decision

No passwords, ever. A one-time invite redeems into a durable per-device session.
Two sign-in paths mint **the same kind of session**: `POST /auth/session` for the
app and MCP on the direct host, `POST /auth/sso/cloudflare` for a browser that
already cleared Access on the tunneled host. Accounts, invites and sessions are
tier-2 tables. Auth is unconditional — there is no mode in which Chronicle serves
an unauthenticated caller anything but a health probe.

## Copied from Lyceum without change

These are settled by precedent; the PR should not re-litigate them.

- **Only the SHA-256 of a token is stored.** Plaintext is returned exactly once,
  by the call that mints it, and is not recoverable afterwards.
- **256-bit tokens**, `base64.RawURLEncoding`, with a fixed prefix so a token is
  recognisable in a paste or a log.
- **Invites are single-use with a 7-day TTL; sessions do not expire.** Redeeming
  an invite is a conditional `UPDATE ... WHERE used_at IS NULL` inside the same
  transaction that mints the session, so two devices racing the same invite
  cannot both win.
- **A spent, expired or unknown invite is 401 with an identical body.** Probing
  cannot distinguish a used invite from one that never existed.
- **Session token by `Authorization: Bearer`, falling back to an HttpOnly
  cookie**, header first. The cookie is not belt-and-braces: a browser loading a
  sub-resource (an `<img>`, an audio element streaming a memo) sends no
  Authorization header, so gating on the header alone would break every one of
  them. `Secure` only when the request arrived over TLS; `SameSite=Lax`.
- **Sign-out revokes only the credential the request rode in on.** Other devices
  keep working. `GET /auth/sessions` is the visible device list, and it is not
  optional — a non-expiring session is only safe if the person holding it can see
  and cut off a lost device.
- **The owner is seeded by migration and reconciled from env at boot**, is not
  deletable, and is the only account that may reach `/admin/users*`.
- **An invite is never auto-provisioned from an Access email.** A verified email
  with no local account gets 403 `sso_no_account`, naming the address so the
  person knows what to ask for.

## Seven deviations, and why

### 1 · No `CHRONICLE_AUTH` flag. Auth is unconditional.

Lyceum's flag defaults off and that default is right *for Lyceum*: it had a
running single-user install that predates accounts. Chronicle has no install to
keep working, so the flag would exist only to be forgotten.

It is not a free option. With auth off, Lyceum's `authenticate` serves every
request as the owner, which is the bug `verifyAuthMode` (LYCM-116) exists to
catch, and PRSR-10 found a real `.env` one `docker compose up` away from tripping
it. Declining the flag deletes `authenticate`'s fallback branch, both
"requires LYCEUM_AUTH" refusals in the admin paths, and the boot guard — four
pieces of machinery whose entire job is to contain a mode Chronicle never needs.

Consequence to accept: there is no way to run Chronicle without minting a
credential first. That is the intended shape.

### 2 · `users` carries a `kind`, because the Scribe is a participant.

This is the one place Chronicle genuinely needs more than Lyceum, and it comes
from a locked IDEA-21 decision: *discussions are real threads and the Scribe is a
second participant*. A thread whose author column reads "owner" for both sides is
a thread you cannot read.

So accounts are not a household feature here — they are what makes authorship
expressible.

```sql
kind TEXT NOT NULL DEFAULT 'person' CHECK (kind IN ('person', 'agent'))
```

An `agent` account can hold sessions and author discussion turns, and is
structurally refused at `/admin/users*` — the owner check becomes
`is_owner AND kind = 'person'`. Adding the column now costs one line; retrofitting
authorship semantics onto a single-identity table later costs a data migration
over authored, irreplaceable rows.

### 3 · Everything lands in `tier2`, credentials included.

`users`, `user_tokens` — authored, irreplaceable, not derivable from anything.
They go in the `tier2` schema and the `chronicle_tier1` role cannot see them,
which CHRN-14's grants already enforce.

This gives CHRN-52 a far better assertion than an abstract note table: *the
tier-1 role cannot read the credentials table.* If that test ever goes red the
failure is unambiguous.

Tier-1 tables that need an author store the UUID without a foreign key.
A cross-schema FK from tier1 to tier2 would be a write path from the derived side
into the authored side, which is the thing the split exists to prevent.

### 4 · UUID primary keys.

House style (`google/uuid`, per CLAUDE.md), against Lyceum's `BIGINT GENERATED
ALWAYS AS IDENTITY`. Also the right call on its own terms here: a sequential user
id in `/admin/users/{id}` enumerates the account list, and Chronicle's admin
surface is reachable from the WAN entrypoint in a way Lyceum's is not.

### 5 · The Cloudflare Access JWT is verified in-process. The header is never trusted.

Traefik's `cf-access-jwt` middleware (SERV-106) already verifies it on the
`internal` entrypoint, and `strip-cf-access` removes it on `public`. Chronicle
verifies anyway — RS256 pinned, issuer, audience and expiry all checked against
the team domain's JWKS.

The reason is not distrust of Traefik, it is that trusting the header makes
correctness depend on a middleware *staying attached to a router in a separate
file*. Chronicle publishes a direct router on the WAN-forwarded entrypoint; the
day that router is edited and the strip middleware is dropped, a forged
`Cf-Access-Jwt-Assertion` header would authenticate as anybody. Verifying costs
one lazily-fetched, cached JWKS and closes that off permanently.

**[rev] Ported from `construct-server/services/cf-access-guard`, not from
Lyceum.** Lyceum's copy is the older one and has regressed against it: it lacks
the `len(e) > 8` exponent bound (a JWKS key with a longer exponent panics the
process — LYCM-122), the 2048-bit modulus floor, the `use != "sig"` filter and
the bounded body read. It also gates its refresh cooldown on `v.keys != nil`,
which makes the cooldown inert in the only two situations it exists for: a cold
start and a certs endpoint that is failing. construct-server stamps the attempt
unconditionally, before the fetch; that is what is copied.

Still hand-rolled against stdlib `crypto/rsa` — no new dependency, algorithm
pinned so a token cannot downgrade to `none` and an RSA public key cannot be
replayed as an HMAC secret.

**[rev]** The audience is a **list**. Access AUD tags are per-application, so
CHRN-65's MCP endpoint behind its own Access application will present a
different tag. Switchyard reached the same shape at SWY-260.

### 6 · No pairing code in this ticket.

Lyceum's short typeable code (LYCM-88) is a second credential carrier with
materially less entropy, and it drags along its own table, its own 15-minute TTL,
its own rate limiter and its own brute-force argument.

Chronicle's onboarding is one person scanning a QR off a terminal they are
already sitting at. Defer it. The schema does not foreclose it: a pairing code is
a row pointing at a `user_tokens` invite, addable as a later migration with no
change to anything decided here. Revisit only if onboarding actually fails for
want of it, rather than shipping the mitigation for a problem we have not had.

### 7 · Both sign-in endpoints are rate-limited in-process, per real client.

Lyceum deliberately does *not* limit its token path — a 256-bit secret needs no
help, and that reasoning is sound. Chronicle adds one anyway, and the reason is
lateral rather than external: the container is reachable on the shared Docker
network by every other service on the box, without passing Traefik at all. The
edge limiter CHRN-16 attaches (`chronicle-login-ratelimit`) does nothing for that
path.

**[rev]** The first cut keyed on `RemoteAddr` and never `X-Forwarded-For`,
reasoning that the header is caller-controlled. That is right about the header
and wrong about the outcome: every request through Traefik carries Traefik's
address, so one bucket covered all legitimate traffic and a stranger hammering
the direct host could lock the owner out of their own service at twenty requests
a minute. A limiter a stranger can turn into a denial of service is worse than
none.

The header is now trusted exactly when its source is — only when the immediate
peer is in `CHRONICLE_TRUSTED_PROXIES`. That is sound because Traefik's
entrypoints set `forwardedHeaders.trustedIPs: []`, which makes Traefik overwrite
whatever the caller sent with the address it actually observed. A neighbour
container connecting directly is not a trusted peer, so its header is ignored
and it is keyed on its real address — the lateral case this limiter exists for.
Unset, the coarse behaviour returns and boot warns about it rather than leaving
it to be discovered.

**[rev]** It covers `POST /auth/sso/cloudflare` too. Limiting only one of the two
unauthenticated credential-minting endpoints just moves the target: the SSO path
drives a JWKS fetch and a database write per call.

## Surface

| route | auth | notes |
|---|---|---|
| `GET /healthz`, `GET /readyz` | open | already shipped in CHRN-15. `/healthz` must stay dependency-free — CHRN-59's QR flow probes it before committing a server address |
| `POST /auth/session` | open | `{token, device_label}` → `{user, session_token}` + cookie. Rate-limited |
| `POST /auth/sso/cloudflare` | open | verifies the Access JWT, then **reuses the session the browser already holds** for that account, minting only when there is none. Rate-limited. Browser only |
| `GET /auth/me`, `PATCH /auth/me` | session | |
| `DELETE /auth/session` | session | this device only |
| `GET /auth/sessions`, `DELETE /auth/sessions/{id}` | session | own devices only; another user's id reports 404, not 403 |
| `POST /auth/invite` | session | add my own next device. Not an admin route: it confers nothing the calling session does not already hold. Retires this account's previous unredeemed device invite so a double-tap does not leave two live keys nothing can revoke |
| `POST\|GET /admin/users`, `POST /admin/users/{id}/invite`, `DELETE /admin/users/{id}` | owner | invite returned exactly once |
| everything else | session | there is no unauthenticated read surface |

## Configuration

| var | required | |
|---|---|---|
| `CHRONICLE_OWNER_EMAIL` | yes | boot fails if unset. Left at a placeholder it can never match an Access email, so SSO would silently never work — a named boot failure beats a mode that looks configured and is not |
| `CHRONICLE_OWNER_NAME` | no | defaults to the email |
| `CHRONICLE_CF_ACCESS_TEAM_DOMAIN` / `_AUD` | pair | both set → SSO on. Neither → `/auth/sso/cloudflare` returns `sso_disabled`. Exactly one → boot error. **[rev]** `_AUD` is a comma-separated list |
| `CHRONICLE_COOKIE_SECURE` | no | **[rev]** default true. Not derived from the request — see the cookie note above |
| `CHRONICLE_TRUSTED_PROXIES` | no | **[rev]** IPs/CIDRs whose `X-Forwarded-For` the sign-in limiter believes. Unset, the limit is global and boot says so |
| `CHRONICLE_MOBILE_BASE_URL` | no | origin baked into the invite QR. Validated at boot as an absolute http(s) URL: LYCM-102 is the precedent — a malformed base both produces a QR that scans to nothing *and* suppresses the client-side fallback that would have worked, so it must be a boot error, never a shrug |

No `CHRONICLE_AUTH`.

## Bootstrap

Migration seeds one owner row with placeholder identity; boot reconciles it from
env. If no session exists for the owner and no invite is outstanding, boot mints
one and logs it, plus a `chronicle mint-invite` subcommand as the durable path.

The invite goes into a structured JSON log on stdout, which is a channel Dozzle
reads and Datadog could. It is single-use, expires in seven days, and is emitted
only when nobody can currently sign in — but it is a live credential in a log
line, so: emitted at `warn` with a distinct message, and never re-emitted while
an unredeemed one stands. The alternative, CLI-only, was rejected because the
first human to bring up a container has no reason to know the CLI exists.

## What this does not decide

**Whether Chronicle's MCP endpoint also sits behind Cloudflare Access.** CHRN-65
defers to SERV-98 and puts it behind Access; CHRN-16 argues for a session token
on the direct host. This decision deliberately makes that a non-blocker: MCP
authenticates with an ordinary session token minted against an `agent` account,
and the two compose — Access at the edge decides whether the request is served,
the session decides who it is served as. Either edge placement works against the
same credential, so CHRN-65 can settle it later without reopening anything here.

`GET /sign-in` — the page the QR's `<server>/sign-in?token=…` lands on — is
CHRN-59's and the web ticket's. This decision only fixes the URL shape so both
can rely on it.

## Done when

Straight from the ticket, one test each:

1. an invite redeems into a per-device session;
2. a second device needs a second invite (single-use is enforced under a race);
3. signing out one device leaves the others working;
4. an unauthenticated request to a non-health route gets nothing;
5. a spent invite is byte-identical in response to an unknown one;
6. and — the tier assertion, handed to CHRN-52 — the `chronicle_tier1` role
   cannot read `tier2.user_tokens`.

**[rev]** Assertion 6 originally scanned every probe into an `int`, including
`SELECT token_hash`, which is a TEXT column — so that probe errored whatever the
privileges were and could not fail. It now scans into `any` and additionally
requires the error to be a *permission* denial, so "the migration never ran"
cannot pass as "the role is locked out".

## [rev] Also settled by the review

- **A non-expiring session is only safe because of the device list**, so
  anything that floods that list disables the safety while leaving every
  credential valid. That is why SSO reuses rather than mints.
- **Every JSON endpoint requires `Content-Type: application/json`.** A
  cross-site form can post `text/plain` that `json.Decode` accepts, and
  `/auth/session` answers with `Set-Cookie` — login CSRF, which `SameSite=Lax`
  does not address because Lax governs which cookies are *sent*, not which are
  accepted from a response.
- **Bodies are capped and every field is length-checked**, because a TEXT column
  has no length of its own and the sign-in route is unauthenticated.
- **Retire-and-mint for a device invite is one transaction.** As two statements
  the concurrent double-tap it defends against can interleave and leave two live
  keys — defeated by exactly the case it was written for.
- **Invite labels are their own namespace**, never a token *kind* value.
- **`ReconcileOwner` replaces the migration placeholder but never a chosen
  name.** Falling back to the stored value leaves "Owner" forever; falling back
  to the email unconditionally undoes `PATCH /auth/me` on every restart.
