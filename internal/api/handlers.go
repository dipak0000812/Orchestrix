package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/dipak0000812/orchestrix/internal/auth"
	"github.com/dipak0000812/orchestrix/internal/job/service"
	"github.com/dipak0000812/orchestrix/internal/job/state"
	"github.com/dipak0000812/orchestrix/internal/metrics"
)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	jobService *service.JobService
	metrics    *metrics.Metrics
}

// NewHandler creates a new API handler.
func NewHandler(jobService *service.JobService, m *metrics.Metrics) *Handler {
	return &Handler{
		jobService: jobService,
		metrics:    m,
	}
}

func (h *Handler) CreateJob(w http.ResponseWriter, r *http.Request) {
	callerKeyID, ok := auth.CallerKeyID(r.Context())
	if !ok {
		// Defensive: should be unreachable, since auth.Middleware rejects
		// unauthenticated requests before they reach this handler. If this
		// ever fires, it's a wiring bug (a route registered outside the
		// auth middleware), not a runtime condition to recover from.
		log.Printf("CreateJob: no caller key ID on request context (auth middleware not applied?)")
		respondError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	var req CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.metrics.HTTPRequests.WithLabelValues("POST", "/api/v1/jobs", "400").Inc()
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.Type == "" {
		h.metrics.HTTPRequests.WithLabelValues("POST", "/api/v1/jobs", "400").Inc()
		respondError(w, http.StatusBadRequest, "job type is required")
		return
	}

	job, err := h.jobService.CreateJob(r.Context(), &callerKeyID, req.Type, req.Payload, req.DependsOn)
	if err != nil {
		// Log the real error server-side; never return it to the caller.
		// Internal error strings can include DB constraint names, driver
		// messages, and other implementation details that shouldn't be
		// exposed to an API consumer.
		log.Printf("Failed to create job: %v", err)
		h.metrics.HTTPRequests.WithLabelValues("POST", "/api/v1/jobs", "400").Inc()
		respondError(w, http.StatusBadRequest, "invalid job request")
		return
	}

	h.metrics.JobsCreated.Inc()
	h.metrics.HTTPRequests.WithLabelValues("POST", "/api/v1/jobs", "201").Inc()
	respondJSON(w, http.StatusCreated, toJobResponse(job))
}

func (h *Handler) GetJob(w http.ResponseWriter, r *http.Request) {
	callerKeyID, ok := auth.CallerKeyID(r.Context())
	if !ok {
		respondError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "job ID is required")
		return
	}

	job, err := h.jobService.GetJob(r.Context(), id)
	if err != nil || job == nil || job.OwnerKeyID == nil || *job.OwnerKeyID != callerKeyID {
		if err != nil {
			log.Printf("Failed to get job %s: %v", id, err)
		}
		// Deliberately identical response whether the job doesn't exist
		// or belongs to a different caller -- distinguishing them would
		// confirm to an unauthorized caller that a given job ID exists.
		respondError(w, http.StatusNotFound, "job not found")
		return
	}

	respondJSON(w, http.StatusOK, toJobResponse(job))
}

func (h *Handler) ListJobs(w http.ResponseWriter, r *http.Request) {
	callerKeyID, ok := auth.CallerKeyID(r.Context())
	if !ok {
		respondError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	stateParam := r.URL.Query().Get("state")
	limitParam := r.URL.Query().Get("limit")

	limit := 10
	if limitParam != "" {
		if parsed, err := strconv.Atoi(limitParam); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	jobState := state.PENDING
	if stateParam != "" {
		jobState = state.State(stateParam)
		if !jobState.IsValid() {
			respondError(w, http.StatusBadRequest, "invalid state parameter")
			return
		}
	}

	jobs, err := h.jobService.ListJobsByStateAndOwner(r.Context(), jobState, callerKeyID, limit)
	if err != nil {
		log.Printf("Failed to list jobs: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to list jobs")
		return
	}

	jobResponses := make([]JobResponse, len(jobs))
	for i, job := range jobs {
		jobResponses[i] = toJobResponse(job)
	}

	respondJSON(w, http.StatusOK, ListJobsResponse{
		Jobs:  jobResponses,
		Total: len(jobResponses),
	})
}

func (h *Handler) CancelJob(w http.ResponseWriter, r *http.Request) {
	callerKeyID, ok := auth.CallerKeyID(r.Context())
	if !ok {
		respondError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "job ID is required")
		return
	}

	job, err := h.jobService.GetJob(r.Context(), id)
	if err != nil || job == nil || job.OwnerKeyID == nil || *job.OwnerKeyID != callerKeyID {
		if err != nil {
			log.Printf("Failed to look up job %s for cancellation: %v", id, err)
		}
		respondError(w, http.StatusNotFound, "job not found")
		return
	}

	if err := h.jobService.CancelJob(r.Context(), id); err != nil {
		log.Printf("Failed to cancel job %s: %v", id, err)
		respondError(w, http.StatusBadRequest, "unable to cancel job")
		return
	}

	h.metrics.JobsCancelled.Inc()
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		// Headers are already written at this point, so the response body
		// may be partially sent; there's nothing left to do but log it.
		log.Printf("respondJSON: failed to encode response: %v", err)
	}
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, ErrorResponse{Error: message})
}
