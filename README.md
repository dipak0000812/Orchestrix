# Orchestrix

A PostgreSQL-backed asynchronous job orchestration service in Go. Orchestrix provides explicit state-machine lifecycle tracking, DAG dependency resolution, exponential retry backoff, concurrency control, and Prometheus telemetry.

---

## Features

- **Job Lifecycle State Machine** — Strict transition gating (`PENDING` → `SCHEDULED` → `RUNNING` → `SUCCEEDED` / `FAILED` / `RETRYING` / `CANCELLED` / `WAITING`).
- **DAG Dependency Execution** — Cycle-safe dependency graphs. Jobs with `depends_on` wait for parent completion; permanently failed parents cascade cancellations downstream.
- **Automatic Retries & Backoff** — Configurable exponential backoff with jitter and persisted `next_run_at` scheduling.
- **Concurrent Worker Pool** — Bounded worker pools with panic isolation and context cancellation.
- **Adaptive Scheduling** — Immediately claims another batch when the previous claim was full; waits for the configured poll interval after a partial or empty claim. Claims use `SELECT ... FOR UPDATE SKIP LOCKED`.
- **Tenant Isolation & Auth** — API key authentication (`Bearer`) with SHA-256 hash storage and tenant-scoped job operations.
- **SSRF-Safe Webhooks** — Outbound webhook requests validated at dial-time against private/link-local/loopback CIDRs.
- **Persistent Storage** — PostgreSQL persistence via `pgx/v5` connection pooling with versioned SQL migrations.
- **Observability** — Built-in Prometheus metrics (`/metrics`) and health checks (`/health`).
- **Bounded Worker Shutdown** — Lets in-flight jobs run for a grace period, then cancels their contexts and waits for a bounded force-stop window.

---

## Quick Start

### Prerequisites
- Docker & Docker Compose
- Go 1.24+ (for local development)

### Run with Docker
```bash
git clone https://github.com/dipak0000812/Orchestrix.git
cd Orchestrix
docker-compose up -d
curl http://localhost:8080/health
```

*Note on Authentication:* On initial startup without pre-existing keys, the server generates an administrative API key and logs it once:
```bash
docker-compose logs orchestrix | grep -A2 "Generated initial API key"
```
Pass this key in the `Authorization: Bearer <key>` header. To pre-configure your own key, set the `ADMIN_API_KEY` environment variable prior to boot.

### Run Locally
```bash
# 1. Start PostgreSQL
docker-compose up -d postgres

# 2. Run migrations
make migrate-up

# 3. Start server
DB_PASSWORD=orchestrix_dev_password go run cmd/server/main.go
```

---

## API Usage

All API endpoints (except `/health`) require `Authorization: Bearer <key>`. Requests are isolated per API key.

### 1. Create a Job
```bash
curl -X POST http://localhost:8080/api/v1/jobs \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "compute_checksum",
    "payload": {"data": "hello world", "work_factor": 1}
  }'
```

**Response (`201 Created`):**
```json
{
  "id": "01KG94QDSXNW96W84543ZG5PY5",
  "type": "compute_checksum",
  "state": "PENDING",
  "attempt": 1,
  "max_attempts": 3,
  "created_at": "2026-07-21T03:33:26Z"
}
```

Built-in job types:
- `demo_job` — Configurable fixed-delay simulation.
- `compute_checksum` — Chained SHA-256 CPU workload (`work_factor` capped at 100,000).
- `http_webhook` — SSRF-guarded outbound HTTP request.

### 2. Get Job Status
```bash
curl http://localhost:8080/api/v1/jobs/01KG94QDSXNW96W84543ZG5PY5 \
  -H "Authorization: Bearer $API_KEY"
```

### 3. List Jobs by State
```bash
curl "http://localhost:8080/api/v1/jobs?state=SUCCEEDED&limit=10" \
  -H "Authorization: Bearer $API_KEY"
```

### 4. Cancel a Job
```bash
curl -X DELETE http://localhost:8080/api/v1/jobs/01KG94QDSXNW96W84543ZG5PY5 \
  -H "Authorization: Bearer $API_KEY"
```

