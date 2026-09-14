package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// BenchmarkChecksumExecutor measures real per-job CPU cost at several work
// factors, since compute_checksum's work_factor field exists specifically
// to give load/stress tests a dial for per-job cost.
func BenchmarkChecksumExecutor(b *testing.B) {
	for _, wf := range []int{1, 10, 100, 1000} {
		b.Run(fmt.Sprintf("work_factor_%d", wf), func(b *testing.B) {
			exec := NewChecksumExecutor()
			payload, _ := json.Marshal(ChecksumPayload{Data: "benchmark-payload", WorkFactor: wf})
			ctx := context.Background()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := exec.Execute(ctx, payload); err != nil {
					b.Fatalf("Execute failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkWebhookExecutor measures the executor's own overhead (payload
// parsing, request construction) on top of a fast local server, isolating
// it from real network latency.
func BenchmarkWebhookExecutor(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exec := NewWebhookExecutor(server.Client())
	payload, _ := json.Marshal(WebhookPayload{URL: server.URL})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := exec.Execute(ctx, payload); err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
	}
}
