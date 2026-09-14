package repository

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dipak0000812/orchestrix/internal/job/model"
	"github.com/dipak0000812/orchestrix/internal/job/state"
)

// PostgresJobRepository implements JobRepository using PostgreSQL.
type PostgresJobRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresJobRepository creates a new PostgreSQL-backed job repository.
func NewPostgresJobRepository(pool *pgxpool.Pool) *PostgresJobRepository {
	return &PostgresJobRepository{
		pool: pool,
	}
}

// Create inserts a new job into the database.
func (r *PostgresJobRepository) Create(ctx context.Context, job *model.Job) error {
	query := `
		INSERT INTO jobs (
			id, type, payload, state, attempt, max_attempts, last_error,
			next_run_at, created_at, scheduled_at, started_at, completed_at,
			owner_key_id
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
		)
	`

	_, err := r.pool.Exec(
		ctx,
		query,
		job.ID,
		job.Type,
		job.Payload,
		job.State,
		job.Attempt,
		job.MaxAttempts,
		job.LastError,
		job.NextRunAt,
		job.CreatedAt,
		job.ScheduledAt,
		job.StartedAt,
		job.CompletedAt,
		job.OwnerKeyID,
	)

	if err != nil {
		return fmt.Errorf("failed to create job: %w", err)
	}

	return nil
}

// GetByID retrieves a job by its ID.
func (r *PostgresJobRepository) GetByID(ctx context.Context, id string) (*model.Job, error) {
	query := `
		SELECT 
			id, type, payload, state, attempt, max_attempts, last_error,
			next_run_at, created_at, scheduled_at, started_at, completed_at,
			owner_key_id
		FROM jobs
		WHERE id = $1
	`

	var job model.Job
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&job.ID,
		&job.Type,
		&job.Payload,
		&job.State,
		&job.Attempt,
		&job.MaxAttempts,
		&job.LastError,
		&job.NextRunAt,
		&job.CreatedAt,
		&job.ScheduledAt,
		&job.StartedAt,
		&job.CompletedAt,
		&job.OwnerKeyID,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // Job not found, return nil without error
		}
		return nil, fmt.Errorf("failed to get job by ID: %w", err)
	}

	return &job, nil
}

