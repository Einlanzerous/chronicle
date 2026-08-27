# Deploying Chronicle

Chronicle runs as a container on `construct_net`, behind the estate's existing
Cloudflare tunnel → Traefik arrangement, and **also** on the WAN-forwarded
`public` entrypoint for clients that cannot do browser SSO. Nothing here is a
new pattern: the tunneled half is Switchyard's shape, the direct half is
Lyceum's (SERV-60), and the database is provisioned the way Purser's is.

## Files

| file | what it is |
|---|---|
| `Dockerfile` | static Go binary on Alpine. `docker build -f deploy/Dockerfile -t chronicle:local .` |
| `provision-db.sh` | database, roles and the tier lockdown. Run once, as superuser, under `signet exec` |
| `compose.chronicle.yml` | the service block to paste into `~/construct-server/docker-compose.yml` |
| `traefik-chronicle.yml` | the routers and middleware to paste into `config/traefik/dynamic/routers.yml` |

## Order

1. **Database** — `signet exec --secret construct-server/CHRONICLE_DB_PASSWORD --secret construct-server/CHRONICLE_TIER1_DB_PASSWORD -- deploy/provision-db.sh`
2. **Secret on disk** — the compose file reads `${CHRONICLE_DB_PASSWORD}`, so Signet needs file targets:
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
   application has to exist before the AUD in `compose.chronicle.yml` means
   anything, and `check-edge-auth.sh` fails the config if a gated router has no
   matching `CF_ACCESS_AUD_MAP` entry.
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
6. **Compose** — paste `compose.chronicle.yml`, set `CHRONICLE_OWNER_EMAIL` in
   `.env`, `docker compose up -d chronicle`. The service refuses to start
   without it: auth is unconditional (CHRN-71) and the owner is who the first
   invite belongs to.

   The Access team domain, AUD and mobile base URL are **literals in the compose
   block**, not `.env` entries — they are identifiers and hostnames, not
   secrets, `check-edge-auth.sh` reads the AUD straight out of the file, and the
   team domain and AUD must be set together or `config.Load` refuses to serve.
7. **First sign-in** — the first boot logs a single-use invite at `warn`:
   `docker compose logs chronicle | grep first-boot`. It expires in seven days
   and is never shown again; `docker compose exec chronicle chronicle
   mint-invite` issues another.
8. **Traefik** — paste `traefik-chronicle.yml` (three routers, one service, one
   middleware). Dynamic config; no restart needed. Then add
   `chronicle.zerogravity.industries` to the guard's `CF_ACCESS_AUD_MAP`, and
   run `./scripts/check-edge-auth.sh` in `construct-server`.

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
| `chronicle.…` | `internal` | `cf-access-jwt` | browser, via Access → `POST /auth/sso/cloudflare` |
| `chronicle-direct.…` | `public` | `crowdsec-bouncer`, `strip-cf-access` | the app and MCP, via invite → `POST /auth/session` |
| ↳ `PathPrefix(/auth/)` | `public` | + `chronicle-login-ratelimit` | the credential endpoints specifically |
| ↳ `PathPrefix(/admin)` | `public` | `deny-all` → blackhole | **403. `/admin` is Access-only** |

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
