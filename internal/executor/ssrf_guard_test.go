package executor

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIsBlockedIP(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		blocked bool
	}{
		{"loopback v4", "127.0.0.1", true},
		{"loopback v6", "::1", true},
		{"private 10.x", "10.1.2.3", true},
		{"private 172.16.x", "172.16.0.5", true},
		{"private 192.168.x", "192.168.1.1", true},
		{"link-local / cloud metadata", "169.254.169.254", true},
		{"link-local v6", "fe80::1", true},
		{"unique local v6", "fc00::1", true},
		{"unspecified v4", "0.0.0.0", true},
		{"unspecified v6", "::", true},
		{"IPv4-mapped IPv6 loopback", "::ffff:127.0.0.1", true},
		{"public v4 (documentation range, not actually reached)", "203.0.113.5", false},
		{"public v4 (google dns, not actually reached)", "8.8.8.8", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse test IP %q", tt.ip)
			}
			got := isBlockedIP(ip)
			if got != tt.blocked {
				t.Errorf("isBlockedIP(%s) = %v, want %v", tt.ip, got, tt.blocked)
			}
		})
	}
}

func TestValidateWebhookURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"valid http", "http://example.com/hook", false},
		{"valid https", "https://example.com/hook", false},
		{"file scheme rejected", "file:///etc/passwd", true},
		{"gopher scheme rejected", "gopher://example.com", true},
		{"missing host", "http:///path", true},
		{"malformed url", "http://[::1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateWebhookURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateWebhookURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

// TestSSRFSafeExecutor_BlocksRealLocalServer is the test that actually
// matters: it proves the SSRF-safe executor refuses to connect to a real,
// running local HTTP server, not just that isBlockedIP returns the right
// boolean in isolation. httptest.NewServer listens on 127.0.0.1, which is
// exactly the kind of destination this guard exists to block.
func TestSSRFSafeExecutor_BlocksRealLocalServer(t *testing.T) {
	hit := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true // if this ever runs, the SSRF guard failed
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exec := NewSSRFSafeWebhookExecutor(2 * time.Second)
	payload := []byte(`{"url":"` + server.URL + `"}`)

	err := exec.Execute(context.Background(), payload)
	if err == nil {
		t.Fatal("expected the SSRF-safe executor to refuse a real request to a local server")
	}
	var blocked *ErrBlockedDestination
	if !errors.As(err, &blocked) {
		t.Errorf("expected error to wrap ErrBlockedDestination, got: %v", err)
	}
	var permErr *PermanentError
	if !errors.As(err, &permErr) {
		t.Errorf("expected an SSRF block to be a PermanentError (not retryable), got: %v", err)
	}
	if hit {
		t.Fatal("the local server handler was actually invoked -- SSRF guard did not block the connection")
	}
}

// TestSSRFSafeExecutor_AllowsPublicScheme proves the guard doesn't
// over-block: a well-formed https:// URL to a normal-looking public host
// passes the pre-flight scheme/structure validation (actual connection
// isn't attempted here to avoid a real external network dependency in
// tests, but this confirms legitimate requests aren't rejected before
// even reaching the network layer).
func TestSSRFSafeExecutor_AllowsPublicScheme(t *testing.T) {
	if _, err := validateWebhookURL("https://api.example.com/webhook"); err != nil {
		t.Errorf("a normal https URL should pass validation, got: %v", err)
	}
}

// TestUnsafeExecutor_StillReachesLocalServer documents, deliberately, that
// NewWebhookExecutor (the test-injectable constructor) does NOT apply the
// SSRF guard -- this is intentional (it's what lets other webhook tests
// point at an httptest.Server), but it means production code must use
// NewSSRFSafeWebhookExecutor, never this constructor directly.
func TestUnsafeExecutor_StillReachesLocalServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exec := NewWebhookExecutor(server.Client())
	payload := []byte(`{"url":"` + server.URL + `"}`)

	if err := exec.Execute(context.Background(), payload); err != nil {
		t.Errorf("NewWebhookExecutor should not apply SSRF blocking (by design, for test use); got unexpected error: %v", err)
	}
}
