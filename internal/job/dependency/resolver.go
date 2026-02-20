package dependency

import (
	"context"
	"fmt"

	"github.com/dipak0000812/orchestrix/internal/job/model"
	"github.com/dipak0000812/orchestrix/internal/job/state"
)

// Repository defines the database operations needed by the resolver.
// Using an interface keeps this package decoupled from postgres specifics.
type Repository interface {
	// GetParents returns all parent job IDs for a given job.
	GetParents(ctx context.Context, jobID string) ([]string, error)

	// GetChildren returns all child job IDs for a given job.
	GetChildren(ctx context.Context, jobID string) ([]string, error)

	// AddDependency creates a parent → child relationship.
	AddDependency(ctx context.Context, parentID, childID string) error

	// GetByID retrieves a job by ID.
	GetByID(ctx context.Context, id string) (*model.Job, error)

	// UpdateJobState transitions a job to a new state.
	UpdateJobState(ctx context.Context, jobID string, newState state.State) error
}

// Resolver handles job dependency logic.
type Resolver struct {
	repo Repository
}

// NewResolver creates a new dependency resolver.
func NewResolver(repo Repository) *Resolver {
	return &Resolver{repo: repo}
}

// AddDependencies registers parent jobs for a child job.
// Returns error if:
//   - Any parent job doesn't exist
//   - Adding any dependency would create a cycle
func (r *Resolver) AddDependencies(ctx context.Context, childID string, parentIDs []string) error {
	for _, parentID := range parentIDs {
		// Step 1: Verify parent exists
		if _, err := r.repo.GetByID(ctx, parentID); err != nil {
			return fmt.Errorf("parent job %s not found: %w", parentID, err)
		}

		// Step 2: Check if adding this dependency creates a cycle
		// A cycle exists if childID is reachable from parentID
		// (meaning parentID is already a descendant of childID)
		hasCycle, err := r.wouldCreateCycle(ctx, childID, parentID)
		if err != nil {
			return fmt.Errorf("cycle detection failed: %w", err)
		}
		if hasCycle {
			return fmt.Errorf(
				"adding dependency %s → %s would create a cycle",
				parentID, childID,
			)
		}

		// Step 3: Safe to add
		if err := r.repo.AddDependency(ctx, parentID, childID); err != nil {
			return fmt.Errorf("failed to add dependency %s → %s: %w", parentID, childID, err)
		}
	}
	return nil
}

// wouldCreateCycle checks if adding parentID → childID creates a cycle.
//
// Strategy: Do a DFS starting from childID, following existing dependencies.
// If we reach parentID during the search, a cycle would be created.
//
// Example:
//
//	Existing: A → B → C
//	Adding:   C → A (parentID=C, childID=A)
//	DFS from A: A → B → C → found parentID! Cycle detected.
func (r *Resolver) wouldCreateCycle(ctx context.Context, startID, targetID string) (bool, error) {
	// visited tracks nodes we've already explored (prevents re-processing)
	visited := make(map[string]bool)

	// Use iterative DFS with an explicit stack (safer than recursion for deep graphs)
	stack := []string{startID}

	for len(stack) > 0 {
		// Pop from stack
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		// Already visited this node? Skip it.
		if visited[current] {
			continue
		}
		visited[current] = true

		// Get all children of current node (nodes that depend on current)
		children, err := r.repo.GetChildren(ctx, current)
		if err != nil {
			return false, fmt.Errorf("failed to get children of %s: %w", current, err)
		}

		for _, child := range children {
			// Found the target - adding this dependency would create a cycle
			if child == targetID {
				return true, nil
			}
			// Add to stack for further exploration
			if !visited[child] {
				stack = append(stack, child)
			}
		}
	}

	// Exhausted all paths without finding targetID - no cycle
	return false, nil
}

// OnJobSucceeded is called when a job succeeds.
// It checks all child jobs and transitions any whose parents have all succeeded
// from WAITING → PENDING (making them eligible for execution).
func (r *Resolver) OnJobSucceeded(ctx context.Context, jobID string) error {
	// Get all children of this job
	children, err := r.repo.GetChildren(ctx, jobID)
	if err != nil {
		return fmt.Errorf("failed to get children of %s: %w", jobID, err)
	}

	for _, childID := range children {
		// Check if ALL parents of this child have succeeded
		allSucceeded, err := r.allParentsSucceeded(ctx, childID)
		if err != nil {
			return fmt.Errorf("failed to check parents of %s: %w", childID, err)
		}

		if allSucceeded {
			// All parents done - child can now be executed
			if err := r.repo.UpdateJobState(ctx, childID, state.PENDING); err != nil {
				return fmt.Errorf("failed to transition %s to PENDING: %w", childID, err)
			}
		}
	}
	return nil
}

// OnJobFailed is called when a job fails permanently.
// It cancels ALL descendants (not just direct children) of the failed job.
func (r *Resolver) OnJobFailed(ctx context.Context, jobID string) error {
	// Get all descendants using BFS
	descendants, err := r.getAllDescendants(ctx, jobID)
	if err != nil {
		return fmt.Errorf("failed to get descendants of %s: %w", jobID, err)
	}

	// Cancel all descendants
	for _, descendantID := range descendants {
		job, err := r.repo.GetByID(ctx, descendantID)
		if err != nil {
			return fmt.Errorf("failed to get descendant job %s: %w", descendantID, err)
		}

		// Only cancel jobs that are still waiting or pending
		// Don't touch jobs that are already running or terminal
		if job.State == state.WAITING || job.State == state.PENDING {
			if err := r.repo.UpdateJobState(ctx, descendantID, state.CANCELLED); err != nil {
				return fmt.Errorf("failed to cancel descendant %s: %w", descendantID, err)
			}
		}
	}
	return nil
}

// allParentsSucceeded returns true if every parent of jobID is in SUCCEEDED state.
func (r *Resolver) allParentsSucceeded(ctx context.Context, jobID string) (bool, error) {
	parents, err := r.repo.GetParents(ctx, jobID)
	if err != nil {
		return false, err
	}

	// No parents = no dependencies = ready to run
	if len(parents) == 0 {
		return true, nil
	}

	for _, parentID := range parents {
		parent, err := r.repo.GetByID(ctx, parentID)
		if err != nil {
			return false, fmt.Errorf("failed to get parent job %s: %w", parentID, err)
		}
		if parent.State != state.SUCCEEDED {
			return false, nil
		}
	}
	return true, nil
}

// getAllDescendants returns ALL descendants of a job using BFS.
// Descendants = children + children's children + ...
func (r *Resolver) getAllDescendants(ctx context.Context, jobID string) ([]string, error) {
	var descendants []string
	visited := make(map[string]bool)
	queue := []string{jobID}

	for len(queue) > 0 {
		// Dequeue
		current := queue[0]
		queue = queue[1:]

		if visited[current] {
			continue
		}
		visited[current] = true

		children, err := r.repo.GetChildren(ctx, current)
		if err != nil {
			return nil, fmt.Errorf("failed to get children of %s: %w", current, err)
		}

		for _, child := range children {
			if !visited[child] {
				descendants = append(descendants, child)
				queue = append(queue, child)
			}
		}
	}
	return descendants, nil
}
