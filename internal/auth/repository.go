package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrKeyNotFound is returned when a key hash has no matching, non-revoked
// row.
var ErrKeyNotFound = errors.New("api key not found or revoked")

// KeyRecord is a persisted API key (never includes the plaintext value).
type KeyRecord struct {
	ID        string
	Name      string
	CreatedAt time.Time
	RevokedAt *time.Time
}

// KeyRepository provides Postgres-backed storage for API keys.
type KeyRepository struct {
	pool *pgxpool.Pool
}

// NewKeyRepository creates a KeyRepository using the given connection pool
// (the same pool used by the rest of the application -- no separate
// database or connection is introduced).
func NewKeyRepository(pool *pgxpool.Pool) *KeyRepository {
	return &KeyRepository{pool: pool}
}

// Create persists a new API key record. Only the hash is stored; the
// plaintext (available only on the *GeneratedKey* returned by
// GenerateAPIKey) is never written anywhere.
func (r *KeyRepository) Create(ctx context.Context, id, hash, name string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO api_keys (id, key_hash, name) VALUES ($1, $2, $3)`,
		id, hash, name,
	)
	if err != nil {
		return fmt.Errorf("failed to create api key: %w", err)
	}
	return nil
}

// LookupByHash returns the key record matching the given hash, if it
// exists and has not been revoked. Returns ErrKeyNotFound otherwise --
// callers must treat "not found" and "revoked" identically (both mean
// "reject this request"), so this deliberately doesn't distinguish them in
// its return value, avoiding any temptation to leak that distinction to
// an API caller.
func (r *KeyRepository) LookupByHash(ctx context.Context, hash string) (*KeyRecord, error) {
	var rec KeyRecord
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, created_at, revoked_at FROM api_keys
		 WHERE key_hash = $1 AND revoked_at IS NULL`,
		hash,
	).Scan(&rec.ID, &rec.Name, &rec.CreatedAt, &rec.RevokedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrKeyNotFound
		}
		return nil, fmt.Errorf("failed to look up api key: %w", err)
	}
	return &rec, nil
}

// Count returns the total number of API key rows (revoked or not), used
// at startup to decide whether to bootstrap an initial key.
func (r *KeyRepository) Count(ctx context.Context) (int, error) {
	var count int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM api_keys`).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count api keys: %w", err)
	}
	return count, nil
}
