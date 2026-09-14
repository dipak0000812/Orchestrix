package worker

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/dipak0000812/orchestrix/internal/job/model"
	"github.com/dipak0000812/orchestrix/internal/job/state"
	"github.com/dipak0000812/orchestrix/internal/scheduler"
)

// panicExecutor deliberately panics on every execution, to verify the
// worker pool's recover() actually protects the pool rather than only
// working by accident on the one job type that happens not to panic.
type panicExecutor struct{}

func (panicExecutor) Execute(ctx context.Context, payload []byte) error {
	panic("deliberate panic for reliability testing")
}

// hungExecutor deliberately ignores context cancellation and blocks on a
// real time.Sleep, simulating a misbehaving job (e.g. a blocking C binding
// or unbounded synchronous I/O call) that won't cooperate with shutdown.
// It exists to prove Stop()'s grace-period bound holds even in that case.
type hungExecutor struct{ sleep time.Duration }

func (h hungExecutor) Execute(ctx context.Context, payload []byte) error {
	time.Sleep(h.sleep)
	return nil
}

// pollUntil polls fn every interval until it returns true or the timeout
// elapses, returning the last observed error (if any) for a useful failure
// message. This mirrors real operational monitoring: we don't assert on
// exact timing, we assert on eventual, observed state.
func pollUntil(t *testing.T, timeout, interval time.Duration, fn func() (bool, error)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		ok, err := fn()
		if err != nil {
			lastErr = err
		}
		if ok {
			return
		}
		time.Sleep(interval)
	}
	t.Fatalf("condition not met within %v (last error: %v)", timeout, lastErr)
}

// TestReliability_WorkerPanicRecovery verifies that a job whose executor
// panics does not crash the worker goroutine or the pool, that the job
// itself lands in FAILED (panics are treated as non-retryable), and that
// the same pool continues processing subsequent jobs afterward.
func TestReliability_WorkerPanicRecovery(t *testing.T) {
	jobService, repo, sched, workers, _ := setupIntegrationTest(t)
	ctx := context.Background()

	sched.Start()
	workers.Start()
	defer sched.Stop()
	defer workers.Stop()

	payload, _ := json.Marshal(map[string]string{"x": "1"})
	panicJob, err := jobService.CreateJob(ctx, nil, "panic_job", payload, nil)
	if err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}

	pollUntil(t, 5*time.Second, 100*time.Millisecond, func() (bool, error) {
		j, err := repo.GetByID(ctx, panicJob.ID)
		if err != nil {
			return false, err
		}
		return j.State == state.FAILED, nil
	})

	// The pool must still be usable: submit a normal job on the same pool
	// and confirm it completes. If the panic had taken down a worker
	// goroutine (e.g. missing recover(), or recover() in the wrong
	// defer scope), the pool would be short a worker but might still
	// technically make progress with fewer workers — so we also confirm
	// throughput isn't degraded to zero by requiring completion within
	// the same timeout used for a healthy pool.
	normalJob, err := jobService.CreateJob(ctx, nil, "demo_job", payload, nil)
	if err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}
	pollUntil(t, 5*time.Second, 100*time.Millisecond, func() (bool, error) {
		j, err := repo.GetByID(ctx, normalJob.ID)
		if err != nil {
			return false, err
		}
		return j.State == state.SUCCEEDED, nil
	})
}

