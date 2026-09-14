DROP INDEX IF EXISTS idx_jobs_retry_due;
ALTER TABLE jobs DROP COLUMN IF EXISTS next_run_at;
