# Deploying Chronicle

Chronicle runs as a container on `construct_net`, behind the estate's existing
Cloudflare tunnel → Traefik arrangement. Nothing here is a new pattern; it is
Switchyard's shape for a tunneled app, plus a database provisioned the way
Purser's is.

## Files

| file | what it is |
|---|---|
| `Dockerfile` | static Go binary on Alpine. `docker build -f deploy/Dockerfile -t chronicle:local .` |
| `provision-db.sh` | database, roles and the tier lockdown. Run once, as superuser, under `signet exec` |
| `compose.chronicle.yml` | the service block to paste into `~/construct-server/docker-compose.yml` |
| `traefik-chronicle.yml` | the routers to paste into `config/traefik/dynamic/routers.yml` |

## Order

1. **Database** — `signet exec --secret construct-server/CHRONICLE_DB_PASSWORD --secret construct-server/CHRONICLE_TIER1_DB_PASSWORD -- deploy/provision-db.sh`
2. **Secret on disk** — the compose file reads `${CHRONICLE_DB_PASSWORD}`, so Signet needs file targets:
   ```
   signet target add-key --project construct-server --path /home/magos/construct-server/.env --name CHRONICLE_DB_PASSWORD
   signet target add-key --project construct-server --path /opt/construct-server/.env      --name CHRONICLE_DB_PASSWORD
   signet sync
   ```
3. **Image** — published by CI as `ghcr.io/einlanzerous/chronicle` (CHRN-17).
4. **Compose** — paste `compose.chronicle.yml`, `docker compose up -d chronicle`.
5. **Traefik** — paste `traefik-chronicle.yml`. Dynamic config; no restart needed.
6. **Cloudflare** — see below. This part is **not** in any repo.

## The Cloudflare half is dashboard-managed

The tunnel's ingress rules are remotely managed and deliberately absent from
`construct-server`, as its compose file says. So steps that cannot be scripted
from this repo:

- add `chronicle.zerogravity.industries` → `http://traefik:9080` to the tunnel
- create the Cloudflare Access application for that hostname
- the DNS record (CNAME to the tunnel)

Without them the Traefik router exists but nothing routes to it.

## What is deployed, and what is deliberately not

**Deployed shape:** one router on the `internal` entrypoint carrying
`cf-access-jwt`, exactly like Switchyard. Everything Chronicle serves — `/admin`
included — sits behind Cloudflare Access.

**Not deployed:** the direct app / MCP path. CHRN-16 asks for the Android app
and MCP surfaces to skip browser SSO. On this estate that means a router on the
`public` (WAN-forwarded) entrypoint, in the shape of Lyceum's SERV-60 direct
path, because every router on `internal` must carry `cf-access-jwt` (SERV-106)
and `public` is the only other entrypoint.

That router is written out in `traefik-chronicle.yml` and commented out, because
**Chronicle has no authentication of any kind yet.**

The estate permits exactly one Access exemption today — Switchyard's GitHub
webhook — and holds it to an explicit standard: the endpoint authenticates
"with something Cloudflare Access cannot express", namely an HMAC verified one
layer down, and the router is pinned to an exact `Path()` rather than a prefix.
Chronicle cannot meet that standard until it has its own credential surface.
Enabling the direct router before then would publish an unauthenticated service
to the WAN.

**So the order in the plan needs a decision**: either the app path waits for
Chronicle's own auth, or it ships behind Access for now and loses the
"no browser SSO for the app" property until auth lands.
