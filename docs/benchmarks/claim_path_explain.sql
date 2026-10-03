\echo 'BEFORE: a5fbf96^ single PENDING/RETRYING query'
BEGIN;
EXPLAIN (ANALYZE, BUFFERS)
SELECT
    id, type, payload, state, attempt, max_attempts, last_error,
    created_at, scheduled_at, started_at, completed_at
FROM jobs
WHERE state IN ('PENDING', 'RETRYING')
ORDER BY created_at ASC
LIMIT 50
FOR UPDATE SKIP LOCKED;
ROLLBACK;

\echo 'AFTER: a5fbf96 PENDING query'
BEGIN;
EXPLAIN (ANALYZE, BUFFERS)
SELECT
    id, type, payload, state, attempt, max_attempts, last_error,
    next_run_at, created_at, scheduled_at, started_at, completed_at
FROM jobs
WHERE state = 'PENDING'
ORDER BY created_at ASC
LIMIT 50
FOR UPDATE SKIP LOCKED;
ROLLBACK;

\echo 'AFTER: a5fbf96 due RETRYING query'
BEGIN;
EXPLAIN (ANALYZE, BUFFERS)
SELECT
    id, type, payload, state, attempt, max_attempts, last_error,
    next_run_at, created_at, scheduled_at, started_at, completed_at
FROM jobs
WHERE state = 'RETRYING' AND next_run_at <= NOW()
ORDER BY created_at ASC
LIMIT 50
FOR UPDATE SKIP LOCKED;
ROLLBACK;