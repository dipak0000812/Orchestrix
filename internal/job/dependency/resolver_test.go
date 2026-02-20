package dependency

import (
	"context"
	"fmt"
	"testing"

	"github.com/dipak0000812/orchestrix/internal/job/model"
	"github.com/dipak0000812/orchestrix/internal/job/state"
)

// mockRepo is an in-memory repository for testing.
// No database, no Docker, just a map.
type mockRepo struct {
	jobs     map[string]*model.Job
	parents  map[string][]string // childID → []parentID
	children map[string][]string // parentID → []childID
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		jobs:     make(map[string]*model.Job),
		parents:  make(map[string][]string),
		children: make(map[string][]string),
	}
}

func (m *mockRepo) addJob(id string, s state.State) {
	m.jobs[id] = &model.Job{ID: id, State: s}
}

func (m *mockRepo) GetByID(_ context.Context, id string) (*model.Job, error) {
	job, ok := m.jobs[id]
	if !ok {
		return nil, fmt.Errorf("job %s not found", id)
	}
	return job, nil
}

func (m *mockRepo) GetParents(_ context.Context, jobID string) ([]string, error) {
	return m.parents[jobID], nil
}

func (m *mockRepo) GetChildren(_ context.Context, jobID string) ([]string, error) {
	return m.children[jobID], nil
}

func (m *mockRepo) AddDependency(_ context.Context, parentID, childID string) error {
	m.parents[childID] = append(m.parents[childID], parentID)
	m.children[parentID] = append(m.children[parentID], childID)
	return nil
}

func (m *mockRepo) UpdateJobState(_ context.Context, jobID string, newState state.State) error {
	if job, ok := m.jobs[jobID]; ok {
		job.State = newState
	}
	return nil
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestCycleDetection_NoCycle_LinearChain(t *testing.T) {
	// A → B → C (no cycle)
	repo := newMockRepo()
	repo.addJob("A", state.SUCCEEDED)
	repo.addJob("B", state.WAITING)
	repo.addJob("C", state.WAITING)

	resolver := NewResolver(repo)
	ctx := context.Background()

	// A → B
	if err := resolver.AddDependencies(ctx, "B", []string{"A"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// B → C
	if err := resolver.AddDependencies(ctx, "C", []string{"B"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCycleDetection_DirectCycle(t *testing.T) {
	// A → B, then try B → A (direct cycle)
	repo := newMockRepo()
	repo.addJob("A", state.WAITING)
	repo.addJob("B", state.WAITING)

	resolver := NewResolver(repo)
	ctx := context.Background()

	// A depends on B
	if err := resolver.AddDependencies(ctx, "A", []string{"B"}); err != nil {
		t.Fatalf("unexpected error adding B → A: %v", err)
	}

	// Now try B depends on A - should be rejected
	err := resolver.AddDependencies(ctx, "B", []string{"A"})
	if err == nil {
		t.Fatal("expected cycle detection error, got nil")
	}
	t.Logf("correctly rejected cycle: %v", err)
}

func TestCycleDetection_IndirectCycle(t *testing.T) {
	// A → B → C, then try C → A (indirect cycle)
	repo := newMockRepo()
	repo.addJob("A", state.WAITING)
	repo.addJob("B", state.WAITING)
	repo.addJob("C", state.WAITING)

	resolver := NewResolver(repo)
	ctx := context.Background()

	resolver.AddDependencies(ctx, "B", []string{"A"}) // A → B
	resolver.AddDependencies(ctx, "C", []string{"B"}) // B → C

	// Try C → A - should be rejected
	err := resolver.AddDependencies(ctx, "A", []string{"C"})
	if err == nil {
		t.Fatal("expected cycle detection error, got nil")
	}
	t.Logf("correctly rejected indirect cycle: %v", err)
}

func TestCycleDetection_DiamondPattern(t *testing.T) {
	//     A
	//    / \
	//   B   C
	//    \ /
	//     D
	repo := newMockRepo()
	repo.addJob("A", state.SUCCEEDED)
	repo.addJob("B", state.WAITING)
	repo.addJob("C", state.WAITING)
	repo.addJob("D", state.WAITING)

	resolver := NewResolver(repo)
	ctx := context.Background()

	resolver.AddDependencies(ctx, "B", []string{"A"})      // A → B
	resolver.AddDependencies(ctx, "C", []string{"A"})      // A → C
	resolver.AddDependencies(ctx, "D", []string{"B", "C"}) // B,C → D

	// No cycles - should all succeed
	t.Log("diamond pattern created successfully")
}

func TestOnJobSucceeded_TransitionsChild(t *testing.T) {
	// A → B. A succeeds. B should become PENDING.
	repo := newMockRepo()
	repo.addJob("A", state.SUCCEEDED)
	repo.addJob("B", state.WAITING)
	repo.AddDependency(context.Background(), "A", "B")

	resolver := NewResolver(repo)
	if err := resolver.OnJobSucceeded(context.Background(), "A"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.jobs["B"].State != state.PENDING {
		t.Errorf("expected B to be PENDING, got %s", repo.jobs["B"].State)
	}
}

func TestOnJobSucceeded_WaitsForAllParents(t *testing.T) {
	// A → D, B → D. Only A succeeds. D should stay WAITING.
	repo := newMockRepo()
	repo.addJob("A", state.SUCCEEDED)
	repo.addJob("B", state.RUNNING) // Still running
	repo.addJob("D", state.WAITING)
	repo.AddDependency(context.Background(), "A", "D")
	repo.AddDependency(context.Background(), "B", "D")

	resolver := NewResolver(repo)
	if err := resolver.OnJobSucceeded(context.Background(), "A"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// D should still be WAITING because B hasn't finished
	if repo.jobs["D"].State != state.WAITING {
		t.Errorf("expected D to be WAITING, got %s", repo.jobs["D"].State)
	}
}

func TestOnJobFailed_CancelsAllDescendants(t *testing.T) {
	// A → B → C → D. A fails. B, C, D should all be CANCELLED.
	repo := newMockRepo()
	repo.addJob("A", state.FAILED)
	repo.addJob("B", state.WAITING)
	repo.addJob("C", state.WAITING)
	repo.addJob("D", state.WAITING)
	repo.AddDependency(context.Background(), "A", "B")
	repo.AddDependency(context.Background(), "B", "C")
	repo.AddDependency(context.Background(), "C", "D")

	resolver := NewResolver(repo)
	if err := resolver.OnJobFailed(context.Background(), "A"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, id := range []string{"B", "C", "D"} {
		if repo.jobs[id].State != state.CANCELLED {
			t.Errorf("expected %s to be CANCELLED, got %s", id, repo.jobs[id].State)
		}
	}
}
