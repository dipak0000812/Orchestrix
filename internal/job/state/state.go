package state

import "fmt"

// State represents the lifecycle state of a job.
// States form a directed graph with explicit transition rules.
type State string

// Job lifecycle states
const (
	// PENDING: Job created but not yet scheduled for execution.
	// This is the initial state when a job is first created.
	PENDING State = "PENDING"

	// SCHEDULED: Job has been picked by the scheduler and is queued for execution.
	// Waiting for an available worker to process it.
	SCHEDULED State = "SCHEDULED"

	// RUNNING: Job is currently being executed by a worker.
	// Used for crash recovery (detect incomplete work).
	RUNNING State = "RUNNING"

	// SUCCEEDED: Job completed successfully.
	// Terminal state — no further transitions allowed.
	SUCCEEDED State = "SUCCEEDED"

	// FAILED: Job failed and exhausted all retry attempts.
	// Terminal state — requires manual intervention or new job creation.
	FAILED State = "FAILED"

	// RETRYING: Job failed but has retry attempts remaining.
	// Will transition back to SCHEDULED after retry delay.
	RETRYING State = "RETRYING"

	// CANCELLED: Job was explicitly cancelled by user.
	// Terminal state — job will not execute or retry.
	CANCELLED State = "CANCELLED"
)

// IsTerminal returns true if the state is terminal (no transitions out).
// Terminal states: SUCCEEDED, FAILED, CANCELLED
func (s State) IsTerminal() bool {
	return s == SUCCEEDED || s == FAILED || s == CANCELLED
}

// IsValid returns true if the state is a recognized job state.
func (s State) IsValid() bool {
	switch s {
	case PENDING, SCHEDULED, RUNNING, SUCCEEDED, FAILED, RETRYING, CANCELLED:
		return true
	default:
		return false
	}
}

// StateMachine enforces state transition rules for jobs.
// It acts as a gatekeeper, preventing illegal state changes.
type StateMachine struct {
	// Stateless validator — no fields needed.
	// Transition rules are hardcoded in methods.
}

// NewStateMachine creates a new state machine instance.
func NewStateMachine() *StateMachine {
	return &StateMachine{}
}

// CanTransition checks if a state transition is allowed.
// Returns true if the transition from -> to is valid, false otherwise.
//
// Use this for quick boolean checks without error messages.
func (sm *StateMachine) CanTransition(from, to State) bool {
	// Self-transitions are not allowed
	if from == to {
		return false
	}

	// Terminal states cannot transition to anything
	if from.IsTerminal() {
		return false
	}

	// Define allowed transitions as a set of valid (from, to) pairs
	switch from {
	case PENDING:
		return to == SCHEDULED || to == CANCELLED

	case SCHEDULED:
		return to == RUNNING || to == CANCELLED

	case RUNNING:
		return to == SUCCEEDED || to == FAILED || to == RETRYING || to == CANCELLED

	case RETRYING:
		return to == SCHEDULED || to == CANCELLED

	default:
		// Unknown or terminal state
		return false
	}
}

// ValidateTransition checks if a state transition is allowed.
// Returns nil if valid, or a descriptive error if invalid.
//
// Use this when you need detailed error messages for logging/debugging.
func (sm *StateMachine) ValidateTransition(from, to State) error {
	// Validate that both states are recognized
	if !from.IsValid() {
		return fmt.Errorf("invalid source state: %s", from)
	}
	if !to.IsValid() {
		return fmt.Errorf("invalid target state: %s", to)
	}

	// Check self-transition
	if from == to {
		return fmt.Errorf("self-transition not allowed: %s -> %s", from, to)
	}

	// Check terminal state
	if from.IsTerminal() {
		return fmt.Errorf("cannot transition from terminal state %s to %s", from, to)
	}

	// Validate specific transition
	if !sm.CanTransition(from, to) {
		return fmt.Errorf("invalid transition: %s -> %s (not allowed by state machine rules)", from, to)
	}

	return nil
}

// AllowedTransitions returns all valid target states from the given state.
// Useful for debugging and documentation.
func (sm *StateMachine) AllowedTransitions(from State) []State {
	if !from.IsValid() || from.IsTerminal() {
		return nil
	}

	var allowed []State
	// Check all possible states
	allStates := []State{PENDING, SCHEDULED, RUNNING, SUCCEEDED, FAILED, RETRYING, CANCELLED}

	for _, to := range allStates {
		if sm.CanTransition(from, to) {
			allowed = append(allowed, to)
		}
	}

	return allowed
}
