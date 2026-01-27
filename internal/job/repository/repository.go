package repository

import (
	"context"

	"github.com/dipak0000812/orchestrix/internal/job/model"
	"github.com/dipak0000812/orchestrix/internal/job/state"
)

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

	// Update modifies an existing job's fields (except ID).
	// Used for updating attempt count, error messages, timestamps, etc.
	Update(ctx context.Context, job *model.Job) error

	// Delete removes a job from the repository (soft delete in production).
	// Mainly for testing and cleanup. Production might use soft deletes instead.
	Delete(ctx context.Context, id string) error
}
