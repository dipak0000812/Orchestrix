package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/dipak0000812/orchestrix/internal/job/model"
	"github.com/dipak0000812/orchestrix/internal/job/state"
)

// setupTestDB creates a connection pool for testing. Accepts testing.TB so
// it can be shared between *testing.T tests and *testing.B benchmarks.
func setupTestDB(t testing.TB) *PostgresJobRepository {
	cfg := DBConfig{
		Host:            "localhost",
		Port:            5434,
		User:            "orchestrix",
		Password:        "orchestrix_dev_password",
		Database:        "orchestrix_dev",
		SSLMode:         "disable",
		MaxConnections:  5,
		MinConnections:  1,
		MaxConnLifetime: 30 * time.Minute,
		MaxConnIdleTime: 5 * time.Minute,
	}

	pool, err := NewConnectionPool(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Failed to create connection pool: %v", err)
	}

	// Clean up test data before each test
	_, err = pool.Exec(context.Background(), "DELETE FROM jobs")
	if err != nil {
		t.Fatalf("Failed to clean test data: %v", err)
	}

	return NewPostgresJobRepository(pool)
}

func TestCreate(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	payload, _ := json.Marshal(map[string]string{"email": "test@example.com"})

	job := &model.Job{
		ID:          "test_job_1",
		Type:        "send_email",
		Payload:     payload,
		State:       state.PENDING,
		Attempt:     1,
		MaxAttempts: 3,
		CreatedAt:   time.Now(),
	}

	err := repo.Create(ctx, job)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify job was created
	retrieved, err := repo.GetByID(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if retrieved == nil {
		t.Fatal("Expected job to exist, got nil")
	}

	if retrieved.ID != job.ID {
		t.Errorf("ID mismatch: got %s, want %s", retrieved.ID, job.ID)
	}
}

func TestCreateWaitingJob(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	job := &model.Job{
		ID:          "waiting_job",
		Type:        "test",
		Payload:     []byte(`{"message":"waiting"}`),
		State:       state.WAITING,
		Attempt:     1,
		MaxAttempts: 3,
		CreatedAt:   time.Now(),
	}

	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("create WAITING job: %v", err)
	}
}

func TestClaimPendingJobs_RespectsRetryDeadline(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()
	now := time.Now()
	past := now.Add(-time.Second)
	future := now.Add(time.Hour)

	jobs := []*model.Job{
		{
			ID: "pending_job", Type: "test", Payload: []byte(`{}`), State: state.PENDING,
			Attempt: 1, MaxAttempts: 3, CreatedAt: now,
		},
		{
			ID: "due_retry", Type: "test", Payload: []byte(`{}`), State: state.RETRYING,
			Attempt: 2, MaxAttempts: 3, NextRunAt: &past, CreatedAt: now.Add(time.Millisecond),
		},
		{
			ID: "future_retry", Type: "test", Payload: []byte(`{}`), State: state.RETRYING,
			Attempt: 2, MaxAttempts: 3, NextRunAt: &future, CreatedAt: now.Add(2 * time.Millisecond),
		},
	}
	for _, job := range jobs {
		if err := repo.Create(ctx, job); err != nil {
			t.Fatalf("create %s: %v", job.ID, err)
		}
	}

	claimed, err := repo.ClaimPendingJobs(ctx, 10)
	if err != nil {
		t.Fatalf("claim jobs: %v", err)
	}
	if len(claimed) != 2 {
		t.Fatalf("claimed %d jobs, want 2", len(claimed))
	}

	claimedIDs := make(map[string]bool, len(claimed))
	for _, job := range claimed {
		claimedIDs[job.ID] = true
	}
	if !claimedIDs["pending_job"] || !claimedIDs["due_retry"] {
		t.Errorf("claimed IDs = %v, want pending_job and due_retry", claimedIDs)
	}
	if claimedIDs["future_retry"] {
		t.Error("future retry was claimed before its deadline")
	}

	futureJob, err := repo.GetByID(ctx, "future_retry")
	if err != nil {
		t.Fatalf("get future retry: %v", err)
	}
	if futureJob.State != state.RETRYING {
		t.Errorf("future retry state = %s, want RETRYING", futureJob.State)
	}
}

