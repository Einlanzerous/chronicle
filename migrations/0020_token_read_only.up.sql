-- SERV-307 (plan SERV-293, Row 2). A credential that the API itself refuses to
-- let write.
--
-- Until now "a hosted MCP session is read-only" was a property of which tools
-- the MCP server OFFERED (Identity.MayWrite, CHRN-65). That is a promise about a
-- menu, not about the credential: the session token a hosted conversation holds
-- is an ordinary session as the person, and anything that obtained it could
-- call any route as them. This column moves the refusal to the API, where
-- requireUser reads it and answers 403 on every route outside an enumerated
-- read allowlist (internal/api/policy.go).
--
-- It is a property of the TOKEN, not of the account: the same person's browser
-- cookie and invite-redeemed sessions stay read_only = false, and no runtime
-- path flips an existing row. MintHostedSession sets it on every mint.
--
-- DEFAULT false is the existing behaviour for every durable session, so the
-- column changes nothing for any row it is not set on.
ALTER TABLE tier2.user_tokens
    ADD COLUMN read_only boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN tier2.user_tokens.read_only IS
    'SERV-307. The API refuses this credential on every route outside the read allowlist (internal/api/policy.go). Set by MintHostedSession; never set on a cookie or invite-redeemed session.';

-- A hosted session already live when this applies was minted writable. It lives
-- at most store.HostedSessionTTL, but there is no reason to leave it so: the
-- same three-way shape MintHostedSession's sweep uses (a session, the hosted
-- label, and an expiry) names exactly these rows and cannot reach a durable
-- session, which has no expiry.
UPDATE tier2.user_tokens
   SET read_only = true
 WHERE kind = 'session'
   AND label = 'MCP (hosted)'
   AND expires_at IS NOT NULL;
