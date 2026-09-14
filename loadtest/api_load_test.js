// k6 load test for Orchestrix's REST API.
//
// Run with a live orchestrix server (see README for how to start one),
// then:
//
//   k6 run loadtest/api_load_test.js
//
// Override the target and load profile via env vars, e.g.:
//
//   k6 run -e BASE_URL=http://localhost:8080 \
//          -e MAX_VUS=50 \
//          -e RAMP_DURATION=30s \
//          -e HOLD_DURATION=1m \
//          loadtest/api_load_test.js
//
// This script does not assert throughput/latency numbers into any
// documentation by itself -- it only measures. Real P50/P95/P99 numbers
// must come from an actual run against a real server; see the
// LOAD_TEST_RESULTS.md template in this directory for how results get
// recorded.

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Counter } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const MAX_VUS = parseInt(__ENV.MAX_VUS || '20', 10);
const RAMP_DURATION = __ENV.RAMP_DURATION || '30s';
const HOLD_DURATION = __ENV.HOLD_DURATION || '1m';

// Per-endpoint latency, tracked separately so create (a write, hits
// Postgres INSERT) and get/list (reads) don't average each other out.
const createJobDuration = new Trend('create_job_duration', true);
const getJobDuration = new Trend('get_job_duration', true);
const listJobsDuration = new Trend('list_jobs_duration', true);
const createJobFailures = new Counter('create_job_failures');
const getJobFailures = new Counter('get_job_failures');

export const options = {
  scenarios: {
    ramping_load: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: RAMP_DURATION, target: MAX_VUS },
        { duration: HOLD_DURATION, target: MAX_VUS },
        { duration: RAMP_DURATION, target: 0 },
      ],
    },
  },
  // Report p50 (median), p95, p99 explicitly, as requested -- k6's
  // default summary only shows p90/p95 unless configured.
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
  // These are target SLOs, not claims. Whether the run actually meets
  // them is exactly what this test exists to find out.
  thresholds: {
    'create_job_duration': ['p(95)<500'],
    'get_job_duration': ['p(95)<200'],
    'http_req_failed': ['rate<0.01'],
  },
};

export default function () {
  // 1. Create a job -- a real write, hits the Postgres INSERT path.
  // NOTE: `payload` must be a raw nested JSON object here, not a
  // JSON.stringify()'d string -- the server's CreateJobRequest.Payload is
  // json.RawMessage, so a stringified value would arrive double-encoded
  // and fail executor-side unmarshaling (confirmed via manual curl
  // testing against a real server before writing this).
  const body = JSON.stringify({
    type: 'compute_checksum',
    payload: { data: `loadtest-${__VU}-${__ITER}`, work_factor: 1 },
  });

  const createRes = http.post(`${BASE_URL}/api/v1/jobs`, body, {
    headers: { 'Content-Type': 'application/json' },
    tags: { endpoint: 'create_job' },
  });
  createJobDuration.add(createRes.timings.duration);

  const createOk = check(createRes, {
    'create job status is 201': (r) => r.status === 201,
    'create job returns an id': (r) => {
      try {
        return JSON.parse(r.body).id !== undefined;
      } catch (e) {
        return false;
      }
    },
  });
  if (!createOk) {
    createJobFailures.add(1);
    sleep(1);
    return;
  }

  const jobID = JSON.parse(createRes.body).id;

  // 2. Read the job back -- a real point-lookup read.
  const getRes = http.get(`${BASE_URL}/api/v1/jobs/${jobID}`, {
    tags: { endpoint: 'get_job' },
  });
  getJobDuration.add(getRes.timings.duration);

  const getOk = check(getRes, {
    'get job status is 200': (r) => r.status === 200,
  });
  if (!getOk) {
    getJobFailures.add(1);
  }

  // 3. Occasionally list jobs -- a heavier read, exercises pagination /
  // full-table-scan-shaped queries rather than a single indexed lookup.
  // NOTE: GET /api/v1/jobs defaults to `state=PENDING` when no `state`
  // query param is given (confirmed via manual testing) -- this is not a
  // "list everything" call, it's effectively "list the current queue
  // depth", which is arguably the more realistic thing to load test
  // anyway (it's what a monitoring/ops dashboard would actually poll).
  if (__ITER % 5 === 0) {
    const listRes = http.get(`${BASE_URL}/api/v1/jobs`, {
      tags: { endpoint: 'list_jobs' },
    });
    listJobsDuration.add(listRes.timings.duration);
    check(listRes, { 'list jobs status is 200': (r) => r.status === 200 });
  }

  sleep(0.5);
}
