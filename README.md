# Orchestrix

Orchestrix is a backend job orchestration service built in Go that manages asynchronous task execution with explicit lifecycle control, retries, and observability.

The system is designed as a **single-binary monolith** with strong internal boundaries, prioritizing correctness, debuggability, and operational clarity over premature distribution.

---

## Project Status

🚧 **Under Active Development**

**Current Phase:** Foundation  
The core runtime shell, configuration system, and architectural groundwork are complete.  
Domain logic and execution engine are under active development.

---

## Problem Statement

Modern backend systems often need to execute long-running or failure-prone tasks asynchronously:
- background processing
- retries with backoff
- reliable state tracking
- safe cancellation
- operational visibility

Orchestrix addresses this by providing a **job lifecycle–driven execution engine** that ensures:
- no silent failures
- explicit state transitions
- durable execution semantics
- observability-first design

---

## High-Level Architecture

At a high level, Orchestrix consists of:

- **HTTP API** — job submission and inspection
- **Job Service** — business logic and invariants
- **State Machine** — enforces valid lifecycle transitions
- **Scheduler** — selects runnable jobs
- **Worker Pool** — executes jobs concurrently
- **Persistence Layer** — durable job state storage
- **Observability Layer** — structured logs and metrics

📄 Full system design: **[`docs/design.md`](docs/design.md)**

---

## Job Lifecycle Model

Jobs move through an explicit, auditable state machine:

PENDING → SCHEDULED → RUNNING → SUCCEEDED
↓
FAILED
↓
┌──────── retries remaining ────────┐
↓ ↓
RETRYING FAILED (terminal)
↓
SCHEDULED

CANCELLED (terminal, from any non-terminal state)

yaml
Copy code

All transitions are validated and recorded to provide a complete execution history.

---

## Planned Features

- ✅ REST API for job submission and inspection
- ✅ Explicit job state machine with audit trail
- ✅ Asynchronous execution via worker pool
- ✅ Automatic retries with exponential backoff
- ✅ Safe cancellation of pending/running jobs
- ✅ PostgreSQL-backed persistence
- ✅ Structured logging (`log/slog`)
- ✅ Prometheus-compatible metrics
- ✅ Graceful shutdown and crash recovery

---

## Project Structure

cmd/
server/ Application entry point

internal/
api/ HTTP handlers and routing
job/
model/ Job domain models
service/ Business logic
state/ State machine
repository/ Data persistence
scheduler/ Job scheduling logic
worker/ Worker pool and execution
executor/ Job type executors
config/ Configuration management
observability/ Logging and metrics

pkg/
errors/ Shared error types

docs/ Architecture and API documentation
migrations/ Database schema migrations
configs/ Configuration files

yaml
Copy code

---

## Technology Stack

- **Language:** Go 1.22+
- **HTTP:** `net/http` (stdlib)
- **Logging:** `log/slog` (stdlib)
- **Database:** PostgreSQL 15+
- **Metrics:** Prometheus-compatible
- **Configuration:** YAML-based, validated at startup

No frameworks, no ORMs, no hidden magic.

---

## Design Principles

- **Domain-first development**  
  Infrastructure exists to support the domain, not the other way around.

- **Explicit state over implicit behavior**  
  Every job transition is validated and observable.

- **At-least-once execution semantics**  
  Jobs must be idempotent by design.

- **Fail fast, fail loud**  
  Invalid states and configuration errors surface immediately.

- **Single-node first, scale later**  
  Distribution is a future concern, not an assumption.

---

## Running Locally

> 🚧 Coming soon

Local execution instructions will be added once:
- core domain logic is complete
- persistence layer is finalized

---

## API Documentation

API specifications and examples will live in:

docs/api.md

yaml
Copy code

---

## Roadmap (High Level)

1. Core domain state machine
2. In-memory execution engine
3. Scheduler and worker pool
4. Persistence and recovery
5. HTTP API
6. Observability hardening
7. Production readiness

Each step is developed and committed incrementally.

---

## Non-Goals

Orchestrix intentionally does **not** aim to be:
- a distributed queue system
- a workflow DAG engine (yet)
- a cloud-managed service
- a Kubernetes replacement

Those concerns are explicitly deferred.

---

## License

MIT License

---

## Author

Built and maintained by **Dipak** as a serious backend systems project, with a focus on correctness, clarity, and long-term maintainability.
