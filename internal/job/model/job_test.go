package model

import (
	"errors"
	"testing"
	"time"

	"github.com/dipak0000812/orchestrix/internal/job/state"
)

// TestJob_IsTerminal verifies that the IsTerminal method correctly identifies
// whether a job is in a terminal state (SUCCEEDED, FAILED, or CANCELLED).
//
// WHY THIS TEST MATTERS:
// Terminal states are "end of life" for a job. Once a job reaches a terminal state,
// it should never transition to any other state. This test ensures we can reliably
// detect when a job is "done" so we don't accidentally try to reschedule or retry it.
//
// REAL-WORLD EXAMPLE:
// Before deleting old jobs from the database, we check if they're terminal.
// If IsTerminal() has a bug, we might delete active jobs or keep finished ones forever.
func TestJob_IsTerminal(t *testing.T) {
	// Table-driven test: Define all test cases in a slice
	// Each test case has: a descriptive name, an input state, and expected output
	tests := []struct {
		name     string      // Human-readable description of what we're testing
		state    state.State // The job state we're checking
		terminal bool        // Expected result: should this state be terminal?
	}{
		// Non-terminal states: These jobs are still "in progress"
		{"pending not terminal", state.PENDING, false}, // Job waiting to be scheduled
		{"running not terminal", state.RUNNING, false}, // Job actively executing

		// Terminal states: These jobs are "done" (no more work possible)
		{"succeeded is terminal", state.SUCCEEDED, true}, // Job completed successfully
		{"failed is terminal", state.FAILED, true},       // Job failed permanently
		{"cancelled is terminal", state.CANCELLED, true}, // User cancelled the job
	}

	// Run each test case as a subtest
	// WHY SUBTESTS? If one case fails, the others still run. Better debugging.
	for _, tt := range tests {
		// t.Run creates a named subtest
		// The name appears in test output, making failures easy to identify
		t.Run(tt.name, func(t *testing.T) {
			// Create a job with the state we want to test
			// We use &Job{} to create a pointer because IsTerminal() is a method
			// on *Job (receiver is pointer)
			job := &Job{State: tt.state}

			// Call the method we're testing
			got := job.IsTerminal()

			// Assert: Compare actual result (got) with expected result (tt.terminal)
			if got != tt.terminal {
				// If they don't match, the test fails with a descriptive error
				t.Errorf("Job.IsTerminal() = %v, want %v", got, tt.terminal)
				// Example failure message:
				// "Job.IsTerminal() = false, want true"
			}
		})
	}
}

// TestJob_CanRetry verifies the retry eligibility logic.
//
// WHY THIS TEST MATTERS:
// This determines whether a failed job should retry or give up permanently.
// If this logic is wrong, we might:
// - Retry forever (infinite loops, wasted resources)
// - Give up too early (jobs fail that could have succeeded)
//
// BUSINESS LOGIC:
// A job can retry if: current_attempt < max_attempts
// Example: If MaxAttempts = 3, we allow attempts 1, 2, and 3.
// After attempt 3 fails, we've exhausted retries (3 < 3 is false).
//
// REAL-WORLD EXAMPLE:
// Email sending fails due to temporary network issue.
// - Attempt 1 fails → CanRetry() = true → Retry
// - Attempt 2 fails → CanRetry() = true → Retry
// - Attempt 3 fails → CanRetry() = false → Mark as FAILED, alert admin
func TestJob_CanRetry(t *testing.T) {
	tests := []struct {
		name        string // Description of this scenario
		attempt     int    // Current attempt number
		maxAttempts int    // Maximum attempts allowed
		canRetry    bool   // Expected: can we retry?
	}{
		// Scenario 1: First attempt, plenty of retries left
		// 1 < 3 → true → Can retry
		{"first attempt can retry", 1, 3, true},

		// Scenario 2: Second attempt, still have 1 retry left
		// 2 < 3 → true → Can retry
		{"second attempt can retry", 2, 3, true},

		// Scenario 3: Third attempt, this is our last chance
		// 3 < 3 → false → Cannot retry (we've used all 3 attempts)
		{"last attempt cannot retry", 3, 3, false},

		// Scenario 4: Edge case - somehow attempt counter exceeded max
		// This shouldn't happen, but we test it anyway (defensive programming)
		// 4 < 3 → false → Cannot retry
		{"exceeded attempts cannot retry", 4, 3, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a job with specific attempt counters
			job := &Job{
				Attempt:     tt.attempt,     // How many times we've tried
				MaxAttempts: tt.maxAttempts, // How many times we're allowed to try
			}

			// Test the CanRetry logic
			got := job.CanRetry()

			if got != tt.canRetry {
				t.Errorf("Job.CanRetry() = %v, want %v (Attempt=%d, Max=%d)",
					got, tt.canRetry, tt.attempt, tt.maxAttempts)
				// Enhanced error message shows the input values for easier debugging
			}
		})
	}
}

