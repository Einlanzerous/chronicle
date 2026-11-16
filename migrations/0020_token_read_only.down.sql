-- Reverses 0020_token_read_only.
ALTER TABLE tier2.user_tokens
    DROP COLUMN IF EXISTS read_only;
