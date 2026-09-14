package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dipak0000812/orchestrix/internal/job/dependency"
	"github.com/dipak0000812/orchestrix/internal/job/model"
	"github.com/dipak0000812/orchestrix/internal/job/state"
)

// Mock ID Generator (for predictable tests)
type mockIDGenerator struct {
	nextID string
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

func (m *mockIDGenerator) Generate() string {
	return m.nextID
}

// Mock Repository (in-memory, for unit tests)
type mockRepository struct {
	jobs     map[string]*model.Job
	parents  map[string][]string
	children map[string][]string
}

func newMockRepository() *mockRepository {
	return &mockRepository{
		jobs:     make(map[string]*model.Job),
		parents:  make(map[string][]string),
		children: make(map[string][]string),
	}
}

func cloneJob(job *model.Job) *model.Job {
	clone := *job
	clone.DependsOn = append([]string(nil), job.DependsOn...)
	return &clone
}

func (r *mockRepository) Create(ctx context.Context, job *model.Job) error {
	if _, exists := r.jobs[job.ID]; exists {
		return errors.New("job already exists")
	}
	r.jobs[job.ID] = cloneJob(job)
	return nil
}

func (r *mockRepository) GetByID(ctx context.Context, id string) (*model.Job, error) {
	job, exists := r.jobs[id]
	if !exists {
		return nil, nil
	}
	return cloneJob(job), nil
}

func (r *mockRepository) Update(ctx context.Context, job *model.Job) error {
	if _, exists := r.jobs[job.ID]; !exists {
		return errors.New("job not found")
	}
	r.jobs[job.ID] = cloneJob(job)
	return nil
}

func (r *mockRepository) UpdateIfState(_ context.Context, job *model.Job, expectedState state.State) error {
	stored, exists := r.jobs[job.ID]
	if !exists {
		return errors.New("job not found")
	}
	if stored.State != expectedState {
		return fmt.Errorf("state conflict: expected %s, got %s", expectedState, stored.State)
	}
	r.jobs[job.ID] = cloneJob(job)
	return nil
}

func (r *mockRepository) UpdateState(ctx context.Context, id string, newState state.State) error {
	job, exists := r.jobs[id]
	if !exists {
		return errors.New("job not found")
	}
	job.State = newState
	return nil
}

func (r *mockRepository) ListByState(ctx context.Context, jobState state.State, limit int) ([]*model.Job, error) {
	var jobs []*model.Job
	for _, job := range r.jobs {
		if job.State == jobState {
			jobs = append(jobs, job)
			if len(jobs) >= limit {
				break
			}
		}
	}
	return jobs, nil
}

func (r *mockRepository) ListByStateAndOwner(ctx context.Context, jobState state.State, ownerKeyID string, limit int) ([]*model.Job, error) {
	var jobs []*model.Job
	for _, job := range r.jobs {
		if job.State == jobState && job.OwnerKeyID != nil && *job.OwnerKeyID == ownerKeyID {
			jobs = append(jobs, job)
			if len(jobs) >= limit {
				break
			}
		}
	}
	return jobs, nil
}

func (r *mockRepository) Delete(ctx context.Context, id string) error {
	delete(r.jobs, id)
	return nil
}

func (r *mockRepository) GetParents(_ context.Context, id string) ([]string, error) {
	return append([]string(nil), r.parents[id]...), nil
}

func (r *mockRepository) GetChildren(_ context.Context, id string) ([]string, error) {
	return append([]string(nil), r.children[id]...), nil
}

func (r *mockRepository) AddDependency(_ context.Context, parentID, childID string) error {
	r.parents[childID] = append(r.parents[childID], parentID)
	r.children[parentID] = append(r.children[parentID], childID)
	return nil
}

func (r *mockRepository) UpdateJobStateIfCurrent(_ context.Context, id string, expectedState, newState state.State) (bool, error) {
	job, exists := r.jobs[id]
	if !exists || job.State != expectedState {
		return false, nil
	}
	job.State = newState
	return true, nil
}

// Test helper: create test service
func setupTestService() *JobService {
	repo := newMockRepository()
	stateMachine := state.NewStateMachine()
	idGen := &mockIDGenerator{nextID: "test_job_123"}
	retryConfig := DefaultRetryConfig()

	return NewJobService(repo, stateMachine, idGen, retryConfig, dependency.NewResolver(repo))
}

func TestHandleFailure_PersistsRetryDeadline(t *testing.T) {
	service := setupTestService()
	now := time.Date(2026, time.July, 18, 9, 0, 0, 0, time.UTC)
	service.clock = fixedClock{now: now}
	service.retryConfig = RetryConfig{
		BaseDelay: 2 * time.Second,
		MaxDelay:  time.Minute,
		MaxJitter: 0,
	}
	ctx := context.Background()

	job, err := service.CreateJob(ctx, nil, "test_job", []byte(`{"value":1}`), nil)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	for _, nextState := range []state.State{state.SCHEDULED, state.RUNNING} {
		if err := service.TransitionState(ctx, job.ID, nextState); err != nil {
			t.Fatalf("transition to %s: %v", nextState, err)
		}
	}

	if err := service.HandleFailure(ctx, job.ID, errors.New("temporary failure")); err != nil {
		t.Fatalf("handle failure: %v", err)
	}

	updated, err := service.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get retried job: %v", err)
	}
	if updated.State != state.RETRYING {
		t.Fatalf("state = %s, want RETRYING", updated.State)
	}
	if updated.NextRunAt == nil {
		t.Fatal("next run time was not persisted")
	}
	want := now.Add(2 * time.Second)
	if !updated.NextRunAt.Equal(want) {
		t.Errorf("next run at = %s, want %s", updated.NextRunAt, want)
	}
}