// UpdateState updates only the state field of a job.
func (r *PostgresJobRepository) UpdateState(ctx context.Context, id string, newState state.State) error {
	query := `
		UPDATE jobs
		SET state = $1
		WHERE id = $2
	`

	result, err := r.pool.Exec(ctx, query, newState, id)
	if err != nil {
		return fmt.Errorf("failed to update job state: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("job not found: %s", id)
	}

	return nil
}

// Update modifies all fields of an existing job.
func (r *PostgresJobRepository) Update(ctx context.Context, job *model.Job) error {
	query := `
		UPDATE jobs
		SET 
			type = $2,
			payload = $3,
			state = $4,
			attempt = $5,
			max_attempts = $6,
			last_error = $7,
			next_run_at = $8,
			created_at = $9,
			scheduled_at = $10,
			started_at = $11,
			completed_at = $12
		WHERE id = $1
	`

	result, err := r.pool.Exec(
		ctx,
		query,
		job.ID,
		job.Type,
		job.Payload,
		job.State,
		job.Attempt,
		job.MaxAttempts,
		job.LastError,
		job.NextRunAt,
		job.CreatedAt,
		job.ScheduledAt,
		job.StartedAt,
		job.CompletedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to update job: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("job not found: %s", job.ID)
	}

	return nil
}

// UpdateIfState writes a job only when its persisted state still matches the
// state observed by the caller. This prevents stale workers from overwriting a
// concurrent cancellation or another terminal transition.
func (r *PostgresJobRepository) UpdateIfState(ctx context.Context, job *model.Job, expectedState state.State) error {
	query := `
		UPDATE jobs
		SET
			type = $2,
			payload = $3,
			state = $4,
			attempt = $5,
			max_attempts = $6,
			last_error = $7,
			next_run_at = $8,
			created_at = $9,
			scheduled_at = $10,
			started_at = $11,
			completed_at = $12
		WHERE id = $1 AND state = $13
	`

	result, err := r.pool.Exec(
		ctx,
		query,
		job.ID,
		job.Type,
		job.Payload,
		job.State,
		job.Attempt,
		job.MaxAttempts,
		job.LastError,
		job.NextRunAt,
		job.CreatedAt,
		job.ScheduledAt,
		job.StartedAt,
		job.CompletedAt,
		expectedState,
	)
	if err != nil {
		return fmt.Errorf("conditionally update job: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("%w: job %s was no longer %s", ErrStateConflict, job.ID, expectedState)
	}
	return nil
}

// ListByState returns jobs with a specific state, ordered by creation time.
func (r *PostgresJobRepository) ListByState(ctx context.Context, jobState state.State, limit int) ([]*model.Job, error) {
	query := `
		SELECT 
			id, type, payload, state, attempt, max_attempts, last_error,
			next_run_at, created_at, scheduled_at, started_at, completed_at
		FROM jobs
		WHERE state = $1
		ORDER BY created_at ASC
		LIMIT $2
	`

	rows, err := r.pool.Query(ctx, query, jobState, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs by state: %w", err)
	}
	defer rows.Close()

	var jobs []*model.Job
	for rows.Next() {
		var job model.Job
		err := rows.Scan(
			&job.ID,
			&job.Type,
			&job.Payload,
			&job.State,
			&job.Attempt,
			&job.MaxAttempts,
			&job.LastError,
			&job.NextRunAt,
			&job.CreatedAt,
			&job.ScheduledAt,
			&job.StartedAt,
			&job.CompletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job: %w", err)
		}
		jobs = append(jobs, &job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating jobs: %w", err)
	}

	return jobs, nil
}

// ListByStateAndOwner returns jobs in a specific state, owned by the given
// API key, ordered by creation time. See interface doc comment for why
// this filters in SQL rather than in application code after ListByState.
func (r *PostgresJobRepository) ListByStateAndOwner(ctx context.Context, jobState state.State, ownerKeyID string, limit int) ([]*model.Job, error) {
	query := `
		SELECT 
			id, type, payload, state, attempt, max_attempts, last_error,
			next_run_at, created_at, scheduled_at, started_at, completed_at,
			owner_key_id
		FROM jobs
		WHERE state = $1 AND owner_key_id = $2
		ORDER BY created_at ASC
		LIMIT $3
	`

	rows, err := r.pool.Query(ctx, query, jobState, ownerKeyID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs by state and owner: %w", err)
	}
	defer rows.Close()

	var jobs []*model.Job
	for rows.Next() {
		var job model.Job
		err := rows.Scan(
			&job.ID,
			&job.Type,
			&job.Payload,
			&job.State,
			&job.Attempt,
			&job.MaxAttempts,
			&job.LastError,
			&job.NextRunAt,
			&job.CreatedAt,
			&job.ScheduledAt,
			&job.StartedAt,
			&job.CompletedAt,
			&job.OwnerKeyID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job: %w", err)
		}
		jobs = append(jobs, &job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating jobs: %w", err)
	}

	return jobs, nil
}

// CountByState returns the number of jobs currently in the given state,
// via a single aggregate COUNT query. Exists specifically so callers that
// only need a count (e.g. progress monitoring during a large backlog
// drain) aren't forced to fetch every matching row's full payload via
// ListByState -- at real scale (hundreds of thousands+ of terminal jobs),
// polling with ListByState would itself pull that much JSONB data into
// memory repeatedly, distorting whatever it's trying to measure.
func (r *PostgresJobRepository) CountByState(ctx context.Context, jobState state.State) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE state = $1`, jobState).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count jobs by state: %w", err)
	}
	return count, nil
}

// Delete removes a job from the database.
func (r *PostgresJobRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM jobs WHERE id = $1`

	result, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete job: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("job not found: %s", id)
	}

	return nil
}

// ClaimPendingJobs atomically claims pending jobs by locking and transitioning them to SCHEDULED.
// This prevents race conditions when multiple schedulers are running.
// ClaimPendingJobs atomically claims pending and retrying jobs by locking and transitioning them to SCHEDULED.
// This prevents race conditions when multiple schedulers are running.
func (r *PostgresJobRepository) ClaimPendingJobs(ctx context.Context, limit int) ([]*model.Job, error) {
	// Start a transaction - critical for holding the lock
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		// Rollback if we don't commit. pgx.ErrTxClosed is expected here on
		// the success path (the transaction was already committed), so it's
		// not logged; any other error means the rollback itself failed.
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			log.Printf("ClaimPendingJobs: failed to rollback transaction: %v", rbErr)
		}
	}()

	// Query with FOR UPDATE SKIP LOCKED to prevent race conditions.
	// PENDING jobs are immediately runnable. RETRYING jobs become runnable
	// only after their persisted retry deadline, evaluated by PostgreSQL's
	// clock.
	//
	// This runs as two separate queries (not a single `WHERE state = $1
	// OR (state = $2 AND next_run_at <= NOW())` clause, and not a SQL
	// UNION) merged in application code. Two things were tried and ruled
	// out empirically before landing here:
	//
	//  1. The original single-clause OR: measured at 321.8ms against a
	//     realistic 1M-row table (990k old completed jobs + 10k recent
	//     pending ones). The planner could not use the composite
	//     (state, created_at) index for an OR across two different state
	//     values combined with a global ORDER BY, so it fell back to
	//     scanning idx_jobs_created_at from the oldest row forward,
	//     filtering out all 990,000 non-matching historical rows before
	//     finding the 10 pending ones.
	//  2. A SQL UNION ALL of two independently-indexed branches, each
	//     with its own FOR UPDATE SKIP LOCKED: Postgres rejects this
	//     outright ("FOR UPDATE is not allowed with UNION/INTERSECT/
	//     EXCEPT"), even with the FOR UPDATE nested inside each branch's
	//     subquery.
	//
	// So: two plain, separately-executed `WHERE state = X ORDER BY
	// created_at LIMIT $N FOR UPDATE SKIP LOCKED` queries, each of which
	// the planner *can* satisfy directly from an index (confirmed via
	// EXPLAIN ANALYZE: 0.075ms for the equivalent single-state lookup at
	// the same 1M-row scale). Results are merged, sorted, and truncated
	// to `limit` below. Any locked-but-unselected rows from the union of
	// up to 2*limit candidates are simply not included in the UPDATE
	// further down, so their lock is released, unmodified, at commit.
	pendingQuery := `
		SELECT
			id, type, payload, state, attempt, max_attempts, last_error,
			next_run_at, created_at, scheduled_at, started_at, completed_at
		FROM jobs
		WHERE state = $1
		ORDER BY created_at ASC
		LIMIT $2
		FOR UPDATE SKIP LOCKED
	`
	retryingQuery := `
		SELECT
			id, type, payload, state, attempt, max_attempts, last_error,
			next_run_at, created_at, scheduled_at, started_at, completed_at
		FROM jobs
		WHERE state = $1 AND next_run_at <= NOW()
		ORDER BY created_at ASC
		LIMIT $2
		FOR UPDATE SKIP LOCKED
	`

	var candidates []*model.Job
	for _, q := range []struct {
		sql   string
		state state.State
	}{
		{pendingQuery, state.PENDING},
		{retryingQuery, state.RETRYING},
	} {
		rows, err := tx.Query(ctx, q.sql, q.state, limit)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s jobs: %w", q.state, err)
		}
		for rows.Next() {
			job := &model.Job{}
			if err := rows.Scan(
				&job.ID, &job.Type, &job.Payload, &job.State, &job.Attempt,
				&job.MaxAttempts, &job.LastError, &job.NextRunAt, &job.CreatedAt,
				&job.ScheduledAt, &job.StartedAt, &job.CompletedAt,
			); err != nil {
				rows.Close()
				return nil, fmt.Errorf("failed to scan job: %w", err)
			}
			candidates = append(candidates, job)
		}
		rowsErr := rows.Err()
		rows.Close()
		if rowsErr != nil {
			return nil, fmt.Errorf("error iterating %s rows: %w", q.state, rowsErr)
		}
	}

	// Merge the two independently-fetched (and already individually
	// created_at-ordered) result sets, then keep only the oldest `limit`
	// overall -- this is the step that would otherwise have needed the
	// SQL-level UNION + ORDER BY + LIMIT that isn't compatible with
	// FOR UPDATE.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	jobs := candidates

	// Collect the IDs of the jobs we're actually claiming (as opposed to
	// any extra locked-but-unselected candidates from the merge above).
	var jobIDs []string
	for _, job := range jobs {
		jobIDs = append(jobIDs, job.ID)
	}

	// If no jobs found, return empty slice (not an error)
	if len(jobs) == 0 {
		return []*model.Job{}, nil
	}

	// Update all claimed jobs to SCHEDULED state in a single query
	updateQuery := `
		UPDATE jobs
		SET state = $1, scheduled_at = $2, next_run_at = NULL
		WHERE id = ANY($3)
	`

	now := time.Now()
	_, err = tx.Exec(ctx, updateQuery, state.SCHEDULED, now, jobIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to update jobs to SCHEDULED: %w", err)
	}

	// Commit the transaction - this releases the locks
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Update the in-memory job objects to reflect the new state
	for _, job := range jobs {
		job.State = state.SCHEDULED
		job.ScheduledAt = &now
		job.NextRunAt = nil
	}

	return jobs, nil
}

