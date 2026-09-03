-- Reverse of 0008.
DROP TRIGGER IF EXISTS memo_links_guard ON tier2.memo_links;
DROP FUNCTION IF EXISTS tier2.memo_links_guard();
DROP TABLE IF EXISTS tier2.memo_links;
