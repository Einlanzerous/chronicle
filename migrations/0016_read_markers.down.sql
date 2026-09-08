-- Reverses 0016_read_markers.
--
-- THE GUARD AND THE TABLE COMMENT ARE RESTORED, NOT DROPPED, and that is the
-- only part of this file that is not a drop. 0016 replaced a function 0015
-- wrote and amended a comment 0015 wrote; a down that removed only the columns
-- would leave the widened allow list and the reworded comment behind, and
-- up-then-down-then-up would not produce a byte-identical schema.sql. CI finds
-- that a commit later, on somebody else's branch.
--
-- Everything below the columns is 0015_discussions.up.sql VERBATIM.

CREATE OR REPLACE FUNCTION tier2.discussion_participants_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
DECLARE
    changed TEXT;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        SELECT string_agg(n.key, ', ' ORDER BY n.key) INTO changed
          FROM jsonb_each(to_jsonb(NEW)) n
          JOIN jsonb_each(to_jsonb(OLD)) o ON o.key = n.key
         WHERE n.value IS DISTINCT FROM o.value
           AND n.key <> ALL (ARRAY['removed_at', 'removed_by']);
        IF changed IS NOT NULL THEN
            RAISE EXCEPTION
                'tier2.discussion_participants permits updating removed_at, removed_by only; added_at and added_by mean FIRST added; refused: %',
                changed
                USING ERRCODE = 'CH100',
                      CONSTRAINT = 'discussion_participants_update_allow_list';
        END IF;
    END IF;

    -- A PERSON ADDS AND A PERSON REMOVES. Membership decides who is expected
    -- to read a thread, and an agent quietly removing a person from a
    -- conversation is the shape CH041 exists to refuse one table over. Both
    -- ends, because an INSERT-only test would leave the removal half reading
    -- as enforced while doing nothing — 0014:378-384's failure, named there.
    --
    -- CONSTRAINT named for CH080's reason, one function up.
    IF NOT EXISTS (SELECT 1 FROM tier2.users
                    WHERE id = NEW.added_by AND kind = 'person') THEN
        RAISE EXCEPTION 'a participant is added by a person, not by an agent'
            USING ERRCODE = 'CH100',
                  CONSTRAINT = 'discussion_participants_actor_is_a_person';
    END IF;
    IF NEW.removed_by IS NOT NULL
    AND NOT EXISTS (SELECT 1 FROM tier2.users
                     WHERE id = NEW.removed_by AND kind = 'person') THEN
        RAISE EXCEPTION 'a participant is removed by a person, not by an agent'
            USING ERRCODE = 'CH100',
                  CONSTRAINT = 'discussion_participants_actor_is_a_person';
    END IF;

    RETURN NEW;
END
$fn$;

COMMENT ON TABLE tier2.discussion_participants IS
  'CHRN-43 / CHRN-44. Who is expected to READ a thread — CHRN-45''s question, '
  'not "who may write". Current state rather than a journal: a removed '
  'participant''s turns still render, so nothing is lost by not keeping the '
  'add/remove history. added_at and added_by are frozen and mean FIRST added.';

-- The columns come off last, because the restored guard no longer mentions
-- them and nothing else does either.
ALTER TABLE tier2.discussion_participants
    DROP CONSTRAINT IF EXISTS discussion_participants_read_pair;

ALTER TABLE tier2.discussion_participants
    DROP COLUMN IF EXISTS last_read_at,
    DROP COLUMN IF EXISTS last_read_seq;
