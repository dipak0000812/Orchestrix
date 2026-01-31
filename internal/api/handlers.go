package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/dipak0000812/orchestrix/internal/job/service"
	"github.com/dipak0000812/orchestrix/internal/job/state"
)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	jobService *service.JobService
}

// NewHandler creates a new API handler.
func NewHandler(jobService *service.JobService) *Handler {
	return &Handler{
		jobService: jobService,
	}
}

// CreateJob handles POST /api/v1/jobs
func (h *Handler) CreateJob(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var req CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Validate input
	if req.Type == "" {
		respondError(w, http.StatusBadRequest, "job type is required")
		return
	}

	// Call service
	job, err := h.jobService.CreateJob(r.Context(), req.Type, req.Payload)
	if err != nil {
		log.Printf("Failed to create job: %v", err)
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Return success
	respondJSON(w, http.StatusCreated, toJobResponse(job))
}

// GetJob handles GET /api/v1/jobs/{id}
func (h *Handler) GetJob(w http.ResponseWriter, r *http.Request) {
	// Extract job ID from URL path
	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "job ID is required")
		return
	}

	// Call service
	job, err := h.jobService.GetJob(r.Context(), id)
	if err != nil {
		log.Printf("Failed to get job %s: %v", id, err)
		respondError(w, http.StatusNotFound, "job not found")
		return
	}

	// Return success
	respondJSON(w, http.StatusOK, toJobResponse(job))
}

// ListJobs handles GET /api/v1/jobs
func (h *Handler) ListJobs(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	stateParam := r.URL.Query().Get("state")
	limitParam := r.URL.Query().Get("limit")

	// Default limit
	limit := 10
	if limitParam != "" {
		if parsed, err := strconv.Atoi(limitParam); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	// If no state filter, default to PENDING
	jobState := state.PENDING
	if stateParam != "" {
		jobState = state.State(stateParam)
		if !jobState.IsValid() {
			respondError(w, http.StatusBadRequest, "invalid state parameter")
			return
		}
	}

	// Call service
	jobs, err := h.jobService.ListJobsByState(r.Context(), jobState, limit)
	if err != nil {
		log.Printf("Failed to list jobs: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to list jobs")
		return
	}

	// Convert to response format
	jobResponses := make([]JobResponse, len(jobs))
	for i, job := range jobs {
		jobResponses[i] = toJobResponse(job)
	}

	// Return success
	respondJSON(w, http.StatusOK, ListJobsResponse{
		Jobs:  jobResponses,
		Total: len(jobResponses),
	})
}

// CancelJob handles DELETE /api/v1/jobs/{id}
func (h *Handler) CancelJob(w http.ResponseWriter, r *http.Request) {
	// Extract job ID from URL path
	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "job ID is required")
		return
	}

	// Call service
	if err := h.jobService.CancelJob(r.Context(), id); err != nil {
		log.Printf("Failed to cancel job %s: %v", id, err)
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Return success (204 No Content)
	w.WriteHeader(http.StatusNoContent)
}

// Health handles GET /health
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

// respondJSON sends a JSON response.
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// respondError sends an error response.
func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, ErrorResponse{Error: message})
}
