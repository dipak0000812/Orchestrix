package scheduler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/dipak0000812/orchestrix/internal/job/model"
	"github.com/dipak0000812/orchestrix/internal/job/repository"
)

const (
	workerDispatchTimeout  = 5 * time.Second
	staleScheduleThreshold = 30 * time.Second
)

// ClaimEvent describes the outcome of a single ClaimPendingJobs call, for
// instrumentation (benchmarking, metrics) without hardcoding measurement
// concerns into the scheduling loop itself.
type ClaimEvent struct {
	Duration time.Duration
	Claimed  int
	// Full is true when Claimed == batchSize, i.e. there may be more work
	// waiting immediately -- this is what adaptive polling uses to decide
	// whether to reclaim immediately instead of sleeping.
	Full bool
}

// Scheduler polls the database for PENDING jobs and schedules them.
type Scheduler struct {
	repository   *repository.PostgresJobRepository
	pollInterval time.Duration
	batchSize    int
	jobChannel   chan *model.Job

	// adaptivePolling, when true, skips the sleep between polls whenever a
	// claim comes back full (batchSize reached) and reclaims immediately
	// instead, only falling back to pollInterval once a poll returns a
	// partial or empty batch. Off by default -- production behavior is
	// unchanged unless explicitly enabled via EnableAdaptivePolling.
	adaptivePolling bool

	// onClaim, if set, is invoked after every ClaimPendingJobs call. Used
	// by benchmarks/tests to record claim latency and batch fullness
	// without adding a hard dependency on the metrics package to the
	// scheduler itself.
	onClaim func(ClaimEvent)

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewScheduler creates a new scheduler. Defaults to fixed-interval
// polling; call EnableAdaptivePolling to switch modes before Start.
func NewScheduler(
	jobRepository *repository.PostgresJobRepository,
	pollInterval time.Duration,
	batchSize int,
	jobChannel chan *model.Job,
) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())

	return &Scheduler{
		repository:   jobRepository,
		pollInterval: pollInterval,
		batchSize:    batchSize,
		jobChannel:   jobChannel,
		ctx:          ctx,
		cancel:       cancel,
	}
}

// EnableAdaptivePolling switches the scheduler to adaptive mode: after a
// claim that fills the whole batch, it reclaims immediately instead of
// waiting out pollInterval; it only sleeps once a claim comes back partial
// or empty. Must be called before Start.
func (s *Scheduler) EnableAdaptivePolling() {
	s.adaptivePolling = true
}

// SetClaimObserver registers a callback invoked after every claim attempt.
// For instrumentation only; must be called before Start.
func (s *Scheduler) SetClaimObserver(fn func(ClaimEvent)) {
	s.onClaim = fn
}

// Start begins the scheduling loop.
func (s *Scheduler) Start() {
	s.recoverStaleScheduledJobs(context.Background())

	s.wg.Add(1)
	go s.run()
	log.Println("Scheduler started")
}

// Stop gracefully stops the scheduler.
func (s *Scheduler) Stop() {
	log.Println("Scheduler stopping...")
	s.cancel()
	s.wg.Wait()
	log.Println("Scheduler stopped")
}

// run is the main scheduling loop.
func (s *Scheduler) run() {
	defer s.wg.Done()

	if s.adaptivePolling {
		s.runAdaptive()
		return
	}
	s.runFixedInterval()
}

// runFixedInterval is the original, default behavior: poll unconditionally
// once per pollInterval, regardless of how full the last claim was.
func (s *Scheduler) runFixedInterval() {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.pollAndSchedule()

		case <-s.ctx.Done():
			return
		}
	}
}

// runAdaptive reclaims immediately after a full batch (there may be more
// work waiting right now) and only sleeps pollInterval after a partial or
// empty claim. This keeps latency low under sustained backlog while
// avoiding unnecessary polling once the queue is drained, without needing
// a shorter fixed pollInterval (and its constant DB load) at all times.
func (s *Scheduler) runAdaptive() {
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		full := s.pollAndSchedule()
		if full {
			continue
		}

		select {
		case <-time.After(s.pollInterval):
		case <-s.ctx.Done():
			return
		}
	}
}

// pollAndSchedule finds and claims PENDING jobs atomically. Returns true
// if the claim came back full (== batchSize), the signal adaptive mode
// uses to decide whether to reclaim immediately.
func (s *Scheduler) pollAndSchedule() bool {
	// A worker can stop after a job is claimed but before it transitions the
	// job to RUNNING. Recover on every poll so those jobs do not depend on a
	// process restart to become eligible again.
	s.recoverStaleScheduledJobs(s.ctx)

	// Atomically claim pending jobs (locks + updates state to SCHEDULED)
	claimStart := time.Now()
	jobs, err := s.repository.ClaimPendingJobs(s.ctx, s.batchSize)
	claimDuration := time.Since(claimStart)

	full := len(jobs) == s.batchSize
	if s.onClaim != nil {
		s.onClaim(ClaimEvent{Duration: claimDuration, Claimed: len(jobs), Full: full})
	}

	if err != nil {
		log.Printf("Failed to claim pending jobs: %v", err)
		return false
	}

	if len(jobs) == 0 {
		return false // No jobs to schedule
	}

	log.Printf("Found %d pending jobs", len(jobs))

	// Send jobs to worker pool
	for _, job := range jobs {
		if err := s.sendToWorkers(job); err != nil {
			log.Printf("Failed to send job %s to workers: %v", job.ID, err)
			requeueCtx, cancel := context.WithTimeout(context.Background(), workerDispatchTimeout)
			if requeueErr := s.repository.RequeueScheduledJob(requeueCtx, job.ID); requeueErr != nil {
				log.Printf("Failed to requeue job %s after dispatch failure: %v", job.ID, requeueErr)
			}
			cancel()
			continue
		}
	}

	return full
}

// recoverStaleScheduledJobs requeues jobs that were claimed but never started
// to PENDING. The repository predicate only matches SCHEDULED jobs, so a
// worker that already transitioned a job to RUNNING is never reclaimed.
func (s *Scheduler) recoverStaleScheduledJobs(parent context.Context) {
	recoveryCtx, cancel := context.WithTimeout(parent, workerDispatchTimeout)
	defer cancel()

	recovered, err := s.repository.RecoverStaleScheduledJobs(
		recoveryCtx,
		time.Now().Add(-staleScheduleThreshold),
	)
	if err != nil {
		log.Printf("Failed to recover stale scheduled jobs: %v", err)
		return
	}
	if recovered > 0 {
		log.Printf("Requeued %d stale scheduled jobs", recovered)
	}
}

// sendToWorkers sends a job to the worker pool channel.
func (s *Scheduler) sendToWorkers(job *model.Job) error {
	// Job is already in SCHEDULED state from ClaimPendingJobs
	select {
	case s.jobChannel <- job:
		log.Printf("Scheduled job %s (type: %s)", job.ID, job.Type)
		return nil

	case <-time.After(workerDispatchTimeout):
		return fmt.Errorf("timeout sending job to channel")

	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}
