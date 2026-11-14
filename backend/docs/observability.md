# Observability, API spec and load testing (B10)

## Metrics (Prometheus)

Set `METRICS_ADDR=host:port` on the API and/or the worker to serve `/metrics` on a **separate
listener**. It is never on the public port (the public port answers 404 for `/metrics`). Keep it on an
internal network or bind it to `127.0.0.1` and scrape through a sidecar. There is no client library:
`internal/metrics` writes the text format directly.

| Metric | Where | Labels | Meaning |
|---|---|---|---|
| `http_requests_total` | API | method, route, class | requests; `route` is a bounded group (`videos`, `auth`, `stream`, `health`, `other`), ids and tokens never become labels |
| `http_request_duration_seconds` | API | route | latency histogram |
| `db_pool_acquired_conns`, `_idle_`, `_total_`, `_max_conns` | API | | PostgreSQL pool saturation |
| `worker_jobs_total` | worker | type, result | `completed`, `retry`, `failed`, `released`, `lease_lost` |
| `worker_job_duration_seconds` | worker | type | wall time of an attempt |
| `worker_queue_jobs` | worker | type, status | QUEUED / PROCESSING / FAILED (last 24 h), explicit zeros |
| `worker_queue_oldest_seconds` | worker | type, status | how long the oldest job has waited |
| `worker_queue_scrape_ok` | worker | | 0 when the queue cannot be read |
| `go_goroutines`, `go_memstats_*` | both | | process health |

Suggested alerts:

```
worker_queue_oldest_seconds{status="QUEUED"} > 900                       # uploads are not being processed
increase(worker_jobs_total{result="failed"}[15m]) > 5                    # encodes or scans failing
rate(http_requests_total{class="5xx"}[5m]) / rate(http_requests_total[5m]) > 0.02
db_pool_acquired_conns / db_pool_max_conns > 0.9
worker_queue_scrape_ok == 0
```

Logs are structured JSON (`APP_ENV` other than development): request id, method, path (stream tokens are
redacted), status, latency; the worker logs job id, video id, type and attempt.

## OpenAPI

`docs/openapi.json` (OpenAPI 3.0.3) is **generated** from the route registrations with `go/ast`
(`go run ./cmd/openapi`, or `make openapi`): method, path, path parameters, success status, security
(age cookie / session / verified email / role, detected from the wrappers around each handler) and the
descriptions in `api-contract.md`. A test fails when the file is stale, so a route cannot be added
without appearing in the spec. Open it in Swagger UI or Redoc.

It is route-complete, not schema-complete: request and response bodies are described in prose in
`api-contract.md`; routes that the contract does not describe yet say so in their description.

## Load test

`loadtest/k6-read.js` simulates viewers (age gate, feed, trending, search, detail, first stream
playlist and segment) with thresholds on p95 latency and error rate:

```
k6 run -e BASE_URL=https://staging-api.example.com -e ORIGIN=https://staging.example.com -e VUS=200 loadtest/k6-read.js
```

It was written but **not run** against a production-sized environment: do that on staging with realistic
data before launch, and watch `db_pool_*`, the worker queue and the stream route while it runs.