---

## Architecture

```
┌─────────────┐
│   HTTP API  │  ← REST endpoints (port 8080, Bearer Auth)
└──────┬──────┘
       │
┌──────▼──────┐
│ Job Service │  ← Validation, State Machine, ULID Generator
└──────┬──────┘
       │
┌──────▼──────┐
│ Repository  │  ← PostgreSQL (Optimistic Concurrency / CAS)
└──────┬──────┘
       │
┌──────▼──────┐
│  Database   │  ← PostgreSQL (FOR UPDATE SKIP LOCKED)
└─────────────┘

Background Execution:
┌────────────────┐      ┌────────────┐      ┌──────────┐
│   Scheduler    │─────→│ Job Queue  │─────→│ Workers  │
│ (Adaptive poll)│      │ (Channel)  │      │ (Pool)   │
└────────────────┘      └────────────┘      └──────────┘
```

### Job Lifecycle State Graph
```
WAITING ──(all parents succeed)──► PENDING ──► SCHEDULED ──► RUNNING ──► SUCCEEDED
   │                                  │           │             │
   │                                  │           │             ├──► RETRYING ──► SCHEDULED
   │                                  │           │             │        │
   │                                  │           │             │        └──(max retries)──► FAILED
   │                                  │           │             │
   └─────────────► CANCELLED ◄────────┴───────────┴─────────────┴─────────────────────────► FAILED
```

---

## Performance and Benchmarks

The following are single local measurements, not maximum-capacity or production claims. Test setup, host details, raw output, and limitations are recorded with each result.

| Workload | Configuration | Measured result |
| :--- | :--- | :--- |
| API load ([k6 report](docs/benchmarks/k6-local-2026-10-02.md)) | 100 VUs; 15s ramp, 45s hold, 15s ramp-down | 10,936 successful creates; 145.29 creates/sec; 320.24 HTTP requests/sec; create p95 41.39 ms; 0 failed requests |
| Scheduler drain ([report](docs/benchmarks/concurrent-scheduler-2026-10-02.md)) | 2 adaptive schedulers, 5 workers, batch 50; 1,000 checksum jobs and 10 failing retry jobs | 1,010 terminal jobs in 23.05s; 43.82 jobs/sec effective drain rate; claim p50/p95 54.06/147.01 ms; 0 duplicate executions; retries exhausted as expected |

These results came from different workloads and should not be compared as competing throughput measurements. The scheduler rate includes retry backoff time. The API run measures a single host and does not establish a saturation point. The raw k6 output is in [k6-summary.json](k6-summary.json).

`internal/job/repository/postgres_bench_test.go` benchmarks database operations. `internal/worker/throughput_bench_test.go` also defines a 17-configuration scheduler sweep, seeding 500 ordinary jobs plus 10 retry-check jobs per configuration. It is not the 1-million-row query fixture described in a source comment. The full sweep uses Linux `/proc` counters and is skipped unless explicitly enabled:

```bash
RUN_THROUGHPUT_REVIEW=1 go test ./internal/worker/... -run '^TestThroughputDesignReview$' -v -timeout 30m
```

The focused two-scheduler drain measurement can be repeated with:

```bash
RUN_CONCURRENT_SCHEDULER_BENCHMARK=1 go test ./internal/worker/... -run '^TestConcurrentSchedulerDrain$' -count=1 -v -timeout 3m
```

The worker and repository integration/benchmark helpers delete rows from the `jobs` table. Use only a disposable test database. The k6 script requires an API key and sends it to each API endpoint. Set `ORCHESTRIX_API_KEY` to a key for the test server, then run:

```bash
k6 run -e API_KEY="$ORCHESTRIX_API_KEY" -e MAX_VUS=100 -e RAMP_DURATION=15s -e HOLD_DURATION=45s --summary-export=k6-summary.json loadtest/api_load_test.js
```

Keep generated summaries with the run's commit, server/database configuration, and machine details. Do not commit API keys.

---

## Security Hardening

