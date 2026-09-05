-- Reverses 0011_notes.
--
-- The two tables reference each other, so the composite constraint comes off
-- first and the drops are then ordinary. DROP TABLE does not fire row
-- triggers, so note_revisions_guard does not refuse this.

ALTER TABLE IF EXISTS tier2.notes DROP CONSTRAINT IF EXISTS notes_current_revision;

DROP TABLE IF EXISTS tier2.note_revisions;
DROP TABLE IF EXISTS tier2.notes;
DROP SEQUENCE IF EXISTS tier2.note_number_seq;
DROP FUNCTION IF EXISTS tier2.note_revisions_guard();
DROP FUNCTION IF EXISTS tier2.notes_guard();
