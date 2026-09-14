package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// WebhookPayload is the expected job payload for the "http_webhook" job
// type. It lets a job trigger an arbitrary outbound HTTP call, which is a
// common real-world orchestration primitive (notify a service, call an
// internal API, hit a downstream integration).
type WebhookPayload struct {
	URL     string            `json:"url"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// WebhookExecutor performs a real outbound HTTP request per job.
type WebhookExecutor struct {
	client *http.Client
}

// NewWebhookExecutor creates a webhook executor using the given HTTP
// client verbatim -- no SSRF protection is applied to a client supplied
// this way, since this constructor exists specifically so tests can point
// at an httptest.Server (which listens on loopback, a destination
// production traffic must never be allowed to reach). Production code
// must use NewSSRFSafeWebhookExecutor instead.
func NewWebhookExecutor(client *http.Client) *WebhookExecutor {
	if client == nil {
		client = http.DefaultClient
	}
	return &WebhookExecutor{client: client}
}

// NewSSRFSafeWebhookExecutor creates a webhook executor whose HTTP client
// validates every connection (including ones made to follow a redirect)
// against a blocklist of loopback/private/link-local/unspecified
// destinations at actual dial time -- see safeDialContext for why dial-time
// validation matters versus checking the URL string alone. This is what
// production wiring (cmd/server/main.go) must use.
func NewSSRFSafeWebhookExecutor(timeout time.Duration) *WebhookExecutor {
	return &WebhookExecutor{
		client: &http.Client{
			Timeout:   timeout,
			Transport: newSSRFSafeTransport(),
		},
	}
}

// Execute sends the configured HTTP request. The job's context (which
// already carries the worker's per-job timeout) governs the request
// deadline, so a slow or hung endpoint fails the same way any other job
// timeout does.
func (e *WebhookExecutor) Execute(ctx context.Context, payload []byte) error {
	var wp WebhookPayload
	if err := json.Unmarshal(payload, &wp); err != nil {
		// Malformed payload will never succeed on retry.
		return NewPermanentError(fmt.Errorf("invalid webhook payload: %w", err))
	}

	if wp.URL == "" {
		return NewPermanentError(fmt.Errorf("webhook payload missing required field: url"))
	}

	if _, err := validateWebhookURL(wp.URL); err != nil {
		// An invalid scheme or malformed URL will never succeed on retry.
		return NewPermanentError(fmt.Errorf("webhook URL rejected: %w", err))
	}

	method := wp.Method
	if method == "" {
		method = http.MethodPost
	}

	var bodyReader io.Reader
	if len(wp.Body) > 0 {
		bodyReader = bytes.NewReader(wp.Body)
	}

	req, err := http.NewRequestWithContext(ctx, method, wp.URL, bodyReader)
	if err != nil {
		// A malformed method/URL is a permanent, not transient, problem.
		return NewPermanentError(fmt.Errorf("failed to build request: %w", err))
	}
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range wp.Headers {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		var blocked *ErrBlockedDestination
		if errors.As(err, &blocked) {
			return NewPermanentError(fmt.Errorf("webhook request blocked: %w", err))
		}
		// Network-level failures (DNS, connection refused, timeout) are
		// transient by nature: retry.
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	// Drain and discard the body so the connection can be reused, but cap
	// it to avoid an unbounded read from a misbehaving endpoint.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		// Rate limiting and server errors are worth retrying.
		return fmt.Errorf("webhook returned retryable status %d", resp.StatusCode)
	default:
		// Other 4xx (bad request, unauthorized, not found, ...) won't be
		// fixed by retrying with the same payload.
		return NewPermanentError(fmt.Errorf("webhook returned permanent status %d", resp.StatusCode))
	}
}