// TestReliability_SchedulerRecoversStaleClaimedJob verifies the crash
// scenario the codebase's own comments describe: a job claimed (SCHEDULED)
// but never dispatched to a worker, e.g. because the process died between
// ClaimPendingJobs and sending on the job channel. A fresh Scheduler
// instance starting up (simulating a process restart) must recover it back
// to PENDING and it must go on to actually execute.
func TestReliability_SchedulerRecoversStaleClaimedJob(t *testing.T) {
	_, repo, _, workers, jobChannel := setupIntegrationTest(t)
	ctx := context.Background()

	// Directly insert a job in SCHEDULED state with an old scheduled_at,
	// simulating what the DB would contain right after a crash between
	// claim and dispatch.
	payload, _ := json.Marshal(map[string]string{"x": "1"})
	staleJob := &model.Job{
		ID:          "reliability_stale_scheduled",
		Type:        "demo_job",
		Payload:     payload,
		State:       state.SCHEDULED,
		Attempt:     1,
		MaxAttempts: 3,
		CreatedAt:   time.Now().Add(-2 * time.Hour),
		ScheduledAt: ptrTime(time.Now().Add(-1 * time.Hour)),
	}
	if err := repo.Create(ctx, staleJob); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// A brand new scheduler instance, as if the process had just restarted.
	freshScheduler := scheduler.NewScheduler(repo, 500*time.Millisecond, 5, jobChannel)
	freshScheduler.Start()
	workers.Start()
	defer freshScheduler.Stop()
	defer workers.Stop()

	// Start() runs stale-job recovery synchronously before returning, so
	// the state flip to PENDING should already be visible.
	recovered, err := repo.GetByID(ctx, staleJob.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if recovered.State != state.PENDING {
		t.Fatalf("expected job to be recovered to PENDING immediately after Start(), got %s", recovered.State)
	}

	// And it should go on to actually run to completion, proving recovery
	// isn't just a state flip that then gets stuck again.
	pollUntil(t, 5*time.Second, 100*time.Millisecond, func() (bool, error) {
		j, err := repo.GetByID(ctx, staleJob.ID)
		if err != nil {
			return false, err
		}
		return j.State == state.SUCCEEDED, nil
	})
}

// TestReliability_GracefulShutdownMidJob verifies that Stop() lets an
// in-flight job actually finish (within the shutdown grace period) rather
// than cancelling it immediately. This test previously caught a real bug:
// the in-flight job's context was derived from the pool's own shutdown
// context, so Stop() cancelled every running job's context immediately
// instead of waiting, and the resulting failure-handling call used that
// same cancelled context and couldn't even persist FAILED — jobs were left
// stuck in RUNNING with no owner. Fixed by giving in-flight jobs an
// independent context that's only force-cancelled if the grace period
// elapses.
func TestReliability_GracefulShutdownMidJob(t *testing.T) {
	jobService, repo, sched, workers, _ := setupIntegrationTest(t)
	ctx := context.Background()

	sched.Start()
	workers.Start()
	defer sched.Stop()

	payload, _ := json.Marshal(map[string]string{"x": "1"})
	slowJob, err := jobService.CreateJob(ctx, nil, "slow_job", payload, nil)
	if err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}

	// Wait for the job to actually be picked up and start running.
	pollUntil(t, 5*time.Second, 50*time.Millisecond, func() (bool, error) {
		j, err := repo.GetByID(ctx, slowJob.ID)
		if err != nil {
			return false, err
		}
		return j.State == state.RUNNING, nil
	})

	stopStart := time.Now()
	workers.Stop()
	stopDuration := time.Since(stopStart)

	final, err := repo.GetByID(ctx, slowJob.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	t.Logf("MEASURED: Stop() took %v while slow_job's executor sleeps 3s. Job ended in state=%s, attempt=%d.",
		stopDuration, final.State, final.Attempt)

	// Stop() should have waited for the ~3s job to actually finish, not
	// returned near-instantly.
	if stopDuration < 2*time.Second {
		t.Errorf("Stop() returned in %v, expected it to wait for the in-flight 3s job to finish", stopDuration)
	}
	if final.State != state.SUCCEEDED {
		t.Errorf("expected the in-flight job to finish successfully during graceful shutdown, got state=%s", final.State)
	}
}

// TestReliability_ShutdownBoundedAgainstHungExecutor verifies Stop()'s
// hard bound: even a worker running a job that completely ignores context
// cancellation (hungExecutor sleeps for 30s regardless of ctx) cannot make
// Stop() block indefinitely. It should return within roughly
// shutdownGracePeriod + forceStopTimeout (5s + 5s here), not 30s -- proving
// the fix trades "guaranteed clean stop of every worker" for "Stop() can
// never hang process shutdown", which is the only guarantee Go's
// non-preemptible goroutines actually allow.
func TestReliability_ShutdownBoundedAgainstHungExecutor(t *testing.T) {
	jobService, repo, sched, workers, _ := setupIntegrationTest(t)
	ctx := context.Background()

	sched.Start()
	workers.Start()
	defer sched.Stop()

	payload, _ := json.Marshal(map[string]string{"x": "1"})
	hungJob, err := jobService.CreateJob(ctx, nil, "hung_job", payload, nil)
	if err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}

	pollUntil(t, 5*time.Second, 50*time.Millisecond, func() (bool, error) {
		j, err := repo.GetByID(ctx, hungJob.ID)
		if err != nil {
			return false, err
		}
		return j.State == state.RUNNING, nil
	})

	stopStart := time.Now()
	workers.Stop()
	stopDuration := time.Since(stopStart)

	t.Logf("MEASURED: Stop() returned in %v against a job that sleeps 30s and ignores context cancellation.", stopDuration)

	if stopDuration >= 30*time.Second {
		t.Errorf("Stop() blocked for %v, waiting on a non-cancellable job; expected it to give up within its bounded window", stopDuration)
	}
	if stopDuration > 12*time.Second {
		t.Errorf("Stop() took %v, longer than the configured grace+force window suggests it should", stopDuration)
	}
}

