-- 0013_backlinks_and_tags — CHRN-42. Note-to-note links, resolved both ways,
-- plus tags.
--
-- ============================================================================
-- THE TWO HALVES OF THIS TICKET LAND ON OPPOSITE SIDES OF THE TIER LINE.
-- ============================================================================
--
-- That is the whole reason this file is worth reading rather than skimming,
-- and it is CLAUDE.md's first invariant applied inside one ticket.
--
-- LINKS ARE DERIVED. Nobody types a link row: a note that mentions CHR-0311
-- links to CHR-0311, and the ticket says so — "without anyone maintaining a
-- list by hand". Delete every row in tier1.note_links and re-extract them from
-- the current revision of every note and you get the same graph back. That is
-- the definition of tier 1, and CLAUDE.md names this exact case: "whatever
-- Chronicle derives from its own corpus: Scribe proposals, EXTRACTED ENTITIES,
-- search indexes."
--
-- TAGS ARE AUTHORED. A person decided this note is about hardware. Nothing
-- regenerates that, no text contains it, and losing it loses a judgement
-- somebody made. Tier 2.
--
-- The ticket's own sentence — "internal links are the one kind of link that is
-- stored, because both ends live in this database" — is about INVARIANT 2, not
-- about tiers. It says Chronicle may store its own graph because neither end
-- is a foreign source of truth that can go stale behind its back. It does not
-- say the graph is irreplaceable, and it is not.
--
-- ============================================================================
-- A LINK STORES THE NUMBER, NOT THE TARGET'S ID, AND RESOLVES AT READ TIME.
-- ============================================================================
--
-- Somebody writes CHR-0500 in a note before note 500 exists — while drafting,
-- or because they misremembered. Storing a resolved to_note_id would mean
-- either dropping that reference (and never noticing when 500 arrives) or
-- carrying a nullable id that something has to come back and repair.
--
-- Storing the NUMBER makes the dangling case resolve itself: the read joins
-- tier2.notes on number, a reference to a note that does not exist joins to
-- nothing, and the moment note 500 is created the same row starts resolving.
-- Nothing repairs anything. It is the same principle invariant 2 applies to
-- SY-412 — resolve at render time — turned inward, where it is cheap because
-- both ends really are here.

CREATE TABLE IF NOT EXISTS tier1.note_links (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- NO FOREIGN KEY into tier2.notes, and not an oversight: 0004 established
    -- that a tier-1 table referencing tier 2 is the cross-schema path the
    -- doctrine forbids, and 0005, 0006 and 0007 each repeated it. The
    -- consequence is the one memo_jobs and memo_proposals already carry — a
    -- row can outlive its note — and the answer is the same: this table is
    -- regenerable, so a rebuild collects the orphans rather than a sweep.
    from_note_id     UUID NOT NULL,

    -- WHICH REVISION'S TEXT PRODUCED THIS EDGE. Also no foreign key, same
    -- reason. It is what makes a stale row recognisable as stale: a link whose
    -- from_revision_id is not the note's current revision was extracted from
    -- text that has since been superseded.
    from_revision_id UUID NOT NULL,

    -- THE TARGET AS WRITTEN, resolved against tier2.notes.number at read time.
    -- Not an id: see the header. CHR-0311 and CHR-311 both land here as 311,
    -- because the number is the fact and the padding is a rendering.
    to_number        BIGINT NOT NULL CHECK (to_number > 0),

    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- ONE EDGE PER PAIR. A note that mentions CHR-0311 three times refers to
    -- it once as far as a backlink list is concerned; showing the source note
    -- three times in the target's sidebar would be noise, not information.
    UNIQUE (from_note_id, to_number)
);

CREATE INDEX IF NOT EXISTS note_links_to ON tier1.note_links (to_number);
CREATE INDEX IF NOT EXISTS note_links_from ON tier1.note_links (from_note_id);

COMMENT ON TABLE tier1.note_links IS
  'Derived from the current revision of each note by CHRN-42. Regenerable: delete every row and rebuild. Never hand-edited.';

-- ============================================================================
-- TAGS: TIER 2, AND A FILTER RATHER THAN A HIERARCHY.
-- ============================================================================
--
-- The ticket asks that tags be "a filter on the tree rather than a second
-- hierarchy", and the schema is how that is held to. A tag is ONE FLAT LABEL:
-- there is no parent_tag column, no path, and no nesting, so a tag can never
-- become a second way to organise the corpus that has to be kept in step with
-- the first. `hardware` and `hardware/audio` are two unrelated labels, and the
-- slug CHECK forbids the second from even being written.
CREATE TABLE IF NOT EXISTS tier2.note_tags (
    note_id    UUID NOT NULL REFERENCES tier2.notes(id) ON DELETE RESTRICT,

    -- The same shape as a page slug, and deliberately so: one vocabulary for
    -- the things a person types to organise this corpus. Lowercase because two
    -- tags differing only in case are two filters nobody can tell apart, and
    -- the '/' the pattern excludes is what would make a tag a path.
    tag        TEXT NOT NULL CHECK (tag ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (note_id, tag)
);

CREATE INDEX IF NOT EXISTS note_tags_tag ON tier2.note_tags (tag);

-- Redundant, and stated anyway as documentation of intent at the point the
-- tier boundary is defined, per the pattern 0002 established and 0003 words.
-- Note that tier1.note_links gets no such line: it is a TIER-1 table, and the
-- app role writes it as it writes tier1.memo_proposals.
--
-- WHAT MAKES IT REDUNDANT IS NARROWER THAN "0001 REVOKED THE SCHEMA", and the
-- difference matters because 0007:52 re-granted USAGE on schema tier2. The
-- fact that survives 0007: no GRANT on tier2.note_tags was ever issued, and
-- 0007 deliberately added no ALTER DEFAULT PRIVILEGES on schema tier2 — so a
-- tier-2 table created today is unreachable by chronicle_tier1 on table
-- privileges alone, whatever it holds on the schema.
REVOKE ALL ON tier2.note_tags FROM chronicle_tier1;
