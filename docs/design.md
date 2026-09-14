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
- Retry failed jobs using configurable exponential backoff with persisted schedules.
- Execute DAG dependency graphs, holding child jobs in `WAITING` until all parent jobs succeed.
- Automatically cancel downstream dependent jobs when a parent permanently fails.
- Provide HTTP REST endpoints to inspect, list, and safely cancel jobs.
- Enforce tenant isolation and API key authentication.

### Non-Functional Goals
- Zero silent job loss.
- Deterministic, Compare-And-Swap (CAS) state transitions.
- SSRF-safe outbound network execution.
- Observability via structured metrics and health endpoints.
- Predictable, bounded shutdown and crash recovery.

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
3. **Failure Cascade**: When a parent reaches `FAILED`, `OnJobFailed` performs a Breadth-First Search (BFS) and cancels all waiting descendant jobs (`WAITING → CANCELLED`).

---

## Scheduling & Concurrency Model

- **Atomic Claiming**: Uses `SELECT ... FOR UPDATE SKIP LOCKED` to prevent duplicate execution across concurrent schedulers.
- **Adaptive Polling**: Claims immediately when a batch is full; backs off to `pollInterval` (1s) when idle, minimizing database CPU overhead.
- **Worker Pool**: Buffered work queue consumed by a fixed set of goroutines with per-worker panic recovery.
- **Optimistic Concurrency Control**: Repository updates use `UPDATE jobs ... WHERE id = $1 AND state = $expectedState`, preventing race conditions.

---

## Security Architecture

1. **Authentication**: All routes (except `/health`) require `Authorization: Bearer <token>`. Keys are stored as SHA-256 hashes.
2. **Tenant Isolation**: Jobs are partitioned by `owner_key_id`. Cross-tenant queries return uniform `404 Not Found`.
3. **SSRF Guard**: Custom HTTP dialer resolves hostnames and blocks connections to private RFC 1918, loopback, link-local, or multicast IPs before opening TCP sockets.
4. **DoS Mitigation**: Global request body limiter (1 MiB), result pagination cap (500), and CPU `work_factor` ceiling (100,000).

---

## Graceful Teardown

1. Stops accepting incoming HTTP requests.
2. Stops scheduler poller (no new jobs claimed).
3. Closes worker dispatch channels.
4. Waits for in-flight worker executions to complete up to `shutdownGracePeriod`.
5. Cancels contexts and cleanly closes PostgreSQL connection pools.
