-- 0010_pages — CHRN-37. The page tree: paths, slugs, and a move that orphans
-- nothing.
--
-- Tier 2. A page is structure a person authored and nothing regenerates it,
-- but the irreplaceable part is what hangs off it: CHRN-38's notes address a
-- page by id, and E5's whole thesis is that authored text does not go missing
-- as a side effect of something structural moving. RESTRICT everywhere is what
-- enforces that.
--
-- ============================================================================
-- THE PATH IS NOT A COLUMN.
-- ============================================================================
--
-- `estate/conventions/naming` is what a person types, what the canvas draws
-- and what Scribe routes against — and it is DERIVED, from parent_id and slug,
-- by walking to the root.
--
-- Materialising it would make lookup a single index probe, and it would make
-- one fact have two representations. Renaming `estate/conventions` then has to
-- rewrite every descendant's copy, and the first descendant that rewrite
-- misses is a page answering to a path it no longer has — silently, because
-- both the stale copy and the true ancestry look perfectly well-formed on
-- their own. CLAUDE.md's second invariant is written about foreign sources of
-- truth, but the failure it names is this one exactly, and a table can be its
-- own second source.
--
-- The corpus is one operator's notes. A recursive CTE over a few hundred rows
-- is not a cost worth buying a class of silent drift to avoid.
--
-- ============================================================================
-- IDENTITY IS THE UUID; THE PATH IS AN ADDRESS.
-- ============================================================================
--
-- The ticket names this as the thing to get right, and the reason is the move:
-- a page that moves must keep its notes and its inbound links. Both hang off
-- `id`, so a move is an UPDATE of parent_id and/or slug and touches nothing
-- else. Nothing anywhere stores a path as a reference to a page.
--
-- ============================================================================
-- WHAT THE DATABASE ENFORCES, AND WHAT THE APPLICATION MAINTAINS.
-- ============================================================================
--
-- The split is by consequence, not by convenience:
--
--   * INTEGRITY IS A TRIGGER. A cycle detaches a subtree from the root, so
--     every page in it becomes unreachable and unnameable while still holding
--     notes. That must be impossible for anything holding a connection, not
--     merely for code that goes through the Go store.
--
--   * REDIRECTS ARE APPLICATION LOGIC. The `Done when` asks that an old path
--     "either redirects or is explicitly gone" — both are acceptable answers,
--     which makes a redirect a courtesy rather than an invariant. MovePage
--     writes them inside its transaction; a page moved by hand in psql simply
--     leaves its old path gone, which is the other half of what the ticket
--     already permits.

