-- Persist retry eligibility so retry delays survive process restarts.
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS next_run_at TIMESTAMPTZ;

-- Existing retries were created before next_run_at existed. Make them eligible
-- immediately rather than leaving them stranded after the migration.
UPDATE jobs
SET next_run_at = NOW()
WHERE state = 'RETRYING' AND next_run_at IS NULL;

-- This partial index matches the delayed-retry branch of the scheduler query.
CREATE INDEX IF NOT EXISTS idx_jobs_retry_due
    ON jobs (next_run_at, created_at)
    WHERE state = 'RETRYING';
