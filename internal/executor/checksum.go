package executor

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// ChecksumPayload is the expected job payload for the "compute_checksum"
// job type.
type ChecksumPayload struct {
	// Data is the input to hash.
	Data string `json:"data"`
	// WorkFactor repeats the hashing WorkFactor times, chained
	// (digest -> hash again -> hash again ...). It defaults to 1 and exists
	// to give load/benchmark tests a way to dial in real, controllable CPU
	// cost per job without touching the executor's code.
	WorkFactor int `json:"work_factor,omitempty"`
}

// maxWorkFactor bounds the CPU cost a single compute_checksum job can
// request. At ~0.13us/iteration (measured: 128us for 1000 iterations),
// this caps a single job's worst-case hashing time at roughly 13ms --
// enough headroom for legitimate load-testing use (the field's original
// purpose) without letting a caller request billions of iterations and
// peg a worker indefinitely.
const maxWorkFactor = 100_000

// maxChecksumDataBytes bounds the size of the input being hashed. Only
// the first iteration actually hashes cp.Data (subsequent iterations
// re-hash the fixed-size 32-byte digest), so this matters less than
// maxWorkFactor for CPU cost, but an unbounded string here still means an
// unbounded JSON payload for the DB/JSON-decoding path to handle.
const maxChecksumDataBytes = 1 << 20 // 1 MiB

// ChecksumExecutor performs genuine CPU-bound work: it computes a SHA-256
// digest of the payload's data field, optionally chained WorkFactor times.
// Unlike DemoExecutor (which just sleeps), this is representative of real
// job types like file integrity checks, dedup fingerprinting, or content
// hashing pipelines.
type ChecksumExecutor struct{}

// NewChecksumExecutor creates a checksum executor.
func NewChecksumExecutor() *ChecksumExecutor {
	return &ChecksumExecutor{}
}

// Execute computes the digest. It periodically checks ctx so a cancelled
// or timed-out job doesn't keep burning CPU under a large work factor.
func (e *ChecksumExecutor) Execute(ctx context.Context, payload []byte) error {
	var cp ChecksumPayload
	if err := json.Unmarshal(payload, &cp); err != nil {
		return NewPermanentError(fmt.Errorf("invalid checksum payload: %w", err))
	}
	if cp.Data == "" {
		return NewPermanentError(fmt.Errorf("checksum payload missing required field: data"))
	}
	if len(cp.Data) > maxChecksumDataBytes {
		return NewPermanentError(fmt.Errorf("checksum payload data exceeds maximum size of %d bytes (got %d)", maxChecksumDataBytes, len(cp.Data)))
	}
	if cp.WorkFactor > maxWorkFactor {
		return NewPermanentError(fmt.Errorf("work_factor %d exceeds maximum of %d", cp.WorkFactor, maxWorkFactor))
	}

	iterations := cp.WorkFactor
	if iterations < 1 {
		iterations = 1
	}

	if _, err := chainedDigest(ctx, []byte(cp.Data), iterations); err != nil {
		return err
	}
	return nil
}

// chainedDigest hashes data with SHA-256, then re-hashes the resulting
// digest (iterations-1) more times. It checks ctx between iterations so a
// cancelled or timed-out job doesn't keep burning CPU under a large work
// factor.
func chainedDigest(ctx context.Context, data []byte, iterations int) ([]byte, error) {
	digest := data
	for i := 0; i < iterations; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		sum := sha256.Sum256(digest)
		digest = sum[:]
	}
	return digest, nil
}
