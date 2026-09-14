package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dipak0000812/orchestrix/internal/executor"
	"github.com/dipak0000812/orchestrix/internal/job/dependency"
	"github.com/dipak0000812/orchestrix/internal/job/model"
	"github.com/dipak0000812/orchestrix/internal/job/service"
	"github.com/dipak0000812/orchestrix/internal/job/state"
	"github.com/dipak0000812/orchestrix/internal/metrics"
	"github.com/dipak0000812/orchestrix/internal/scheduler"
)

// countingExecutor wraps the real checksum executor and atomically counts
// how many times Execute is called per job ID, so duplicate execution
// (should be impossible given SKIP LOCKED, but is asserted here rather
// than assumed) is directly measurable rather than inferred.
type countingExecutor struct {
	inner  *executor.ChecksumExecutor
	counts sync.Map // job ID (from payload marker) -> *int32
}

// countingChecksumPayload extends the real payload with a per-job marker
// so the counting executor can attribute Execute calls to a specific
// seeded job even though the Executor interface only receives the raw
// payload bytes, not the job ID.
type countingChecksumPayload struct {
	Data       string `json:"data"`
	WorkFactor int    `json:"work_factor,omitempty"`
	Marker     string `json:"marker"`
}

func (c *countingExecutor) Execute(ctx context.Context, payload []byte) error {
	var p countingChecksumPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return executor.NewPermanentError(err)
	}
	v, _ := c.counts.LoadOrStore(p.Marker, new(int32))
	atomic.AddInt32(v.(*int32), 1)

	inner, _ := json.Marshal(executor.ChecksumPayload{Data: p.Data, WorkFactor: p.WorkFactor})
	return c.inner.Execute(ctx, inner)
}

// duplicateCount returns how many job markers were executed more than
// once.
func (c *countingExecutor) duplicateCount() int {
	dupes := 0
	c.counts.Range(func(_, v interface{}) bool {
		if atomic.LoadInt32(v.(*int32)) > 1 {
			dupes++
		}
		return true
	})
	return dupes
}

// throughputConfig is one point in the batchSize x pollInterval sweep.
type throughputConfig struct {
	label        string
	batchSize    int
	pollInterval time.Duration
	adaptive     bool
}

// throughputResult holds every metric measured for one config, all
// directly observed -- nothing here is estimated or extrapolated.
type throughputResult struct {
	config           throughputConfig
	jobsPerSec       float64
	drainDuration    time.Duration
	claimLatencyP50  time.Duration
	claimLatencyP95  time.Duration
	claimLatencyMax  time.Duration
	claimCount       int
	maxQueueDepth    int
	avgQueueDepth    float64
	duplicateExecs   int
	postgresCPUPct   float64
	orchestrixCPUPct float64
	retryOK          bool
}

// cpuTicks reads (utime+stime) in clock ticks for every process whose comm
// matches name, summed. Used to measure Postgres (which is multiple
// backend processes, one per connection) as a single aggregate.
func cpuTicks(name string) int64 {
	out, err := exec.Command("sh", "-c", fmt.Sprintf(`for p in /proc/[0-9]*; do c=$(cat $p/comm 2>/dev/null); if [ "$c" = "%s" ]; then awk '{print $14+$15}' $p/stat 2>/dev/null; fi; done`, name)).Output()
	if err != nil {
		return 0
	}
	var total int64
	for _, line := range strings.Fields(string(out)) {
		v, err := strconv.ParseInt(line, 10, 64)
		if err == nil {
			total += v
		}
	}
	return total
}

// selfCPUTicks reads (utime+stime) for the current test process, which
// contains the scheduler and worker goroutines running in-process (they
// aren't separable from each other or from the test harness's own
// goroutines this way -- see the caveat in the write-up).
func selfCPUTicks() int64 {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", os.Getpid()))
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 15 {
		return 0
	}
	utime, _ := strconv.ParseInt(fields[13], 10, 64)
	stime, _ := strconv.ParseInt(fields[14], 10, 64)
	return utime + stime
}

const clockTicksPerSec = 100 // standard Linux USER_HZ; matches getconf CLK_TCK on this system

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

