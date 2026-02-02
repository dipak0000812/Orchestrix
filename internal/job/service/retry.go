package service

import (
	"math"
	"math/rand"
	"time"
)

// RetryConfig holds retry policy settings.
type RetryConfig struct {
	BaseDelay time.Duration // Initial delay (e.g., 2s)
	MaxDelay  time.Duration // Maximum delay (e.g., 5m)
	MaxJitter time.Duration // Random jitter range
}

// DefaultRetryConfig returns sensible retry defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		BaseDelay: 10 * time.Millisecond,
		MaxDelay:  50 * time.Millisecond,
		MaxJitter: 0,
	}
}

// CalculateBackoff computes exponential backoff delay with jitter.
//
// Formula: min(BaseDelay * 2^attempt, MaxDelay) + jitter
//
// Example with BaseDelay=2s, MaxDelay=5m:
//
//	Attempt 1: 2s  * 2^0 = 2s  + jitter
//	Attempt 2: 2s  * 2^1 = 4s  + jitter
//	Attempt 3: 2s  * 2^2 = 8s  + jitter
//	Attempt 4: 2s  * 2^3 = 16s + jitter
//	Attempt 5: 2s  * 2^4 = 32s + jitter
//	Attempt 10: Capped at 5m + jitter
func (c RetryConfig) CalculateBackoff(attempt int) time.Duration {
	// Exponential backoff: BaseDelay * 2^attempt
	delay := float64(c.BaseDelay) * math.Pow(2, float64(attempt-1))

	// Cap at MaxDelay
	if delay > float64(c.MaxDelay) {
		delay = float64(c.MaxDelay)
	}

	// Add random jitter (prevents thundering herd)
	// Only add jitter if MaxJitter > 0 to avoid panic in rand.Int63n
	var jitter time.Duration
	if c.MaxJitter > 0 {
		jitter = time.Duration(rand.Int63n(int64(c.MaxJitter)))
	}

	return time.Duration(delay) + jitter
}
