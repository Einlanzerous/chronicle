-- Reverses 0013_backlinks_and_tags.
--
-- tier1.note_links carries no foreign keys in either direction, so the order
-- here is free; tier2.note_tags references tier2.notes and so must go before
-- anything 0011 drops, which the migrator's ordering already guarantees.

DROP TABLE IF EXISTS tier2.note_tags;
DROP TABLE IF EXISTS tier1.note_links;
