#!/usr/bin/env bash
# The ASR service: database, role, and the lockdown. Run once, as a Postgres
# superuser, against the shared Postgres 16 on construct_net.
#
# ITS OWN DATABASE AND ITS OWN ROLE, not a schema inside Chronicle's — CHRN-25
# §1. The convenient alternative is a job table next to tier1.memo_uploads, and
# it stays convenient for exactly as long as Chronicle is the only client: the
# moment Catenary submits a job it needs a credential on CHRONICLE's database,
# and the whole reason E3 is an estate service rather than a Chronicle package
# collapses into Catenary depending on Chronicle's schema.
#
# It is NOT a tier question. The tier split governs what lives in Chronicle's
# two stores; these rows are another service's own state, in another service's
# database, and the tier rule does not reach across that boundary in either
# direction.
#
# Passwords are never written here or into a compose file — they live in Signet
# and are injected for the length of this command:
#
#   signet exec --secret construct-server/ASR_DB_PASSWORD -- asr/deploy/provision-db.sh
#
# Idempotent: re-running resets the role password to the current Signet value
# and re-asserts the grants, which is also how a rotation is applied.
set -euo pipefail

: "${ASR_DB_PASSWORD:?not set — run under: signet exec --secret construct-server/ASR_DB_PASSWORD ...}"

PG_CONTAINER="${PG_CONTAINER:-postgres}"
DB="${ASR_DB_NAME:-asr}"

psql_super() {
  docker exec -i \
    -e APP_PW="$ASR_DB_PASSWORD" \
    -e DB="$DB" \
    "$PG_CONTAINER" psql -U postgres -v ON_ERROR_STOP=1 -q "$@"
}

# --- role and the database itself (cluster scope) -------------------------
psql_super -d postgres <<'SQL'
\getenv app_pw APP_PW
\getenv db     DB

-- asr: the service role. Owns everything inside its own database and holds
-- nothing anywhere else. In particular it is NOT granted CONNECT on
-- chronicle: the ASR service has no business in Chronicle's store, and the
-- credential is where that is enforced rather than merely intended.
SELECT format('CREATE ROLE asr LOGIN PASSWORD %L', :'app_pw')
 WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'asr');
\gexec
ALTER ROLE asr LOGIN PASSWORD :'app_pw';

SELECT format('CREATE DATABASE %I OWNER asr', :'db')
 WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = :'db');
\gexec
SQL

# --- lockdown inside the ASR database -------------------------------------
psql_super -d "$DB" <<'SQL'
\getenv db DB

-- Postgres grants CONNECT to PUBLIC by default; that is revoked, so nobody
-- reaches this database who was not named. There is exactly one name.
REVOKE ALL ON DATABASE :"db" FROM PUBLIC;
GRANT CONNECT ON DATABASE :"db" TO asr;

ALTER SCHEMA public OWNER TO asr;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
GRANT ALL ON SCHEMA public TO asr;
SQL

# Said out loud, because it is the one thing about this service that is easy to
# get wrong later and hard to notice: nothing in this database is
# irreplaceable. Every row and every byte can be recomputed from audio a client
# still holds, which is what makes the whole store safe to drop — and what a
# reviewer should check on any change here is that nothing in it has quietly
# become the only copy of anything.
echo "provisioned: database ${DB}, role asr"
echo "note: this store is DISPOSABLE by construction — dropping it costs queue position and nothing else."
