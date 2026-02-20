-- Create job dependencies table
CREATE TABLE IF NOT EXISTS job_dependencies (
    parent_job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    child_job_id  TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (parent_job_id, child_job_id),
    CHECK (parent_job_id <> child_job_id)
);

-- Fast lookup: "what are the parents of job X?"
CREATE INDEX IF NOT EXISTS idx_job_dependencies_child
    ON job_dependencies(child_job_id);

-- Fast lookup: "what are the children of job X?"
CREATE INDEX IF NOT EXISTS idx_job_dependencies_parent
    ON job_dependencies(parent_job_id);
