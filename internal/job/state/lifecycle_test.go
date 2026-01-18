package state

import "testing"

import "fmt"

// TestLifecycle_HappyPath simulates a job that succeeds on first attempt.
// Flow: PENDING → SCHEDULED → RUNNING → SUCCEEDED
func TestLifecycle_HappyPath(t *testing.T) {
	sm := NewStateMachine()

	// Track current state as we simulate transitions
	currentState := PENDING

	// Step 1: Scheduler picks the job
	if err := sm.ValidateTransition(currentState, SCHEDULED); err != nil {
		t.Fatalf("PENDING → SCHEDULED failed: %v", err)
	}
	currentState = SCHEDULED

	// Step 2: Worker starts execution
	if err := sm.ValidateTransition(currentState, RUNNING); err != nil {
		t.Fatalf("SCHEDULED → RUNNING failed: %v", err)
	}
	currentState = RUNNING

	// Step 3: Job completes successfully
	if err := sm.ValidateTransition(currentState, SUCCEEDED); err != nil {
		t.Fatalf("RUNNING → SUCCEEDED failed: %v", err)
	}
	currentState = SUCCEEDED

	// Step 4: Terminal state — no further transitions allowed
	if sm.CanTransition(currentState, PENDING) {
		t.Error("SUCCEEDED should not transition to any state")
	}
	if sm.CanTransition(currentState, RUNNING) {
		t.Error("SUCCEEDED should not transition to any state")
	}
}

// TestLifecycle_SingleRetry simulates a job that fails once then succeeds.
// Flow: PENDING → SCHEDULED → RUNNING → RETRYING → SCHEDULED → RUNNING → SUCCEEDED
func TestLifecycle_SingleRetry(t *testing.T) {
	sm := NewStateMachine()
	currentState := PENDING

	// First attempt starts
	mustTransition(t, sm, &currentState, SCHEDULED, "scheduler picks job")
	mustTransition(t, sm, &currentState, RUNNING, "worker starts execution")

	// First attempt fails, but retries remain
	mustTransition(t, sm, &currentState, RETRYING, "job failed, will retry")

	// Retry delay elapses, job goes back to queue
	mustTransition(t, sm, &currentState, SCHEDULED, "retry scheduled")

	// Second attempt starts
	mustTransition(t, sm, &currentState, RUNNING, "worker starts retry")

	// Second attempt succeeds
	mustTransition(t, sm, &currentState, SUCCEEDED, "job succeeded on retry")

	// Verify terminal state
	if !currentState.IsTerminal() {
		t.Errorf("Expected terminal state, got %s", currentState)
	}
}

// TestLifecycle_ExhaustRetries simulates a job that fails all retry attempts.
// Flow: PENDING → SCHEDULED → RUNNING → RETRYING → ... → FAILED
func TestLifecycle_ExhaustRetries(t *testing.T) {
	sm := NewStateMachine()
	currentState := PENDING

	// Simulate 3 attempts (typical max_retries = 3)
	const maxAttempts = 3

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Queue the job
		mustTransition(t, sm, &currentState, SCHEDULED,
			"attempt %d: scheduled", attempt)

		// Start execution
		mustTransition(t, sm, &currentState, RUNNING,
			"attempt %d: running", attempt)

		// Decide outcome based on attempt
		if attempt < maxAttempts {
			// Still have retries left
			mustTransition(t, sm, &currentState, RETRYING,
				"attempt %d: failed, retrying", attempt)
		} else {
			// Last attempt — no retries left
			mustTransition(t, sm, &currentState, FAILED,
				"attempt %d: failed permanently", attempt)
		}
	}

	// Verify terminal state
	if currentState != FAILED {
		t.Errorf("Expected FAILED state, got %s", currentState)
	}

	// Terminal state should reject all transitions
	if sm.CanTransition(currentState, RETRYING) {
		t.Error("FAILED should not transition to RETRYING")
	}
}

// TestLifecycle_CancellationPaths tests cancellation from different states.
func TestLifecycle_CancellationPaths(t *testing.T) {
	sm := NewStateMachine()

	tests := []struct {
		name        string
		startState  State
		description string
	}{
		{
			name:        "cancel_from_pending",
			startState:  PENDING,
			description: "User cancels before scheduling",
		},
		{
			name:        "cancel_from_scheduled",
			startState:  SCHEDULED,
			description: "User cancels while queued",
		},
		{
			name:        "cancel_from_running",
			startState:  RUNNING,
			description: "User cancels during execution",
		},
		{
			name:        "cancel_from_retrying",
			startState:  RETRYING,
			description: "User cancels during retry delay",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify transition to CANCELLED is allowed
			if err := sm.ValidateTransition(tt.startState, CANCELLED); err != nil {
				t.Errorf("%s: %s → CANCELLED failed: %v",
					tt.description, tt.startState, err)
			}

			// Verify CANCELLED is terminal
			if sm.CanTransition(CANCELLED, PENDING) {
				t.Error("CANCELLED should be terminal")
			}
		})
	}
}

