package api

import (
	"encoding/json"
	"time"

	"github.com/dipak0000812/orchestrix/internal/job/model"
)

// CreateJobRequest represents the request body for creating a job.
type CreateJobRequest struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// JobResponse represents a job in API responses.
type JobResponse struct {
	ID          string     `json:"id"`
	Type        string     `json:"type"`
	State       string     `json:"state"`
	Attempt     int        `json:"attempt"`
	MaxAttempts int        `json:"max_attempts"`
	LastError   *string    `json:"last_error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// ListJobsResponse represents the response for listing jobs.
type ListJobsResponse struct {
	Jobs  []JobResponse `json:"jobs"`
	Total int           `json:"total"`
}

// ErrorResponse represents an error response.
type ErrorResponse struct {
	Error string `json:"error"`
}

// HealthResponse represents the health check response.
type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

// toJobResponse converts a model.Job to JobResponse.
func toJobResponse(job *model.Job) JobResponse {
	return JobResponse{
		ID:          job.ID,
		Type:        job.Type,
		State:       string(job.State),
		Attempt:     job.Attempt,
		MaxAttempts: job.MaxAttempts,
		LastError:   job.LastError,
		CreatedAt:   job.CreatedAt,
		ScheduledAt: job.ScheduledAt,
		StartedAt:   job.StartedAt,
		CompletedAt: job.CompletedAt,
	}
}
