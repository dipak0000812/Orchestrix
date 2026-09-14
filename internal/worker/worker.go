package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/dipak0000812/orchestrix/internal/executor"
	"github.com/dipak0000812/orchestrix/internal/job/model"
	"github.com/dipak0000812/orchestrix/internal/job/service"
	"github.com/dipak0000812/orchestrix/internal/job/state"
	"github.com/dipak0000812/orchestrix/internal/metrics"
)

// WorkerPool manages a pool of workers that execute jobs.
type WorkerPool struct {
	numWorkers          int
	jobChannel          chan *model.Job
	executors           *executor.ExecutorRegistry
	service             *service.JobService
	metrics             *metrics.Metrics
	jobTimeout          time.Duration
	shutdownGracePeriod time.Duration
	// forceStopTimeout bounds how long Stop() waits after force-cancelling
	// in-flight job contexts. Cancellation only helps executors that
	// actually check ctx.Done(); Go cannot forcibly kill a goroutine stuck
	// in a call that ignores its context, so this is a hard ceiling on how
	// long Stop() itself will block, accepting that a truly hung worker's
	// goroutine may be left running (leaked) rather than ever blocking
	// process shutdown indefinitely.
	forceStopTimeout time.Duration

	// stopCh signals workers to stop picking up new jobs. It is separate
	// from hardCtx so that Stop() does not immediately cancel in-flight
	// job execution.
	stopCh   chan struct{}
	stopOnce sync.Once

	// hardCtx is the parent context for in-flight job execution. It is
	// only cancelled if shutdownGracePeriod elapses without all workers
	// finishing on their own, so a graceful shutdown gives running jobs a
	// real chance to complete instead of aborting them immediately.
	hardCtx    context.Context
	hardCancel context.CancelFunc

	wg sync.WaitGroup
}

// NewWorkerPool creates a new worker pool.
func NewWorkerPool(
	numWorkers int,
	jobChannel chan *model.Job,
	executors *executor.ExecutorRegistry,
	jobService *service.JobService,
	m *metrics.Metrics,
	jobTimeout time.Duration,
) *WorkerPool {
	hardCtx, hardCancel := context.WithCancel(context.Background())

	return &WorkerPool{
		numWorkers: numWorkers,
		jobChannel: jobChannel,
		executors:  executors,
		service:    jobService,
		metrics:    m,
		jobTimeout: jobTimeout,
		// Give an in-flight job at least its own full timeout to finish
		// naturally before the pool force-cancels it on shutdown.
		shutdownGracePeriod: jobTimeout,
		// After force-cancelling, allow a further bounded window for
		// cooperative executors to actually unwind before giving up.
		forceStopTimeout: 5 * time.Second,
		stopCh:           make(chan struct{}),
		hardCtx:          hardCtx,
		hardCancel:       hardCancel,
	}
}

// Start spawns worker goroutines.
func (p *WorkerPool) Start() {
	for i := 0; i < p.numWorkers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
	log.Printf("Worker pool started with %d workers", p.numWorkers)
}

// Stop gracefully stops all workers. It stops handing out new jobs
// immediately, then waits up to shutdownGracePeriod for in-flight jobs to
// finish on their own. If that elapses, it cancels in-flight job contexts
// (which only helps executors that actually check ctx.Done()) and waits
// one further bounded window (forceStopTimeout). If a worker is still not
// done after that -- e.g. it's blocked in a call that ignores context
// entirely -- Stop() logs a warning and returns anyway rather than risking
// an indefinite block on process shutdown; that worker's goroutine is
// leaked until it eventually finishes on its own.
func (p *WorkerPool) Stop() {
	log.Println("Worker pool stopping...")
	p.stopOnce.Do(func() { close(p.stopCh) })

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("Worker pool stopped (all in-flight jobs completed)")
		return
	case <-time.After(p.shutdownGracePeriod):
		log.Printf("Worker pool: shutdown grace period (%v) exceeded, cancelling in-flight job contexts", p.shutdownGracePeriod)
		p.hardCancel()
	}

	select {
	case <-done:
		log.Println("Worker pool stopped (forced)")
	case <-time.After(p.forceStopTimeout):
		log.Printf("Worker pool: still waiting on workers after the forced-shutdown window (%v); they are likely blocked in non-cancellable work and will be abandoned", p.forceStopTimeout)
	}
}

