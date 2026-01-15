# Orchestrix — System Design Document

## Overview

Orchestrix is a backend job orchestration service designed to execute asynchronous tasks reliably while providing explicit lifecycle management, retry semantics, and operational visibility.

The system is intentionally built as a **single-binary monolith** with strong internal boundaries. This design prioritizes correctness, debuggability, and maintainability before horizontal scalability.

This document describes the **architecture, domain model, execution flow, and design decisions** behind Orchestrix.

---

## Goals

### Functional Goals
- Accept jobs asynchronously and return immediately
- Execute jobs in the background with controlled concurrency
- Track job lifecycle explicitly via a state machine
- Retry failed jobs with bounded exponential backoff
- Allow inspection of job status and execution history
- Support safe cancellation of jobs

### Non-Functional Goals
- Zero silent job loss
- Deterministic state transitions
- Clear failure modes
- Observability-first design
- Safe shutdown and crash recovery

---

## Non-Goals

Orchestrix explicitly does **not** aim to:
- Be a distributed queue system (e.g., Kafka, SQS)
- Provide exactly-once execution guarantees
- Support multi-node coordination in v1
- Act as a DAG/workflow engine
- Abstract infrastructure details behind heavy frameworks

These concerns are deferred intentionally.

---

## High-Level Architecture

```
Client
  │
  ▼
HTTP API
  │
  ▼
Job Service
  │
  ▼
State Machine ──── Repository (DB)
  │                     │
  ▼                     ▼
Scheduler          Job Records
  │
  ▼
Worker Pool
  │
  ▼
Job Executor
```

Each component has a **single, well-defined responsibility** and communicates through explicit interfaces.

---

## Core Domain Model

### Job

A job represents a unit of asynchronous work.

Key properties:
- Unique identity
- Explicit lifecycle state
- Retry metadata
- Execution timestamps
- Failure context

Jobs are **stateful**, not fire-and-forget.

---

## Job Lifecycle State Machine

### States

| State       | Description |
|------------|-------------|
| `PENDING`   | Job accepted but not yet scheduled |
| `SCHEDULED` | Selected for execution |
| `RUNNING`   | Currently executing |
| `SUCCEEDED` | Completed successfully (terminal) |
| `FAILED`    | Permanently failed (terminal) |
| `RETRYING`  | Waiting for retry backoff |
| `CANCELLED` | Cancelled by user/system (terminal) |

### State Diagram

```
PENDING → SCHEDULED → RUNNING → SUCCEEDED
                         ↓
                      FAILED
                         ↓
          ┌──── retries remaining ────┐
          ↓                            ↓
      RETRYING                    FAILED (terminal)
          ↓
      SCHEDULED

CANCELLED (terminal, from any non-terminal state)
```

### Invariants

- Terminal states are irreversible
- All transitions are validated
- No implicit state changes
- Every transition is auditable

The **state machine is the source of truth** for correctness.

---

## State Transitions

Transitions are enforced centrally and never bypassed.

Examples:
- `PENDING → SCHEDULED` (scheduler selection)
- `SCHEDULED → RUNNING` (worker pickup)
- `RUNNING → FAILED` (execution error)
- `FAILED → RETRYING` (retry policy)
- `RUNNING → CANCELLED` (user cancellation)

Invalid transitions fail fast.

---

## Scheduler

### Responsibility
Select runnable jobs and dispatch them for execution.

### Design
- Polls the repository periodically
- Selects jobs in `PENDING` or `RETRYING` state
- Attempts state transition before enqueueing
- Never executes jobs directly

### Rationale
Polling is chosen for:
- Predictability
- Simplicity
- Single-node correctness

Event-driven scheduling is deferred.

---

## Worker Pool

### Responsibility
Execute jobs concurrently within bounded capacity.

### Design
- Fixed-size worker pool
- Shared buffered work queue
- Panic recovery per worker
- Context-based cancellation

### Execution Guarantees
- At-least-once execution
- Idempotency required at job level
- Worker failure does not crash the system

---

## Retry Strategy

### Semantics
- Retries occur only after failure
- Retries are bounded
- Backoff grows exponentially

### Backoff Model
```
delay = min(base * 2^attempt, maxDelay)
```

### Guarantees
- No infinite retry loops
- No thundering herd on failure
- Deterministic retry behavior

---

## Persistence Layer

### Responsibility
Provide durable storage for job state and history.

### Design
- Relational database (PostgreSQL)
- Explicit transactions
- Optimistic locking on state updates
- Job events stored separately for audit

### Crash Recovery
On startup:
- Jobs in `RUNNING` state are reconciled
- Failed executions are retried or marked terminal

---

## Observability

### Logging
- Structured logs using `log/slog`
- Correlation by job ID
- No logging inside domain entities

### Metrics
- Job counts by state
- Execution durations
- Retry counts
- Worker utilization

### Health
- Liveness checks
- Dependency readiness

Observability is treated as a **first-class feature**, not an add-on.

---

## Error Handling Strategy

- Domain errors are typed and explicit
- Infrastructure errors are wrapped
- Errors propagate upward without being swallowed
- API layer maps errors to external responses

This ensures failures are visible and actionable.

---

## Configuration Philosophy

- Minimal configuration surface
- Validation at startup
- No runtime mutation
- Explicit defaults

Configuration exists to support behavior, not to define it.

---

## Concurrency Model

- Goroutines for workers and scheduler
- Channels for work dispatch
- Mutexes only at repository boundaries
- Database as the final arbiter of correctness

The system avoids shared mutable state outside well-defined boundaries.

---

## Shutdown Semantics

On shutdown:
1. Stop accepting new jobs
2. Halt scheduler
3. Drain worker queue
4. Wait for in-flight jobs (bounded)
5. Exit cleanly

Shutdown is **deterministic and observable**.

---

## Design Tradeoffs

| Decision | Benefit | Cost |
|----------|---------|------|
| Monolith | Simpler correctness | No horizontal scaling |
| Polling scheduler | Predictable | Scheduling latency |
| At-least-once | Simpler | Requires idempotent jobs |
| No frameworks | Transparency | More boilerplate |
| Explicit state machine | Correctness | More code |

These tradeoffs are **intentional**, not accidental.

---

## Future Considerations (Explicitly Deferred)

- Multi-node scheduling
- Distributed locking
- Workflow/DAG support
- Priority queues
- Rate limiting per job type
- Exactly-once semantics

Deferred features are documented to avoid accidental scope creep.

---

## Summary

Orchestrix is designed as a **correctness-first backend system**.

The architecture favors:
- Explicit state
- Observable behavior
- Bounded complexity
- Incremental evolution

This design allows Orchestrix to grow without rewriting its foundations.
