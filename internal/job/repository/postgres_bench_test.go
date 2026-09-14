package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/dipak0000812/orchestrix/internal/job/model"
	"github.com/dipak0000812/orchestrix/internal/job/state"
)

// benchPayload is a small, fixed JSON payload used across benchmarks so
// payload marshaling cost doesn't dominate the measurement.
var benchPayload, _ = json.Marshal(map[string]string{"data": "benchmark"})

func newBenchJob(id string) *model.Job {
	return &model.Job{
		ID:          id,
		Type:        "compute_checksum",
		Payload:     benchPayload,
		State:       state.PENDING,
		Attempt:     1,
		MaxAttempts: 3,
		CreatedAt:   time.Now(),
	}
}

// BenchmarkCreate measures single-row insert throughput/latency against a
// real Postgres instance (connection pooling, network round trip, and the
// unique-index write all included, since that's what production insert
// cost actually looks like).
func BenchmarkCreate(b *testing.B) {
	repo := setupTestDB(b)
	ctx := context.Background()
	runNonce := time.Now().UnixNano()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		job := newBenchJob(fmt.Sprintf("bench_create_%d_%d", runNonce, i))
		if err := repo.Create(ctx, job); err != nil {
			b.Fatalf("Create failed: %v", err)
		}
	}
}

// BenchmarkGetByID measures point-lookup latency for a pre-populated table.
func BenchmarkGetByID(b *testing.B) {
	repo := setupTestDB(b)
	ctx := context.Background()
	runNonce := time.Now().UnixNano()

	const seedCount = 1000
	ids := make([]string, seedCount)
	for i := 0; i < seedCount; i++ {
		id := fmt.Sprintf("bench_get_%d_%d", runNonce, i)
		ids[i] = id
		if err := repo.Create(ctx, newBenchJob(id)); err != nil {
			b.Fatalf("seed Create failed: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := repo.GetByID(ctx, ids[i%seedCount]); err != nil {
			b.Fatalf("GetByID failed: %v", err)
		}
	}
}

// BenchmarkClaimPendingJobs measures the scheduler's core primitive: the
// atomic SELECT FOR UPDATE SKIP LOCKED + batch state transition that claims
// a batch of PENDING jobs. This is the single most important number for a
// "how many jobs/sec can the scheduler dispatch" claim, since every job
// passes through this exact call before a worker ever sees it.
func BenchmarkClaimPendingJobs(b *testing.B) {
	for _, batchSize := range []int{1, 10, 50} {
		b.Run(fmt.Sprintf("batch_%d", batchSize), func(b *testing.B) {
			repo := setupTestDB(b)
			ctx := context.Background()

			// The benchmark framework invokes this function body multiple
			// times with increasing b.N while calibrating, so the seeded
			// IDs must be unique per invocation (not just per iteration)
			// to avoid colliding with a previous calibration pass.
			runNonce := time.Now().UnixNano()

			// Each iteration claims up to batchSize jobs, so seed enough
			// PENDING jobs up front to satisfy the whole run without the
			// insert cost leaking into the timed loop.
			total := b.N * batchSize
			for i := 0; i < total; i++ {
				id := fmt.Sprintf("bench_claim_%d_%d_%d", runNonce, batchSize, i)
				if err := repo.Create(ctx, newBenchJob(id)); err != nil {
					b.Fatalf("seed Create failed: %v", err)
				}
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := repo.ClaimPendingJobs(ctx, batchSize); err != nil {
					b.Fatalf("ClaimPendingJobs failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkUpdateIfState measures the CAS-style conditional update used
// throughout the job lifecycle (RUNNING -> COMPLETED, RUNNING -> FAILED,
// etc.) to prevent lost updates from concurrent workers.
func BenchmarkUpdateIfState(b *testing.B) {
	repo := setupTestDB(b)
	ctx := context.Background()
	runNonce := time.Now().UnixNano()

	jobs := make([]*model.Job, b.N)
	for i := 0; i < b.N; i++ {
		job := newBenchJob(fmt.Sprintf("bench_cas_%d_%d", runNonce, i))
		if err := repo.Create(ctx, job); err != nil {
			b.Fatalf("seed Create failed: %v", err)
		}
		jobs[i] = job
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		jobs[i].State = state.RUNNING
		if err := repo.UpdateIfState(ctx, jobs[i], state.PENDING); err != nil {
			b.Fatalf("UpdateIfState failed: %v", err)
		}
	}
}