// worker is the main worker loop.
func (p *WorkerPool) worker(id int) {
	defer p.wg.Done()

	log.Printf("Worker %d started", id)

	for {
		select {
		case job := <-p.jobChannel:
			p.executeJob(id, job)

		case <-p.stopCh:
			log.Printf("Worker %d stopping", id)
			return
		}
	}
}

// executeJob executes a single job.
func (p *WorkerPool) executeJob(workerID int, job *model.Job) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Worker %d: PANIC during job %s: %v", workerID, job.ID, r)
			// Deliberately independent of hardCtx/stopCh: finalizing job
			// state after a panic must be attempted even if the pool is
			// mid-shutdown, so it gets its own bounded-but-unrelated
			// timeout rather than inheriting a context that may already
			// be cancelled.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			p.handleFailure(ctx, job, fmt.Errorf("panic: %v", r), false)
		}
	}()

	log.Printf("Worker %d executing job %s (type: %s, attempt: %d)",
		workerID, job.ID, job.Type, job.Attempt)

	ctx, cancel := context.WithTimeout(p.hardCtx, p.jobTimeout)
	defer cancel()

	// Transition to RUNNING
	if err := p.service.TransitionState(ctx, job.ID, state.RUNNING); err != nil {
		log.Printf("Worker %d failed to transition job %s to RUNNING: %v",
			workerID, job.ID, err)
		return
	}

	// Get executor for this job type
	exec, err := p.executors.Get(job.Type)
	if err != nil {
		log.Printf("Worker %d: no executor for job type '%s'", workerID, job.Type)
		p.handleFailure(ctx, job, err, false)
		return
	}

	// Execute the job
	startTime := time.Now()
	err = exec.Execute(ctx, job.Payload)
	duration := time.Since(startTime)

	p.metrics.JobDuration.Observe(duration.Seconds())

	if err != nil {
		log.Printf("Worker %d: job %s failed after %v: %v",
			workerID, job.ID, duration, err)
		var permErr *executor.PermanentError
		retryable := !errors.As(err, &permErr)
		p.handleFailure(ctx, job, err, retryable)
	} else {
		log.Printf("Worker %d: job %s succeeded in %v",
			workerID, job.ID, duration)
		p.handleSuccess(ctx, job)
	}
}

// handleSuccess handles successful job execution.
func (p *WorkerPool) handleSuccess(ctx context.Context, job *model.Job) {
	if err := p.service.TransitionState(ctx, job.ID, state.SUCCEEDED); err != nil {
		log.Printf("Failed to transition job %s to SUCCEEDED: %v", job.ID, err)
		return
	}
	p.metrics.JobsSucceeded.Inc()
}

// handleFailure handles failed job execution.
func (p *WorkerPool) handleFailure(ctx context.Context, job *model.Job, execErr error, retryable bool) {
	if !retryable {
		log.Printf("Job %s failed permanently: %v", job.ID, execErr)
		if err := p.service.TransitionState(ctx, job.ID, state.FAILED); err != nil {
			log.Printf("Failed to transition job %s to FAILED: %v", job.ID, err)
			return
		}
		p.metrics.JobsFailed.Inc()
		return
	}

	// Retryable error
	if err := p.service.HandleFailure(ctx, job.ID, execErr); err != nil {
		log.Printf("Failed to handle job failure for %s: %v", job.ID, err)
		return
	}

	// Check if retries are now exhausted
	updatedJob, err := p.service.GetJob(ctx, job.ID)
	if err != nil {
		log.Printf("Failed to get job %s after failure: %v", job.ID, err)
		return
	}
	if updatedJob.State == state.FAILED {
		p.metrics.JobsFailed.Inc()
	}
}
