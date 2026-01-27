package repository

import (
	"context"
	"encoding/json"
	"fmt" // ← Add this
	"testing"
	"time"

	"github.com/dipak0000812/orchestrix/internal/job/model"
	"github.com/dipak0000812/orchestrix/internal/job/state"
)

// setupTestDB creates a connection pool for testing.
func setupTestDB(t *testing.T) *PostgresJobRepository {
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
