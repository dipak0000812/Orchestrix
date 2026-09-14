package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/dipak0000812/orchestrix/internal/api"
	"github.com/dipak0000812/orchestrix/internal/auth"
	"github.com/dipak0000812/orchestrix/internal/executor"
	"github.com/dipak0000812/orchestrix/internal/job/dependency"
	"github.com/dipak0000812/orchestrix/internal/job/repository"
	"github.com/dipak0000812/orchestrix/internal/job/service"
	"github.com/dipak0000812/orchestrix/internal/job/state"
	"github.com/dipak0000812/orchestrix/internal/metrics"
)

// sharedMetrics is created once for the whole test binary. metrics.NewMetrics()
// registers on Prometheus's global default registry via promauto, which
// panics on a second registration in the same process -- so every test in
// this file reuses one instance rather than each creating its own.
var (
	sharedMetricsOnce sync.Once
	sharedMetrics     *metrics.Metrics
)

func testMetrics() *metrics.Metrics {
	sharedMetricsOnce.Do(func() {
		sharedMetrics = metrics.NewMetrics()
	})
	return sharedMetrics
}

// setupAuthedServer builds a real HTTP test server backed by a real
// Postgres connection, wired exactly like cmd/server/main.go (auth
// middleware wrapping the router, /health exempt). Returns the server and
// two distinct, real, registered API keys so tests can verify cross-owner
// isolation.
func setupAuthedServer(t *testing.T) (server *httptest.Server, keyA, keyB string) {
	t.Helper()
	cfg := repository.DBConfig{
		Host: "localhost", Port: 5434, User: "orchestrix",
		Password: "orchestrix_dev_password", Database: "orchestrix_dev",
		SSLMode: "disable", MaxConnections: 10, MinConnections: 1,
		MaxConnLifetime: 30 * time.Minute, MaxConnIdleTime: 5 * time.Minute,
	}
	pool, err := repository.NewConnectionPool(context.Background(), cfg)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DELETE FROM jobs"); err != nil {
		t.Fatalf("failed to clean jobs table: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DELETE FROM api_keys"); err != nil {
		t.Fatalf("failed to clean api_keys table: %v", err)
	}

	repo := repository.NewPostgresJobRepository(pool)
	stateMachine := state.NewStateMachine()
	idGen := service.NewULIDGenerator()
	retryConfig := service.DefaultRetryConfig()
	jobService := service.NewJobService(repo, stateMachine, idGen, retryConfig, dependency.NewResolver(repo))

	executors := executor.NewExecutorRegistry()
	executors.Register("demo_job", executor.NewDemoExecutor(10*time.Millisecond))

	m := testMetrics()
	handler := api.NewHandler(jobService, m)

	keyRepo := auth.NewKeyRepository(pool)
	genA, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("failed to generate key A: %v", err)
	}
	if err := keyRepo.Create(context.Background(), genA.ID, genA.Hash, "test-key-a"); err != nil {
		t.Fatalf("failed to persist key A: %v", err)
	}
	genB, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("failed to generate key B: %v", err)
	}
	if err := keyRepo.Create(context.Background(), genB.ID, genB.Hash, "test-key-b"); err != nil {
		t.Fatalf("failed to persist key B: %v", err)
	}

	router := http.NewServeMux()
	router.HandleFunc("POST /api/v1/jobs", handler.CreateJob)
	router.HandleFunc("GET /api/v1/jobs/{id}", handler.GetJob)
	router.HandleFunc("GET /api/v1/jobs", handler.ListJobs)
	router.HandleFunc("DELETE /api/v1/jobs/{id}", handler.CancelJob)
	router.HandleFunc("GET /health", handler.Health)

	bodyLimited := api.MaxBodySizeMiddleware(1 << 20)(router) // 1 MiB, matches production wiring
	authedRouter := auth.Middleware(keyRepo, map[string]bool{"/health": true})(bodyLimited)
	srv := httptest.NewServer(authedRouter)
	t.Cleanup(srv.Close)

	return srv, genA.Plaintext, genB.Plaintext
}

func createJob(t *testing.T, srv *httptest.Server, apiKey string) (status int, id string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"type": "demo_job", "payload": map[string]string{"x": "1"}})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return resp.StatusCode, ""
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}
	return resp.StatusCode, created.ID
}

