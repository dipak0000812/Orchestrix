# Orchestrix 🎯

A distributed, asynchronous job orchestration engine in Go backed by PostgreSQL. Orchestrix provides explicit state-machine lifecycle tracking, DAG dependency resolution, exponential retry backoff, concurrency control, and native Prometheus telemetry.

---

## Features

- **Job Lifecycle State Machine** — Strict transition gating (`PENDING` → `SCHEDULED` → `RUNNING` → `SUCCEEDED` / `FAILED` / `RETRYING` / `CANCELLED` / `WAITING`).
- **DAG Dependency Execution** — Cycle-safe dependency graphs. Jobs with `depends_on` wait for parent completion; permanently failed parents cascade cancellations downstream.
- **Automatic Retries & Backoff** — Configurable exponential backoff with jitter and persisted `next_run_at` scheduling.
- **Concurrent Worker Pool** — Bounded worker pools with panic isolation and context cancellation.
- **Adaptive Scheduling** — Continuous claim draining under backlog with idle backoff using `SELECT ... FOR UPDATE SKIP LOCKED`.
- **Tenant Isolation & Auth** — API key authentication (`Bearer`) with SHA-256 hash storage and tenant-scoped job operations.
- **SSRF-Safe Webhooks** — Outbound webhook requests validated at dial-time against private/link-local/loopback CIDRs.
- **Persistent Storage** — PostgreSQL persistence via `pgx/v5` connection pooling with versioned SQL migrations.
- **Observability** — Built-in Prometheus metrics (`/metrics`) and health checks (`/health`).
- **Graceful Teardown** — Dual-stage shutdown letting in-flight jobs finish cleanly within bounded timeframes.

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

## Performance & Benchmarks

### Microbenchmarks
```text
pkg: internal/job/state
BenchmarkValidateTransition-12             126,167,770 ops       10.01 ns/op    (0 allocs/op)
BenchmarkValidateTransition_Invalid-12       3,925,479 ops      362.50 ns/op    (Error formatting)

pkg: internal/executor
BenchmarkChecksumExecutor/work_factor_1      1,456,398 ops      840.20 ns/op
BenchmarkChecksumExecutor/work_factor_1000       7,870 ops   162,338.00 ns/op
BenchmarkWebhookExecutor-12                      4,174 ops   256,197.00 ns/op
```

### Scheduler Sweeps & Concurrency Scaling
Measured under real PostgreSQL backlogs:

| Configuration | Jobs/sec | Claim p95 Latency | In-Memory Depth | Duplicates |
| :--- | :--- | :--- | :--- | :--- |
| `batch=10, poll=1s` | 9.09 | 7.0 ms | 5 | 0 |
| `batch=50, poll=250ms` | 67.71 | 8.0 ms | 45 | 0 |
| `batch=50, poll=100ms` | 83.75 | 6.6 ms | 45 | 0 |
| **Adaptive (`batch=50, poll=1s fallback`)** | **84.10** | **7.2 ms** | **48** | **0** |

### API Concurrency (k6 Load Test)
```bash
k6 run -e MAX_VUS=100 -e RAMP_DURATION=15s -e HOLD_DURATION=45s loadtest/api_load_test.js
```

| Virtual Users (VUs) | Requests | Error Rate | `create_job` p95 | `create_job` p99 | Throughput |
| :--- | :--- | :--- | :--- | :--- | :--- |
| 5 VUs | 332 | 0.00% | 4.48 ms | 6.59 ms | 16.5 req/s |
| 30 VUs | 7,866 | 0.00% | 8.72 ms | 25.28 ms | 104.2 req/s |
| 100 VUs | 25,286 | 0.00% | 33.31 ms | 111.42 ms | **335.8 req/s** |

---

## Security Hardening

| Area | Protection Mechanism |
| :--- | :--- |
| **Authentication** | Bearer API Keys stored as SHA-256 hashes. Verified via constant-time comparison. |
| **Tenant Isolation** | All queries scoped by `owner_key_id`. Cross-tenant lookups return uniform `404 Not Found`. |
| **SSRF Prevention** | Custom `http.Transport` validating resolved IPs at dial time (blocks loopback, RFC 1918, link-local, multicast). |
| **DoS Defenses** | Request body capped at 1 MiB (`MaxBytesReader`), `?limit=` capped at 500, CPU `work_factor` capped at 100,000. |
| **Error Masking** | Internal SQL and driver error details logged server-side only; callers receive safe generic messages. |

---

## Configuration

Configuration values are loaded from `configs/base.yaml` and overridden via environment variables:

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

```bash
# Run unit and integration tests
go test ./...

# Run race detector and coverage
go test -race -coverprofile=coverage.out ./...

# Run benchmarks
go test -bench=. -benchmem ./...
```

---

## License

MIT License — see [LICENSE](LICENSE) for details.
