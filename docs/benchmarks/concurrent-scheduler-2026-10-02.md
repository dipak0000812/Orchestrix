# Concurrent Scheduler Drain - 2026-10-02

## Measurement

One local run of `TestConcurrentSchedulerDrain` on the working tree based on commit `bf700c8` (the benchmark test itself was uncommitted during this run).

| Measurement | Result |
| :--- | :--- |
| Jobs seeded | 1,000 checksum jobs + 10 deliberately failing jobs |
| Schedulers / workers | 2 adaptive schedulers / 5 workers |
| Claim batch | 50 per scheduler; 1s fallback poll interval |
| Terminal jobs | 1,000 `SUCCEEDED` + 10 `FAILED` after retry exhaustion |
| Drain duration | 23.0489s, measured after seeding and before shutdown |
| Effective throughput | 43.82 jobs/sec (1,010 terminal jobs / drain duration) |
| Claim latency | p50 54.0628ms; p95 147.0135ms |
| Duplicate executions | 0 |
| Retry exhaustion check | Passed |
| Test result | Passed in 35.90s total test time |

Exact test log line:

```text
MEASURED schedulers=2 workers=5 batch=50 adaptive=true jobs=1010 duration=23.0489017s jobs/sec=43.82 claims=62 claim(p50/p95)=54.0628ms/147.0135ms duplicates=0 retryOK=true
```

## Environment

- Host OS: Windows 11 Pro 10.0.26200, 64-bit.
- CPU: 12th Gen Intel Core i5-1245U, 10 cores / 12 logical processors.
- Visible RAM: 15.6 GiB.
- Database: PostgreSQL 16.15, in a disposable Docker container with no persistent volume.
- Docker server: 29.7.2.
- Go: 1.27.0, Windows/amd64.
- Base revision: `bf700c8`; only the benchmark test was an uncommitted source change during the run.

## Reproduction

Start a disposable, migrated PostgreSQL database at `localhost:5434`, then run from the repository root:

```powershell
$env:RUN_CONCURRENT_SCHEDULER_BENCHMARK = '1'
go test ./internal/worker/... -run '^TestConcurrentSchedulerDrain$' -count=1 -v -timeout 3m
```

The worker test helper clears the `jobs` table, so this command must only target a disposable database.

## Caveats

This is one local run with a fixed workload, not a repeated statistical benchmark, maximum-capacity test, production result, or 1-million-row test. The rate is effective end-to-end job drain throughput for this configuration and includes time spent waiting for the deliberately failing jobs to exhaust retries. Repeat runs on a stable host before treating the number as a comparative performance claim.
