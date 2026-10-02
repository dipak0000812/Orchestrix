# Orchestrix — System Design Document

## Overview

Orchestrix is a backend job orchestration service designed to execute asynchronous tasks reliably with explicit lifecycle management, DAG dependency resolution, retry semantics, and operational visibility.

The system is built as a single-binary architecture with strong internal domain boundaries, prioritizing correctness, debuggability, and maintainability.

---

## Goals

### Functional Goals
- Accept jobs asynchronously and return immediately with generated ULIDs.
- Execute jobs in the background with controlled worker pool concurrency.
- Enforce explicit job lifecycle transitions via a strict state machine.
- Retry failed jobs using exponential backoff with jitter and persisted schedules.
- Execute DAG dependency graphs, holding child jobs in `WAITING` until all parent jobs succeed.
- Automatically cancel downstream dependent jobs when a parent permanently fails.
- Provide HTTP REST endpoints to inspect, list, and safely cancel jobs.
- Enforce tenant isolation and API key authentication.

### Non-Functional Goals
- Recover jobs left in `SCHEDULED` after a missed dispatch; automatic recovery of jobs left in `RUNNING` is not implemented.
- Deterministic, Compare-And-Swap (CAS) state transitions.
- SSRF-safe outbound network execution.
- Observability via structured metrics and health endpoints.
- Bounded worker shutdown and explicitly limited crash recovery.

---

## Non-Goals

Orchestrix explicitly does not aim to:
- Act as a distributed stream broker (e.g., Kafka, SQS).
- Provide distributed multi-datacenter consensus in v1 (relies on PostgreSQL row locks).
- Abstract database internals behind heavy ORM frameworks.

---

## High-Level Architecture

```
Client
  │ (Bearer Auth)
  ▼
HTTP API Router & Middleware (Body limit, Auth)
  │
  ▼
Job Service ──── ULID Generator & Retry Backoff Calculator
  │
  ├───────────────────────────────┐
  ▼                               ▼
State Machine (In-Memory)   DAG Dependency Resolver
  │                               │
  └──────────────┬────────────────┘
                 ▼
          Repository (PostgreSQL)
                 │
                 ▼
          Scheduler (Adaptive Poller)
                 │
                 ▼ (jobChannel)
          Worker Pool (Workers: 5)
                 │
                 ▼
          Executor Registry (SSRF Guard, Demo, Checksum)
```

---

## Core Domain Model

### Job
A stateful unit of asynchronous execution containing:
- `ID`: 26-character time-sortable monotonic ULID.
- `Type`: Job identifier mapped to an Executor implementation.
- `Payload`: Job-specific JSON bytes (max 1 MiB).
- `State`: Explicit lifecycle state.
- `Attempt`: Current attempt counter (1-indexed).
- `MaxAttempts`: Maximum allowable retries.
- `LastError`: Last execution failure message.
- `OwnerKeyID`: Multi-tenant ownership key.
- `NextRunAt`: Timestamp for retry backoff scheduling.
- `CreatedAt`, `ScheduledAt`, `StartedAt`, `CompletedAt`: Execution timestamps.

---

## Job Lifecycle State Machine

### States
| State | Description | Terminal |
| :--- | :--- | :--- |
| `WAITING` | Waiting for parent dependency jobs to complete | No |
| `PENDING` | Accepted and ready for scheduling | No |
| `SCHEDULED` | Claimed by scheduler and dispatched to worker channel | No |
| `RUNNING` | Actively executing on a worker goroutine | No |
| `RETRYING` | Failed execution; waiting for exponential backoff elapsed time | No |
| `SUCCEEDED` | Successfully completed | **Yes** |
| `FAILED` | Permanently failed (retries exhausted or fatal error) | **Yes** |
| `CANCELLED` | Explicitly cancelled by user or parent failure cascade | **Yes** |

### Transition Rules
```
WAITING ──(all parents SUCCEEDED)──► PENDING ──► SCHEDULED ──► RUNNING ──► SUCCEEDED
   │                                    │           │             │
   │                                    │           │             ├──► RETRYING ──► SCHEDULED
   │                                    │           │             │        │
   │                                    │           │             │        └──(max retries)──► FAILED
   │                                    │           │             │
   └──────────────► CANCELLED ◄─────────┴───────────┴─────────────┴──────────────────────────► FAILED
```

---

## DAG Dependency Management

Dependencies are modeled as parent-to-child directed edges in the `job_dependencies` table:
1. **Cycle Detection**: Iterative Depth-First Search (DFS) runs before inserting edges to prevent dependency cycles.
2. **Success Cascade**: When a parent completes, `OnJobSucceeded` queries child jobs. If all parents are `SUCCEEDED`, the child transitions `WAITING → PENDING`.
3. **Failure Cascade**: When a parent reaches `FAILED`, `OnJobFailed` performs a Breadth-First Search (BFS) and attempts to cancel descendant jobs currently in `WAITING` or `PENDING`. Scheduled, running, and terminal descendants are not changed.

---

## Scheduling & Concurrency Model

- **Atomic Claiming**: Within a transaction, the scheduler queries pending jobs and due retries separately using `SELECT ... FOR UPDATE SKIP LOCKED`, merges the candidates, updates selected rows to `SCHEDULED`, and commits. This prevents concurrent schedulers from claiming the same locked rows; it is not an exactly-once execution guarantee.
- **Adaptive Polling**: Claims again immediately when a batch is full; waits `pollInterval` after a partial or empty claim. The server currently configures this interval as one second.
- **Stale Dispatch Recovery**: At startup and during polling, jobs left in `SCHEDULED` for more than 30 seconds are returned to `PENDING`. `RUNNING` jobs are deliberately excluded because there is no heartbeat-based recovery protocol.
- **Worker Pool**: Buffered work queue consumed by a fixed set of goroutines with per-worker panic recovery.
- **Optimistic Concurrency Control**: Repository updates use `UPDATE jobs ... WHERE id = $1 AND state = $expectedState`, preventing race conditions.

---

## Security Architecture

1. **Authentication**: All routes (except `/health`) require `Authorization: Bearer <token>`. Keys are stored as SHA-256 hashes.
2. **Tenant Isolation**: Jobs are partitioned by `owner_key_id`. Cross-tenant queries return uniform `404 Not Found`.
3. **SSRF Guard**: Custom HTTP dialer resolves hostnames and blocks connections to private RFC 1918, loopback, link-local, or multicast IPs before opening TCP sockets.
4. **DoS Mitigation**: Global request body limiter (1 MiB), result pagination cap (500), and CPU `work_factor` ceiling (100,000).

---

## Shutdown and Crash Behavior

On process shutdown, the HTTP server stops accepting new requests. The worker pool stops picking up jobs, allows in-flight work up to its configured job timeout, cancels execution contexts if that grace period expires, and waits up to a further five seconds. An executor that ignores context cancellation can outlive `Stop()`. The scheduler is stopped afterward, and the PostgreSQL pool is closed when `main` returns.

The scheduler recovers stale `SCHEDULED` jobs only. A process crash while a job is `RUNNING` can leave it in that state; the system does not promise exactly-once execution or automatic recovery of external side effects. Deployments that need recovery of running work require an explicit lease/heartbeat and idempotency strategy.