func TestRequeueScheduledJob(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()
	now := time.Now()
	job := &model.Job{
		ID:          "scheduled_job",
		Type:        "test",
		Payload:     []byte(`{}`),
		State:       state.SCHEDULED,
		Attempt:     1,
		MaxAttempts: 3,
		CreatedAt:   now,
		ScheduledAt: &now,
	}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("create scheduled job: %v", err)
	}

	if err := repo.RequeueScheduledJob(ctx, job.ID); err != nil {
		t.Fatalf("requeue scheduled job: %v", err)
	}

	updated, err := repo.GetByID(ctx, job.ID)
	if err != nil {
		t.Fatalf("get requeued job: %v", err)
	}
	if updated.State != state.PENDING || updated.ScheduledAt != nil {
		t.Errorf("requeued job = state=%s scheduled_at=%v, want PENDING and nil", updated.State, updated.ScheduledAt)
	}
}

func TestRecoverStaleScheduledJobs(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()
	now := time.Now()
	staleAt := now.Add(-time.Hour)
	freshAt := now.Add(-time.Second)
	for _, job := range []*model.Job{
		{
			ID: "stale_scheduled", Type: "test", Payload: []byte(`{}`), State: state.SCHEDULED,
			Attempt: 1, MaxAttempts: 3, CreatedAt: staleAt, ScheduledAt: &staleAt,
		},
		{
			ID: "fresh_scheduled", Type: "test", Payload: []byte(`{}`), State: state.SCHEDULED,
			Attempt: 1, MaxAttempts: 3, CreatedAt: freshAt, ScheduledAt: &freshAt,
		},
	} {
		if err := repo.Create(ctx, job); err != nil {
			t.Fatalf("create %s: %v", job.ID, err)
		}
	}

	recovered, err := repo.RecoverStaleScheduledJobs(ctx, now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("recover stale jobs: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered %d jobs, want 1", recovered)
	}

	stale, err := repo.GetByID(ctx, "stale_scheduled")
	if err != nil {
		t.Fatalf("get stale job: %v", err)
	}
	if stale.State != state.PENDING {
		t.Errorf("stale job state = %s, want PENDING", stale.State)
	}
	fresh, err := repo.GetByID(ctx, "fresh_scheduled")
	if err != nil {
		t.Fatalf("get fresh job: %v", err)
	}
	if fresh.State != state.SCHEDULED {
		t.Errorf("fresh job state = %s, want SCHEDULED", fresh.State)
	}
}

func TestGetByID_NotFound(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	job, err := repo.GetByID(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetByID returned error: %v", err)
	}

	if job != nil {
		t.Error("Expected nil for nonexistent job, got job")
	}
}

func TestUpdateState(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	payload, _ := json.Marshal(map[string]string{"task": "test"})

	job := &model.Job{
		ID:          "test_job_2",
		Type:        "test_task",
		Payload:     payload,
		State:       state.PENDING,
		Attempt:     1,
		MaxAttempts: 3,
		CreatedAt:   time.Now(),
	}

	// Create job
	err := repo.Create(ctx, job)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Update state
	err = repo.UpdateState(ctx, job.ID, state.SCHEDULED)
	if err != nil {
		t.Fatalf("UpdateState failed: %v", err)
	}

	// Verify state changed
	retrieved, _ := repo.GetByID(ctx, job.ID)
	if retrieved.State != state.SCHEDULED {
		t.Errorf("State not updated: got %s, want %s", retrieved.State, state.SCHEDULED)
	}
}