func TestHandleFailure_RejectsNonRunningJob(t *testing.T) {
	service := setupTestService()
	ctx := context.Background()

	job, err := service.CreateJob(ctx, nil, "test_job", []byte(`{"value":1}`), nil)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := service.HandleFailure(ctx, job.ID, errors.New("unexpected")); err == nil {
		t.Fatal("expected failure transition from PENDING to be rejected")
	}

	updated, err := service.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if updated.State != state.PENDING || updated.NextRunAt != nil {
		t.Errorf("job mutated after invalid failure transition: state=%s next_run_at=%v", updated.State, updated.NextRunAt)
	}
}

func TestDependencyJob_UnblocksAfterParentSucceeds(t *testing.T) {
	service := setupTestService()
	ctx := context.Background()
	payload := json.RawMessage(`{"message":"test"}`)

	service.idGenerator.(*mockIDGenerator).nextID = "parent"
	parent, err := service.CreateJob(ctx, nil, "demo_job", payload, nil)
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}

	service.idGenerator.(*mockIDGenerator).nextID = "child"
	child, err := service.CreateJob(ctx, nil, "demo_job", payload, []string{parent.ID})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if child.State != state.WAITING {
		t.Fatalf("child state = %s, want WAITING", child.State)
	}

	for _, nextState := range []state.State{state.SCHEDULED, state.RUNNING, state.SUCCEEDED} {
		if err := service.TransitionState(ctx, parent.ID, nextState); err != nil {
			t.Fatalf("transition parent to %s: %v", nextState, err)
		}
	}

	updatedChild, err := service.GetJob(ctx, child.ID)
	if err != nil {
		t.Fatalf("get child: %v", err)
	}
	if updatedChild.State != state.PENDING {
		t.Errorf("child state = %s, want PENDING", updatedChild.State)
	}
	if len(updatedChild.DependsOn) != 1 || updatedChild.DependsOn[0] != parent.ID {
		t.Errorf("child dependencies = %v, want [%s]", updatedChild.DependsOn, parent.ID)
	}
}

