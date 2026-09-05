# Deploying Chronicle

Chronicle runs as a container on `construct_net`, behind the estate's existing
Cloudflare tunnel → Traefik arrangement, and **also** on the WAN-forwarded
`public` entrypoint for clients that cannot do browser SSO. Nothing here is a
new pattern: the tunneled half is Switchyard's shape, the direct half is
Lyceum's (SERV-60), and the database is provisioned the way Purser's is.

## The deploy configuration is not in this repo

`construct-server` is the source of truth. It declares every service inline in `docker-compose.yml` and every router in `config/traefik/dynamic/routers.yml`; nothing is assembled from service-repo fragments at deploy time.

**One in-repo copy remains, and it is not this directory's:** `asr/deploy/compose.asr.yml`, in the sealed ASR subtree. It carries the same rationale these files did, and it has drifted the dangerous way — its healthcheck is still `["CMD", "/usr/local/bin/asrd", "version"]` under a comment arguing for `/healthz`, while the deployed block runs an actual `/healthz` GET asserting a 200. construct-server's own comment on that block explains the change: `asrd version` *"prints a string and exits 0 from a bare container with no GPU, no database, no config and no server — verified — so it reports healthy for a wedged process for ever."* Its models-volume default is `~/tools/...` where the deployment pins an absolute path, because prod compose runs from `/opt/construct-server` and `~` expands to the wrong home. **Do not deploy ASR from that file.** CHRN-90 removes it; `asr/` is a sealed subtree with its own release, so it is a ticket of its own rather than a reach from this one.

This directory used to carry `compose.chronicle.yml` and `traefik-chronicle.yml` — copies of both, kept so Chronicle's deploy shape was reviewable in Chronicle's own repo. The intent was right and the mechanism was not. **Nothing checked that the copies agreed with the deployment, and by the time they were removed (CHRN-89) both had diverged, in opposite directions:**

- the **compose** copy was *ahead* — it declared `CHRONICLE_ASR_MODEL` and `CHRONICLE_TRANSCRIBE_INTERVAL`, which are not deployed;
- the **Traefik** copy was *behind*, and in the direction that matters — it had no `chronicle-proxy-secret`, a middleware live on **every** router reaching this service (CHRN-75 / SERV-148). A reader following it would have built a routing config missing a security middleware, while the compose copy beside it happily carried the `CHRONICLE_PROXY_SECRET` that middleware supplies.

Two documents making one claim, with the second going stale, is the CHRN-79 shape this project already named. So this file aims at **decisions — what cannot be derived from the configuration — rather than the configuration itself.** One table below is the exception and is worth naming rather than glossing: the deployed-routes table lists per-router middlewares, which *is* configuration, and it is the thing this deletion had to correct because it predated `chronicle-proxy-secret`. It earns its place by making the routing split legible in one view, and it is the one thing here that can still go stale — check it against `config/traefik/dynamic/routers.yml` when you change a router.

## Files

| file | what it is |
|---|---|
| `Dockerfile` | static Go binary on Alpine. `docker build -f deploy/Dockerfile -t chronicle:local .` |
| `provision-db.sh` | database, roles and the tier lockdown. Run once, as superuser, under `signet exec` |
| `../asr/` | the shared estate ASR service — the pinned whisper.cpp image, the job contract, and `asrd`. Its own subtree, database, role and release; see `asr/README.md` |

## Order

1. **Database** — `signet exec --secret construct-server/CHRONICLE_DB_PASSWORD --secret construct-server/CHRONICLE_TIER1_DB_PASSWORD -- deploy/provision-db.sh`
2. **Secret on disk** — `construct-server`'s `docker-compose.yml` reads `${CHRONICLE_DB_PASSWORD}`, so Signet needs file targets:
   ```
   signet target add-key --project construct-server --path /home/magos/construct-server/.env --name CHRONICLE_DB_PASSWORD
   signet target add-key --project construct-server --path /opt/construct-server/.env      --name CHRONICLE_DB_PASSWORD
   signet sync
   ```
