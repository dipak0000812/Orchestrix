package repository

import (
	"context"
	"errors"

	"github.com/dipak0000812/orchestrix/internal/job/model"
	"github.com/dipak0000812/orchestrix/internal/job/state"
)

// ErrStateConflict indicates that another actor changed a job after it was
// read. Callers must not overwrite the newer state with stale data.
var ErrStateConflict = errors.New("job state changed concurrently")

// JobRepository defines the contract for job data persistence.
// Any storage backend (PostgreSQL, MySQL, MongoDB, in-memory) must implement this interface.
//
// WHY AN INTERFACE?
// - Testability: Can mock this in tests without needing real database
// - Flexibility: Swap database implementations without changing business logic
// - Clarity: Explicitly defines what operations are available
type JobRepository interface {
	// Create inserts a new job into the repository.
	// Returns an error if the job ID already exists or if validation fails.
	Create(ctx context.Context, job *model.Job) error

	// GetByID retrieves a job by its unique identifier.
	// Returns nil if the job doesn't exist.
	GetByID(ctx context.Context, id string) (*model.Job, error)

	// UpdateState changes the state of a job.
	// This is the most frequent operation (every state transition).
	UpdateState(ctx context.Context, id string, newState state.State) error

	// ListByState returns all jobs in a specific state, ordered by creation time.
	// Used by scheduler to find PENDING jobs, workers to find SCHEDULED jobs, etc.
	// Limit controls how many jobs to return (pagination).
	ListByState(ctx context.Context, state state.State, limit int) ([]*model.Job, error)

	// ListByStateAndOwner returns jobs in a specific state AND owned by the
	// given API key, ordered by creation time. Used by the public list-jobs
	// API endpoint to scope results to the calling key -- filtering by
	// owner in SQL (rather than fetching via ListByState and filtering the
	// result in application code) avoids silently under-returning a
	// caller's own jobs when LIMIT is reached before their rows are found.
	ListByStateAndOwner(ctx context.Context, state state.State, ownerKeyID string, limit int) ([]*model.Job, error)

	// Update modifies an existing job's fields (except ID).
	// Used for updating attempt count, error messages, timestamps, etc.
	Update(ctx context.Context, job *model.Job) error

	// UpdateIfState updates a job only while it remains in expectedState.
	// It is the write-side guard for lifecycle transitions and cancellation.
	UpdateIfState(ctx context.Context, job *model.Job, expectedState state.State) error

	// Delete removes a job from the repository (soft delete in production).
	// Mainly for testing and cleanup. Production might use soft deletes instead.
	Delete(ctx context.Context, id string) error
}