func TestDependencyJob_CancelsAfterParentFails(t *testing.T) {
	service := setupTestService()
	ctx := context.Background()
	payload := json.RawMessage(`{"message":"test"}`)

	service.idGenerator.(*mockIDGenerator).nextID = "parent"
	parent, err := service.CreateJob(ctx, nil, "demo_job", payload, nil)
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}

	service.idGenerator.(*mockIDGenerator).nextID = "child"
	child, err := service.CreateJob(ctx, nil, "demo_job", payload, []string{parent.ID})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	for _, nextState := range []state.State{state.SCHEDULED, state.RUNNING, state.FAILED} {
		if err := service.TransitionState(ctx, parent.ID, nextState); err != nil {
			t.Fatalf("transition parent to %s: %v", nextState, err)
		}
	}

	updatedChild, err := service.GetJob(ctx, child.ID)
	if err != nil {
		t.Fatalf("get child: %v", err)
	}
	if updatedChild.State != state.CANCELLED {
		t.Errorf("child state = %s, want CANCELLED", updatedChild.State)
	}
}

func TestDependencyJob_WithCompletedParentStartsPending(t *testing.T) {
	service := setupTestService()
	ctx := context.Background()
	payload := json.RawMessage(`{"message":"test"}`)

	service.idGenerator.(*mockIDGenerator).nextID = "parent"
	parent, err := service.CreateJob(ctx, nil, "demo_job", payload, nil)
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	for _, nextState := range []state.State{state.SCHEDULED, state.RUNNING, state.SUCCEEDED} {
		if err := service.TransitionState(ctx, parent.ID, nextState); err != nil {
			t.Fatalf("transition parent to %s: %v", nextState, err)
		}
	}

	service.idGenerator.(*mockIDGenerator).nextID = "child"
	child, err := service.CreateJob(ctx, nil, "demo_job", payload, []string{parent.ID})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if child.State != state.PENDING {
		t.Errorf("child state = %s, want PENDING", child.State)
	}
}

func TestCreateJob(t *testing.T) {
	service := setupTestService()
	ctx := context.Background()

	payload, _ := json.Marshal(map[string]string{"email": "user@example.com"})

	job, err := service.CreateJob(ctx, nil, "send_email", payload, nil)
	if err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}

	// Verify job properties
	if job.ID != "test_job_123" {
		t.Errorf("ID = %s, want test_job_123", job.ID)
	}

	if job.Type != "send_email" {
		t.Errorf("Type = %s, want send_email", job.Type)
	}

	if job.State != state.PENDING {
		t.Errorf("State = %s, want PENDING", job.State)
	}

	if job.Attempt != 1 {
		t.Errorf("Attempt = %d, want 1", job.Attempt)
	}
}

func TestCreateJob_EmptyType(t *testing.T) {
	service := setupTestService()
	ctx := context.Background()

	_, err := service.CreateJob(ctx, nil, "", []byte("{}"), nil)
	if err == nil {
		t.Error("Expected error for empty job type")
	}
}

func TestCreateJob_InvalidJSON(t *testing.T) {
	service := setupTestService()
	ctx := context.Background()

	_, err := service.CreateJob(ctx, nil, "test", []byte("{invalid json"), nil)
	if err == nil {
		t.Error("Expected error for invalid JSON payload")
	}
}

func TestGetJob(t *testing.T) {
	service := setupTestService()
	ctx := context.Background()

	// Create a job first
	payload, _ := json.Marshal(map[string]string{"test": "data"})
	created, _ := service.CreateJob(ctx, nil, "test_job", payload, nil)

	// Retrieve it
	retrieved, err := service.GetJob(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetJob failed: %v", err)
	}

	if retrieved.ID != created.ID {
		t.Errorf("Retrieved wrong job: got %s, want %s", retrieved.ID, created.ID)
	}
}

func TestGetJob_NotFound(t *testing.T) {
	service := setupTestService()
	ctx := context.Background()

	_, err := service.GetJob(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent job")
	}
}

