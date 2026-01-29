package worker

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dipak0000812/orchestrix/internal/executor"
	"github.com/dipak0000812/orchestrix/internal/job/model" // ← Add this
	"github.com/dipak0000812/orchestrix/internal/job/repository"
	"github.com/dipak0000812/orchestrix/internal/job/service"
	"github.com/dipak0000812/orchestrix/internal/job/state"
	"github.com/dipak0000812/orchestrix/internal/scheduler"
)

// setupIntegrationTest creates a complete test environment.
func setupIntegrationTest(t *testing.T) (
	*service.JobService,
	*scheduler.Scheduler,
	*WorkerPool,
	chan *model.Job,
) {
	// Create database connection
	cfg := repository.DBConfig{
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

	pool, err := repository.NewConnectionPool(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Failed to create connection pool: %v", err)
	}

	// Clean up test data
	_, err = pool.Exec(context.Background(), "DELETE FROM jobs")
	if err != nil {
		t.Fatalf("Failed to clean test data: %v", err)
	}

	// Create repository
	repo := repository.NewPostgresJobRepository(pool)

	// Create job service
	stateMachine := state.NewStateMachine()
	idGen := service.NewULIDGenerator()
	retryConfig := service.DefaultRetryConfig()
	jobService := service.NewJobService(repo, stateMachine, idGen, retryConfig)

	// Create executor registry
	executors := executor.NewExecutorRegistry()
	executors.Register("demo_job", executor.NewDemoExecutor(100*time.Millisecond))
	executors.Register("failing_job", executor.NewFailingExecutor())

	// Create job channel
	jobChannel := make(chan *model.Job, 10)

	// Create scheduler
	sched := scheduler.NewScheduler(
		jobService,
		500*time.Millisecond, // Poll every 500ms
		5,                    // Batch size
		jobChannel,
	)

	// Create worker pool
	workers := NewWorkerPool(
		3, // 3 workers
		jobChannel,
		executors,
		jobService,
		5*time.Second, // Job timeout
	)

	return jobService, sched, workers, jobChannel
}

func TestIntegration_HappyPath(t *testing.T) {
	jobService, sched, workers, _ := setupIntegrationTest(t)
	ctx := context.Background()

	// Start scheduler and workers
	sched.Start()
	workers.Start()
	defer sched.Stop()
	defer workers.Stop()

	// Create a job
	payload, _ := json.Marshal(map[string]string{"message": "test"})
	job, err := jobService.CreateJob(ctx, "demo_job", payload)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Wait for job to complete (scheduler polls every 500ms, job takes 100ms)
	time.Sleep(2 * time.Second)

	// Verify job succeeded
	updated, err := jobService.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("Failed to get job: %v", err)
	}

	if updated.State != state.SUCCEEDED {
		t.Errorf("Expected SUCCEEDED, got %s", updated.State)
	}
}

func TestIntegration_JobFailsAndRetries(t *testing.T) {
	jobService, sched, workers, _ := setupIntegrationTest(t)
	ctx := context.Background()

	// Start scheduler and workers
	sched.Start()
	workers.Start()
	defer sched.Stop()
	defer workers.Stop()

	// Create a failing job
	payload, _ := json.Marshal(map[string]string{"test": "data"})
	job, err := jobService.CreateJob(ctx, "failing_job", payload)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Wait for job to fail and retry multiple times
	// Each attempt: schedule (500ms) + execute (instant fail) + retry delay
	time.Sleep(8 * time.Second)

	// Verify job failed after exhausting retries
	updated, err := jobService.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("Failed to get job: %v", err)
	}

	if updated.State != state.FAILED {
		t.Errorf("Expected FAILED, got %s", updated.State)
	}

	if updated.Attempt != 3 {
		t.Errorf("Expected attempt 3, got %d", updated.Attempt)
	}

	if updated.LastError == nil {
		t.Error("Expected error to be recorded")
	}
}

func TestIntegration_MultipleJobs(t *testing.T) {
	jobService, sched, workers, _ := setupIntegrationTest(t)
	ctx := context.Background()

	// Start scheduler and workers
	sched.Start()
	workers.Start()
	defer sched.Stop()
	defer workers.Stop()

	// Create 10 jobs
	numJobs := 10
	payload, _ := json.Marshal(map[string]string{"test": "data"})

	for i := 0; i < numJobs; i++ {
		_, err := jobService.CreateJob(ctx, "demo_job", payload)
		if err != nil {
			t.Fatalf("Failed to create job %d: %v", i, err)
		}
	}

	// Wait for all jobs to complete
	// 3 workers can process 3 jobs in parallel
	// 10 jobs / 3 workers = ~4 batches
	// Each job takes 100ms, so ~400ms total
	// Add scheduler polling time: 500ms
	time.Sleep(3 * time.Second)

	// Verify all jobs succeeded
	succeededJobs, err := jobService.ListJobsByState(ctx, state.SUCCEEDED, 20)
	if err != nil {
		t.Fatalf("Failed to list succeeded jobs: %v", err)
	}

	if len(succeededJobs) != numJobs {
		t.Errorf("Expected %d succeeded jobs, got %d", numJobs, len(succeededJobs))
	}
}
