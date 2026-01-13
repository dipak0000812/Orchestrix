# Orchestrix


A distributed job orchestration service built in Go that handles asynchronous task execution with retry logic, state management, and observability.

## Project Status

🚧 **Under Development** - Phase 1: Foundation

## Architecture

See [docs/design.md](docs/design.md) for complete system design.

## Features (Planned)

- ✅ RESTful API for job submission
- ✅ Asynchronous job execution
- ✅ Automatic retry with exponential backoff
- ✅ Job state machine with audit trail
- ✅ Concurrent worker pool
- ✅ PostgreSQL persistence
- ✅ Structured logging
- ✅ Prometheus metrics
- ✅ Graceful shutdown

## Project Structure
```
cmd/
  server/           - Application entry point
internal/
  api/              - HTTP handlers and routing
  job/
    model/          - Job domain models
    service/        - Business logic
    state/          - State machine
    repository/     - Data persistence
  scheduler/        - Job scheduling logic
  worker/           - Worker pool and execution
  executor/         - Job type executors
  config/           - Configuration management
  observability/    - Logging and metrics
pkg/
  errors/           - Shared error types
docs/               - Documentation
migrations/         - Database schema migrations
configs/            - Configuration files
```

## Technology Stack

- **Language:** Go 1.22+
- **Database:** PostgreSQL 15+
- **Logging:** slog (stdlib)
- **HTTP:** net/http (stdlib)
- **Metrics:** Prometheus-compatible

## Development Setup

_Coming soon_

## Running Locally

_Coming soon_

## API Documentation

See [docs/api.md](docs/api.md)

## License

MIT
