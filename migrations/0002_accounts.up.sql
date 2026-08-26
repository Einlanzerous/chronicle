-- 0002_accounts — CHRN-71. Accounts, invites and per-device sessions.
--
-- Tier 2, deliberately. An account and the credential that reaches it are
-- authored and irreplaceable: nothing regenerates them, and losing them locks
-- the corpus away from the person who wrote it. So they live beside the memos
-- rather than beside the derived indexes, and chronicle_tier1 cannot see them.
-- That gives CHRN-52 its sharpest assertion — the tier-1 role cannot read the
-- credentials table.
--
-- No passwords anywhere. A one-time invite is redeemed for a durable
-- per-device session; only the SHA-256 of either is stored, so the plaintext
-- is shown exactly once at mint time and is not recoverable afterwards.

CREATE TABLE IF NOT EXISTS tier2.users (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email        TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    -- 'agent' is not a permission level, it is an authorship fact. A locked
    -- IDEA-21 decision makes the Scribe a participant in discussions rather
    -- than a process acting on them, and a thread whose author column reads
    -- "owner" on both sides cannot be read. Accounts exist here to make
    -- authorship expressible; the household case is incidental.
    kind         TEXT NOT NULL DEFAULT 'person' CHECK (kind IN ('person', 'agent')),
    is_owner     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- The owner administers accounts, which is not something an automated
    -- participant should ever inherit by having its kind changed.
    CONSTRAINT users_owner_is_a_person CHECK (NOT is_owner OR kind = 'person')
);

-- At most one owner. The partial index covers only is_owner rows, so ordinary
-- accounts are unconstrained.
CREATE UNIQUE INDEX IF NOT EXISTS users_single_owner
    ON tier2.users ((is_owner)) WHERE is_owner;

-- Credentials. An 'invite' is single-use and short-lived; redeeming it yields
-- a 'session', which is the long-lived per-device credential clients carry.
-- Splitting the two is what lets one device be revoked on its own, and lets a
-- person be re-invited without their account being touched.
CREATE TABLE IF NOT EXISTS tier2.user_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES tier2.users(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('invite', 'session')),
    token_hash   TEXT NOT NULL UNIQUE,
    label        TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    used_at      TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS user_tokens_user_id ON tier2.user_tokens (user_id);

-- Seed the owner here rather than at boot so the row exists before anything
-- can reference it, and so the whole thing lands in one transaction. The
-- identity is a placeholder; cmd/chronicle reconciles it from
-- CHRONICLE_OWNER_EMAIL / CHRONICLE_OWNER_NAME on startup. The display name
-- literal below is mirrored by store.OwnerPlaceholderName, which is how boot
-- knows it may replace it — change one and you must change the other, or the
-- placeholder becomes permanent.
INSERT INTO tier2.users (email, display_name, is_owner)
SELECT 'owner@localhost', 'Owner', TRUE
WHERE NOT EXISTS (SELECT 1 FROM tier2.users);

-- Redundant against 0001 — chronicle_tier1 holds no USAGE on schema tier2 —
-- and stated anyway as documentation of intent, at the point where the tier
-- boundary is defined and on the two tables whose exposure would matter most.
--
-- It does NOT make a later loosened grant appear in schema.sql, which is what
-- this comment used to claim (CHRN-78). pg_dump emits only non-default ACLs,
-- so revoking a privilege the role never held leaves nothing to emit — which
-- is why schema.sql carries no REVOKE for tier2.users at all. A loosened GRANT
-- is a non-default ACL and shows up in the diff on its own, with or without
-- these two lines. 0003 states it this way; this is 0002 catching up.
REVOKE ALL ON tier2.users FROM chronicle_tier1;
REVOKE ALL ON tier2.user_tokens FROM chronicle_tier1;
