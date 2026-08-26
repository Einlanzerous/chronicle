-- Reverses 0003_memos. This drops the corpus: every memo, every delivery
-- record, and the guard that keeps the state machine honest. Nothing here is
-- regenerable — down is for a failed deploy on an empty database, not for
-- undoing a decision on a populated one.
DROP TRIGGER IF EXISTS memos_guard ON tier2.memos;
DROP TABLE IF EXISTS tier2.memo_arrivals;
DROP TABLE IF EXISTS tier2.memos;
DROP FUNCTION IF EXISTS tier2.memos_guard();
DROP FUNCTION IF EXISTS tier2.retention_rank(TEXT);
