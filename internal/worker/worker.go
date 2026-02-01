package worker

import (
	"context"
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
	numWorkers int
	jobChannel chan *model.Job
	executors  *executor.ExecutorRegistry
	service    *service.JobService
	metrics    *metrics.Metrics
	jobTimeout time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
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
	ctx, cancel := context.WithCancel(context.Background())

	return &WorkerPool{
		numWorkers: numWorkers,
		jobChannel: jobChannel,
		executors:  executors,
		service:    jobService,
		metrics:    m,
		jobTimeout: jobTimeout,
		ctx:        ctx,
		cancel:     cancel,
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

// Stop gracefully stops all workers.
func (p *WorkerPool) Stop() {
	log.Println("Worker pool stopping...")
	p.cancel()
	p.wg.Wait()
	log.Println("Worker pool stopped")
}

// worker is the main worker loop.
func (p *WorkerPool) worker(id int) {
	defer p.wg.Done()

	log.Printf("Worker %d started", id)

	for {
		select {
		case job := <-p.jobChannel:
			p.executeJob(id, job)

		case <-p.ctx.Done():
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
			ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
			defer cancel()
			p.handleFailure(ctx, job, fmt.Errorf("panic: %v", r), false)
		}
	}()

	log.Printf("Worker %d executing job %s (type: %s, attempt: %d)",
		workerID, job.ID, job.Type, job.Attempt)

	ctx, cancel := context.WithTimeout(p.ctx, p.jobTimeout)
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
		p.handleFailure(ctx, job, err, true)
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
