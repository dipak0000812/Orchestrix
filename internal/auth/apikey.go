// Package auth provides API key generation, hashing, and verification for
// Orchestrix. Keys are never stored or logged in plaintext -- only a
// SHA-256 hash is persisted, following standard practice for bearer
// credentials (comparable to how password hashes are handled, though a
// fast hash is acceptable here specifically because API keys are
// high-entropy random values, not user-chosen passwords, so brute-forcing
// the hash isn't a practical attack the way it is for passwords).
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

// KeyPrefix is prepended to every generated key so keys are visually
// identifiable (e.g. in logs that accidentally capture a header, or when a
// developer is scanning a config file) and so a future key-format version
// bump doesn't collide with existing keys.
const KeyPrefix = "orx_"

// GeneratedKey holds a newly created API key. Plaintext is populated only
// at creation time and must be shown to the caller immediately -- it is
// never recoverable again afterward (only its hash is persisted).
type GeneratedKey struct {
	ID        string // stable identifier for the key row (safe to log)
	Plaintext string // the actual bearer credential -- show once, never store
	Hash      string // sha256 hex digest of Plaintext, what actually gets persisted
}

// GenerateAPIKey creates a new random API key: 32 bytes of crypto/rand
// entropy, base64url-encoded, prefixed with KeyPrefix.
func GenerateAPIKey() (*GeneratedKey, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("failed to generate key entropy: %w", err)
	}
	plaintext := KeyPrefix + base64.RawURLEncoding.EncodeToString(raw)

	entropy := ulid.Monotonic(rand.Reader, 0)
	id := ulid.MustNew(ulid.Timestamp(time.Now()), entropy).String()

	return &GeneratedKey{
		ID:        id,
		Plaintext: plaintext,
		Hash:      HashKey(plaintext),
	}, nil
}

// HashKey returns the hex-encoded SHA-256 digest of a plaintext key.
func HashKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// VerifyKeyHash compares two hex-encoded hashes in constant time, to avoid
// leaking information about a partial match via response-timing
// differences.
func VerifyKeyHash(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
