DROP INDEX IF EXISTS jobs_attempts;
ALTER TABLE jobs DROP COLUMN IF EXISTS last_release_reason;
