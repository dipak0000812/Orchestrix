-- Create jobs table
CREATE TABLE IF NOT EXISTS jobs (
    -- Identity
    id TEXT PRIMARY KEY,
    
    -- Work definition
    type TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    
    -- State tracking
    state TEXT NOT NULL,
    attempt INTEGER NOT NULL DEFAULT 1,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    last_error TEXT,
    
    -- Timestamps
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    scheduled_at TIMESTAMP,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    
    -- Constraints
    CONSTRAINT valid_state CHECK (state IN ('PENDING', 'SCHEDULED', 'RUNNING', 'SUCCEEDED', 'FAILED', 'RETRYING', 'CANCELLED')),
    CONSTRAINT valid_attempts CHECK (attempt >= 1 AND attempt <= max_attempts),
    CONSTRAINT valid_max_attempts CHECK (max_attempts >= 1)
);

-- Index on state for efficient querying (scheduler needs this)
CREATE INDEX IF NOT EXISTS idx_jobs_state ON jobs(state);

-- Index on created_at for ordering
CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs(created_at);

-- Composite index for scheduler queries (state + created_at)
CREATE INDEX IF NOT EXISTS idx_jobs_state_created_at ON jobs(state, created_at);