package executor

import (
	"context"
	"fmt"
)

// Executor defines the interface for job execution.
// Each job type (send_email, process_video, etc.) implements this interface.
type Executor interface {
	// Execute runs the job with the given payload.
	// Returns error if execution fails.
	Execute(ctx context.Context, payload []byte) error
}

// ExecutorRegistry maps job types to their executors.
type ExecutorRegistry struct {
	executors map[string]Executor
}

// NewExecutorRegistry creates a new executor registry.
func NewExecutorRegistry() *ExecutorRegistry {
	return &ExecutorRegistry{
		executors: make(map[string]Executor),
	}
}

// Register adds an executor for a specific job type.
func (r *ExecutorRegistry) Register(jobType string, executor Executor) {
	r.executors[jobType] = executor
}

// Get retrieves the executor for a job type.
// Returns error if executor not found.
func (r *ExecutorRegistry) Get(jobType string) (Executor, error) {
	executor, exists := r.executors[jobType]
	if !exists {
		return nil, fmt.Errorf("no executor registered for job type: %s", jobType)
	}
	return executor, nil
}

// Has checks if an executor is registered for a job type.
func (r *ExecutorRegistry) Has(jobType string) bool {
	_, exists := r.executors[jobType]
	return exists
}
