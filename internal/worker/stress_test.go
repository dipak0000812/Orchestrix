package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/dipak0000812/orchestrix/internal/executor"
	"github.com/dipak0000812/orchestrix/internal/job/dependency"
	"github.com/dipak0000812/orchestrix/internal/job/model"
	"github.com/dipak0000812/orchestrix/internal/job/repository"
	"github.com/dipak0000812/orchestrix/internal/job/service"
	"github.com/dipak0000812/orchestrix/internal/job/state"
	"github.com/dipak0000812/orchestrix/internal/metrics"
	"github.com/dipak0000812/orchestrix/internal/scheduler"
)

// TestStress_BacklogDrain measures how long the real scheduler+worker pool
// takes to drain a large pre-existing backlog of PENDING jobs, using the
// same configuration cmd/server/main.go actually starts with (poll=1s,
// batch=10, 5 workers, 10s job timeout), not tuned-up test values.
//
// This is skipped by default because at production poll/batch settings,
// draining even a moderate backlog takes real wall-clock minutes (that's
// the finding, not an inconvenience to work around). Run explicitly with:
//
//	STRESS_JOB_COUNT=2000 go test ./internal/worker/... -run TestStress_BacklogDrain -v -timeout 30m
func TestStress_BacklogDrain(t *testing.T) {
	countStr := os.Getenv("STRESS_JOB_COUNT")
	if countStr == "" {
		t.Skip("set STRESS_JOB_COUNT=<n> to run this stress test (it uses production scheduler config: batch=10, poll=1s, so drain time is real wall-clock minutes for large n)")
	}
	jobCount, err := strconv.Atoi(countStr)
	if err != nil || jobCount <= 0 {
		t.Fatalf("invalid STRESS_JOB_COUNT=%q", countStr)
	}

	repo := connectStressTestDB(t)
	ctx := context.Background()

	// Bulk-insert the backlog directly via the repository, bypassing HTTP
	// entirely, so this measures scheduler/worker throughput in isolation
	// from anything already measured in the k6 load test.
	runNonce := time.Now().UnixNano()
	checksumPayload, _ := json.Marshal(struct {
		Data       string `json:"data"`
		WorkFactor int    `json:"work_factor"`
	}{Data: "stress", WorkFactor: 1})

	insertStart := time.Now()
	for i := 0; i < jobCount; i++ {
		job := &model.Job{
			ID:          fmt.Sprintf("stress_%d_%d", runNonce, i),
			Type:        "compute_checksum",
			Payload:     checksumPayload,
			State:       state.PENDING,
			Attempt:     1,
			MaxAttempts: 3,
			CreatedAt:   time.Now(),
		}
		if err := repo.Create(ctx, job); err != nil {
			t.Fatalf("bulk insert failed at job %d: %v", i, err)
		}
	}
	insertDuration := time.Since(insertStart)
	t.Logf("MEASURED: bulk-inserted %d jobs in %v (%.1f inserts/sec)",
		jobCount, insertDuration, float64(jobCount)/insertDuration.Seconds())

	// Production-matching configuration -- this is the whole point of the
	// test, so it deliberately does NOT use the faster settings the other
	// integration tests use.
	jobChannel := make(chan *model.Job, 100)
	m := metrics.NewMetrics()
	executors := executor.NewExecutorRegistry()
	executors.Register("compute_checksum", executor.NewChecksumExecutor())
	stateMachine := state.NewStateMachine()
	idGen := service.NewULIDGenerator()
	retryConfig := service.DefaultRetryConfig()
	jobService := service.NewJobService(repo, stateMachine, idGen, retryConfig, dependency.NewResolver(repo))

	sched := scheduler.NewScheduler(repo, 1*time.Second, 10, jobChannel)
	workers := NewWorkerPool(5, jobChannel, executors, jobService, m, 10*time.Second)

	drainStart := time.Now()
	sched.Start()
	workers.Start()

	var succeeded, failed, pending int
	pollInterval := 2 * time.Second
	maxWait := 30 * time.Minute
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		succeeded, failed = countTerminalStates(t, repo, ctx)
		pending = jobCount - succeeded - failed
		if succeeded+failed == jobCount {
			break
		}
		time.Sleep(pollInterval)
	}
	drainDuration := time.Since(drainStart)

	sched.Stop()
	workers.Stop()

	throughput := float64(succeeded+failed) / drainDuration.Seconds()
	t.Logf("MEASURED: drained %d/%d jobs in %v (%.2f jobs/sec effective throughput). succeeded=%d failed=%d still_pending_or_running=%d",
		succeeded+failed, jobCount, drainDuration, throughput, succeeded, failed, pending)

	if pending > 0 {
		t.Errorf("%d jobs still not terminal after %v (deadline exceeded); backlog did not fully drain", pending, maxWait)
	}
}

// countTerminalStates queries the actual current count of SUCCEEDED and
// FAILED jobs directly from Postgres via two aggregate COUNT queries. This
// deliberately does not fetch row data -- at stress-test scale, pulling
// every matching job's full payload (as ListByState would) on every poll
// would itself add meaningful memory/DB load and distort the very
// throughput number being measured.
func countTerminalStates(t *testing.T, repo *repository.PostgresJobRepository, ctx context.Context) (succeeded, failed int) {
	t.Helper()
	s, err := repo.CountByState(ctx, state.SUCCEEDED)
	if err != nil {
		t.Logf("countTerminalStates: CountByState(SUCCEEDED) failed: %v", err)
	}
	f, err := repo.CountByState(ctx, state.FAILED)
	if err != nil {
		t.Logf("countTerminalStates: CountByState(FAILED) failed: %v", err)
	}
	return s, f
}

// connectStressTestDB connects to the local test database and clears any
// existing job rows. Defined locally because the repository package's
// equivalent test helper is unexported test-only code in a different
// package and isn't importable from here.
func connectStressTestDB(t *testing.T) *repository.PostgresJobRepository {
	t.Helper()
	cfg := repository.DBConfig{
		Host:            "localhost",
		Port:            5434,
		User:            "orchestrix",
		Password:        "orchestrix_dev_password",
		Database:        "orchestrix_dev",
		SSLMode:         "disable",
		MaxConnections:  20,
		MinConnections:  2,
		MaxConnLifetime: 30 * time.Minute,
		MaxConnIdleTime: 5 * time.Minute,
	}
	pool, err := repository.NewConnectionPool(context.Background(), cfg)
	if err != nil {
		t.Fatalf("failed to create connection pool: %v", err)
	}
	t.Cleanup(func() { repository.ClosePool(pool) })
	if _, err := pool.Exec(context.Background(), "DELETE FROM jobs"); err != nil {
		t.Fatalf("failed to clean test data: %v", err)
	}
	return repository.NewPostgresJobRepository(pool)
}
