#!/usr/bin/env bash
# Regenerate schema.sql from the migrations.
#
# schema.sql is a GENERATED artefact: the schema you get by applying every
# migration in order to an empty database. CI regenerates it and fails if the
# result differs from what is committed, which is what stops the two drifting.
# Catenary's R4 spent a whole gate learning that a generated artefact with no
# guard is one somebody hand-edits.
#
#   scripts/gen-schema.sh
#
# With no arguments it brings its OWN Postgres: a throwaway container on a
# port Docker picks, removed on exit. Nothing you already have is reachable
# from it, because the server did not exist until this script started.
#
# CI points it at the service container it already runs, which is the only
# case that should be supplying a DSN:
#
#   SCHEMAGEN_SUPERUSER_DSN=postgres://postgres:pw@localhost:5432/postgres?sslmode=disable \
#     scripts/gen-schema.sh
#
# A supplied server is INSPECTED BEFORE ANYTHING IS DROPPED, and the script
# refuses to touch one holding a database it did not create. It used to open
# with `DROP DATABASE` against whatever it was given, and the address in this
# docstring was `127.0.0.1:5432` — which on construct-server is the estate's
# shared Postgres, 21 databases including this service's own. (CHRN-77)
#
# There is deliberately no override for that guard. An escape hatch is the
# thing that ends up pasted into the next invocation, and there is nothing to
# escape to: drop SCHEMAGEN_SUPERUSER_DSN and you get a server of your own.
#
# psql and pg_dump run from a PINNED image rather than whatever the host has,
# because pg_dump output varies between versions and the whole point is a
# byte-comparable file.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

PG_IMAGE="${PG_IMAGE:-postgres:16.15-alpine}"
SCRATCH="${SCHEMAGEN_DB:-chronicle_schemagen}"
OUT="${SCHEMAGEN_OUT:-schema.sql}"

die() { printf '\ngen-schema.sh: %s\n\n' "$1" >&2; exit 1; }

# The scratch name is the last thing standing between the DROP below and a
# real database, so it has to look like a scratch. `chronicle` is one
# SCHEMAGEN_DB away from the live database name.
case "$SCRATCH" in
  *schemagen*) ;;
  *) die "SCHEMAGEN_DB='${SCRATCH}' does not contain 'schemagen'.
  This script DROPs that database. Name it so the call site says so." ;;
esac

pg() { docker run --rm --network host -i "$PG_IMAGE" "$@"; }

own_pg=""
cleanup() { [ -n "$own_pg" ] && docker rm -f "$own_pg" >/dev/null 2>&1; return 0; }
trap cleanup EXIT

if [ -z "${SCHEMAGEN_SUPERUSER_DSN:-}" ]; then
  # Our own server. Docker assigns the port (`127.0.0.1::5432`) rather than
  # this script naming one: construct-server already has postgres on 5432 and
  # postgres-dev on 55432, and a hardcoded guess is how you end up talking to
  # somebody else's database again.
  own_pg="chronicle-schemagen-$$"
  own_pw="schemagen-throwaway-$$"
  docker run -d --rm --name "$own_pg" \
    -e POSTGRES_USER=postgres \
    -e POSTGRES_PASSWORD="$own_pw" \
    -e POSTGRES_DB=postgres \
    -p 127.0.0.1::5432 "$PG_IMAGE" >/dev/null

  own_port="$(docker port "$own_pg" 5432/tcp | head -1)"
  own_port="${own_port##*:}"
  [ -n "$own_port" ] || die "could not read the mapped port for ${own_pg}."
  SCHEMAGEN_SUPERUSER_DSN="postgres://postgres:${own_pw}@127.0.0.1:${own_port}/postgres?sslmode=disable"

  echo "starting a throwaway postgres on 127.0.0.1:${own_port} (${PG_IMAGE})"
  for _ in $(seq 1 60); do
    docker exec "$own_pg" pg_isready -q -U postgres -h 127.0.0.1 && break
    sleep 0.5
  done
  docker exec "$own_pg" pg_isready -q -U postgres -h 127.0.0.1 \
    || die "the throwaway postgres never became ready."
