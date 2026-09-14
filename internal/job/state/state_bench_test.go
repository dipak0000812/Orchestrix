package state

import "testing"

// BenchmarkValidateTransition measures the state machine's own CPU cost,
// isolated from any database I/O. Every job lifecycle transition
// (PENDING->SCHEDULED->RUNNING->SUCCEEDED/FAILED, plus retries) goes
// through this check, so its cost is a fixed per-transition overhead added
// on top of whatever the repository call takes.
func BenchmarkValidateTransition(b *testing.B) {
	sm := NewStateMachine()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sm.ValidateTransition(RUNNING, SUCCEEDED)
	}
}

// BenchmarkValidateTransition_Invalid measures the cost of the rejection
// path (e.g. a stale worker trying to transition an already-terminal job),
// which real deployments hit under contention.
func BenchmarkValidateTransition_Invalid(b *testing.B) {
	sm := NewStateMachine()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sm.ValidateTransition(SUCCEEDED, RUNNING)
	}
}