3. **Image** — published to `ghcr.io/einlanzerous/chronicle` by
   `.github/workflows/publish.yml` (**CHRN-73**). Until that landed nothing in
   this repo had ever built an image, and this line claimed CHRN-17 published
   one; it does not, and that claim is why a previous pass reached a deploy with
   no artifact to deploy. `:latest` follows `main`; a release-please `v*` tag
   publishes the semver tags.

   **Check the package is pullable from this host before step 6.** A GHCR
   package created by a workflow's `GITHUB_TOKEN` is **private** by default, and
   `docker compose up` will fail on `pull access denied` rather than on anything
   informative. The estate does it both ways — `lyceum` and `argosy` answer an
   anonymous manifest request, `switchyard` and `signet` do not — so either
   making the package public or confirming the host's `ghcr.io` login covers it
   is fine. What is not fine is finding out at `up` time. `docker pull
   ghcr.io/einlanzerous/chronicle:latest` on the host settles it in one command.
4. **Cloudflare** — do this **before** compose, not after. See below: the Access
   application has to exist before the AUD in the compose block means anything,
   and `check-edge-auth.sh` fails the config if a gated router has no matching
   `CF_ACCESS_AUD_MAP` entry.
5. **Audio and inbox directories** — `sudo mkdir -p /data/chronicle/audio /data/chronicle/inbox`.

   `CHRONICLE_AUDIO_DIR` points here and the service **refuses to boot if the
   path is not readable** rather than creating it (CHRN-23). That is deliberate:
   a directory that springs into existence on a typo is how tier-2 audio ends up
   on the container's writable layer instead of the NVMe, which looks like it
   works until the next redeploy takes the corpus with it. `/data` is the NVMe —
   458 G with 256 G free — and the same volume Copyparty serves at `/w/hdd`,
   which is why CHRN-19's watched folder will land under the same root.

   `GET /admin/storage` (owner only, Access-gated host) reports what the corpus
   costs and whether the disk and the database agree.

   **The app's upload path (CHRN-20) needs nothing beyond this directory.**
   `POST /memos/uploads` is on as soon as `CHRONICLE_AUDIO_DIR` is set; unset,
   the four `/memos/uploads` routes answer **503 naming the variable** and the
   boot log says so. Uploads in flight are assembled in a reserved
   subdirectory, `/data/chronicle/audio/.uploads`, which the service creates
   itself — it is a fixed name inside a path you already supplied, not a path
   anyone can typo. It is inside the audio root on purpose: finalising an upload
   is an `os.Rename`, which is atomic only within one filesystem, and a staging
   area on another mount would silently become a copy.

   That directory is **not corpus**. `GET /admin/storage` counts it separately
   as `disk.staging` / `disk.staging_bytes`, and an hourly sweep removes
   sessions idle for seven days along with their bytes. Expiry is measured from
   last activity, so a slow upload that is still progressing is never collected.

   Sizing, so `.uploads` is not a surprise on the disk graph: an account may
   hold **32 open sessions**, each declaring at most **1 GiB**. Both bounds
   exist to contain a mistake rather than to be met — an hour of voice Opus is
   around 14 MB — but the arithmetic worth knowing is that the ceiling is 32 GiB
   per account against 256 G free.

   **`CHRONICLE_INBOX_DIR` is the Copyparty seam (CHRN-19), and it needs two
   more things before a phone can use it:**

   - **One subdirectory per account, named after the account's UUID.**
     `sudo mkdir -p /data/chronicle/inbox/$(uuid)` — get the UUID from
     `GET /admin/users`. A file carries no identity of its own and
     `tier2.memos.author_id` is `NOT NULL`, so the directory is the only thing
     that can supply one. **The watcher never creates a directory**: one that
     does not name an existing account is logged once and ignored, so dropping
     files into an invented path ingests nothing.
   - **A Copyparty volume, which does not exist yet.** `config/copyparty.conf`
     in construct-server publishes exactly one volume — `[/] → /w/hdd/media` —
     so `/data/chronicle/inbox` is on the same disk but is **not reachable from
     a phone**. Adding it is a construct-server change, and it carries a
     question worth answering deliberately rather than by default: that config
     is `accs: rwmd: *`, anonymous, gated only by Tailscale at the network
     layer. Published the same way, **anyone on the tailnet could drop a file
     into any account's inbox and have the memo attributed to them.** For
     `/data/media` that is the estate's accepted posture; for authored tier-2
     content it is a different question. Chronicle's own upload endpoint
     (CHRN-20) is the authenticated path.

   Until that volume exists the watcher runs correctly against an inbox nothing
   fills, and `CHRONICLE_INBOX_DIR` can be left set: it costs one `readdir` per
   interval.

   **Latency, as a number rather than a footnote.** With the shipped defaults —
   `CHRONICLE_WATCH_SETTLE=10s`, `CHRONICLE_WATCH_INTERVAL=5s` — a file's worst
   case from written to memo row is **about fifteen seconds**. The settle window
   is the larger half and it is buying something: it is the guard that keeps a
   half-written upload from being read at all. Lower it to trade latency back;
   the guarantee against a partial read is the re-stat after the copy, not this.

   Both directories are checked at boot and **not created** — a typo'd path
   springing into existence is how tier-2 audio lands on the container's
   writable layer and vanishes at the next redeploy. Setting
   `CHRONICLE_INBOX_DIR` without `CHRONICLE_AUDIO_DIR` is refused outright: a
   watcher with nowhere to copy recordings to would record memos whose audio is
   immediately `missing`.
