-- Reverses 0015_discussions.
--
-- THE verb COMMENT IS RESTORED, NOT DROPPED, and it is the only part of this
-- file that is not a drop. 0015 amended a comment 0014 wrote; a down that only
-- removed the three tables would leave the amended text behind, and
-- up-then-down-then-up would not produce a byte-identical schema.sql. CI's
-- staleness guard finds that, one commit after the person who caused it has
-- moved on.
--
-- The text below is 0014_revisions_and_soft_delete.up.sql:126-128 verbatim.
COMMENT ON COLUMN tier2.note_revisions.verb IS
  'CHRN-39. What a person confirmed about a Scribe proposal: create, append, '
  'supersede or relate. NULL means authored directly.';

-- DROP TABLE does not fire row triggers, so discussion_turns_guard's CH090
-- does not refuse this — the same note 0011 and 0014 both carry.
--
-- Participants and turns both reference tier2.discussions, so they go first.
DROP TRIGGER IF EXISTS discussion_participants_guard ON tier2.discussion_participants;
DROP TABLE IF EXISTS tier2.discussion_participants;
DROP FUNCTION IF EXISTS tier2.discussion_participants_guard();

DROP TRIGGER IF EXISTS discussion_turns_guard ON tier2.discussion_turns;
DROP TABLE IF EXISTS tier2.discussion_turns;
DROP FUNCTION IF EXISTS tier2.discussion_turns_guard();

DROP TRIGGER IF EXISTS discussions_guard ON tier2.discussions;
DROP TABLE IF EXISTS tier2.discussions;
DROP FUNCTION IF EXISTS tier2.discussions_guard();

DROP SEQUENCE IF EXISTS tier2.discussion_number_seq;