// TestReliability_RetryExhaustionEndToEnd drives a permanently-failing job
// through the real scheduler + worker retry loop (not a direct call to
// HandleFailure) and verifies it ends in FAILED with the exact expected
// attempt count once MaxAttempts is reached.
func TestReliability_RetryExhaustionEndToEnd(t *testing.T) {
	jobService, repo, sched, workers, _ := setupIntegrationTest(t)
	ctx := context.Background()

	sched.Start()
	workers.Start()
	defer sched.Stop()
	defer workers.Stop()

	payload, _ := json.Marshal(map[string]string{"x": "1"})
	job, err := jobService.CreateJob(ctx, nil, "failing_job", payload, nil)
	if err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}

	// MaxAttempts defaults to 3 with 1s base backoff (see
	// service.DefaultRetryConfig / JobService default), so worst case is
	// roughly 1s + 2s of backoff plus scheduler poll latency. 15s is a
	// generous ceiling, not a tuned value.
	pollUntil(t, 15*time.Second, 200*time.Millisecond, func() (bool, error) {
		j, err := repo.GetByID(ctx, job.ID)
		if err != nil {
			return false, err
		}
		return j.State == state.FAILED, nil
	})

	final, err := repo.GetByID(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if final.Attempt != final.MaxAttempts {
		t.Errorf("expected Attempt to equal MaxAttempts (%d) once exhausted, got Attempt=%d",
			final.MaxAttempts, final.Attempt)
	}
	if final.CompletedAt == nil {
		t.Errorf("expected CompletedAt to be set on permanent failure")
	}
}

// TestReliability_DatabaseOutage is gated behind an environment variable
// and skipped by default because it stops and restarts the OS-level
// Postgres service directly (`service postgresql stop/start`), which
// requires root and controls a real system service. That's reproducible on
// a local machine or a self-hosted runner, but not on GitHub Actions' own
// Postgres *service container*, which a test running inside the job can't
// stop this way. Run locally with:
//
//	ORCHESTRIX_RUN_DB_OUTAGE_TEST=1 go test ./internal/worker/... -run TestReliability_DatabaseOutage -v
func TestReliability_DatabaseOutage(t *testing.T) {
	if os.Getenv("ORCHESTRIX_RUN_DB_OUTAGE_TEST") == "" {
		t.Skip("set ORCHESTRIX_RUN_DB_OUTAGE_TEST=1 to run this test (stops/starts the real local Postgres service; not portable to CI's service-container Postgres)")
	}

	jobService, repo, sched, workers, _ := setupIntegrationTest(t)
	ctx := context.Background()

	sched.Start()
	workers.Start()
	defer sched.Stop()
	defer workers.Stop()

	restart := func(action string) error {
		cmd := exec.Command("service", "postgresql", action)
		return cmd.Run()
	}

	t.Log("stopping postgresql service to simulate a database outage")
	if err := restart("stop"); err != nil {
		t.Fatalf("failed to stop postgresql: %v", err)
	}

	// Attempt an operation while the DB is down. Expected observation: a
	// clean error, no panic.
	payload, _ := json.Marshal(map[string]string{"x": "1"})
	_, createErr := jobService.CreateJob(ctx, nil, "demo_job", payload, nil)
	t.Logf("OBSERVED: CreateJob while DB is down returned: %v", createErr)
	if createErr == nil {
		t.Error("expected CreateJob to fail while the database is down")
	}

	t.Log("restarting postgresql service")
	if err := restart("start"); err != nil {
		t.Fatalf("failed to restart postgresql: %v", err)
	}
	// Give the pool a moment to notice the connection is usable again.
	time.Sleep(2 * time.Second)

	// System should self-heal: a job created after recovery should
	// complete normally with no manual intervention.
	job, err := jobService.CreateJob(ctx, nil, "demo_job", payload, nil)
	if err != nil {
		t.Fatalf("CreateJob failed after DB recovery: %v", err)
	}
	pollUntil(t, 10*time.Second, 200*time.Millisecond, func() (bool, error) {
		j, err := repo.GetByID(ctx, job.ID)
		if err != nil {
			return false, err
		}
		return j.State == state.SUCCEEDED, nil
	})
}

func ptrTime(t time.Time) *time.Time { return &t }