| Area | Protection Mechanism |
| :--- | :--- |
| **Authentication** | Bearer API Keys stored as SHA-256 hashes. Verified via constant-time comparison. |
| **Tenant Isolation** | Public API job lookups and lists are scoped by `owner_key_id`. Cross-tenant lookups return uniform `404 Not Found`. |
| **SSRF Prevention** | Custom `http.Transport` validating resolved IPs at dial time (blocks loopback, RFC 1918, link-local, multicast). |
| **DoS Defenses** | Request body capped at 1 MiB (`MaxBytesReader`), `?limit=` capped at 500, CPU `work_factor` capped at 100,000. |
| **Error Masking** | Internal SQL and driver error details logged server-side only; callers receive safe generic messages. |

---

## Configuration

The HTTP port and shutdown timeout are loaded from `configs/base.yaml`. Database connection settings, the config-file path, and the optional bootstrap API key are supplied through environment variables:

| Variable | Default | Description |
| :--- | :--- | :--- |
| `CONFIG_PATH` | `configs/base.yaml` | Path to server YAML config |
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5434` | PostgreSQL port |
| `DB_USER` | `orchestrix` | Database user |
| `DB_PASSWORD` | *(Required)* | Database password |
| `DB_NAME` | `orchestrix_dev` | Database name |
| `DB_SSLMODE` | `disable` | SSL mode (`disable`, `require`, `verify-full`) |
| `ADMIN_API_KEY` | *(Optional)* | Initial API key to seed on first startup |

---

## Observability

### Prometheus Metrics (`GET /metrics`)
- `orchestrix_jobs_created_total` — Counter of created jobs.
- `orchestrix_jobs_succeeded_total` — Counter of succeeded jobs.
- `orchestrix_jobs_failed_total` — Counter of failed jobs.
- `orchestrix_jobs_cancelled_total` — Counter of cancelled jobs.
- `orchestrix_job_duration_seconds` — Execution duration histogram.
- `orchestrix_queue_depth` — Current queue gauge.
- `orchestrix_http_requests_total` — Counter vector by method, endpoint, and status code.

### Health Check (`GET /health`)
Returns `200 OK` with timestamp when the HTTP server is alive.

---

## Project Structure

```
orchestrix/
├── cmd/server/           # Application entrypoint
├── internal/
│   ├── api/              # HTTP routing, handlers, middleware
│   ├── auth/             # API key hashing, storage, auth middleware
│   ├── config/           # YAML/env configuration loader
│   ├── executor/         # Job executors (Demo, Webhook, Checksum, SSRF Guard)
│   ├── job/
│   │   ├── dependency/   # DAG dependency resolver & cycle detector
│   │   ├── model/        # Job domain model & validation
│   │   ├── repository/   # PostgreSQL data access layer & pool
│   │   ├── service/      # Orchestration business logic, retry & ID generators
│   │   └── state/        # State machine transition rules
│   ├── metrics/          # Prometheus metrics definitions
│   ├── scheduler/        # Poller with adaptive batch claiming
│   └── worker/           # Concurrency-controlled worker pool
├── migrations/           # Versioned PostgreSQL migrations
├── loadtest/             # k6 API load test suites
├── configs/              # Base YAML configuration
├── .github/workflows/    # CI pipelines
├── docker-compose.yml    # Container orchestration
└── Dockerfile            # Multi-stage production container build
```

---

## Testing & Verification

Tests and database benchmarks connect to PostgreSQL at `localhost:5434` and test helpers delete existing rows from the `jobs` table. Start the migrations against a disposable database before running them; do not point these commands at data you need to keep.

```bash
# Run tests (requires the disposable, migrated PostgreSQL database)
go test ./...

# Match the CI race-detector and coverage command
go test ./... -race -count=1 -coverprofile=coverage.out -coverpkg=./...
go tool cover -func=coverage.out

# Run Go benchmarks (database-backed benchmarks use the same disposable database)
go test -bench=. -benchmem ./...
```
No raw benchmark output is archived, so this README makes no measured performance claims.

