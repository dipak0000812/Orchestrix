package state

import (
	"testing"
)

// TestStateIsTerminal verifies terminal state detection
func TestStateIsTerminal(t *testing.T) {
	tests := []struct {
		state    State
		terminal bool
	}{
		{PENDING, false},
		{SCHEDULED, false},
		{RUNNING, false},
		{RETRYING, false},
		{SUCCEEDED, true},
		{FAILED, true},
		{CANCELLED, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.IsTerminal(); got != tt.terminal {
				t.Errorf("State(%s).IsTerminal() = %v, want %v", tt.state, got, tt.terminal)
			}
		})
	}
}

// TestStateIsValid verifies state validation
func TestStateIsValid(t *testing.T) {
	tests := []struct {
		state State
		valid bool
	}{
		{PENDING, true},
		{SCHEDULED, true},
		{RUNNING, true},
		{SUCCEEDED, true},
		{FAILED, true},
		{RETRYING, true},
		{CANCELLED, true},
		{"INVALID", false},
		{"", false},
		{"pending", false}, // Case-sensitive
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.IsValid(); got != tt.valid {
				t.Errorf("State(%s).IsValid() = %v, want %v", tt.state, got, tt.valid)
			}
		})
	}
}

// TestCanTransition_ValidTransitions tests all allowed state transitions
func TestCanTransition_ValidTransitions(t *testing.T) {
	sm := NewStateMachine()

	tests := []struct {
		name string
		from State
		to   State
	}{
		// From PENDING
		{"PENDING to SCHEDULED", PENDING, SCHEDULED},
		{"PENDING to CANCELLED", PENDING, CANCELLED},

		// From SCHEDULED
		{"SCHEDULED to RUNNING", SCHEDULED, RUNNING},
		{"SCHEDULED to CANCELLED", SCHEDULED, CANCELLED},

		// From RUNNING
		{"RUNNING to SUCCEEDED", RUNNING, SUCCEEDED},
		{"RUNNING to FAILED", RUNNING, FAILED},
		{"RUNNING to RETRYING", RUNNING, RETRYING},
		{"RUNNING to CANCELLED", RUNNING, CANCELLED},

		// From RETRYING
		{"RETRYING to SCHEDULED", RETRYING, SCHEDULED},
		{"RETRYING to CANCELLED", RETRYING, CANCELLED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !sm.CanTransition(tt.from, tt.to) {
				t.Errorf("CanTransition(%s, %s) = false, want true", tt.from, tt.to)
			}
		})
	}
}

// TestCanTransition_InvalidTransitions tests forbidden state transitions
func TestCanTransition_InvalidTransitions(t *testing.T) {
	sm := NewStateMachine()

	tests := []struct {
		name string
		from State
		to   State
	}{
		// Self-transitions (all forbidden)
		{"PENDING to PENDING", PENDING, PENDING},
		{"RUNNING to RUNNING", RUNNING, RUNNING},

		// From terminal states (all forbidden)
		{"SUCCEEDED to PENDING", SUCCEEDED, PENDING},
		{"SUCCEEDED to RUNNING", SUCCEEDED, RUNNING},
		{"FAILED to PENDING", FAILED, PENDING},
		{"FAILED to RETRYING", FAILED, RETRYING},
		{"CANCELLED to SCHEDULED", CANCELLED, SCHEDULED},

		// Skipping states (forbidden)
		{"PENDING to RUNNING", PENDING, RUNNING},
		{"PENDING to SUCCEEDED", PENDING, SUCCEEDED},
		{"SCHEDULED to SUCCEEDED", SCHEDULED, SUCCEEDED},
		{"SCHEDULED to FAILED", SCHEDULED, FAILED},
		{"RETRYING to RUNNING", RETRYING, RUNNING},
		{"RETRYING to SUCCEEDED", RETRYING, SUCCEEDED},

		// Backwards transitions (forbidden)
		{"RUNNING to PENDING", RUNNING, PENDING},
		{"RUNNING to SCHEDULED", RUNNING, SCHEDULED},
		{"SCHEDULED to PENDING", SCHEDULED, PENDING},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if sm.CanTransition(tt.from, tt.to) {
				t.Errorf("CanTransition(%s, %s) = true, want false", tt.from, tt.to)
			}
		})
	}
}

// TestValidateTransition_ValidCases tests ValidateTransition for allowed transitions
func TestValidateTransition_ValidCases(t *testing.T) {
	sm := NewStateMachine()

	tests := []struct {
		from State
		to   State
	}{
		{PENDING, SCHEDULED},
		{SCHEDULED, RUNNING},
		{RUNNING, SUCCEEDED},
		{RUNNING, RETRYING},
		{RETRYING, SCHEDULED},
	}

	for _, tt := range tests {
		t.Run(string(tt.from)+"_to_"+string(tt.to), func(t *testing.T) {
			if err := sm.ValidateTransition(tt.from, tt.to); err != nil {
				t.Errorf("ValidateTransition(%s, %s) returned error: %v", tt.from, tt.to, err)
			}
		})
	}
}

// TestValidateTransition_InvalidCases tests ValidateTransition error messages
func TestValidateTransition_InvalidCases(t *testing.T) {
	sm := NewStateMachine()

	tests := []struct {
		name        string
		from        State
		to          State
		expectError bool
	}{
		{"invalid source state", "INVALID", SCHEDULED, true},
		{"invalid target state", PENDING, "INVALID", true},
		{"self transition", PENDING, PENDING, true},
		{"from terminal state", SUCCEEDED, PENDING, true},
		{"forbidden transition", PENDING, RUNNING, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sm.ValidateTransition(tt.from, tt.to)
			if (err != nil) != tt.expectError {
				t.Errorf("ValidateTransition(%s, %s) error = %v, expectError = %v",
					tt.from, tt.to, err, tt.expectError)
			}
		})
	}
}

// TestAllowedTransitions verifies the AllowedTransitions helper
func TestAllowedTransitions(t *testing.T) {
	sm := NewStateMachine()

	tests := []struct {
		state   State
		allowed []State
	}{
		{PENDING, []State{SCHEDULED, CANCELLED}},
		{SCHEDULED, []State{RUNNING, CANCELLED}},
		{RUNNING, []State{SUCCEEDED, FAILED, RETRYING, CANCELLED}},
		{RETRYING, []State{SCHEDULED, CANCELLED}},
		{SUCCEEDED, nil}, // Terminal
		{FAILED, nil},    // Terminal
		{CANCELLED, nil}, // Terminal
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			allowed := sm.AllowedTransitions(tt.state)

			if len(allowed) != len(tt.allowed) {
				t.Errorf("AllowedTransitions(%s) returned %d states, want %d",
					tt.state, len(allowed), len(tt.allowed))
				return
			}

			// Check that all expected states are present
			allowedMap := make(map[State]bool)
			for _, s := range allowed {
				allowedMap[s] = true
			}

			for _, expected := range tt.allowed {
				if !allowedMap[expected] {
					t.Errorf("AllowedTransitions(%s) missing state %s", tt.state, expected)
				}
			}
		})
	}
}

// TestTransitionCoverage ensures every valid state has a path to terminal state
// This is a sanity check to ensure no states are "trapped"
func TestTransitionCoverage(t *testing.T) {
	sm := NewStateMachine()

	// Verify each non-terminal state has at least one allowed transition
	nonTerminalStates := []State{PENDING, SCHEDULED, RUNNING, RETRYING}

	for _, state := range nonTerminalStates {
		allowed := sm.AllowedTransitions(state)
		if len(allowed) == 0 {
			t.Errorf("State %s has no allowed transitions (trapped state)", state)
		}
	}
}
