-- Reverses 0010_pages.
--
-- page_redirects first: it references pages, and RESTRICT means the reference
-- is real. Triggers and indexes go with their table; the function does not.

DROP TABLE IF EXISTS tier2.page_redirects;
DROP TABLE IF EXISTS tier2.pages;
DROP FUNCTION IF EXISTS tier2.pages_guard();