func TestAuth_NoCredentials_Rejected(t *testing.T) {
	srv, _, _ := setupAuthedServer(t)

	status, _ := createJob(t, srv, "")
	if status != http.StatusUnauthorized {
		t.Errorf("CreateJob with no Authorization header: got status %d, want 401", status)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/jobs", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("ListJobs with no Authorization header: got status %d, want 401", resp.StatusCode)
	}
}

func TestAuth_InvalidKey_Rejected(t *testing.T) {
	srv, _, _ := setupAuthedServer(t)

	status, _ := createJob(t, srv, "orx_this_key_does_not_exist")
	if status != http.StatusUnauthorized {
		t.Errorf("CreateJob with an invalid key: got status %d, want 401", status)
	}
}

func TestAuth_HealthEndpoint_ExemptFromAuth(t *testing.T) {
	srv, _, _ := setupAuthedServer(t)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/health", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /health with no credentials: got status %d, want 200 (should be exempt)", resp.StatusCode)
	}
}

func TestAuth_ValidKey_CanCreateAndReadOwnJob(t *testing.T) {
	srv, keyA, _ := setupAuthedServer(t)

	status, id := createJob(t, srv, keyA)
	if status != http.StatusCreated {
		t.Fatalf("CreateJob with a valid key: got status %d, want 201", status)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/jobs/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+keyA)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("owner GetJob: got status %d, want 200", resp.StatusCode)
	}
}

func TestAuth_CrossOwner_CannotReadOthersJob(t *testing.T) {
	srv, keyA, keyB := setupAuthedServer(t)

	status, id := createJob(t, srv, keyA)
	if status != http.StatusCreated {
		t.Fatalf("CreateJob (key A) failed: status %d", status)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/jobs/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+keyB)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("key B reading key A's job: got status %d, want 404 (not 403 -- existence shouldn't be confirmable)", resp.StatusCode)
	}
}

func TestAuth_CrossOwner_CannotCancelOthersJob(t *testing.T) {
	srv, keyA, keyB := setupAuthedServer(t)

	status, id := createJob(t, srv, keyA)
	if status != http.StatusCreated {
		t.Fatalf("CreateJob (key A) failed: status %d", status)
	}

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/jobs/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+keyB)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("key B cancelling key A's job: got status %d, want 404", resp.StatusCode)
	}
}

func TestAuth_ListJobs_OnlyShowsOwnJobs(t *testing.T) {
	srv, keyA, keyB := setupAuthedServer(t)

	statusA, _ := createJob(t, srv, keyA)
	if statusA != http.StatusCreated {
		t.Fatalf("CreateJob (key A) failed: status %d", statusA)
	}
	statusB, _ := createJob(t, srv, keyB)
	if statusB != http.StatusCreated {
		t.Fatalf("CreateJob (key B) failed: status %d", statusB)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/jobs?state=PENDING&limit=100", nil)
	req.Header.Set("Authorization", "Bearer "+keyA)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var listResp struct {
		Total int `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}
	if listResp.Total != 1 {
		t.Errorf("ListJobs for key A: got %d jobs, want exactly 1 (key A's own job, not key B's)", listResp.Total)
	}
}

// TestMaxBodySize_RejectsOversizedRequest proves the body-size limit is
// actually enforced against a real oversized request through the real
// server, not just that the middleware compiles.
func TestMaxBodySize_RejectsOversizedRequest(t *testing.T) {
	srv, keyA, _ := setupAuthedServer(t)

	// 2 MiB of padding in the payload -- well over the 1 MiB limit
	// applied in setupAuthedServer, matching production wiring.
	oversized := make([]byte, 2<<20)
	for i := range oversized {
		oversized[i] = 'x'
	}
	body, _ := json.Marshal(map[string]any{
		"type":    "demo_job",
		"payload": map[string]string{"padding": string(oversized)},
	})

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+keyA)

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("oversized request body: got status %d, want 400", resp.StatusCode)
	}

	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil {
		if errResp.Error != "invalid JSON body" {
			t.Errorf("expected the generic 'invalid JSON body' message (not a raw error leak), got: %q", errResp.Error)
		}
	}
}

// TestMaxBodySize_AllowsNormalRequest confirms the limit doesn't
// over-block ordinary, well within-limit requests.
func TestMaxBodySize_AllowsNormalRequest(t *testing.T) {
	srv, keyA, _ := setupAuthedServer(t)

	status, _ := createJob(t, srv, keyA)
	if status != http.StatusCreated {
		t.Errorf("a normal-sized request should succeed, got status %d", status)
	}
}