// TestLifecycle_InvalidTransitions verifies common mistakes are caught.
func TestLifecycle_InvalidTransitions(t *testing.T) {
	sm := NewStateMachine()

	tests := []struct {
		name        string
		from        State
		to          State
		description string
	}{
		{
			name:        "skip_scheduling",
			from:        PENDING,
			to:          RUNNING,
			description: "Cannot skip SCHEDULED state",
		},
		{
			name:        "skip_execution",
			from:        SCHEDULED,
			to:          SUCCEEDED,
			description: "Cannot succeed without running",
		},
		{
			name:        "retry_without_scheduling",
			from:        RETRYING,
			to:          RUNNING,
			description: "Retry must go through SCHEDULED",
		},
		{
			name:        "rewind_execution",
			from:        RUNNING,
			to:          PENDING,
			description: "Cannot rewind to PENDING",
		},
		{
			name:        "resurrect_success",
			from:        SUCCEEDED,
			to:          PENDING,
			description: "Cannot restart succeeded job",
		},
		{
			name:        "resurrect_failure",
			from:        FAILED,
			to:          RETRYING,
			description: "Cannot retry exhausted job",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Transition should fail
			if sm.CanTransition(tt.from, tt.to) {
				t.Errorf("%s: %s → %s should be forbidden",
					tt.description, tt.from, tt.to)
			}

			// ValidateTransition should return error
			if err := sm.ValidateTransition(tt.from, tt.to); err == nil {
				t.Errorf("%s: expected validation error, got nil", tt.description)
			}
		})
	}
}

// TestLifecycle_ComplexRetryScenario simulates a realistic retry pattern.
// Job fails twice with retries, then succeeds on third attempt.
func TestLifecycle_ComplexRetryScenario(t *testing.T) {
	sm := NewStateMachine()
	currentState := PENDING

	// Track the lifecycle for debugging
	var lifecycle []State
	recordTransition := func(to State) {
		lifecycle = append(lifecycle, to)
		currentState = to
	}

	// Attempt 1: Fail
	mustTransition(t, sm, &currentState, SCHEDULED, "attempt 1: scheduled")
	recordTransition(SCHEDULED)

	mustTransition(t, sm, &currentState, RUNNING, "attempt 1: running")
	recordTransition(RUNNING)

	mustTransition(t, sm, &currentState, RETRYING, "attempt 1: failed")
	recordTransition(RETRYING)

	// Attempt 2: Fail again
	mustTransition(t, sm, &currentState, SCHEDULED, "attempt 2: scheduled")
	recordTransition(SCHEDULED)

	mustTransition(t, sm, &currentState, RUNNING, "attempt 2: running")
	recordTransition(RUNNING)

	mustTransition(t, sm, &currentState, RETRYING, "attempt 2: failed again")
	recordTransition(RETRYING)

	// Attempt 3: Success
	mustTransition(t, sm, &currentState, SCHEDULED, "attempt 3: scheduled")
	recordTransition(SCHEDULED)

	mustTransition(t, sm, &currentState, RUNNING, "attempt 3: running")
	recordTransition(RUNNING)

	mustTransition(t, sm, &currentState, SUCCEEDED, "attempt 3: succeeded")
	recordTransition(SUCCEEDED)

	// Verify final state
	if currentState != SUCCEEDED {
		t.Errorf("Expected SUCCEEDED, got %s", currentState)
	}

	// Verify lifecycle length (should be 10 transitions)
	expectedLength := 10
	if len(lifecycle) != expectedLength {
		t.Errorf("Expected %d state transitions, got %d: %v",
			expectedLength, len(lifecycle), lifecycle)
	}

	// Log lifecycle for debugging
	t.Logf("Job lifecycle: %v", lifecycle)
}

// mustTransition is a test helper that asserts a transition succeeds.
// If transition fails, the test fails immediately with descriptive error.
func mustTransition(t *testing.T, sm *StateMachine, current *State, to State, msgFormat string, args ...interface{}) {
	t.Helper() // Mark as helper to get correct line numbers in failures

	if err := sm.ValidateTransition(*current, to); err != nil {
		t.Fatalf("Transition %s → %s failed (%s): %v",
			*current, to, fmt.Sprintf(msgFormat, args...), err)
	}
	*current = to
}

// Note: We need fmt for mustTransition, add to imports