fi

# What is on this server? Anything beyond the template databases, the
# maintenance database we connect through, and our own scratch means this is
# somebody's real Postgres and the DROP below is not ours to run.
if ! present_raw="$(pg psql "$SCHEMAGEN_SUPERUSER_DSN" -At -q \
  -c 'SELECT datname FROM pg_database ORDER BY datname')"; then
  die "could not list the databases on that server, so cannot tell whose it is.
  Refusing to continue rather than dropping blind."
fi
readarray -t present <<<"$present_raw"

unexpected=()
for db in "${present[@]}"; do
  case "$db" in
    postgres|template0|template1|"$SCRATCH") ;;
    '') ;;
    *) unexpected+=("$db") ;;
  esac
done

if [ "${#unexpected[@]}" -gt 0 ]; then
  die "refusing to run against this server — it holds ${#unexpected[@]} database(s) this script did not create:

    $(printf '%s\n    ' "${unexpected[@]}" | sed '$d')

  The next statement would be DROP DATABASE ${SCRATCH} on that same server.
  On construct-server, 127.0.0.1:5432 is the estate's SHARED Postgres.

  Unset SCHEMAGEN_SUPERUSER_DSN and this script starts a Postgres of its own."
fi

# The scratch database is dropped and rebuilt every run: the artefact must
# describe the migrations alone, never leftover state from a previous run.
pg psql "$SCHEMAGEN_SUPERUSER_DSN" -v ON_ERROR_STOP=1 -q <<SQL
DROP DATABASE IF EXISTS ${SCRATCH};
SELECT 'CREATE ROLE chronicle LOGIN' WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='chronicle');
\gexec
SELECT 'CREATE ROLE chronicle_tier1 LOGIN' WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='chronicle_tier1');
\gexec
CREATE DATABASE ${SCRATCH} OWNER chronicle;
SQL

# Repoint the DSN at the scratch database by replacing the path, whatever it
# held. This was `sed s#/postgres?#...#`, which silently left the DSN alone
# when it did not match — a DSN with no query string, or one already naming a
# real database, and the migrator below ran against THAT instead.
scratch_dsn="$(printf '%s' "$SCHEMAGEN_SUPERUSER_DSN" \
  | sed -E "s#^([a-zA-Z0-9+.-]+://[^/]*)/[^?]*(\??.*)\$#\1/${SCRATCH}\2#")"
case "$scratch_dsn" in
  *"/${SCRATCH}"|*"/${SCRATCH}?"*) ;;
  *) die "could not point the DSN at ${SCRATCH}; refusing to migrate an unknown database." ;;
esac

# Apply via the real migrator, so this also proves the migrator and the files
# agree — not just that the SQL parses.
CHRONICLE_DATABASE_URL="$scratch_dsn" go run ./cmd/chronicle migrate up >/dev/null

# PRIVILEGES are kept deliberately: the tier grants are the doctrine's
# enforcement mechanism, so loosening one must show up here as a schema diff.
#
# OWNERSHIP is not (--no-owner). Who owns an object depends on which role ran
# the migration, which is a deploy concern that provision-db.sh settles — not a
# property of the migrations. Leaving it in makes the artefact differ between a
# CI database and a real one for no useful reason.
#
# Two things are filtered, both non-deterministic rather than meaningful:
#   -- Dumped ...        the server/pg_dump patch version banner
#   \restrict / \unrestrict   a RANDOM nonce pg_dump emits on every run; left
#                        in, the file would differ from itself every time.
{
  echo "-- Generated by scripts/gen-schema.sh from migrations/. Do not edit."
  echo "-- Regenerate with: scripts/gen-schema.sh"
  echo
  pg pg_dump --schema-only --no-owner --no-sync "$scratch_dsn" \
    | grep -vE '^-- Dumped|^\\restrict |^\\unrestrict '
} > "$OUT"

pg psql "$SCHEMAGEN_SUPERUSER_DSN" -v ON_ERROR_STOP=1 -q -c "DROP DATABASE IF EXISTS ${SCRATCH};"

echo "wrote $OUT ($(wc -l < "$OUT") lines)"
