package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// DemoExecutor is a simple executor for testing.
// Just logs the payload and simulates work.
type DemoExecutor struct {
	simulatedDuration time.Duration
}

// NewDemoExecutor creates a demo executor.
func NewDemoExecutor(duration time.Duration) *DemoExecutor {
	return &DemoExecutor{
		simulatedDuration: duration,
	}
}

// Execute simulates job execution.
func (e *DemoExecutor) Execute(ctx context.Context, payload []byte) error {
	// Parse payload (just for demonstration)
	var data map[string]interface{}
	if err := json.Unmarshal(payload, &data); err != nil {
		return fmt.Errorf("invalid payload: %w", err)
	}

	// Simulate work
	select {
	case <-time.After(e.simulatedDuration):
		// Work completed
		return nil
	case <-ctx.Done():
		// Context cancelled (timeout or shutdown)
		return ctx.Err()
	}
}

// FailingExecutor always fails (for testing failure scenarios).
type FailingExecutor struct{}

// NewFailingExecutor creates a failing executor.
func NewFailingExecutor() *FailingExecutor {
	return &FailingExecutor{}
}

// Execute always returns an error.
func (e *FailingExecutor) Execute(ctx context.Context, payload []byte) error {
	return fmt.Errorf("simulated failure")
}
