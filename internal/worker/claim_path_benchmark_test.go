package worker

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dipak0000812/orchestrix/internal/executor"
	"github.com/dipak0000812/orchestrix/internal/job/dependency"
	"github.com/dipak0000812/orchestrix/internal/job/model"
	"github.com/dipak0000812/orchestrix/internal/job/repository"
	"github.com/dipak0000812/orchestrix/internal/job/service"
	"github.com/dipak0000812/orchestrix/internal/job/state"
	"github.com/dipak0000812/orchestrix/internal/metrics"
	"github.com/dipak0000812/orchestrix/internal/scheduler"
)

func claimPathDB(t *testing.T) (*repository.PostgresJobRepository, *pgxpool.Pool) {
	t.Helper()
	port, err := strconv.Atoi(envOrDefault("CLAIM_PATH_DB_PORT", "5435"))
	if err != nil {
		t.Fatalf("parse CLAIM_PATH_DB_PORT: %v", err)
	}
	pool, err := repository.NewConnectionPool(context.Background(), repository.DBConfig{
		Host:            envOrDefault("CLAIM_PATH_DB_HOST", "localhost"),
		Port:            port,
		User:            envOrDefault("CLAIM_PATH_DB_USER", "orchestrix"),
		Password:        envOrDefault("CLAIM_PATH_DB_PASSWORD", "orchestrix_dev_password"),
		Database:        envOrDefault("CLAIM_PATH_DB_NAME", "orchestrix_dev"),
		SSLMode:         "disable",
		MaxConnections:  10,
		MinConnections:  1,
		MaxConnLifetime: 30 * time.Minute,
		MaxConnIdleTime: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("connect benchmark database: %v", err)
	}
	t.Cleanup(func() { repository.ClosePool(pool) })
	return repository.NewPostgresJobRepository(pool), pool
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func TestClaimPathFullCallBenchmark(t *testing.T) {
	if os.Getenv("RUN_CLAIM_PATH_BENCHMARK") == "" {
		t.Skip("set RUN_CLAIM_PATH_BENCHMARK=1 to time full claim calls")
	}

	repo, _ := claimPathDB(t)
	runs, err := strconv.Atoi(envOrDefault("CLAIM_PATH_RUNS", "30"))
	if err != nil || runs < 1 {
		t.Fatalf("CLAIM_PATH_RUNS must be a positive integer")
	}
	batchSize, err := strconv.Atoi(envOrDefault("CLAIM_PATH_BATCH_SIZE", "50"))
	if err != nil || batchSize < 1 {
		t.Fatalf("CLAIM_PATH_BATCH_SIZE must be a positive integer")
	}

	ctx := context.Background()
	durations := make([]time.Duration, 0, runs)
	for run := 1; run <= runs; run++ {
		start := time.Now()
		jobs, err := repo.ClaimPendingJobs(ctx, batchSize)
		duration := time.Since(start)
		if err != nil {
			t.Fatalf("claim run %d: %v", run, err)
		}
		if len(jobs) == 0 {
			t.Fatalf("claim run %d returned no jobs", run)
		}
		durations = append(durations, duration)
		fmt.Printf("CLAIM_SAMPLE cache=%s run=%d claimed=%d duration_ns=%d\n",
			envOrDefault("CLAIM_PATH_CACHE", "unspecified"), run, len(jobs), duration.Nanoseconds())
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	median := durations[len(durations)/2]
	if len(durations)%2 == 0 {
		median = durations[len(durations)/2-1] + (durations[len(durations)/2]-durations[len(durations)/2-1])/2
	}
	p95 := durations[(95*len(durations)+99)/100-1]
	fmt.Printf("CLAIM_SUMMARY cache=%s runs=%d batch=%d p50=%s p95=%s\n",
		envOrDefault("CLAIM_PATH_CACHE", "unspecified"), runs, batchSize,
		median, p95)
}

func TestClaimPathSingleSchedulerDrain(t *testing.T) {
	if os.Getenv("RUN_CLAIM_PATH_DRAIN") == "" {
		t.Skip("set RUN_CLAIM_PATH_DRAIN=1 to drain the seeded pending jobs")
	}

	repo, pool := claimPathDB(t)
	ctx := context.Background()
	const expected = 10000
	var pending int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE id LIKE 'claim_pending_%' AND state = 'PENDING'").Scan(&pending); err != nil {
		t.Fatalf("count seeded pending jobs: %v", err)
	}
	if pending != expected {
		t.Fatalf("found %d seeded pending jobs, want %d; reseed before the drain", pending, expected)
	}

	jobChannel := make(chan *model.Job, 500)
	executors := executor.NewExecutorRegistry()
	executors.Register("compute_checksum", executor.NewChecksumExecutor())
	jobService := service.NewJobService(
		repo,
		state.NewStateMachine(),
		service.NewULIDGenerator(),
		service.DefaultRetryConfig(),
		dependency.NewResolver(repo),
	)
	workers := NewWorkerPool(5, jobChannel, executors, jobService, metrics.NewMetrics(), 30*time.Second)
	sched := scheduler.NewScheduler(repo, 10*time.Millisecond, 50, jobChannel)
	workers.Start()
	start := time.Now()
	sched.Start()
	deadline := start.Add(10 * time.Minute)
	var terminal int
	for time.Now().Before(deadline) {
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE id LIKE 'claim_pending_%' AND state IN ('SUCCEEDED', 'FAILED')").Scan(&terminal); err != nil {
			sched.Stop()
			workers.Stop()
			t.Fatalf("count terminal benchmark jobs: %v", err)
		}
		if terminal == expected {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	duration := time.Since(start)
	sched.Stop()
	workers.Stop()
	if terminal != expected {
		t.Fatalf("drain timed out at %d/%d terminal jobs", terminal, expected)
	}
	fmt.Printf("DRAIN_SUMMARY schedulers=1 workers=5 batch=50 poll=10ms jobs=%d duration=%s jobs_per_second=%.2f\n",
		terminal, duration, float64(terminal)/duration.Seconds())
}
