-- Reverse of 0007. The grant goes back too — a down migration that left the
-- role holding a privilege it did not have before is not a reverse.
DROP TRIGGER IF EXISTS memo_proposals_guard ON tier1.memo_proposals;
DROP FUNCTION IF EXISTS tier1.memo_proposals_guard();
DROP TABLE IF EXISTS tier1.memo_proposals;

REVOKE SELECT ON tier2.memos, tier2.transcripts FROM chronicle_tier1;
REVOKE USAGE ON SCHEMA tier2 FROM chronicle_tier1;
