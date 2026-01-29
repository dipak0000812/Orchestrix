package scheduler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/dipak0000812/orchestrix/internal/job/model"
	"github.com/dipak0000812/orchestrix/internal/job/service"
	"github.com/dipak0000812/orchestrix/internal/job/state"
)

// Scheduler polls the database for PENDING jobs and schedules them.
type Scheduler struct {
	service      *service.JobService
	pollInterval time.Duration
	batchSize    int
	jobChannel   chan *model.Job

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewScheduler creates a new scheduler.
func NewScheduler(
	jobService *service.JobService,
	pollInterval time.Duration,
	batchSize int,
	jobChannel chan *model.Job,
) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())

	return &Scheduler{
		service:      jobService,
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
			// Poll for jobs
			s.pollAndSchedule()

		case <-s.ctx.Done():
			// Shutdown signal received
			return
		}
	}
}

// pollAndSchedule finds PENDING jobs and schedules them.
func (s *Scheduler) pollAndSchedule() {
	// Find PENDING jobs
	jobs, err := s.service.ListJobsByState(s.ctx, state.PENDING, s.batchSize)
	if err != nil {
		log.Printf("Failed to list pending jobs: %v", err)
		return
	}

	if len(jobs) == 0 {
		return // No jobs to schedule
	}

	log.Printf("Found %d pending jobs", len(jobs))

	// Schedule each job
	for _, job := range jobs {
		if err := s.scheduleJob(job); err != nil {
			log.Printf("Failed to schedule job %s: %v", job.ID, err)
			continue
		}
	}
}

// scheduleJob transitions a job to SCHEDULED and sends it to the worker pool.
func (s *Scheduler) scheduleJob(job *model.Job) error {
	// Transition to SCHEDULED
	if err := s.service.TransitionState(s.ctx, job.ID, state.SCHEDULED); err != nil {
		return err
	}

	// Update local copy's state (so workers see correct state)
	job.State = state.SCHEDULED

	// Send to job channel (non-blocking with timeout)
	select {
	case s.jobChannel <- job:
		log.Printf("Scheduled job %s (type: %s)", job.ID, job.Type)
		return nil

	case <-time.After(5 * time.Second):
		// Channel full for 5 seconds, something's wrong
		return fmt.Errorf("timeout sending job to channel")

	case <-s.ctx.Done():
		// Shutdown in progress
		return s.ctx.Err()
	}
}