CREATE TABLE IF NOT EXISTS tier2.pages (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- NULL means a root page. RESTRICT, deliberately: deleting a page that
    -- still has children would either orphan them or cascade a subtree of
    -- authored notes out of existence, and both are the conversation this
    -- should be instead. tier2.memos uses RESTRICT against users for the same
    -- reason.
    parent_id  UUID REFERENCES tier2.pages(id) ON DELETE RESTRICT,

    -- ONE SEGMENT OF A PATH, and the CHECK is what keeps it one. A slug
    -- containing '/' would let a single row claim two levels of the tree, so
    -- `estate/conventions` could exist both as a nesting and as a literal
    -- slug, resolving to two different pages depending on which query ran.
    -- Lowercase because a path a person types is not case-sensitive anywhere
    -- else in the estate, and two slugs differing only in case would be two
    -- pages nobody can tell apart out loud.
    slug       TEXT NOT NULL CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- THERE IS NO title COLUMN, and its absence is deliberate rather than
-- forgotten. The ticket asks for paths, slugs and moves; it does not ask for a
-- display name. Adding one now would put unversioned authored text on a
-- mutable tier-2 row in the same epic whose CHRN-38 decision is that authored
-- text lives in revisions — a rename would be an UPDATE that loses what a
-- person wrote, with no revision log to recover it from. If the canvas wants a
-- display title later it is a deliberate decision about versioning, taken
-- where that question can be asked properly.

-- SIBLINGS CANNOT COLLIDE, in two indexes rather than one constraint, because
-- NULL parent_id means "root" and NULLs do not compare equal — a plain
-- UNIQUE (parent_id, slug) would permit any number of root pages all called
-- `estate`, which is precisely the collision the constraint exists to stop.
CREATE UNIQUE INDEX IF NOT EXISTS pages_sibling_slug
    ON tier2.pages (parent_id, slug) WHERE parent_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS pages_root_slug
    ON tier2.pages (slug) WHERE parent_id IS NULL;

CREATE INDEX IF NOT EXISTS pages_parent ON tier2.pages (parent_id);

-- Where a path used to point. Written by MovePage for the moved page and every
-- descendant, because moving `estate/conventions` changes the path of
-- everything beneath it too.
--
-- from_path IS THE KEY, not page_id: many old paths may point at one page as
-- it is moved repeatedly, and each of them must resolve. A live page path is
-- never left in here — MovePage deletes any redirect whose from_path a move
-- has just made real again, so resolution never has to decide between a page
-- and a redirect claiming the same string.
CREATE TABLE IF NOT EXISTS tier2.page_redirects (
    from_path  TEXT PRIMARY KEY CHECK (from_path <> ''),
    page_id    UUID NOT NULL REFERENCES tier2.pages(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS page_redirects_page ON tier2.page_redirects (page_id);

-- ============================================================================
-- THE GUARD.
-- ============================================================================
--
-- Error codes continue the one-block-per-table shape: CH001-CH005 memos,
-- CH010-CH011 proposals, CH020-CH022 memo_links, CH030 notes and CH040 note
-- revisions (both reserved by CHRN-38's approved plan, which numbered them
-- before this migration was written). Pages take CH050.
CREATE OR REPLACE FUNCTION tier2.pages_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
DECLARE
    ancestor UUID;
    hops     INT := 0;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        -- Identity is immutable. parent_id and slug deliberately are NOT:
        -- changing them is what a move and a rename ARE.
        IF NEW.id IS DISTINCT FROM OLD.id THEN
            RAISE EXCEPTION 'a page id is immutable'
                USING ERRCODE = 'CH051';
        END IF;
        NEW.updated_at := now();
    END IF;

    -- A CYCLE IS THE ONE STRUCTURAL ERROR THAT HIDES. A page made its own
    -- ancestor is still a perfectly valid row, its children still resolve to
    -- it, and every query that walks UP from a leaf runs forever while every
    -- query that walks DOWN from a root simply never reaches it. The subtree
    -- and all the notes in it become unreachable and unnameable without a
    -- single constraint being violated, which is why this is a trigger and not
    -- a rule in Go.
    ancestor := NEW.parent_id;
    WHILE ancestor IS NOT NULL LOOP
        IF ancestor = NEW.id THEN
            RAISE EXCEPTION 'a page may not be its own ancestor (page %)', NEW.id
                USING ERRCODE = 'CH050';
        END IF;
        hops := hops + 1;
        IF hops > 64 THEN
            -- Unreachable while this trigger has always been armed, and it is
            -- here so that a pre-existing cycle raises instead of hanging the
            -- connection that found it.
            RAISE EXCEPTION 'page ancestry exceeds 64 levels, which means a cycle'
                USING ERRCODE = 'CH050';
        END IF;
        SELECT parent_id INTO ancestor FROM tier2.pages WHERE id = ancestor;
    END LOOP;

    RETURN NEW;
END
$fn$;

CREATE OR REPLACE TRIGGER pages_guard BEFORE INSERT OR UPDATE ON tier2.pages
    FOR EACH ROW EXECUTE FUNCTION tier2.pages_guard();

-- Redundant, and stated anyway as documentation of intent at the point the
-- tier boundary is defined, per the pattern 0002 established and 0003 words.
--
-- WHAT MAKES IT REDUNDANT IS NARROWER THAN "0001 REVOKED THE SCHEMA", and the
-- difference matters because 0007:52 re-granted USAGE on schema tier2. The
-- fact that survives 0007: no GRANT on tier2.pages or tier2.page_redirects was
-- ever issued, and 0007 deliberately added no ALTER DEFAULT PRIVILEGES on
-- schema tier2 — so a tier-2 table created today is unreachable by
-- chronicle_tier1 on table privileges alone, whatever it holds on the schema.
REVOKE ALL ON tier2.pages, tier2.page_redirects FROM chronicle_tier1;
