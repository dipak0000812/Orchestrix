DROP INDEX IF EXISTS idx_jobs_owner_key_id;
ALTER TABLE jobs DROP COLUMN IF EXISTS owner_key_id;
DROP TABLE IF EXISTS api_keys;
