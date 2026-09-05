-- 0001_init — the tier split, as schemas, before any table exists.
--
-- Doctrine (see CLAUDE.md): tier 1 is derived and disposable, tier 2 is
-- authored and irreplaceable, and THEY DO NOT SHARE A STORE. Creating both
-- schemas here -- empty -- means every later migration has to choose a side
-- explicitly. A table with no schema qualifier will not land somewhere
-- plausible by accident; there is no default to fall into.

CREATE SCHEMA IF NOT EXISTS tier2;
CREATE SCHEMA IF NOT EXISTS tier1;

COMMENT ON SCHEMA tier2 IS
  'Tier 2 - authored and irreplaceable. What a person said or wrote: memos, '
  'transcripts, notes, discussions, plans. Not derivable from anything, '
  'cannot be rebuilt. Nothing on a tier-1 write path may reach these tables.';

COMMENT ON SCHEMA tier1 IS
  'Tier 1 - derived and disposable. Chronicle''s account of what exists, and '
  'what it derives from its own corpus: proposals, entities, indexes. '
  'Regenerable from a source of truth outside these tables. Never hand-edited.';

-- Nobody gets anything by default, including via PUBLIC.
REVOKE ALL ON SCHEMA public FROM PUBLIC;
REVOKE ALL ON SCHEMA tier1 FROM PUBLIC;
REVOKE ALL ON SCHEMA tier2 FROM PUBLIC;

-- The regeneration role owns tier 1 outright. Its reach into tier 2 is not a
-- schema wall — it is whatever a migration grants BY NAME, and as of 0007:53
-- that is SELECT on tier2.memos and tier2.transcripts and nothing else. Scribe
-- and the search index derive from the corpus, and cannot derive from a corpus
-- they cannot read.
--
-- THIS COMMENT USED TO SAY THE ROLE "cannot see tier 2 at all" (CHRN-88). That
-- was true when 0001 was written and false from 0007 onward, and it was wrong in
-- the load-bearing direction: REVIEW.md section 1 sends a reviewer to these lines
-- to judge a `GRANT ... TO chronicle_tier1`, and the old wording argued such a
-- grant could not be reached anyway — clearing the one thing that check exists to
-- catch. CHRN-32's ruling R4 (accepted 2026-08-30) is where it was ruled to
-- change: say which two tables the role can read, and why.
--
-- What keeps every OTHER tier-2 table unreachable is table privileges, not the
-- REVOKE below: no GRANT was ever issued on them, and 0007 deliberately added no
-- ALTER DEFAULT PRIVILEGES on schema tier2. The invariant is unchanged — no
-- tier-1 WRITE path reaches a tier-2 table — and CHRN-52 is the test that proves
-- it still holds.
GRANT USAGE, CREATE ON SCHEMA tier1 TO chronicle_tier1;
ALTER DEFAULT PRIVILEGES IN SCHEMA tier1
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO chronicle_tier1;
ALTER DEFAULT PRIVILEGES IN SCHEMA tier1
  GRANT USAGE, SELECT ON SEQUENCES TO chronicle_tier1;

REVOKE ALL ON SCHEMA tier2 FROM chronicle_tier1;
