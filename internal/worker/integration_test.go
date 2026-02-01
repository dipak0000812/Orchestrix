package worker

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/dipak0000812/orchestrix/internal/executor"
	"github.com/dipak0000812/orchestrix/internal/job/model"
	"github.com/dipak0000812/orchestrix/internal/job/repository"
	"github.com/dipak0000812/orchestrix/internal/job/service"
	"github.com/dipak0000812/orchestrix/internal/job/state"
	"github.com/dipak0000812/orchestrix/internal/metrics"
	"github.com/dipak0000812/orchestrix/internal/scheduler"
)

// metricsOnce ensures metrics are only registered once across all tests.
// Prometheus panics if you register the same metric name twice.
var (
	testMetrics     *metrics.Metrics
	testMetricsOnce sync.Once
)

func getTestMetrics() *metrics.Metrics {
	testMetricsOnce.Do(func() {
		testMetrics = metrics.NewMetrics()
	})
	return testMetrics
}

// setupIntegrationTest creates a complete test environment.
func setupIntegrationTest(t *testing.T) (
	*service.JobService,
	*scheduler.Scheduler,
	*WorkerPool,
	chan *model.Job,
) {
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

	_, err = pool.Exec(context.Background(), "DELETE FROM jobs")
	if err != nil {
		t.Fatalf("Failed to clean test data: %v", err)
	}

	repo := repository.NewPostgresJobRepository(pool)

	stateMachine := state.NewStateMachine()
	idGen := service.NewULIDGenerator()
	retryConfig := service.DefaultRetryConfig()
	jobService := service.NewJobService(repo, stateMachine, idGen, retryConfig)

	executors := executor.NewExecutorRegistry()
	executors.Register("demo_job", executor.NewDemoExecutor(100*time.Millisecond))
	executors.Register("failing_job", executor.NewFailingExecutor())

	jobChannel := make(chan *model.Job, 10)

	// Use shared metrics instance — not NewMetrics() every time
	m := getTestMetrics()

	sched := scheduler.NewScheduler(
		repo,
		500*time.Millisecond,
		5,
		jobChannel,
	)

	workers := NewWorkerPool(
		3,
		jobChannel,
		executors,
		jobService,
		m,
		5*time.Second,
	)

	return jobService, sched, workers, jobChannel
}

func TestIntegration_HappyPath(t *testing.T) {
	jobService, sched, workers, _ := setupIntegrationTest(t)
	ctx := context.Background()

	sched.Start()
	workers.Start()
	defer sched.Stop()
	defer workers.Stop()

	payload, _ := json.Marshal(map[string]string{"message": "test"})
	job, err := jobService.CreateJob(ctx, "demo_job", payload)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	time.Sleep(2 * time.Second)

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

	sched.Start()
	workers.Start()
	defer sched.Stop()
	defer workers.Stop()

	payload, _ := json.Marshal(map[string]string{"test": "data"})
	job, err := jobService.CreateJob(ctx, "failing_job", payload)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	time.Sleep(8 * time.Second)

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

	sched.Start()
	workers.Start()
	defer sched.Stop()
	defer workers.Stop()

	numJobs := 10
	payload, _ := json.Marshal(map[string]string{"test": "data"})

	for i := 0; i < numJobs; i++ {
		_, err := jobService.CreateJob(ctx, "demo_job", payload)
		if err != nil {
			t.Fatalf("Failed to create job %d: %v", i, err)
		}
	}

	time.Sleep(3 * time.Second)

	succeededJobs, err := jobService.ListJobsByState(ctx, state.SUCCEEDED, 20)
	if err != nil {
		t.Fatalf("Failed to list succeeded jobs: %v", err)
	}

	if len(succeededJobs) != numJobs {
		t.Errorf("Expected %d succeeded jobs, got %d", numJobs, len(succeededJobs))
	}
}
