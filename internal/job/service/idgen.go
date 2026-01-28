package service

import (
	"crypto/rand"
	"time"

	"github.com/oklog/ulid/v2"
)

// IDGenerator generates unique IDs for jobs.
// Interface allows mocking in tests.
type IDGenerator interface {
	Generate() string
}

// ULIDGenerator generates ULIDs (Universally Unique Lexicographically Sortable IDs).
// ULIDs are:
//   - Time-ordered (sortable by creation time)
//   - 128-bit (collision-resistant like UUIDs)
//   - Base32 encoded (URL-safe, case-insensitive)
type ULIDGenerator struct {
	entropy *ulid.MonotonicEntropy
}

// NewULIDGenerator creates a new ULID generator with monotonic entropy.
// Monotonic ensures IDs generated in same millisecond are still ordered.
func NewULIDGenerator() *ULIDGenerator {
	// Use crypto/rand for secure randomness
	entropy := ulid.Monotonic(rand.Reader, 0)

	return &ULIDGenerator{
		entropy: entropy,
	}
}

// Generate creates a new ULID string.
// Format: 01HQX7Z9PMRGWKT8HHFQNR3XYZ (26 characters)
func (g *ULIDGenerator) Generate() string {
	// Generate ULID with current timestamp
	id := ulid.MustNew(ulid.Timestamp(time.Now()), g.entropy)

	// Return as string
	return id.String()
}
