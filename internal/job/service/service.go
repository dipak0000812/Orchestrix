package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dipak0000812/orchestrix/internal/job/model"
	"github.com/dipak0000812/orchestrix/internal/job/repository"
	"github.com/dipak0000812/orchestrix/internal/job/state"
)

// JobService handles job business logic.
// It orchestrates state machines, repositories, and retry logic.
type JobService struct {
	repo         repository.JobRepository
	stateMachine *state.StateMachine
	idGenerator  IDGenerator
	retryConfig  RetryConfig
}

// NewJobService creates a new job service.
func NewJobService(
	repo repository.JobRepository,
	stateMachine *state.StateMachine,
	idGenerator IDGenerator,
	retryConfig RetryConfig,
) *JobService {
	return &JobService{
		repo:         repo,
		stateMachine: stateMachine,
		idGenerator:  idGenerator,
		retryConfig:  retryConfig,
	}
}

// CreateJob creates a new job with initial state PENDING.
func (s *JobService) CreateJob(ctx context.Context, jobType string, payload []byte) (*model.Job, error) {
	// Validate input
	if jobType == "" {
		return nil, fmt.Errorf("job type is required")
	}

	// Validate payload is valid JSON
	if len(payload) > 0 && !json.Valid(payload) {
		return nil, fmt.Errorf("payload must be valid JSON")
	}

	// Generate unique ID
	id := s.idGenerator.Generate()

	// Create job with initial state
	job := &model.Job{
		ID:          id,
		Type:        jobType,
		Payload:     payload,
		State:       state.PENDING,
		Attempt:     1,
		MaxAttempts: 3, // Default, could be configurable
		CreatedAt:   time.Now(),
	}

	// Validate job
	if err := job.Validate(); err != nil {
		return nil, fmt.Errorf("job validation failed: %w", err)
	}

	// Save to repository
	if err := s.repo.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	return job, nil
}

// GetJob retrieves a job by ID.
func (s *JobService) GetJob(ctx context.Context, id string) (*model.Job, error) {
	job, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get job: %w", err)
	}

	if job == nil {
		return nil, fmt.Errorf("job not found: %s", id)
	}

	return job, nil
}

// ListJobsByState lists jobs in a specific state.
func (s *JobService) ListJobsByState(ctx context.Context, jobState state.State, limit int) ([]*model.Job, error) {
	if limit <= 0 {
		limit = 10 // Default limit
	}

	jobs, err := s.repo.ListByState(ctx, jobState, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	return jobs, nil
}

// TransitionState transitions a job to a new state.
// Validates the transition using the state machine.
func (s *JobService) TransitionState(ctx context.Context, id string, newState state.State) error {
	// Get current job
	job, err := s.GetJob(ctx, id)
	if err != nil {
		return err
	}

	// Validate transition
	if err := s.stateMachine.ValidateTransition(job.State, newState); err != nil {
		return fmt.Errorf("invalid state transition: %w", err)
	}

	// Update state
	job.State = newState

	// Update timestamps based on new state
	now := time.Now()
	switch newState {
	case state.SCHEDULED:
		job.ScheduledAt = &now
	case state.RUNNING:
		job.StartedAt = &now
	case state.SUCCEEDED, state.FAILED, state.CANCELLED:
		job.CompletedAt = &now
	}

	// Save changes
	if err := s.repo.Update(ctx, job); err != nil {
		return fmt.Errorf("failed to update job state: %w", err)
	}

	return nil
}

// HandleFailure handles a job failure, deciding whether to retry or fail permanently.
func (s *JobService) HandleFailure(ctx context.Context, id string, failureErr error) error {
	// Get current job
	job, err := s.GetJob(ctx, id)
	if err != nil {
		return err
	}

	// Record error
	job.RecordError(failureErr)

	// Decide: retry or fail permanently?
	if job.CanRetry() {
		// Increment attempt for next retry
		job.IncrementAttempt()

		// Transition to RETRYING
		job.State = state.RETRYING

		// Calculate backoff delay (for scheduler to use)
		// Note: We don't implement the delay here, just calculate it
		_ = s.retryConfig.CalculateBackoff(job.Attempt)
		// In Phase D, scheduler will use this delay

	} else {
		// Max attempts exhausted, fail permanently
		job.State = state.FAILED
		now := time.Now()
		job.CompletedAt = &now
	}

	// Save changes
	if err := s.repo.Update(ctx, job); err != nil {
		return fmt.Errorf("failed to update job after failure: %w", err)
	}

	return nil
}

// CancelJob cancels a job if it's in a cancellable state.
func (s *JobService) CancelJob(ctx context.Context, id string) error {
	// Get current job
	job, err := s.GetJob(ctx, id)
	if err != nil {
		return err
	}

	// Check if job is already terminal
	if job.IsTerminal() {
		return fmt.Errorf("cannot cancel job in terminal state: %s", job.State)
	}

	// Validate transition to CANCELLED
	if err := s.stateMachine.ValidateTransition(job.State, state.CANCELLED); err != nil {
		return fmt.Errorf("cannot cancel job: %w", err)
	}

	// Transition to CANCELLED
	job.State = state.CANCELLED
	now := time.Now()
	job.CompletedAt = &now

	// Save changes
	if err := s.repo.Update(ctx, job); err != nil {
		return fmt.Errorf("failed to cancel job: %w", err)
	}

	return nil
}
