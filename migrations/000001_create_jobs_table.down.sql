-- Rollback: Drop everything we created
DROP INDEX IF EXISTS idx_jobs_state_created_at;
DROP INDEX IF EXISTS idx_jobs_created_at;
DROP INDEX IF EXISTS idx_jobs_state;
DROP TABLE IF EXISTS jobs;