// TestJob_IncrementAttempt verifies that the attempt counter increases correctly.
//
// WHY THIS TEST MATTERS:
// Every time a job fails and retries, we must increment the attempt counter.
// If this doesn't work:
// - We might retry forever (counter never increases)
// - We might give up too early (counter increases too fast)
//
// REAL-WORLD EXAMPLE:
// Job execution flow:
// 1. Attempt = 1, execute, fails
// 2. IncrementAttempt() → Attempt = 2
// 3. Execute again, fails
// 4. IncrementAttempt() → Attempt = 3
// 5. Execute again, succeeds → Done
//
// This test ensures step 2 and 4 work correctly.
func TestJob_IncrementAttempt(t *testing.T) {
	// Start with attempt 1 (first execution)
	job := &Job{Attempt: 1}

	// First increment: 1 → 2 (first retry)
	job.IncrementAttempt()
	if job.Attempt != 2 {
		// If Attempt is not 2, something is broken
		t.Errorf("After first increment, Attempt = %d, want 2", job.Attempt)
	}

	// Second increment: 2 → 3 (second retry)
	job.IncrementAttempt()
	if job.Attempt != 3 {
		t.Errorf("After second increment, Attempt = %d, want 3", job.Attempt)
	}

	// NOTE: We're testing that increment happens sequentially
	// This is a simple test, but it catches bugs like:
	// - Increment doesn't work at all (Attempt stays 1)
	// - Increment adds wrong amount (Attempt becomes 10)
	// - Increment resets counter (Attempt goes back to 1)
}

// TestJob_RecordError verifies error tracking and clearing.
//
// WHY THIS TEST MATTERS:
// When a job fails, we need to know WHY it failed for debugging.
// LastError stores the error message so operators can:
// - Investigate what went wrong
// - Fix the underlying issue (bad credentials, invalid data, etc.)
// - Decide whether to manually retry the job
//
// REAL-WORLD EXAMPLE:
// Job fails with "SMTP connection timeout"
// Operator sees this in the dashboard:
// - Job #123: FAILED, Last Error: "SMTP connection timeout"
// - Operator checks email server, finds it was down
// - Operator restarts email server, manually retries job
// - Job succeeds
//
// Without error tracking, operator would have no clue what went wrong.
func TestJob_RecordError(t *testing.T) {
	// Start with a fresh job (no error yet)
	job := &Job{}

	// Simulate a job failure with an error
	err := errors.New("connection timeout")

	// Record the error in the job
	// This is what the executor does when a job fails
	job.RecordError(err)

	// Verify the error was stored
	if job.LastError == nil {
		// If LastError is still nil, RecordError didn't work
		t.Fatal("LastError should not be nil after recording error")
		// t.Fatal stops the test here (no point continuing if this fails)
	}

	// Verify the error message is correct
	// We use *job.LastError because LastError is a pointer (*string)
	// Dereferencing with * gives us the actual string value
	if *job.LastError != "connection timeout" {
		t.Errorf("LastError = %s, want 'connection timeout'", *job.LastError)
	}

	// Test error clearing (used when retrying a job)
	// WHY CLEAR? When we retry, we want a fresh start.
	// If the retry fails with a different error, we want to see that new error,
	// not the old one mixed in.
	job.ClearError()

	// Verify error was cleared
	if job.LastError != nil {
		t.Error("LastError should be nil after clearing")
	}

	// FLOW EXAMPLE:
	// 1. Job fails: "database connection lost" → LastError = "database connection lost"
	// 2. Retry: Clear error → LastError = nil
	// 3. Retry fails: "invalid SQL syntax" → LastError = "invalid SQL syntax"
	// Operator sees the LATEST error, not both errors mixed together
}

