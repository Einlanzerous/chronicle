-- Reverses 0014_revisions_and_soft_delete.
--
-- The two rewritten guards are restored to 0011's definitions rather than
-- dropped: 0011 created them, so dropping them here would leave the schema
-- missing a guard 0011 is responsible for and make down-then-up produce a
-- different database than up alone. note_tags_guard IS dropped, because 0014
-- created it.
--
-- The columns come off last, because the restored guards no longer mention
-- them and nothing else does either.

DROP TRIGGER IF EXISTS note_tags_guard ON tier2.note_tags;
DROP FUNCTION IF EXISTS tier2.note_tags_guard();

-- DROP TABLE does not fire row triggers, so note_deletions_guard's CH070 does
-- not refuse this.
DROP TRIGGER IF EXISTS note_deletions_guard ON tier2.note_deletions;
DROP TABLE IF EXISTS tier2.note_deletions;
DROP FUNCTION IF EXISTS tier2.note_deletions_guard();

-- Back to 0011's unconditional form, and back to firing on UPDATE OR DELETE
-- only — an INSERT arm with no confirmed_by column to test would fail.
CREATE OR REPLACE FUNCTION tier2.note_revisions_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
BEGIN
    RAISE EXCEPTION 'a note revision is append-only: % is refused', TG_OP
        USING ERRCODE = 'CH040';
END
$fn$;

CREATE OR REPLACE TRIGGER note_revisions_guard
    BEFORE UPDATE OR DELETE ON tier2.note_revisions
    FOR EACH ROW EXECUTE FUNCTION tier2.note_revisions_guard();

-- Back to 0011's freeze-list.
CREATE OR REPLACE FUNCTION tier2.notes_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF NEW.id        IS DISTINCT FROM OLD.id
        OR NEW.number    IS DISTINCT FROM OLD.number
        OR NEW.author_id IS DISTINCT FROM OLD.author_id THEN
            RAISE EXCEPTION 'note identity is immutable (id, number, author)'
                USING ERRCODE = 'CH030';
        END IF;

        NEW.updated_at := now();
    END IF;
    RETURN NEW;
END
$fn$;

CREATE OR REPLACE TRIGGER notes_guard BEFORE INSERT OR UPDATE ON tier2.notes
    FOR EACH ROW EXECUTE FUNCTION tier2.notes_guard();

ALTER TABLE tier2.note_revisions
    DROP CONSTRAINT IF EXISTS note_revisions_restored_from,
    DROP CONSTRAINT IF EXISTS note_revisions_verb_seq,
    DROP CONSTRAINT IF EXISTS note_revisions_verb;

ALTER TABLE tier2.note_revisions
    DROP COLUMN IF EXISTS restored_from,
    DROP COLUMN IF EXISTS verb,
    DROP COLUMN IF EXISTS confirmed_by;

-- The index swap, reversed.
DROP INDEX IF EXISTS tier2.notes_page_live;
CREATE INDEX IF NOT EXISTS notes_page ON tier2.notes (page_id);

ALTER TABLE tier2.notes
    DROP CONSTRAINT IF EXISTS notes_deleted_pair;

ALTER TABLE tier2.notes
    DROP COLUMN IF EXISTS deleted_by,
    DROP COLUMN IF EXISTS deleted_at;