// RequeueScheduledJob returns a claimed job to PENDING when it could not be
// delivered to the in-memory worker channel. The state predicate prevents an
// already-running job from being accidentally requeued.
func (r *PostgresJobRepository) RequeueScheduledJob(ctx context.Context, id string) error {
	query := `
		UPDATE jobs
		SET state = $1, scheduled_at = NULL
		WHERE id = $2 AND state = $3
	`
	result, err := r.pool.Exec(ctx, query, state.PENDING, id, state.SCHEDULED)
	if err != nil {
		return fmt.Errorf("requeue scheduled job: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("%w: job %s was no longer SCHEDULED", ErrStateConflict, id)
	}
	return nil
}

// RecoverStaleScheduledJobs returns jobs that were claimed but never picked up
// by a worker, for example after a process exits between DB claim and channel
// dispatch. RUNNING jobs are deliberately excluded because they require a
// heartbeat-based recovery protocol to reclaim safely.
func (r *PostgresJobRepository) RecoverStaleScheduledJobs(ctx context.Context, staleBefore time.Time) (int64, error) {
	query := `
		UPDATE jobs
		SET state = $1, scheduled_at = NULL
		WHERE state = $2 AND scheduled_at < $3
	`
	result, err := r.pool.Exec(ctx, query, state.PENDING, state.SCHEDULED, staleBefore)
	if err != nil {
		return 0, fmt.Errorf("recover stale scheduled jobs: %w", err)
	}
	return result.RowsAffected(), nil
}

// GetParents returns all parent job IDs for a given job.
func (r *PostgresJobRepository) GetParents(ctx context.Context, jobID string) ([]string, error) {
	query := `
		SELECT parent_job_id 
		FROM job_dependencies 
		WHERE child_job_id = $1
	`
	rows, err := r.pool.Query(ctx, query, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to get parents of %s: %w", jobID, err)
	}
	defer rows.Close()

	var parents []string
	for rows.Next() {
		var parentID string
		if err := rows.Scan(&parentID); err != nil {
			return nil, fmt.Errorf("failed to scan parent ID: %w", err)
		}
		parents = append(parents, parentID)
	}
	return parents, rows.Err()
}

// GetChildren returns all child job IDs for a given job.
func (r *PostgresJobRepository) GetChildren(ctx context.Context, jobID string) ([]string, error) {
	query := `
		SELECT child_job_id 
		FROM job_dependencies 
		WHERE parent_job_id = $1
	`
	rows, err := r.pool.Query(ctx, query, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to get children of %s: %w", jobID, err)
	}
	defer rows.Close()

	var children []string
	for rows.Next() {
		var childID string
		if err := rows.Scan(&childID); err != nil {
			return nil, fmt.Errorf("failed to scan child ID: %w", err)
		}
		children = append(children, childID)
	}
	return children, rows.Err()
}

// AddDependency creates a parent → child relationship in the database.
func (r *PostgresJobRepository) AddDependency(ctx context.Context, parentID, childID string) error {
	query := `
		INSERT INTO job_dependencies (parent_job_id, child_job_id)
		VALUES ($1, $2)
		ON CONFLICT (parent_job_id, child_job_id) DO NOTHING
	`
	_, err := r.pool.Exec(ctx, query, parentID, childID)
	if err != nil {
		return fmt.Errorf("failed to add dependency %s → %s: %w", parentID, childID, err)
	}
	return nil
}

// UpdateJobStateIfCurrent conditionally applies a dependency transition.
// Used by the dependency resolver to transition WAITING → PENDING or CANCELLED.
func (r *PostgresJobRepository) UpdateJobStateIfCurrent(ctx context.Context, jobID string, expectedState, newState state.State) (bool, error) {
	query := `
		UPDATE jobs
		SET state = $1,
			completed_at = CASE WHEN $1 = $4 THEN NOW() ELSE completed_at END
		WHERE id = $2 AND state = $3
	`
	result, err := r.pool.Exec(ctx, query, newState, jobID, expectedState, state.CANCELLED)
	if err != nil {
		return false, fmt.Errorf("conditionally update state of job %s to %s: %w", jobID, newState, err)
	}
	if result.RowsAffected() == 0 {
		return false, nil
	}
	return true, nil
}
