-- deploy/tier1-role.sql — the chronicle_tier1 role's shape, and the database
-- lockdown it relies on. ONE definition, consumed in three places:
--
--   * deploy/provision-db.sh      the one-off superuser run for a Postgres that
--                                 construct-server does not provision;
--   * .github/workflows/ci.yml    applied to chronicle_test, so CI's ACLs match
--                                 production's and the tier-1 audit passes there
--                                 for the right reason rather than because
--                                 nothing was granted;
--   * construct-server/db/init-db.sh   the estate's standing, idempotent path,
--                                 which carries these statements in a dedicated
--                                 block and names this file as their origin.
--
-- It is run CONNECTED TO THE TARGET DATABASE (the schema revoke is per
-- database), as a superuser, with two variables in psql's environment:
--
--   T1_PW   the role's password       DB   the database name
--
-- read via \getenv so the password never appears on a command line.
--
-- What is deliberately NOT here: any GRANT inside the schemas. Those are
-- Chronicle's migrations' — 0001 for tier1, 0007 for the two tier-2 reads —
-- where they are tested. This file gives the role a way in and nothing else.
-- ensure_db in construct-server is not used for this role on purpose: its
-- unconditional GRANT ALL PRIVILEGES ON DATABASE would hand the role CREATE on
-- the tier-2 database on every deploy (SERV-169), and Chronicle's boot audit
-- would then refuse to serve — correctly.
\set ON_ERROR_STOP on
\getenv t1_pw T1_PW
\getenv db DB

-- A plain LOGIN role. Every attribute is stated NO rather than left to the
-- default, so a re-run also undoes an ALTER ROLE somebody made by hand.
--
-- \gset + \if rather than the \gexec idiom, because \gexec ECHOES the generated
-- statement — password and all — to psql's output, and this file's output is
-- a terminal under signet exec or a CI log.
SELECT NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'chronicle_tier1') AS tier1_missing \gset
\if :tier1_missing
CREATE ROLE chronicle_tier1 LOGIN PASSWORD :'t1_pw';
\endif
ALTER ROLE chronicle_tier1 LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS
  PASSWORD :'t1_pw';

-- On the database: CONNECT and nothing else. Postgres grants CONNECT and TEMP
-- to PUBLIC by default; both are taken back, then CONNECT alone is given back
-- by name.
REVOKE ALL ON DATABASE :"db" FROM PUBLIC;
REVOKE ALL ON DATABASE :"db" FROM chronicle_tier1;
GRANT CONNECT ON DATABASE :"db" TO chronicle_tier1;

-- The public schema holds schema_migrations and nothing the role may see.
-- PUBLIC keeps USAGE on it by default (Postgres 15 dropped CREATE, not USAGE),
-- and the audit's rule is "every schema other than tier1 and tier2: nothing".
REVOKE ALL ON SCHEMA public FROM PUBLIC;
REVOKE ALL ON SCHEMA public FROM chronicle_tier1;
