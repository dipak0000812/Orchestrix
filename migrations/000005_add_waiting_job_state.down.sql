-- A rollback is unsafe while waiting jobs exist because the old constraint
-- cannot represent them. Fail explicitly instead of silently losing work.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM jobs WHERE state = 'WAITING') THEN
        RAISE EXCEPTION 'cannot roll back WAITING state migration while WAITING jobs exist';
    END IF;
END
$$;

ALTER TABLE jobs DROP CONSTRAINT IF EXISTS valid_state;

ALTER TABLE jobs ADD CONSTRAINT valid_state CHECK (
    state IN ('PENDING', 'SCHEDULED', 'RUNNING', 'SUCCEEDED', 'FAILED', 'RETRYING', 'CANCELLED')
);