6. **Compose** — the `chronicle` service block lives in `construct-server`'s `docker-compose.yml`. Set `CHRONICLE_OWNER_EMAIL` in `.env`, then `docker compose up -d chronicle`. The service refuses to start without it: auth is unconditional (CHRN-71) and the owner is who the first invite belongs to.

   Four decisions in that block are not derivable from reading it, so they are recorded here:

   - **No `ports:`, deliberately.** Chronicle is reached through Traefik on the internal entrypoint, never from the host. Publishing a port would put an unauthenticated listener on the host and bypass the edge entirely.
   - **The Access team domain, AUD and mobile base URL are literals, not `.env` indirections.** They are identifiers and hostnames, not secrets — and `check-edge-auth.sh` reads the AUD *straight out of the compose file* to assert it agrees with the guard's `CF_ACCESS_AUD_MAP` entry for this host. Behind a `${...}` the check has nothing to read. Two copies of one identity is not ideal; they are cross-checked rather than trusted, which is what SERV-106 exists to do.
   - **The team domain and AUD move together.** `config.Load` errors when exactly one of the pair is set and `runServe` returns that error, so a literal AUD beside an empty `${...}` team domain crash-loops the container rather than serving unverified.
   - **`CHRONICLE_OWNER_EMAIL` unset is not a soft failure.** The owner keeps migration 0002's placeholder, which can never match a Cloudflare-verified email, so browser sign-in would look configured and silently never work.
7. **First sign-in** — the first boot logs a single-use invite at `warn`:
   `docker compose logs chronicle | grep first-boot`. It expires in seven days
   and is never shown again; `docker compose exec chronicle chronicle
   mint-invite` issues another.
8. **Traefik** — the routers, middlewares and service live in `construct-server`'s `config/traefik/dynamic/routers.yml`. Dynamic config; no restart needed. Then add `chronicle.zerogravity.industries` to the guard's `CF_ACCESS_AUD_MAP`, and run `./scripts/check-edge-auth.sh` in `construct-server`.

   Two shapes there that a reader would otherwise have to reverse-engineer:

   - **The login rate limit is a separate higher-priority router, not another middleware on the main one.** It has to reach `/auth/*` and *must not* reach upload traffic: CHRN-20 is resumable upload of memos recorded at arbitrary length, and a 5 req/s bucket across a chunked 40-minute upload throttles ingest rather than attackers. Same construction as `argosy-auth` and `lyceum-public-auth`.
   - **The edge limiter and Chronicle's in-process limiter are for different threats, not redundant.** The container is reachable on `construct_net` by every other service on the box *without passing Traefik*, so the edge limiter does nothing for that path; the in-process one does nothing about volume from the WAN. The edge limit is per client IP, which is meaningful because the `public` entrypoint sets `forwardedHeaders.trustedIPs: []` and so trusts no client-supplied `X-Forwarded-*`.

## Transcription (CHRN-27)

Chronicle talks to the ASR service over `construct_net`; that service publishes
no port, so this is the only route to it. Both variables or neither —
`config.Load` errors on exactly one, so a URL beside an empty token crash-loops
rather than failing every submission with a 401 that reads like the token was
wrong.

1. **The ASR service first** — `asr/README.md`. It has its own database
   and its own role, provisioned by `asr/deploy/provision-db.sh`.
2. **One token, two places.** `ASR_CLIENT_TOKENS` on the asr service carries
   `chronicle:<token>`; `CHRONICLE_ASR_TOKEN` here carries the same value. They
   are the same credential, and `client_id` is derived from it on the far side —
   never sent as a field.
   ```
   signet target add-key --project construct-server --path /opt/construct-server/.env --name CHRONICLE_ASR_TOKEN
   signet target add-key --project construct-server --path /opt/construct-server/.env --name ASR_CLIENT_TOKENS
   signet sync
   ```
