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

// Scheduler polls the database for PENDING jobs and schedules them.
type Scheduler struct {
	repository   *repository.PostgresJobRepository
	pollInterval time.Duration
	batchSize    int
	jobChannel   chan *model.Job

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewScheduler creates a new scheduler.
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

// Start begins the scheduling loop.
func (s *Scheduler) Start() {
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

// pollAndSchedule finds and claims PENDING jobs atomically.
func (s *Scheduler) pollAndSchedule() {
	// Atomically claim pending jobs (locks + updates state to SCHEDULED)
	jobs, err := s.repository.ClaimPendingJobs(s.ctx, s.batchSize)
	if err != nil {
		log.Printf("Failed to claim pending jobs: %v", err)
		return
	}

	if len(jobs) == 0 {
		return // No jobs to schedule
	}

	log.Printf("Found %d pending jobs", len(jobs))

	// Send jobs to worker pool
	for _, job := range jobs {
		if err := s.sendToWorkers(job); err != nil {
			log.Printf("Failed to send job %s to workers: %v", job.ID, err)
			continue
		}
	}
}

// sendToWorkers sends a job to the worker pool channel.
func (s *Scheduler) sendToWorkers(job *model.Job) error {
	// Job is already in SCHEDULED state from ClaimPendingJobs
	select {
	case s.jobChannel <- job:
		log.Printf("Scheduled job %s (type: %s)", job.ID, job.Type)
		return nil

	case <-time.After(5 * time.Second):
		return fmt.Errorf("timeout sending job to channel")

	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}
