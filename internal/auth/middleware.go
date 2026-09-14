package auth

import (
	"context"
	"net/http"
	"strings"
)

type contextKey string

// callerKeyIDContextKey stores the authenticated caller's API key ID on
// the request context, set by Middleware and read by handlers that need
// to scope data to the caller (job ownership checks).
const callerKeyIDContextKey contextKey = "auth.caller_key_id"

// Middleware enforces bearer API-key authentication on every request
// except the given exempt paths (typically just "/health", since
// orchestration/load-balancer liveness probes can't supply a credential).
// On success it stores the authenticated key's ID on the request context
// for downstream handlers to use for ownership scoping.
func Middleware(keyRepo *KeyRepository, exemptPaths map[string]bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if exemptPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			const prefix = "Bearer "
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, prefix) {
				respondUnauthorized(w, "missing or malformed Authorization header (expected: Bearer <api-key>)")
				return
			}
			plaintext := strings.TrimPrefix(authHeader, prefix)
			if plaintext == "" {
				respondUnauthorized(w, "missing or malformed Authorization header (expected: Bearer <api-key>)")
				return
			}

			rec, err := keyRepo.LookupByHash(r.Context(), HashKey(plaintext))
			if err != nil {
				// Deliberately the same generic message whether the key
				// doesn't exist, was revoked, or a DB error occurred --
				// distinguishing these to the caller would leak
				// information useful for enumerating valid key hashes or
				// probing system state.
				respondUnauthorized(w, "invalid or revoked API key")
				return
			}

			ctx := context.WithValue(r.Context(), callerKeyIDContextKey, rec.ID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CallerKeyID returns the authenticated caller's API key ID from a
// request context populated by Middleware. Returns ("", false) if called
// on a context that didn't go through Middleware (a programming error in
// any handler that requires auth, not a runtime condition to recover
// from gracefully).
func CallerKeyID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(callerKeyIDContextKey).(string)
	return v, ok
}

func respondUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"` + message + `"}`))
}