func TestUpdateIfState_RejectsStaleWrite(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	job := &model.Job{
		ID:          "state_conflict_job",
		Type:        "test",
		Payload:     []byte(`{}`),
		State:       state.PENDING,
		Attempt:     1,
		MaxAttempts: 3,
		CreatedAt:   time.Now(),
	}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	staleCopy, err := repo.GetByID(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if err := repo.UpdateState(ctx, job.ID, state.CANCELLED); err != nil {
		t.Fatalf("cancel job: %v", err)
	}

	staleCopy.State = state.SCHEDULED
	if err := repo.UpdateIfState(ctx, staleCopy, state.PENDING); err == nil {
		t.Fatal("expected stale update to be rejected")
	}

	updated, err := repo.GetByID(ctx, job.ID)
	if err != nil {
		t.Fatalf("get updated job: %v", err)
	}
	if updated.State != state.CANCELLED {
		t.Errorf("state = %s, want CANCELLED", updated.State)
	}
}

func TestUpdateJobStateIfCurrent_DoesNotOverwriteCancellation(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	job := &model.Job{
		ID:          "dependency_state_conflict",
		Type:        "test",
		Payload:     []byte(`{}`),
		State:       state.WAITING,
		Attempt:     1,
		MaxAttempts: 3,
		CreatedAt:   time.Now(),
	}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := repo.UpdateState(ctx, job.ID, state.CANCELLED); err != nil {
		t.Fatalf("cancel job: %v", err)
	}

	updated, err := repo.UpdateJobStateIfCurrent(ctx, job.ID, state.WAITING, state.PENDING)
	if err != nil {
		t.Fatalf("conditional dependency transition: %v", err)
	}
	if updated {
		t.Fatal("stale dependency transition unexpectedly succeeded")
	}

	persisted, err := repo.GetByID(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if persisted.State != state.CANCELLED {
		t.Errorf("state = %s, want CANCELLED", persisted.State)
	}
}

func TestUpdateJobStateIfCurrent_CancelsWaitingJob(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()
	job := &model.Job{
		ID:          "dependency_cancel",
		Type:        "test",
		Payload:     []byte(`{}`),
		State:       state.WAITING,
		Attempt:     1,
		MaxAttempts: 3,
		CreatedAt:   time.Now(),
	}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	updated, err := repo.UpdateJobStateIfCurrent(ctx, job.ID, state.WAITING, state.CANCELLED)
	if err != nil {
		t.Fatalf("cancel dependency job: %v", err)
	}
	if !updated {
		t.Fatal("conditional cancellation did not update the waiting job")
	}

	persisted, err := repo.GetByID(ctx, job.ID)
	if err != nil {
		t.Fatalf("get cancelled job: %v", err)
	}
	if persisted.State != state.CANCELLED || persisted.CompletedAt == nil {
		t.Errorf("cancelled job = state=%s completed_at=%v, want CANCELLED with completion time", persisted.State, persisted.CompletedAt)
	}
}

func TestListByState(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	payload, _ := json.Marshal(map[string]string{"data": "test"})

	// Create 3 PENDING jobs
	for i := 1; i <= 3; i++ {
		job := &model.Job{
			ID:          fmt.Sprintf("test_job_%d", i),
			Type:        "test",
			Payload:     payload,
			State:       state.PENDING,
			Attempt:     1,
			MaxAttempts: 3,
			CreatedAt:   time.Now(),
		}
		repo.Create(ctx, job)
		time.Sleep(10 * time.Millisecond) // Ensure different created_at
	}

	// Create 1 RUNNING job
	runningJob := &model.Job{
		ID:          "running_job",
		Type:        "test",
		Payload:     payload,
		State:       state.RUNNING,
		Attempt:     1,
		MaxAttempts: 3,
		CreatedAt:   time.Now(),
	}
	repo.Create(ctx, runningJob)

	// List PENDING jobs
	pendingJobs, err := repo.ListByState(ctx, state.PENDING, 10)
	if err != nil {
		t.Fatalf("ListByState failed: %v", err)
	}

	if len(pendingJobs) != 3 {
		t.Errorf("Expected 3 PENDING jobs, got %d", len(pendingJobs))
	}

	// Verify ordering (oldest first)
	if pendingJobs[0].ID != "test_job_1" {
		t.Errorf("Jobs not ordered correctly, first job ID: %s", pendingJobs[0].ID)
	}
}

func TestUpdate(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	payload, _ := json.Marshal(map[string]string{"original": "data"})

	job := &model.Job{
		ID:          "test_job_update",
		Type:        "test",
		Payload:     payload,
		State:       state.PENDING,
		Attempt:     1,
		MaxAttempts: 3,
		CreatedAt:   time.Now(),
	}

	// Create job
	repo.Create(ctx, job)

	// Modify job
	job.Attempt = 2
	job.State = state.RETRYING
	errorMsg := "connection timeout"
	job.LastError = &errorMsg

	// Update
	err := repo.Update(ctx, job)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify changes
	retrieved, _ := repo.GetByID(ctx, job.ID)
	if retrieved.Attempt != 2 {
		t.Errorf("Attempt not updated: got %d, want 2", retrieved.Attempt)
	}
	if retrieved.State != state.RETRYING {
		t.Errorf("State not updated: got %s, want %s", retrieved.State, state.RETRYING)
	}
	if retrieved.LastError == nil || *retrieved.LastError != errorMsg {
		t.Errorf("LastError not updated correctly")
	}
}

func TestDelete(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	payload, _ := json.Marshal(map[string]string{"test": "delete"})

	job := &model.Job{
		ID:          "test_job_delete",
		Type:        "test",
		Payload:     payload,
		State:       state.PENDING,
		Attempt:     1,
		MaxAttempts: 3,
		CreatedAt:   time.Now(),
	}

	// Create job
	repo.Create(ctx, job)

	// Delete job
	err := repo.Delete(ctx, job.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify deleted
	retrieved, _ := repo.GetByID(ctx, job.ID)
	if retrieved != nil {
		t.Error("Job should be deleted, but still exists")
	}
}
