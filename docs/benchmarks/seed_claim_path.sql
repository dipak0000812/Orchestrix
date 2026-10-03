BEGIN;

TRUNCATE TABLE jobs CASCADE;

INSERT INTO jobs (
    id, type, payload, state, attempt, max_attempts, created_at, completed_at
)
SELECT
    'claim_history_' || lpad(job_number::text, 7, '0'),
    'compute_checksum',
    '{"data":"historical-completed-job"}'::jsonb,
    'SUCCEEDED',
    1,
    3,
    TIMESTAMP '2020-01-01 00:00:00' + job_number * INTERVAL '1 second',
    TIMESTAMP '2020-01-01 00:00:01' + job_number * INTERVAL '1 second'
FROM generate_series(1, 990000) AS job_number;

INSERT INTO jobs (
    id, type, payload, state, attempt, max_attempts, created_at
)
SELECT
    'claim_pending_' || lpad(job_number::text, 5, '0'),
    'compute_checksum',
    '{"data":"claim-path-benchmark"}'::jsonb,
    'PENDING',
    1,
    3,
    TIMESTAMP '2026-10-03 00:00:00' + job_number * INTERVAL '1 millisecond'
FROM generate_series(1, 10000) AS job_number;

ANALYZE jobs;

COMMIT;