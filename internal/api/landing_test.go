package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRootHandler_HTML(t *testing.T) {
	h := NewHandler(nil, nil)
	h.SetDemoKey("orx_test_demo_key")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	w := httptest.NewRecorder()

	h.Root(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("expected text/html content type, got %s", contentType)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Orchestrix") {
		t.Errorf("expected body to contain 'Orchestrix'")
	}
	if !strings.Contains(body, "orx_test_demo_key") {
		t.Errorf("expected body to contain demo key 'orx_test_demo_key'")
	}
	if !strings.Contains(body, "/healthz") {
		t.Errorf("expected body to reference /healthz")
	}
}

func TestRootHandler_JSON(t *testing.T) {
	h := NewHandler(nil, nil)
	h.SetDemoKey("orx_test_demo_key")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	h.Root(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if resp["service"] != "Orchestrix" {
		t.Errorf("expected service Orchestrix, got %v", resp["service"])
	}
}

func TestHealthzEndpoint(t *testing.T) {
	h := NewHandler(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	h.Health(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}

	if resp.Status != "healthy" {
		t.Errorf("expected healthy status, got %s", resp.Status)
	}
}

func TestRateLimiter(t *testing.T) {
	rl := NewRateLimiter(60, 3) // 3 burst tokens
	defer rl.Stop()

	client := "test-client"

	// First 3 should pass
	for i := 0; i < 3; i++ {
		if !rl.Allow(client) {
			t.Errorf("expected request %d to be allowed", i+1)
		}
	}

	// 4th request should be rejected
	if rl.Allow(client) {
		t.Errorf("expected 4th request to exceed burst capacity and be rejected")
	}
}
