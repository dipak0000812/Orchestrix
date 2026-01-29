package worker

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/dipak0000812/orchestrix/internal/executor"
	"github.com/dipak0000812/orchestrix/internal/job/model"
	"github.com/dipak0000812/orchestrix/internal/job/service"
	"github.com/dipak0000812/orchestrix/internal/job/state"
)

// WorkerPool manages a pool of workers that execute jobs.
type WorkerPool struct {
	numWorkers int
	jobChannel chan *model.Job
	executors  *executor.ExecutorRegistry
	service    *service.JobService
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
	jobTimeout time.Duration,
) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())

	return &WorkerPool{
		numWorkers: numWorkers,
		jobChannel: jobChannel,
		executors:  executors,
		service:    jobService,
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
	p.cancel()  // Signal workers to stop
	p.wg.Wait() // Wait for all workers to finish
	log.Println("Worker pool stopped")
}

// worker is the main worker loop.
func (p *WorkerPool) worker(id int) {
	defer p.wg.Done()
	defer func() {
		// Recover from panics to prevent worker death
		if r := recover(); r != nil {
			log.Printf("Worker %d panic recovered: %v", id, r)
		}
	}()

	log.Printf("Worker %d started", id)

	for {
		select {
		case job := <-p.jobChannel:
			// Execute the job
			p.executeJob(id, job)

		case <-p.ctx.Done():
			// Shutdown signal received
			log.Printf("Worker %d stopping", id)
			return
		}
	}
}

// executeJob executes a single job.
func (p *WorkerPool) executeJob(workerID int, job *model.Job) {
	log.Printf("Worker %d executing job %s (type: %s, attempt: %d)",
		workerID, job.ID, job.Type, job.Attempt)

	// Create context with timeout
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
		log.Printf("Worker %d: no executor for job type %s: %v",
			workerID, job.Type, err)
		p.handleFailure(ctx, job, err)
		return
	}

	// Execute the job
	startTime := time.Now()
	err = exec.Execute(ctx, job.Payload)
	duration := time.Since(startTime)

	if err != nil {
		// Execution failed
		log.Printf("Worker %d: job %s failed after %v: %v",
			workerID, job.ID, duration, err)
		p.handleFailure(ctx, job, err)
	} else {
		// Execution succeeded
		log.Printf("Worker %d: job %s succeeded in %v",
			workerID, job.ID, duration)
		p.handleSuccess(ctx, job)
	}
}

// handleSuccess handles successful job execution.
func (p *WorkerPool) handleSuccess(ctx context.Context, job *model.Job) {
	if err := p.service.TransitionState(ctx, job.ID, state.SUCCEEDED); err != nil {
		log.Printf("Failed to transition job %s to SUCCEEDED: %v", job.ID, err)
	}
}

// handleFailure handles failed job execution.
func (p *WorkerPool) handleFailure(ctx context.Context, job *model.Job, execErr error) {
	if err := p.service.HandleFailure(ctx, job.ID, execErr); err != nil {
		log.Printf("Failed to handle job failure for %s: %v", job.ID, err)
	}
}
