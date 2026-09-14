# Orchestrix REST API Reference

Orchestrix exposes a RESTful HTTP API on port `8080` for managing asynchronous jobs and querying metrics.

---

## Authentication

All endpoints (except `GET /health`) require an API key passed via the `Authorization` header:

```http
Authorization: Bearer <API_KEY>
```

Jobs are isolated per API key. Clients can only access and modify jobs created under their authenticated key.

---

## Endpoints

### 1. Health Check

Checks whether the API server is reachable. Does not require authentication.

```http
GET /health
```

#### Response `200 OK`
```json
{
  "status": "healthy",
  "timestamp": "2026-07-21T03:30:00Z"
}
```

---

### 2. Create Job

Creates a new asynchronous job with state `PENDING` (or `WAITING` if dependencies are specified).

```http
POST /api/v1/jobs
Content-Type: application/json
Authorization: Bearer <API_KEY>
```

#### Request Body
```json
{
  "type": "compute_checksum",
  "payload": {
    "data": "input payload",
    "work_factor": 1
  },
  "depends_on": [
    "01KG94QDSXNW96W84543ZG5PY5"
  ]
}
```

#### Field Specifications
| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `type` | `string` | Yes | Job executor type (`demo_job`, `http_webhook`, `compute_checksum`) |
| `payload` | `object` | No | Job-specific JSON payload (Max 1 MiB) |
| `depends_on` | `string[]` | No | List of parent job ULIDs that must succeed before this job executes |

#### Response `201 Created`
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

---

### 3. Get Job by ID

Retrieves details and lifecycle state of a specific job.

```http
GET /api/v1/jobs/{id}
Authorization: Bearer <API_KEY>
```

#### Response `200 OK`
```json
{
  "id": "01KG94QDSXNW96W84543ZG5PY5",
  "type": "compute_checksum",
  "state": "SUCCEEDED",
  "attempt": 1,
  "max_attempts": 3,
  "created_at": "2026-07-21T03:33:26Z",
  "scheduled_at": "2026-07-21T03:33:27Z",
  "started_at": "2026-07-21T03:33:27Z",
  "completed_at": "2026-07-21T03:33:28Z"
}
```

#### Response `404 Not Found`
Returned if the job does not exist or belongs to another API key.

---

### 4. List Jobs

Lists jobs belonging to the authenticated API key, filtered by state.

```http
GET /api/v1/jobs?state=SUCCEEDED&limit=10
Authorization: Bearer <API_KEY>
```

#### Query Parameters
| Parameter | Type | Default | Max | Description |
| :--- | :--- | :--- | :--- | :--- |
| `state` | `string` | `PENDING` | - | Filter by state: `PENDING`, `WAITING`, `SCHEDULED`, `RUNNING`, `SUCCEEDED`, `FAILED`, `RETRYING`, `CANCELLED` |
| `limit` | `integer` | `10` | `500` | Maximum number of results to return |

#### Response `200 OK`
```json
{
  "jobs": [
    {
      "id": "01KG94QDSXNW96W84543ZG5PY5",
      "type": "compute_checksum",
      "state": "SUCCEEDED",
      "attempt": 1,
      "max_attempts": 3,
      "created_at": "2026-07-21T03:33:26Z",
      "completed_at": "2026-07-21T03:33:28Z"
    }
  ],
  "total": 1
}
```

---

### 5. Cancel Job

Cancels a pending, waiting, or running job.

```http
DELETE /api/v1/jobs/{id}
Authorization: Bearer <API_KEY>
```

#### Response `204 No Content`
Job successfully cancelled.

#### Response `400 Bad Request`
Returned if the job is already in a terminal state (`SUCCEEDED`, `FAILED`, `CANCELLED`).

---

### 6. Prometheus Metrics

Exposes Prometheus format metrics for monitoring and alerting.

```http
GET /metrics
Authorization: Bearer <API_KEY>
```

#### Key Metrics Exported
- `orchestrix_jobs_created_total`
- `orchestrix_jobs_succeeded_total`
- `orchestrix_jobs_failed_total`
- `orchestrix_jobs_cancelled_total`
- `orchestrix_job_duration_seconds`
- `orchestrix_queue_depth`
- `orchestrix_http_requests_total`
