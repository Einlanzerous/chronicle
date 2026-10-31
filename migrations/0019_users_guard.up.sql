-- CHRN-101 / CHRN-157. tier2.users.id and .kind are set when an account is made
-- and never changed.
--
-- Ten statements in seven guard functions read tier2.users.kind live, at write
-- time, to decide whether an actor is a person (CH023, CH041, CH080, CH100,
-- CH101) or to copy it onto a turn (CH092). Nothing froze it, so a statement
-- that flipped an account person->agent->person around a write would have
-- written through a guard that saw the wrong kind. No runtime path updates it
-- today; this makes that a property of the database rather than of the code.
--
--   CH110  users   id and kind are never changed after the row exists
--
-- is_owner is NOT frozen: users_owner_is_a_person and the single-owner partial
-- index bound it, and a future owner transfer needs it updatable.
--
-- No existing guard is replaced. This adds one function and one trigger, the
-- same shape as memos_guard, and grants nothing to chronicle_tier1.

CREATE OR REPLACE FUNCTION tier2.users_guard() RETURNS trigger
LANGUAGE plpgsql AS $fn$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id THEN
        RAISE EXCEPTION 'an account id is never changed'
            USING ERRCODE = 'CH110';
    END IF;
    IF NEW.kind IS DISTINCT FROM OLD.kind THEN
        RAISE EXCEPTION 'an account kind is set when the account is made and never changed (account %)', OLD.id
            USING ERRCODE = 'CH110';
    END IF;
    RETURN NEW;
END
$fn$;

CREATE OR REPLACE TRIGGER users_guard BEFORE UPDATE ON tier2.users
    FOR EACH ROW EXECUTE FUNCTION tier2.users_guard();

COMMENT ON COLUMN tier2.users.id IS
  'CHRN-101. Never changed after the row exists (CH110, users_guard).';

COMMENT ON COLUMN tier2.users.kind IS
  'CHRN-101. person or agent, set when the account is made and never changed '
  '(CH110, users_guard). A wrongly-made account is fixed by making a new one; '
  'the old one keeps its authored history. Seven guard functions read this '
  'live to decide whether an actor is a person.';

COMMENT ON COLUMN tier2.discussion_turns.author_kind IS
  'CHRN-43, ruling 5. The author''s kind AT THE TIME THE TURN WAS WRITTEN, '
  'set by discussion_turns_guard from tier2.users.kind and refused from a '
  'caller (CH092). Kept on the turn on purpose: CH091 is a safety rule and a '
  'turn should not depend on a join to an account row. tier2.users.kind is '
  'frozen since 0019 (CH110), so the two agree for any account that exists; '
  'the copy stays an independent snapshot of what was true when it was written.';