// runThroughputBenchmark seeds a fixed backlog (backlogSize main jobs plus
// a small number of deliberately-failing jobs for the retry-correctness
// check), runs the real scheduler+worker pool under cfg, and measures all
// six requested dimensions until the backlog is fully drained.
func runThroughputBenchmark(t *testing.T, cfg throughputConfig, backlogSize int, m *metrics.Metrics) throughputResult {
	t.Helper()

	repo := connectStressTestDB(t)
	ctx := context.Background()

	const failingJobCount = 10
	runNonce := time.Now().UnixNano()

	for i := 0; i < backlogSize; i++ {
		marker := fmt.Sprintf("tp_%d_%d", runNonce, i)
		payload, _ := json.Marshal(countingChecksumPayload{Data: "x", WorkFactor: 1, Marker: marker})
		job := &model.Job{
			ID: marker, Type: "counting_checksum", Payload: payload,
			State: state.PENDING, Attempt: 1, MaxAttempts: 3, CreatedAt: time.Now(),
		}
		if err := repo.Create(ctx, job); err != nil {
			t.Fatalf("seed insert failed: %v", err)
		}
	}
	var failingIDs []string
	for i := 0; i < failingJobCount; i++ {
		id := fmt.Sprintf("tp_failing_%d_%d", runNonce, i)
		failingIDs = append(failingIDs, id)
		job := &model.Job{
			ID: id, Type: "failing_job", Payload: []byte(`{}`),
			State: state.PENDING, Attempt: 1, MaxAttempts: 3, CreatedAt: time.Now(),
		}
		if err := repo.Create(ctx, job); err != nil {
			t.Fatalf("seed failing-job insert failed: %v", err)
		}
	}

	jobChannel := make(chan *model.Job, 1000)
	executors := executor.NewExecutorRegistry()
	counter := &countingExecutor{inner: executor.NewChecksumExecutor()}
	executors.Register("counting_checksum", counter)
	executors.Register("failing_job", executor.NewFailingExecutor())

	stateMachine := state.NewStateMachine()
	idGen := service.NewULIDGenerator()
	retryConfig := service.DefaultRetryConfig()
	jobService := service.NewJobService(repo, stateMachine, idGen, retryConfig, dependency.NewResolver(repo))

	sched := scheduler.NewScheduler(repo, cfg.pollInterval, cfg.batchSize, jobChannel)
	if cfg.adaptive {
		sched.EnableAdaptivePolling()
	}

	var claimMu sync.Mutex
	var claimDurations []time.Duration
	sched.SetClaimObserver(func(ev scheduler.ClaimEvent) {
		claimMu.Lock()
		claimDurations = append(claimDurations, ev.Duration)
		claimMu.Unlock()
	})

	workers := NewWorkerPool(5, jobChannel, executors, jobService, m, 10*time.Second)

	// Queue-depth sampler.
	stopSampling := make(chan struct{})
	var depthMu sync.Mutex
	var maxDepth int
	var depthSum, depthSamples int64
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				d := len(jobChannel)
				depthMu.Lock()
				if d > maxDepth {
					maxDepth = d
				}
				depthSum += int64(d)
				depthSamples++
				depthMu.Unlock()
			case <-stopSampling:
				return
			}
		}
	}()

	pgCPUStart := cpuTicks("postgres")
	selfCPUStart := selfCPUTicks()

	drainStart := time.Now()
	sched.Start()
	workers.Start()

	total := backlogSize + failingJobCount
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		succ, _ := repo.CountByState(ctx, state.SUCCEEDED)
		fail, _ := repo.CountByState(ctx, state.FAILED)
		if succ+fail >= total {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	drainDuration := time.Since(drainStart)

	sched.Stop()
	workers.Stop()
	close(stopSampling)

	pgCPUDelta := cpuTicks("postgres") - pgCPUStart
	selfCPUDelta := selfCPUTicks() - selfCPUStart
	wallSec := drainDuration.Seconds()

	claimMu.Lock()
	sorted := append([]time.Duration(nil), claimDurations...)
	claimCount := len(sorted)
	claimMu.Unlock()
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var maxClaim time.Duration
	if len(sorted) > 0 {
		maxClaim = sorted[len(sorted)-1]
	}

	depthMu.Lock()
	avgDepth := 0.0
	if depthSamples > 0 {
		avgDepth = float64(depthSum) / float64(depthSamples)
	}
	finalMaxDepth := maxDepth
	depthMu.Unlock()

	retryOK := true
	for _, id := range failingIDs {
		j, err := repo.GetByID(ctx, id)
		if err != nil || j.State != state.FAILED || j.Attempt != j.MaxAttempts {
			retryOK = false
			break
		}
	}

	return throughputResult{
		config:           cfg,
		jobsPerSec:       float64(total) / wallSec,
		drainDuration:    drainDuration,
		claimLatencyP50:  percentile(sorted, 0.50),
		claimLatencyP95:  percentile(sorted, 0.95),
		claimLatencyMax:  maxClaim,
		claimCount:       claimCount,
		maxQueueDepth:    finalMaxDepth,
		avgQueueDepth:    avgDepth,
		duplicateExecs:   counter.duplicateCount(),
		postgresCPUPct:   float64(pgCPUDelta) / float64(clockTicksPerSec) / wallSec * 100,
		orchestrixCPUPct: float64(selfCPUDelta) / float64(clockTicksPerSec) / wallSec * 100,
		retryOK:          retryOK,
	}
}

