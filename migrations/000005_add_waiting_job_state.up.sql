-- WAITING is required for jobs whose dependencies have not all completed.
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS valid_state;

ALTER TABLE jobs ADD CONSTRAINT valid_state CHECK (
    state IN ('PENDING', 'WAITING', 'SCHEDULED', 'RUNNING', 'SUCCEEDED', 'FAILED', 'RETRYING', 'CANCELLED')
);
