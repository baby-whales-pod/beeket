# Monitoring and Telemetry

`beeket serve` exposes Prometheus metrics and a status endpoint out of the box — no flags needed. A pre-built Docker Compose stack for Prometheus + Grafana is included in [`examples/monitoring/`](../examples/monitoring/).

## Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/metrics` | GET | Prometheus scrape endpoint |
| `/api/status` | GET | JSON status: version, uptime, backend, loaded models |
| `/healthz` | GET | Liveness probe — always `200 OK` when the process is running |
| `/readyz` | GET | Readiness probe — `200 OK` when the engine is ready |

### `/api/status` response

```json
{
  "version": "0.1.0",
  "commit": "a58605e",
  "built": "2026-05-29T10:00:00Z",
  "uptime_seconds": 1234.5,
  "backend": "metal",
  "max_loaded": 3,
  "num_parallel": 1,
  "loaded_models": [
    {
      "name": "llama-3-8b:q4_k_m",
      "size": 4700000000,
      "last_used": "2026-05-29T10:11:12Z"
    }
  ]
}
```

## Flags

| Flag | Env var | Default | Description |
|------|---------|---------|-------------|
| `--metrics-enabled` | `BEEKET_METRICS_ENABLED` | `true` | Enable/disable the `/metrics` endpoint |
| `--metrics-bind` | `BEEKET_METRICS_BIND` | `""` | Optional secondary listener for `/metrics` (e.g. `0.0.0.0:11436`) |

### Exposing metrics for Prometheus in Docker

By default beeket binds to `127.0.0.1:11435`, which is not reachable from inside a Docker container. Use one of:

```bash
# Option A — expose the main listener on all interfaces
beeket serve --host 0.0.0.0

# Option B — keep the main listener on localhost, open a separate metrics port
beeket serve --metrics-bind 0.0.0.0:11436
```

Then update `prometheus.yml` to scrape `host.docker.internal:11435` (or `:11436`).
On Linux, also use the `docker-compose.linux.yml` override (adds `host-gateway`).

## Metrics reference

### HTTP

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `beeket_http_requests_total` | counter | `method`, `pattern`, `status_code` | Total HTTP requests handled |
| `beeket_http_request_duration_seconds` | histogram | `method`, `pattern` | Request latency |
| `beeket_http_requests_in_flight` | gauge | `method` | Requests currently being processed |

The `pattern` label uses the Go 1.22 `ServeMux` matched route pattern (e.g. `POST /api/chat`) to keep cardinality bounded.

### Inference

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `beeket_inference_requests_total` | counter | `model`, `endpoint`, `outcome` | Inference requests by outcome (`ok`/`error`/`cancelled`) |
| `beeket_inference_time_to_first_token_seconds` | histogram | `model` | Time from request receipt to first generated token |
| `beeket_inference_tokens_per_second` | histogram | `model` | Generated token throughput per request |
| `beeket_inference_duration_seconds` | histogram | `model` | End-to-end inference duration |
| `beeket_inference_tokens_total` | counter | `model`, `kind` | Tokens processed (`prompt` or `eval`) |

### Model lifecycle

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `beeket_models_loaded` | gauge | — | Number of models currently in memory |
| `beeket_model_load_duration_seconds` | histogram | `model` | Time to load a model |
| `beeket_model_evictions_total` | counter | `reason` | Model evictions by reason (`lru`, `idle`) |

### Build / process

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `beeket_build_info` | gauge | `version`, `commit`, `built` | Always 1; labels carry build metadata |
| `beeket_uptime_seconds` | gauge | — | Seconds since `beeket serve` started |
| `go_*` | — | — | Go runtime metrics (goroutines, GC, memory) |
| `process_*` | — | — | OS process metrics (CPU, RSS, open FDs) |

## Example PromQL queries

```promql
# Request rate per endpoint (req/s over 1-minute window)
sum by (pattern) (rate(beeket_http_requests_total[1m]))

# HTTP p95 latency by endpoint
histogram_quantile(0.95, sum by (pattern, le) (
  rate(beeket_http_request_duration_seconds_bucket[5m])
))

# HTTP error ratio (5xx)
sum by (pattern) (rate(beeket_http_requests_total{status_code=~"5.."}[5m]))
/ sum by (pattern) (rate(beeket_http_requests_total[5m]))

# Inference tokens/sec p50 and p95
histogram_quantile(0.50, sum by (model, le) (rate(beeket_inference_tokens_per_second_bucket[5m])))
histogram_quantile(0.95, sum by (model, le) (rate(beeket_inference_tokens_per_second_bucket[5m])))

# TTFT p95
histogram_quantile(0.95, sum by (model, le) (
  rate(beeket_inference_time_to_first_token_seconds_bucket[5m])
))

# Model load time p95
histogram_quantile(0.95, sum by (model, le) (
  rate(beeket_model_load_duration_seconds_bucket[10m])
))

# Models currently loaded
beeket_models_loaded

# Process RSS
process_resident_memory_bytes

# Uptime
beeket_uptime_seconds
```

## Quick start with Docker Compose

See [`examples/monitoring/README.md`](../examples/monitoring/README.md) for the full setup guide.

```bash
cd examples/monitoring

# macOS / Windows
docker compose up -d

# Linux
docker compose -f docker-compose.yml -f docker-compose.linux.yml up -d
```

Open Grafana at http://localhost:3000 (admin / beeket). The **Beeket** dashboard is pre-provisioned.