func TestTransitionState(t *testing.T) {
	service := setupTestService()
	ctx := context.Background()

	// Create job in PENDING state
	payload, _ := json.Marshal(map[string]string{"test": "data"})
	job, _ := service.CreateJob(ctx, nil, "test_job", payload, nil)

	// Transition to SCHEDULED
	err := service.TransitionState(ctx, job.ID, state.SCHEDULED)
	if err != nil {
		t.Fatalf("TransitionState failed: %v", err)
	}

	// Verify state changed
	updated, _ := service.GetJob(ctx, job.ID)
	if updated.State != state.SCHEDULED {
		t.Errorf("State = %s, want SCHEDULED", updated.State)
	}

	// Verify ScheduledAt timestamp was set
	if updated.ScheduledAt == nil {
		t.Error("ScheduledAt should be set after transition to SCHEDULED")
	}
}

func TestTransitionState_Invalid(t *testing.T) {
	service := setupTestService()
	ctx := context.Background()

	// Create job in PENDING state
	payload, _ := json.Marshal(map[string]string{"test": "data"})
	job, _ := service.CreateJob(ctx, nil, "test_job", payload, nil)

	// Try invalid transition: PENDING → SUCCEEDED (must go through SCHEDULED, RUNNING)
	err := service.TransitionState(ctx, job.ID, state.SUCCEEDED)
	if err == nil {
		t.Error("Expected error for invalid state transition")
	}
}

func TestHandleFailure_CanRetry(t *testing.T) {
	service := setupTestService()
	ctx := context.Background()

	// Create job
	payload, _ := json.Marshal(map[string]string{"test": "data"})
	job, _ := service.CreateJob(ctx, nil, "test_job", payload, nil)

	// Transition to RUNNING (simulate job execution)
	service.TransitionState(ctx, job.ID, state.SCHEDULED)
	service.TransitionState(ctx, job.ID, state.RUNNING)

	// Simulate failure
	failureErr := errors.New("connection timeout")
	err := service.HandleFailure(ctx, job.ID, failureErr)
	if err != nil {
		t.Fatalf("HandleFailure failed: %v", err)
	}

	// Verify job transitioned to RETRYING
	updated, _ := service.GetJob(ctx, job.ID)
	if updated.State != state.RETRYING {
		t.Errorf("State = %s, want RETRYING", updated.State)
	}

	// Verify attempt was incremented
	if updated.Attempt != 2 {
		t.Errorf("Attempt = %d, want 2", updated.Attempt)
	}

	// Verify error was recorded
	if updated.LastError == nil || *updated.LastError != "connection timeout" {
		t.Error("LastError should be set to failure message")
	}
}

func TestHandleFailure_ExhaustedRetries(t *testing.T) {
	service := setupTestService()
	ctx := context.Background()

	// Create job
	payload, _ := json.Marshal(map[string]string{"test": "data"})
	job, _ := service.CreateJob(ctx, nil, "test_job", payload, nil)

	// Simulate 3 failed attempts (max_attempts = 3)
	service.TransitionState(ctx, job.ID, state.SCHEDULED)
	service.TransitionState(ctx, job.ID, state.RUNNING)

	// Re-fetch to pick up the RUNNING state set by the transitions above;
	// the local `job` variable is stale (still PENDING) and writing it back
	// via repo.Update would silently revert the job's persisted state.
	job, err := service.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob failed: %v", err)
	}

	// Manually set attempt to 3 (last attempt)
	job.Attempt = 3
	if err := service.repo.Update(ctx, job); err != nil {
		t.Fatalf("repo.Update failed: %v", err)
	}

	// Fail on last attempt
	failureErr := errors.New("permanent error")
	service.HandleFailure(ctx, job.ID, failureErr)

	// Verify job transitioned to FAILED (not RETRYING)
	updated, _ := service.GetJob(ctx, job.ID)
	if updated.State != state.FAILED {
		t.Errorf("State = %s, want FAILED", updated.State)
	}

	// Verify CompletedAt timestamp was set
	if updated.CompletedAt == nil {
		t.Error("CompletedAt should be set after permanent failure")
	}
}