// TestThroughputDesignReview runs the full batchSize x pollInterval sweep
// plus the adaptive-polling variant, and prints a results table. Skipped
// by default (real wall-clock minutes across 17 configs); run with:
//
//	RUN_THROUGHPUT_REVIEW=1 go test ./internal/worker/... -run TestThroughputDesignReview -v -timeout 30m
func TestThroughputDesignReview(t *testing.T) {
	if os.Getenv("RUN_THROUGHPUT_REVIEW") == "" {
		t.Skip("set RUN_THROUGHPUT_REVIEW=1 to run the full scheduler throughput design review")
	}

	const backlogSize = 500

	configs := []throughputConfig{
		{"batch=10 poll=1s", 10, time.Second, false},
		{"batch=10 poll=500ms", 10, 500 * time.Millisecond, false},
		{"batch=10 poll=250ms", 10, 250 * time.Millisecond, false},
		{"batch=10 poll=100ms", 10, 100 * time.Millisecond, false},
		{"batch=25 poll=1s", 25, time.Second, false},
		{"batch=25 poll=500ms", 25, 500 * time.Millisecond, false},
		{"batch=25 poll=250ms", 25, 250 * time.Millisecond, false},
		{"batch=25 poll=100ms", 25, 100 * time.Millisecond, false},
		{"batch=50 poll=1s", 50, time.Second, false},
		{"batch=50 poll=500ms", 50, 500 * time.Millisecond, false},
		{"batch=50 poll=250ms", 50, 250 * time.Millisecond, false},
		{"batch=50 poll=100ms", 50, 100 * time.Millisecond, false},
		{"batch=100 poll=1s", 100, time.Second, false},
		{"batch=100 poll=500ms", 100, 500 * time.Millisecond, false},
		{"batch=100 poll=250ms", 100, 250 * time.Millisecond, false},
		{"batch=100 poll=100ms", 100, 100 * time.Millisecond, false},
		{"adaptive batch=50 poll=1s(fallback)", 50, time.Second, true},
	}

	results := make([]throughputResult, 0, len(configs))
	m := metrics.NewMetrics()
	for _, cfg := range configs {
		t.Run(cfg.label, func(t *testing.T) {
			r := runThroughputBenchmark(t, cfg, backlogSize, m)
			results = append(results, r)
			t.Logf("MEASURED %-32s jobs/sec=%.2f drain=%v claim(p50/p95/max)=%v/%v/%v claims=%d queueDepth(max/avg)=%d/%.1f pgCPU%%=%.1f orchestrixCPU%%=%.1f dupes=%d retryOK=%v",
				cfg.label, r.jobsPerSec, r.drainDuration, r.claimLatencyP50, r.claimLatencyP95, r.claimLatencyMax,
				r.claimCount, r.maxQueueDepth, r.avgQueueDepth, r.postgresCPUPct, r.orchestrixCPUPct, r.duplicateExecs, r.retryOK)
			if r.duplicateExecs != 0 {
				t.Errorf("%s: %d jobs executed more than once", cfg.label, r.duplicateExecs)
			}
			if !r.retryOK {
				t.Errorf("%s: retry correctness check failed", cfg.label)
			}
		})
	}

	t.Log("=== SUMMARY (all real, measured this run) ===")
	for _, r := range results {
		t.Logf("%-32s %8.2f jobs/sec  claim p95=%-10v maxQueue=%-6d pgCPU=%5.1f%%  orchestrixCPU=%5.1f%%  dupes=%d retryOK=%v",
			r.config.label, r.jobsPerSec, r.claimLatencyP95, r.maxQueueDepth, r.postgresCPUPct, r.orchestrixCPUPct, r.duplicateExecs, r.retryOK)
	}
}

