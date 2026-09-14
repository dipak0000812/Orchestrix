-- API keys: never store the key itself, only a SHA-256 hash of it.
CREATE TABLE api_keys (
    id          TEXT PRIMARY KEY,
    key_hash    TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at  TIMESTAMPTZ
);

CREATE INDEX idx_api_keys_key_hash ON api_keys(key_hash) WHERE revoked_at IS NULL;

-- Job ownership: which API key created this job. Nullable so existing
-- rows from before this migration remain valid; all new jobs created
-- through the (now-required) authenticated API will always have this set.
ALTER TABLE jobs ADD COLUMN owner_key_id TEXT REFERENCES api_keys(id);
CREATE INDEX idx_jobs_owner_key_id ON jobs(owner_key_id);
