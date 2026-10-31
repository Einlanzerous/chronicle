-- CHRN-101 / CHRN-157. Undo 0019: the trigger and its function go, and the
-- column comments return to what they said before.

DROP TRIGGER IF EXISTS users_guard ON tier2.users;
DROP FUNCTION IF EXISTS tier2.users_guard();

COMMENT ON COLUMN tier2.users.id IS NULL;
COMMENT ON COLUMN tier2.users.kind IS NULL;

-- >>> 0015 VERBATIM >>>
COMMENT ON COLUMN tier2.discussion_turns.author_kind IS
  'CHRN-43, ruling 5. The author''s kind AT THE TIME THE TURN WAS WRITTEN, '
  'set by discussion_turns_guard from tier2.users.kind and refused from a '
  'caller (CH092). Denormalised on purpose: CH091 is a safety rule and '
  'tier2.users.kind is mutable, so reading it live would let an account edit '
  'rewrite the verdict on turns already written.';
-- <<< 0015 VERBATIM <<<
