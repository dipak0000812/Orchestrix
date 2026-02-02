# Orchestrix 🎯

A production-grade job orchestration engine built in Go.

## Features

- ✅ **Job Lifecycle Management** - Complete state machine (PENDING → SCHEDULED → RUNNING → SUCCEEDED/FAILED)
- ✅ **Automatic Retry** - Exponential backoff with configurable max attempts
- ✅ **Concurrent Execution** - Worker pool with configurable workers
- ✅ **REST API** - HTTP endpoints for job management
- ✅ **Persistent Storage** - PostgreSQL with migrations
- ✅ **Metrics** - Prometheus metrics for monitoring
- ✅ **Graceful Shutdown** - Zero job loss on deployment
- ✅ **Docker Support** - Fully containerized

## Quick Start

### Prerequisites

- Docker & Docker Compose
- Go 1.22+ (for local development)

### Run with Docker (Easiest)
```bash
# Clone repository
git clone https://github.com/dipak0000812/Orchestrix.git
cd Orchestrix

# Start all services
docker-compose up -d

# Check health
curl http://localhost:8080/health
```

### Run Locally
```bash
# Start PostgreSQL
docker-compose up -d postgres

# Run migrations
make migrate-up

# Start server
go run cmd/server/main.go
```

## API Usage

### Create a Job
```bash
curl -X POST http://localhost:8080/api/v1/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "type": "demo_job",
    "payload": {"message": "hello world"}
  }'
```

**Response:**
```json
{
  "id": "01KG94QDSXNW96W84543ZG5PY5",
  "type": "demo_job",
  "state": "PENDING",
  "created_at": "2026-01-31T09:54:37Z"
}
```

### Get Job Status
```bash
curl http://localhost:8080/api/v1/jobs/01KG94QDSXNW96W84543ZG5PY5
```

### List Jobs by State
```bash
curl "http://localhost:8080/api/v1/jobs?state=SUCCEEDED&limit=10"
```

### Cancel a Job
```bash
curl -X DELETE http://localhost:8080/api/v1/jobs/01KG94QDSXNW96W84543ZG5PY5
```

## Architecture
```
┌─────────────┐
│   HTTP API  │  ← REST endpoints (port 8080)
└──────┬──────┘
       │
┌──────▼──────┐
│ Job Service │  ← Business logic, validation
└──────┬──────┘
       │
┌──────▼──────┐
│ Repository  │  ← Data access (PostgreSQL)
└──────┬──────┘
       │
┌──────▼──────┐
│  Database   │  ← PostgreSQL
└─────────────┘

Background Workers:
┌───────────┐      ┌────────────┐      ┌──────────┐
│ Scheduler │─────→│ Job Queue  │─────→│ Workers  │
│ (Polls DB)│      │ (Channel)  │      │ (Pool)   │
└───────────┘      └────────────┘      └──────────┘
```

## Job Lifecycle
```
PENDING → SCHEDULED → RUNNING → SUCCEEDED
                  ↓              ↓
                  └─→ RETRYING ─→ FAILED (after max retries)
                  ↓
                  └─→ CANCELLED (user action)
```

## Configuration

Configuration is loaded from environment variables:
```bash
DB_HOST=localhost           # Database host
DB_PORT=5434               # Database port
DB_USER=orchestrix         # Database user
DB_PASSWORD=***            # Database password
DB_NAME=orchestrix_dev     # Database name
DB_SSLMODE=disable         # SSL mode
```

## Monitoring

### Prometheus Metrics

Available at `http://localhost:8080/metrics`:

- `orchestrix_jobs_created_total` - Total jobs created
- `orchestrix_jobs_succeeded_total` - Total successful jobs
- `orchestrix_jobs_failed_total` - Total failed jobs
- `orchestrix_job_duration_seconds` - Job execution time histogram
- `orchestrix_queue_depth` - Current jobs in queue

### Health Check
```bash
curl http://localhost:8080/health
```

## Development

### Project Structure
```
orchestrix/
├── cmd/server/           # Application entry point
├── internal/
│   ├── api/              # HTTP handlers
│   ├── job/
│   │   ├── model/        # Job domain model
│   │   ├── service/      # Business logic
│   │   ├── state/        # State machine
│   │   └── repository/   # Data access
│   ├── scheduler/        # Job scheduler
│   ├── worker/           # Worker pool
│   └── executor/         # Job executors
├── migrations/           # Database migrations
├── docker-compose.yml    # Docker services
└── Dockerfile           # Container build
```

### Running Tests
```bash
# Unit tests
go test ./...

# Integration tests
go test -v ./internal/worker/ -run Integration

# With coverage
go test -cover ./...
```

### Database Migrations
```bash
# Apply migrations
make migrate-up

# Rollback last migration
make migrate-down

# Create new migration
make migrate-create name=add_priority_column
```

## Production Deployment

### Build Docker Image
```bash
docker build -t orchestrix:latest .
```

### Deploy
```bash
# Using docker-compose
docker-compose up -d

# Or deploy to Kubernetes (k8s manifests not included)
```

### Graceful Shutdown

The server handles SIGTERM/SIGINT signals:

1. Stops accepting new requests
2. Stops scheduler (no new jobs scheduled)
3. Drains job queue (completes in-flight jobs)
4. Shuts down after 30s timeout

## Challenges Solved

### Race Condition in Scheduler
The scheduler polls the database for pending jobs every second. 
When multiple instances run, the same job could be picked up twice.

**Fix**: Used PostgreSQL's `SELECT FOR UPDATE SKIP LOCKED` inside 
a transaction to atomically claim jobs. Each scheduler instance 
gets different jobs with zero duplicates.

### Silent Failures  
When an executor wasn't registered for a job type, the job would 
retry forever, wasting resources.

**Fix**: Classified errors as retryable vs permanent. Missing executors 
and panics go straight to FAILED. Only actual execution errors retry.

### Import Cycle
Adding metrics to the worker package created a circular dependency: 
worker → api → worker.

**Fix**: Extracted metrics into its own dedicated package that both 
api and worker import independently.



Status: v1 complete. Future versions may introduce Redis/Kafka-backed queues and service separation.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## License

MIT License - See LICENSE file

## Author

Built by [@dipak0000812](https://github.com/dipak0000812)

## Support

- Issues: [GitHub Issues](https://github.com/dipak0000812/Orchestrix/issues)
- Discussions: [GitHub Discussions](https://github.com/dipak0000812/Orchestrix/discussions)
