-- 0012_search — CHRN-41. Full-text search over authored notes and memo
-- transcripts.
--
-- ============================================================================
-- TWO INDEXES AND NO TABLE, WHICH IS A DECISION AND NOT A SHORTCUT.
-- ============================================================================
--
-- CLAUDE.md lists "search indexes" among the things tier 1 holds, and CHRN-32
-- §1.1 reasoned from that to a conclusion this migration does not reach:
--
--     "CHRN-41's search index covers 'notes and transcripts' by its own title,
--      so its index cannot be built at all without reading tier 2."
--
-- That is true of an index kept as ROWS IN A TIER-1 TABLE, built by a process
-- running as chronicle_tier1. It is not true of a Postgres expression index,
-- which is an access path belonging to the table it is on, maintained by the
-- server inside the same transaction as the write, and rebuilt by REINDEX. It
-- holds no rows anybody queries, it cannot drift from its table, and it needs
-- no grant, no worker and no rebuild job.
--
-- So the grant question CHRN-32 §1.1 handed forward is DISSOLVED here rather
-- than answered: nothing in this migration or in Store.Search runs as
-- chronicle_tier1, and no GRANT is added. If a later ticket wants derived
-- search artefacts that are rows — extracted entities, embeddings, a ranking
-- cache — that ticket meets the question again, unchanged and undecided.
--
-- This is also the "one less moving part" the ticket asks for, twice over: no
-- Elasticsearch, and no second copy of the corpus inside Postgres either.
--
-- ============================================================================
-- THE TWO-ARGUMENT to_tsvector IS MANDATORY.
-- ============================================================================
--
-- to_tsvector(text) — one argument — reads default_text_search_config, which
-- makes it STABLE rather than IMMUTABLE and therefore not indexable. The
-- two-argument form with a literal 'english' is IMMUTABLE and is what both
-- these indexes and every query in internal/store/search.go must spell, letter
-- for letter, or the query silently sequential-scans while the index sits
-- there looking correct.

-- A NOTE'S TITLE OUTRANKS ITS BODY. Weight A against B is the whole of the
-- ranking model: somebody searching "naming" wants the note called Naming
-- before the one that mentions naming in passing, and ts_rank_cd applies the
-- weights without any further tuning.
--
-- INDEXED OVER EVERY REVISION, QUERIED OVER THE CURRENT ONE. Indexing only the
-- live text is not possible — an index predicate cannot reach through
-- notes.current_revision_id — so the index covers all history and the query
-- joins to notes to discard superseded text. The cost is an index larger than
-- the live corpus; the alternative is a stale-text hit list, which is worse.
CREATE INDEX IF NOT EXISTS note_revisions_fts
    ON tier2.note_revisions USING GIN ((
        setweight(to_tsvector('english', title), 'A') ||
        setweight(to_tsvector('english', body), 'B')
    ));

-- SEARCHING TRANSCRIPTS MATTERS MORE THAN IT SOUNDS, and the ticket says why:
-- CHRN-22 prunes audio at thirty days, so from day thirty-one the transcript is
-- the only remaining account of what was said. A search that covered notes and
-- not transcripts would lose everything the operator never got round to
-- triaging — which is most of it, most of the time.
--
-- Partial transcripts are indexed too. A partial is what somebody said up to
-- the point the decode gave out, and it is still the only record of that much.
CREATE INDEX IF NOT EXISTS transcripts_fts
    ON tier2.transcripts USING GIN ((to_tsvector('english', text)));

-- No REVOKE and no GRANT: this migration creates no table, so there is no new
-- object whose tier boundary needs stating. The indexes belong to
-- tier2.note_revisions and tier2.transcripts, whose REVOKEs 0006 and 0011
-- already carry.
