DROP TABLE IF EXISTS tier1.memo_jobs;
DROP TRIGGER IF EXISTS transcripts_guard ON tier2.transcripts;
DROP FUNCTION IF EXISTS tier2.transcripts_guard();
DROP TABLE IF EXISTS tier2.transcripts;
