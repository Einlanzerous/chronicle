-- Reverses 0012_search. Indexes only — there is no table to drop, which is the
-- point of the design.

DROP INDEX IF EXISTS tier2.note_revisions_fts;
DROP INDEX IF EXISTS tier2.transcripts_fts;
