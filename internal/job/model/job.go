package model

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/dipak0000812/orchestrix/internal/job/state"
)

// Job represents a unit of work to be executed by the orchestration system.
// It contains all metadata needed to track the job through its lifecycle.
type Job struct {
	// ID uniquely identifies this job.
	// Use UUIDs or ULIDs for global uniqueness and time-sortability.
	ID string

	// Type specifies what kind of work this job performs.
	// Examples: "send_email", "process_video", "generate_report"
	Type string

	// Payload contains the job-specific parameters as JSON.
	// The structure depends on the job Type.
	// Example for "send_email": {"to": "user@example.com", "subject": "Hi"}
	Payload []byte

	// State tracks the current lifecycle state of the job.
	State state.State

	// Attempt is the current attempt number (1-indexed).
	// First execution is attempt 1, first retry is attempt 2, etc.
	Attempt int

	// MaxAttempts is the maximum number of execution attempts allowed.
	// After this many failures, the job transitions to FAILED state.
	MaxAttempts int

	// LastError stores the error message from the most recent failure.
	// Nil if the job hasn't failed yet.
	LastError *string

	// CreatedAt is when the job was first created.
	// Always set, never nil.
	CreatedAt time.Time

	// ScheduledAt is when the scheduler picked this job.
	// Nil until the job transitions to SCHEDULED state.
	ScheduledAt *time.Time

	// StartedAt is when the job began executing.
	// Nil until the job transitions to RUNNING state.
	StartedAt *time.Time

	// CompletedAt is when the job finished (success or permanent failure).
	// Nil until the job reaches a terminal state.
	CompletedAt *time.Time
}

// IsTerminal returns true if the job is in a terminal state.
// Terminal states: SUCCEEDED, FAILED, CANCELLED
func (j *Job) IsTerminal() bool {
	return j.State.IsTerminal()
}

// CanRetry returns true if the job can be retried after a failure.
// This is based on whether we've exhausted the maximum attempts.
func (j *Job) CanRetry() bool {
	return j.Attempt < j.MaxAttempts
}

// IncrementAttempt increases the attempt counter.
// Call this when transitioning to RETRYING state.
func (j *Job) IncrementAttempt() {
	j.Attempt++
}

// RecordError stores the error message from a failed execution.
func (j *Job) RecordError(err error) {
	if err != nil {
		msg := err.Error()
		j.LastError = &msg
	}
}

// ClearError removes any stored error message.
// Useful when retrying a job.
func (j *Job) ClearError() {
	j.LastError = nil
}

// Validate checks if the job has valid data.
// Returns an error if any validation rule is violated.
func (j *Job) Validate() error {
	// ID is required
	if j.ID == "" {
		return fmt.Errorf("job ID is required")
	}

	// Type is required
	if j.Type == "" {
		return fmt.Errorf("job type is required")
	}

	// Payload must be valid JSON (if not empty)
	if len(j.Payload) > 0 {
		if !json.Valid(j.Payload) {
			return fmt.Errorf("job payload is not valid JSON")
		}
	}

	// State must be valid
	if !j.State.IsValid() {
		return fmt.Errorf("invalid job state: %s", j.State)
	}

	// MaxAttempts must be at least 1
	if j.MaxAttempts < 1 {
		return fmt.Errorf("max attempts must be at least 1, got %d", j.MaxAttempts)
	}

	// Attempt must not exceed MaxAttempts
	if j.Attempt > j.MaxAttempts {
		return fmt.Errorf("attempt %d exceeds max attempts %d", j.Attempt, j.MaxAttempts)
	}

	// Attempt must be positive if job has started
	if j.Attempt < 0 {
		return fmt.Errorf("attempt must be non-negative, got %d", j.Attempt)
	}

	return nil
}