// TestJob_Validate verifies the validation rules that protect data integrity.
//
// WHY THIS TEST MATTERS:
// Validation is our first line of defense against bad data.
// If we allow invalid jobs into the system:
// - Database constraints fail (crashes)
// - Executors can't parse the job (runtime errors)
// - State machine gets confused (inconsistent states)
//
// VALIDATION STRATEGY:
// We test both positive (valid job passes) and negative (invalid job fails) cases.
// This ensures our validation is neither too strict nor too lenient.
//
// REAL-WORLD ANALOGY:
// This is like airport security screening:
// - Valid passenger (correct ID, ticket) → Allowed through
// - Invalid passenger (no ID) → Rejected
// - We test both cases to ensure security works correctly
func TestJob_Validate(t *testing.T) {
	// Create a baseline valid job
	// All other tests will modify ONE field to test that specific validation rule
	validJob := &Job{
		ID:          "job_123",                           // Required: Unique identifier
		Type:        "send_email",                        // Required: What kind of work
		Payload:     []byte(`{"to":"user@example.com"}`), // Valid JSON
		State:       state.PENDING,                       // Valid state
		Attempt:     1,                                   // Current attempt (>= 0)
		MaxAttempts: 3,                                   // Max attempts (>= 1)
		CreatedAt:   time.Now(),                          // Timestamp
	}

	// Test 1: Valid job should pass validation
	// This is the "control" test - if this fails, validation is broken
	t.Run("valid job", func(t *testing.T) {
		if err := validJob.Validate(); err != nil {
			// A valid job should NOT return an error
			t.Errorf("Valid job failed validation: %v", err)
		}
	})

	// Test 2: Missing ID should fail
	// WHY? Every job needs a unique identifier for tracking, logging, and database queries
	t.Run("missing ID", func(t *testing.T) {
		job := *validJob // Copy the valid job
		job.ID = ""      // Break it: Remove the ID
		if err := job.Validate(); err == nil {
			// If Validate() returns nil, that's a BUG - it should reject empty IDs
			t.Error("Expected error for missing ID")
		}
		// What we're testing: Validate() should return an error like "job ID is required"
	})

	// Test 3: Missing Type should fail
	// WHY? Executors need to know what kind of job this is (email, video, report, etc.)
	// Without a type, the executor doesn't know which code to run
	t.Run("missing type", func(t *testing.T) {
		job := *validJob
		job.Type = "" // Break it: Remove the type
		if err := job.Validate(); err == nil {
			t.Error("Expected error for missing Type")
		}
	})

	// Test 4: Invalid JSON payload should fail
	// WHY? Payload will be parsed by executors using json.Unmarshal()
	// If it's not valid JSON, json.Unmarshal() will fail at runtime
	// Better to catch this at creation time, not execution time
	t.Run("invalid JSON payload", func(t *testing.T) {
		job := *validJob
		job.Payload = []byte(`{invalid json`) // Missing closing brace, invalid JSON
		if err := job.Validate(); err == nil {
			t.Error("Expected error for invalid JSON")
		}
		// Validate() should check json.Valid(payload) and reject this
	})

	// Test 5: Invalid state should fail
	// WHY? State machine only recognizes specific states (PENDING, RUNNING, etc.)
	// A typo like "PENDIN" or garbage like "XYZ" would break the state machine
	t.Run("invalid state", func(t *testing.T) {
		job := *validJob
		job.State = "INVALID_STATE" // Not a recognized state
		if err := job.Validate(); err == nil {
			t.Error("Expected error for invalid state")
		}
		// Validate() calls state.IsValid() to check this
	})

	// Test 6: MaxAttempts too low should fail
	// WHY? A job must be allowed at least 1 execution attempt
	// MaxAttempts = 0 means "never execute" which makes no sense
	t.Run("max attempts too low", func(t *testing.T) {
		job := *validJob
		job.MaxAttempts = 0 // Invalid: Must be at least 1
		if err := job.Validate(); err == nil {
			t.Error("Expected error for MaxAttempts < 1")
		}
	})

	// Test 7: Attempt exceeding MaxAttempts should fail
	// WHY? This represents an inconsistent state
	// Example: Attempt = 5, MaxAttempts = 3
	// How did we get to attempt 5 if max is 3? This is a bug.
	t.Run("attempt exceeds max", func(t *testing.T) {
		job := *validJob
		job.Attempt = 5     // Current attempt
		job.MaxAttempts = 3 // Max allowed
		// 5 > 3 → Impossible state, reject it
		if err := job.Validate(); err == nil {
			t.Error("Expected error for Attempt > MaxAttempts")
		}
	})

	// SUMMARY OF WHAT WE'RE TESTING:
	// ✓ Required fields must not be empty (ID, Type)
	// ✓ JSON must be well-formed (Payload)
	// ✓ State must be recognized (State)
	// ✓ Counters must be logical (MaxAttempts >= 1, Attempt <= MaxAttempts)
	//
	// These rules protect the system from garbage data that would cause
	// crashes, infinite loops, or undefined behavior.
}
