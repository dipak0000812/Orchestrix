package executor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebhookExecutor_Success(t *testing.T) {
	var gotMethod, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	payload, _ := json.Marshal(WebhookPayload{
		URL:  server.URL,
		Body: json.RawMessage(`{"hello":"world"}`),
	})

	exec := NewWebhookExecutor(server.Client())
	if err := exec.Execute(context.Background(), payload); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST (default)", gotMethod)
	}
	if gotBody != `{"hello":"world"}` {
		t.Errorf("body = %s, want the configured JSON body", gotBody)
	}
}

func TestWebhookExecutor_ServerErrorIsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	payload, _ := json.Marshal(WebhookPayload{URL: server.URL})
	exec := NewWebhookExecutor(server.Client())

	err := exec.Execute(context.Background(), payload)
	if err == nil {
		t.Fatal("expected an error for a 503 response")
	}
	var permErr *PermanentError
	if errors.As(err, &permErr) {
		t.Error("a 503 should be retryable, not wrapped as PermanentError")
	}
}

func TestWebhookExecutor_ClientErrorIsPermanent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	payload, _ := json.Marshal(WebhookPayload{URL: server.URL})
	exec := NewWebhookExecutor(server.Client())

	err := exec.Execute(context.Background(), payload)
	if err == nil {
		t.Fatal("expected an error for a 400 response")
	}
	var permErr *PermanentError
	if !errors.As(err, &permErr) {
		t.Errorf("a 400 should be wrapped as PermanentError, got: %v", err)
	}
}

func TestWebhookExecutor_MissingURLIsPermanent(t *testing.T) {
	payload, _ := json.Marshal(WebhookPayload{})
	exec := NewWebhookExecutor(http.DefaultClient)

	err := exec.Execute(context.Background(), payload)
	var permErr *PermanentError
	if !errors.As(err, &permErr) {
		t.Errorf("missing url should be wrapped as PermanentError, got: %v", err)
	}
}

func TestWebhookExecutor_InvalidPayloadIsPermanent(t *testing.T) {
	exec := NewWebhookExecutor(http.DefaultClient)

	err := exec.Execute(context.Background(), []byte("not json"))
	var permErr *PermanentError
	if !errors.As(err, &permErr) {
		t.Errorf("invalid JSON should be wrapped as PermanentError, got: %v", err)
	}
}

func TestWebhookExecutor_ContextTimeoutIsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	payload, _ := json.Marshal(WebhookPayload{URL: server.URL})
	exec := NewWebhookExecutor(server.Client())

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := exec.Execute(ctx, payload)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	var permErr *PermanentError
	if errors.As(err, &permErr) {
		t.Error("a context timeout should be retryable, not wrapped as PermanentError")
	}
}