3. **Check it took.** `GET /admin/transcription` reports `enabled`, how many
   memos are pending, and which are held and why. Left unconfigured, Chronicle
   boots, warns, and ingests memos nobody ever transcribes — which looks like a
   working system until somebody goes looking for a transcript.

`chronicle retranscribe --memo <id>` releases a held memo back to the queue;
with no `--memo` it releases every held memo. It is a host command rather than
an endpoint because re-running transcription costs GPU time on a device three
services share.

## The Cloudflare half is dashboard-managed

The tunnel's ingress rules are remotely managed and deliberately absent from
`construct-server`, as its compose file says. **Two hostnames, and they are set
up differently** — this is the part no repo records, so it lives here.

### `chronicle.zerogravity.industries` — tunneled, Access-gated

- add `chronicle.zerogravity.industries` → `http://traefik:9080` to the tunnel
- create the Cloudflare Access application for that hostname; its **AUD tag**
  goes in two places that are cross-checked against each other — the service's
  `CHRONICLE_CF_ACCESS_AUD` and the guard's `CF_ACCESS_AUD_MAP`
- DNS: **CNAME to the tunnel, proxied** (orange cloud) — the way `lyceum` and
  `switchyard` resolve, to Cloudflare anycast

### `chronicle-direct.zerogravity.industries` — WAN, no Access

- **no tunnel ingress, and no Access application.** An Access app on this host
  would defeat the reason it exists and would make the edge and the origin
  disagree about whether the host is open
- DNS: **A record to the WAN address, DNS-only (grey cloud)** — the way `argosy`
  and `lyceum-direct` resolve, both to the same single WAN IP rather than to
  Cloudflare's

Without these the Traefik routers exist but nothing routes to them.

## What is deployed, and what is deliberately not

**Both halves are deployed**, serving the same backend:

| host | entrypoint | middlewares | for |
|---|---|---|---|
| `chronicle.…` | `internal` | `cf-access-jwt`, `chronicle-proxy-secret` | browser, via Access → `POST /auth/sso/cloudflare` |
| `chronicle-direct.…` | `public` | `crowdsec-bouncer`, `strip-cf-access`, `chronicle-proxy-secret` | the app and MCP, via invite → `POST /auth/session` |
| ↳ `PathPrefix(/auth/)` | `public` | + `chronicle-login-ratelimit` | the credential endpoints specifically |
| ↳ `PathPrefix(/admin)` | `public` | `deny-all` → blackhole | **403. `/admin` is Access-only** |

**`chronicle-proxy-secret` is on every router that reaches this service** (CHRN-75 / SERV-148) — it is how Chronicle knows a request came through this edge. Missing it is not a breakage but a silent coarsening: the app falls back to keying every WAN sign-in on Traefik's own address, and says so in its log rather than failing. Two properties it rests on, both easy to undo by accident:

- `customRequestHeaders` **overwrites** rather than appends, so a client's own `X-Chronicle-Proxy-Secret` is replaced and never reaches the app. Chronicle compares in constant time and never presence-tests.
- The value must be written `{{ env "CHRONICLE_PROXY_SECRET" }}`, **not** `${CHRONICLE_PROXY_SECRET}`. The file provider does not expand `${...}` — it would stamp the literal string, match nothing, and error nowhere. `crowdsecLapiKey` is the existence proof that the template form works.

The direct router was written and left commented out until Chronicle had its own
credential surface. **CHRN-71 landed that**, so the standard `routers.yml` sets
for the estate's Access exemptions — the endpoint must authenticate *"with
something Cloudflare Access cannot express"* — is met: a one-time invite
redeemed into a durable per-device session, verified in-process on every route
outside `/healthz` and `/readyz`.

**`/admin` is not served on the direct host.** `POST /admin/users/{id}/invite`
mints a live credential, and CHRN-16 asks for `/admin` to require Access. A
`Host`-only rule would have served it on the open 443 behind `requireOwner`
alone and outside the `/auth/` rate limit, so a higher-priority router 403s it
there. Lyceum serves its whole surface on both hosts; Chronicle's ticket states
the property explicitly, which is the difference.

Cloudflare Access and Chronicle's own auth are **complementary, not
alternatives** — in Lyceum's words, *"this one decides whether the request is
served at all, Lyceum's decides who it is served as."* The same account is
reachable through Access on the tunneled host and through a redeemed invite on
the direct one.
