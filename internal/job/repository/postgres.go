package repository

import (
	"context"
	"errors"
	"fmt"
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
			created_at, scheduled_at, started_at, completed_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
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
		job.CreatedAt,
		job.ScheduledAt,
		job.StartedAt,
		job.CompletedAt,
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
			created_at, scheduled_at, started_at, completed_at
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
		&job.CreatedAt,
		&job.ScheduledAt,
		&job.StartedAt,
		&job.CompletedAt,
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
			created_at = $8,
			scheduled_at = $9,
			started_at = $10,
			completed_at = $11
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

// ListByState returns jobs with a specific state, ordered by creation time.
func (r *PostgresJobRepository) ListByState(ctx context.Context, jobState state.State, limit int) ([]*model.Job, error) {
	query := `
		SELECT 
			id, type, payload, state, attempt, max_attempts, last_error,
			created_at, scheduled_at, started_at, completed_at
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
	defer tx.Rollback(ctx) // Rollback if we don't commit

	// Query with FOR UPDATE SKIP LOCKED to prevent race conditions
	// Pick up both PENDING (new jobs) and RETRYING (failed jobs ready to retry)
	query := `
		SELECT
			id, type, payload, state, attempt, max_attempts, last_error,
			created_at, scheduled_at, started_at, completed_at
		FROM jobs
		WHERE state IN ($1, $2)
		ORDER BY created_at ASC
		LIMIT $3
		FOR UPDATE SKIP LOCKED
	`

	rows, err := tx.Query(ctx, query, state.PENDING, state.RETRYING, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending jobs: %w", err)
	}
	defer rows.Close()

	// Collect the jobs and their IDs
	var jobs []*model.Job
	var jobIDs []string

	for rows.Next() {
		job := &model.Job{}
		err := rows.Scan(
			&job.ID,
			&job.Type,
			&job.Payload,
			&job.State,
			&job.Attempt,
			&job.MaxAttempts,
			&job.LastError,
			&job.CreatedAt,
			&job.ScheduledAt,
			&job.StartedAt,
			&job.CompletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job: %w", err)
		}

		jobs = append(jobs, job)
		jobIDs = append(jobIDs, job.ID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	// If no jobs found, return empty slice (not an error)
	if len(jobs) == 0 {
		return []*model.Job{}, nil
	}

	// Update all claimed jobs to SCHEDULED state in a single query
	updateQuery := `
		UPDATE jobs
		SET state = $1, scheduled_at = $2
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
	}

	return jobs, nil
}
