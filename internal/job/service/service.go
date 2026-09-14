package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dipak0000812/orchestrix/internal/job/dependency"
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
	resolver     *dependency.Resolver
	clock        clock
}

type clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now()
}

// NewJobService creates a new job service.
func NewJobService(
	repo repository.JobRepository,
	stateMachine *state.StateMachine,
	idGenerator IDGenerator,
	retryConfig RetryConfig,
	resolver *dependency.Resolver,
) *JobService {
	return &JobService{
		repo:         repo,
		stateMachine: stateMachine,
		idGenerator:  idGenerator,
		retryConfig:  retryConfig,
		resolver:     resolver,
		clock:        systemClock{},
	}
}

// CreateJob creates a new job with initial state PENDING.
func (s *JobService) CreateJob(ctx context.Context, ownerKeyID *string, jobType string, payload []byte, dependsOn []string) (*model.Job, error) {
	// Validate input
	if jobType == "" {
		return nil, fmt.Errorf("job type is required")
	}
	// Validate payload is valid JSON
	if len(payload) > 0 && !json.Valid(payload) {
		return nil, fmt.Errorf("payload must be valid JSON")
	}
	if len(dependsOn) > 0 && s.resolver == nil {
		return nil, fmt.Errorf("job dependencies are not configured")
	}
	// Generate unique ID
	id := s.idGenerator.Generate()

	// Jobs with dependencies start in WAITING state.
	// Jobs without dependencies start in PENDING state (ready immediately).
	initialState := state.PENDING
	if len(dependsOn) > 0 {
		initialState = state.WAITING
	}

	// Create job
	job := &model.Job{
		ID:          id,
		Type:        jobType,
		Payload:     payload,
		State:       initialState,
		Attempt:     1,
		MaxAttempts: 3,
		CreatedAt:   s.clock.Now(),
		OwnerKeyID:  ownerKeyID,
	}
	// Validate job
	if err := job.Validate(); err != nil {
		return nil, fmt.Errorf("job validation failed: %w", err)
	}
	// Save to repository
	if err := s.repo.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	// Register dependencies (includes cycle detection)
	if len(dependsOn) > 0 {
		if err := s.resolver.AddDependencies(ctx, job.ID, dependsOn); err != nil {
			// Dependency registration failed - delete the job we just created
			// to avoid orphaned WAITING jobs in the database
			_ = s.repo.Delete(ctx, job.ID)
			return nil, fmt.Errorf("failed to register dependencies: %w", err)
		}
		if err := s.resolver.ActivateIfReady(ctx, job.ID); err != nil {
			_ = s.repo.Delete(ctx, job.ID)
			return nil, fmt.Errorf("failed to activate dependency job: %w", err)
		}
		persistedJob, err := s.repo.GetByID(ctx, job.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to reload dependency job: %w", err)
		}
		if persistedJob == nil {
			return nil, fmt.Errorf("dependency job disappeared after creation: %s", job.ID)
		}
		job.State = persistedJob.State
		job.DependsOn = append([]string(nil), dependsOn...)
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
	if err := s.populateDependencies(ctx, job); err != nil {
		return nil, err
	}

	return job, nil
}

// maxListLimit caps how many jobs a single list request can return,
// regardless of what limit the caller asks for. Without this, an
// unbounded ?limit= lets a caller force the DB to return and the server
// to marshal an arbitrarily large result set on demand, repeatably.
const maxListLimit = 500

// ListJobsByState lists jobs in a specific state.
func (s *JobService) ListJobsByState(ctx context.Context, jobState state.State, limit int) ([]*model.Job, error) {
	limit = clampListLimit(limit)

	jobs, err := s.repo.ListByState(ctx, jobState, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}
	for _, job := range jobs {
		if err := s.populateDependencies(ctx, job); err != nil {
			return nil, err
		}
	}

	return jobs, nil
}

// ListJobsByStateAndOwner lists jobs in a specific state, scoped to the
// given owner API key -- used by the public API so a caller only ever
// sees their own jobs.
func (s *JobService) ListJobsByStateAndOwner(ctx context.Context, jobState state.State, ownerKeyID string, limit int) ([]*model.Job, error) {
	limit = clampListLimit(limit)

	jobs, err := s.repo.ListByStateAndOwner(ctx, jobState, ownerKeyID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}
	for _, job := range jobs {
		if err := s.populateDependencies(ctx, job); err != nil {
			return nil, err
		}
	}

	return jobs, nil
}

func clampListLimit(limit int) int {
	if limit <= 0 {
		return 10 // Default limit
	}
	if limit > maxListLimit {
		return maxListLimit
	}
	return limit
}

// TransitionState transitions a job to a new state.
// Validates the transition using the state machine.
func (s *JobService) TransitionState(ctx context.Context, id string, newState state.State) error {
	// Get current job
	job, err := s.GetJob(ctx, id)
	if err != nil {
		return err
	}
	currentState := job.State

	// Validate transition
	if err := s.stateMachine.ValidateTransition(currentState, newState); err != nil {
		return fmt.Errorf("invalid state transition: %w", err)
	}

	// Update state
	job.State = newState

	// Update timestamps based on new state
	now := s.clock.Now()
	switch newState {
	case state.SCHEDULED:
		job.ScheduledAt = &now
	case state.RUNNING:
		job.StartedAt = &now
	case state.SUCCEEDED, state.FAILED, state.CANCELLED:
		job.CompletedAt = &now
	}

	// Save changes
	if err := s.repo.UpdateIfState(ctx, job, currentState); err != nil {
		return fmt.Errorf("failed to update job state: %w", err)
	}

	if s.resolver == nil {
		return nil
	}

	switch newState {
	case state.SUCCEEDED:
		if err := s.resolver.OnJobSucceeded(ctx, job.ID); err != nil {
			return fmt.Errorf("activate dependent jobs: %w", err)
		}
	case state.FAILED:
		if err := s.resolver.OnJobFailed(ctx, job.ID); err != nil {
			return fmt.Errorf("cancel dependent jobs: %w", err)
		}
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

	// Decide the terminal or retry transition before mutating the job. This
	// prevents a caller from recording a retry against a non-running job.
	nextState := state.FAILED
	if job.CanRetry() {
		nextState = state.RETRYING
	}
	if err := s.stateMachine.ValidateTransition(job.State, nextState); err != nil {
		return fmt.Errorf("invalid failure transition: %w", err)
	}

	job.RecordError(failureErr)
	now := s.clock.Now()
	if nextState == state.RETRYING {
		// The delay is based on the failed attempt, not the next attempt.
		// Attempt 1 therefore waits BaseDelay before attempt 2.
		delay := s.retryConfig.CalculateBackoff(job.Attempt)
		job.IncrementAttempt()
		job.State = state.RETRYING
		nextRunAt := now.Add(delay)
		job.NextRunAt = &nextRunAt
	} else {
		job.State = state.FAILED
		job.NextRunAt = nil
		job.CompletedAt = &now
	}

	// Save changes
	if err := s.repo.UpdateIfState(ctx, job, state.RUNNING); err != nil {
		return fmt.Errorf("failed to update job after failure: %w", err)
	}

	if job.State == state.FAILED && s.resolver != nil {
		if err := s.resolver.OnJobFailed(ctx, job.ID); err != nil {
			return fmt.Errorf("cancel dependent jobs: %w", err)
		}
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
	currentState := job.State

	// Check if job is already terminal
	if job.IsTerminal() {
		return fmt.Errorf("cannot cancel job in terminal state: %s", job.State)
	}

	// Validate transition to CANCELLED
	if err := s.stateMachine.ValidateTransition(currentState, state.CANCELLED); err != nil {
		return fmt.Errorf("cannot cancel job: %w", err)
	}

	// Transition to CANCELLED
	job.State = state.CANCELLED
	job.NextRunAt = nil
	now := s.clock.Now()
	job.CompletedAt = &now

	// Save changes
	if err := s.repo.UpdateIfState(ctx, job, currentState); err != nil {
		return fmt.Errorf("failed to cancel job: %w", err)
	}

	return nil
}

func (s *JobService) populateDependencies(ctx context.Context, job *model.Job) error {
	if s.resolver == nil {
		return nil
	}

	parents, err := s.resolver.GetParents(ctx, job.ID)
	if err != nil {
		return fmt.Errorf("failed to load job dependencies: %w", err)
	}
	job.DependsOn = parents
	return nil
}
