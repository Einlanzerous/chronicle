-- Reverses 0002_accounts. Dropping these destroys every account and every
-- credential; the corpus survives, but nobody can reach it until an invite is
-- minted again. user_tokens first — it is the referencing side.
DROP TABLE IF EXISTS tier2.user_tokens;
DROP TABLE IF EXISTS tier2.users;
