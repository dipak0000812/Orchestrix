package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dipak0000812/orchestrix/internal/auth"
)

type clientBucket struct {
	tokens     float64
	lastRefill time.Time
}

// RateLimiter enforces token-bucket rate limiting per client (API key or IP).
type RateLimiter struct {
	mu          sync.Mutex
	buckets     map[string]*clientBucket
	capacity    float64
	refillRate  float64 // tokens per second
	cleanTicker *time.Ticker
	stopCleanup chan struct{}
}

// NewRateLimiter creates a RateLimiter with the specified requests-per-minute and burst capacity.
func NewRateLimiter(requestsPerMin int, burst int) *RateLimiter {
	rl := &RateLimiter{
		buckets:     make(map[string]*clientBucket),
		capacity:    float64(burst),
		refillRate:  float64(requestsPerMin) / 60.0,
		cleanTicker: time.NewTicker(3 * time.Minute),
		stopCleanup: make(chan struct{}),
	}

	go rl.cleanupLoop()
	return rl
}

func (rl *RateLimiter) cleanupLoop() {
	for {
		select {
		case <-rl.cleanTicker.C:
			rl.mu.Lock()
			cutoff := time.Now().Add(-10 * time.Minute)
			for key, b := range rl.buckets {
				if b.lastRefill.Before(cutoff) {
					delete(rl.buckets, key)
				}
			}
			rl.mu.Unlock()
		case <-rl.stopCleanup:
			rl.cleanTicker.Stop()
			return
		}
	}
}

// Stop terminates the periodic cleanup goroutine.
func (rl *RateLimiter) Stop() {
	close(rl.stopCleanup)
}

// Allow reports whether a request from clientKey is allowed.
func (rl *RateLimiter) Allow(clientKey string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	bucket, exists := rl.buckets[clientKey]
	if !exists {
		rl.buckets[clientKey] = &clientBucket{
			tokens:     rl.capacity - 1.0,
			lastRefill: now,
		}
		return true
	}

	elapsed := now.Sub(bucket.lastRefill).Seconds()
	bucket.tokens += elapsed * rl.refillRate
	if bucket.tokens > rl.capacity {
		bucket.tokens = rl.capacity
	}
	bucket.lastRefill = now

	if bucket.tokens >= 1.0 {
		bucket.tokens -= 1.0
		return true
	}

	return false
}

// Middleware returns an HTTP middleware wrapping handlers with rate limiting.
func (rl *RateLimiter) Middleware(exemptPaths map[string]bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if exemptPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			clientKey := extractClientKey(r)
			if !rl.Allow(clientKey) {
				w.Header().Set("Retry-After", "5")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":"rate limit exceeded, please retry in a few seconds"}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func extractClientKey(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		key := strings.TrimPrefix(authHeader, "Bearer ")
		if key != "" {
			return "key:" + auth.HashKey(key)
		}
	}

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return "ip:" + strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return "ip:" + strings.TrimSpace(xri)
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return "ip:" + host
	}
	return "ip:" + r.RemoteAddr
}