func TestCancelJob(t *testing.T) {
	service := setupTestService()
	ctx := context.Background()

	// Create job
	payload, _ := json.Marshal(map[string]string{"test": "data"})
	job, _ := service.CreateJob(ctx, nil, "test_job", payload, nil)

	// Cancel it
	err := service.CancelJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("CancelJob failed: %v", err)
	}

	// Verify state is CANCELLED
	updated, _ := service.GetJob(ctx, job.ID)
	if updated.State != state.CANCELLED {
		t.Errorf("State = %s, want CANCELLED", updated.State)
	}

	// Verify CompletedAt timestamp was set
	if updated.CompletedAt == nil {
		t.Error("CompletedAt should be set after cancellation")
	}
}

func TestCancelJob_AlreadyTerminal(t *testing.T) {
	service := setupTestService()
	ctx := context.Background()

	// Create job and transition to SUCCEEDED (terminal)
	payload, _ := json.Marshal(map[string]string{"test": "data"})
	job, _ := service.CreateJob(ctx, nil, "test_job", payload, nil)
	service.TransitionState(ctx, job.ID, state.SCHEDULED)
	service.TransitionState(ctx, job.ID, state.RUNNING)
	service.TransitionState(ctx, job.ID, state.SUCCEEDED)

	// Try to cancel (should fail)
	err := service.CancelJob(ctx, job.ID)
	if err == nil {
		t.Error("Expected error when cancelling terminal job")
	}
}

func TestListJobsByState(t *testing.T) {
	service := setupTestService()
	ctx := context.Background()

	// Create multiple jobs in different states
	payload, _ := json.Marshal(map[string]string{"test": "data"})

	// Create 3 PENDING jobs
	for i := 1; i <= 3; i++ {
		service.idGenerator.(*mockIDGenerator).nextID = fmt.Sprintf("job_%d", i)
		service.CreateJob(ctx, nil, "test", payload, nil)
	}

	// Create 1 RUNNING job
	service.idGenerator.(*mockIDGenerator).nextID = "job_running"
	runningJob, _ := service.CreateJob(ctx, nil, "test", payload, nil)
	service.TransitionState(ctx, runningJob.ID, state.SCHEDULED)
	service.TransitionState(ctx, runningJob.ID, state.RUNNING)

	// List PENDING jobs
	pendingJobs, err := service.ListJobsByState(ctx, state.PENDING, 10)
	if err != nil {
		t.Fatalf("ListJobsByState failed: %v", err)
	}

	if len(pendingJobs) != 3 {
		t.Errorf("Expected 3 PENDING jobs, got %d", len(pendingJobs))
	}

	// List RUNNING jobs
	runningJobs, _ := service.ListJobsByState(ctx, state.RUNNING, 10)
	if len(runningJobs) != 1 {
		t.Errorf("Expected 1 RUNNING job, got %d", len(runningJobs))
	}
}

func TestCalculateBackoff(t *testing.T) {
	config := RetryConfig{
		BaseDelay: 2 * time.Second,
		MaxDelay:  1 * time.Minute,
		MaxJitter: 500 * time.Millisecond,
	}

	tests := []struct {
		attempt     int
		minExpected time.Duration
		maxExpected time.Duration
	}{
		{1, 2 * time.Second, 3 * time.Second},   // 2s + jitter
		{2, 4 * time.Second, 5 * time.Second},   // 4s + jitter
		{3, 8 * time.Second, 9 * time.Second},   // 8s + jitter
		{4, 16 * time.Second, 17 * time.Second}, // 16s + jitter
		{10, 1 * time.Minute, 61 * time.Second}, // Capped at 1m + jitter
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			delay := config.CalculateBackoff(tt.attempt)

			if delay < tt.minExpected || delay > tt.maxExpected {
				t.Errorf("Attempt %d: delay = %v, want between %v and %v",
					tt.attempt, delay, tt.minExpected, tt.maxExpected)
			}
		})
	}
}