// TestIdlePollingCost measures the CPU cost of polling an empty queue --
// the other half of the adaptive-polling evaluation. The backlog sweep in
// TestThroughputDesignReview only measures drain throughput under load; it
// says nothing about the cost of polling when there's nothing to claim,
// which is exactly the situation adaptive polling is meant to improve.
// Fixed-interval polling keeps querying at pollInterval regardless of
// whether the queue is empty. Adaptive backs off to pollInterval too, but
// only after an empty/partial claim -- since an empty queue always
// returns a partial (empty) claim, adaptive's fast path never triggers
// once idle, so at steady-state idle it's expected to behave the same as
// a fixed-interval scheduler at the same pollInterval. This test checks
// that expectation empirically rather than assumes it.
//
//	RUN_THROUGHPUT_REVIEW=1 go test ./internal/worker/... -run TestIdlePollingCost -v -timeout 5m
func TestIdlePollingCost(t *testing.T) {
	if os.Getenv("RUN_THROUGHPUT_REVIEW") == "" {
		t.Skip("set RUN_THROUGHPUT_REVIEW=1 to run the idle-polling cost comparison")
	}

	configs := []throughputConfig{
		{"idle: fixed poll=1s", 10, time.Second, false},
		{"idle: fixed poll=100ms", 10, 100 * time.Millisecond, false},
		{"idle: adaptive poll=1s", 10, time.Second, true},
	}

	const idleWindow = 15 * time.Second
	m := metrics.NewMetrics()

	for _, cfg := range configs {
		t.Run(cfg.label, func(t *testing.T) {
			repo := connectStressTestDB(t) // clears the table -- queue starts empty
			jobChannel := make(chan *model.Job, 100)
			executors := executor.NewExecutorRegistry()
			stateMachine := state.NewStateMachine()
			idGen := service.NewULIDGenerator()
			retryConfig := service.DefaultRetryConfig()
			jobService := service.NewJobService(repo, stateMachine, idGen, retryConfig, dependency.NewResolver(repo))

			sched := scheduler.NewScheduler(repo, cfg.pollInterval, cfg.batchSize, jobChannel)
			if cfg.adaptive {
				sched.EnableAdaptivePolling()
			}
			var claimCount int64
			sched.SetClaimObserver(func(scheduler.ClaimEvent) {
				atomic.AddInt64(&claimCount, 1)
			})
			workers := NewWorkerPool(2, jobChannel, executors, jobService, m, 10*time.Second)

			pgCPUStart := cpuTicks("postgres")
			selfCPUStart := selfCPUTicks()

			sched.Start()
			workers.Start()
			time.Sleep(idleWindow)
			sched.Stop()
			workers.Stop()

			pgCPUDelta := cpuTicks("postgres") - pgCPUStart
			selfCPUDelta := selfCPUTicks() - selfCPUStart
			wallSec := idleWindow.Seconds()

			t.Logf("MEASURED %-24s claims=%-5d (%.2f/sec) pgCPU%%=%.2f orchestrixCPU%%=%.2f",
				cfg.label, claimCount, float64(claimCount)/wallSec,
				float64(pgCPUDelta)/float64(clockTicksPerSec)/wallSec*100,
				float64(selfCPUDelta)/float64(clockTicksPerSec)/wallSec*100)
		})
	}
}
