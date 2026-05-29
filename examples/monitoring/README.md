# Beeket Monitoring Stack

Prometheus + Grafana pre-wired for beeket, with an optional Loki+Promtail profile for log aggregation.

## Prerequisites

- Docker Engine ≥ 24 with the Compose plugin
- `beeket serve` running on the **host** (not inside Docker)

## Quick start

### macOS / Windows

```bash
# Terminal 1 — run beeket on the host (default port 11435)
beeket serve

# Terminal 2 — start the stack
cd examples/monitoring
docker compose up -d
```

### Linux

On Linux, containers cannot reach `host.docker.internal` by default.
The `docker-compose.linux.yml` override adds the `host-gateway` mapping:

```bash
# Terminal 1 — expose metrics on a dedicated secondary port
beeket serve --metrics-bind 0.0.0.0:11436
# or expose the main listener on all interfaces: beeket serve --host 0.0.0.0

# Terminal 2
cd examples/monitoring
docker compose -f docker-compose.yml -f docker-compose.linux.yml up -d
```

## Accessing the services

| Service    | URL                        | Credentials   |
|------------|----------------------------|---------------|
| Grafana    | http://localhost:3000      | admin / beeket |
| Prometheus | http://localhost:9090      | —             |

The **Beeket** dashboard is pre-provisioned. Open Grafana → Dashboards → Beeket.

## Stopping

```bash
docker compose down          # keep volumes
docker compose down -v       # also remove stored data
```

## Optional: log aggregation with Loki

Enable the `logs` profile to add Loki + Promtail:

```bash
# Redirect beeket stderr to /var/log/beeket/beeket.log for Promtail to scrape:
sudo mkdir -p /var/log/beeket
beeket serve 2>/var/log/beeket/beeket.log &

# Start the full stack including Loki + Promtail
docker compose --profile logs up -d
```

The Promtail config expects logs at `/var/log/beeket/*.log`. Edit
`promtail/promtail.yml` to match your actual log path.

## Customising the Prometheus scrape target

Edit `prometheus/prometheus.yml` if beeket is running on a non-default port
or address, then reload: `curl -X POST http://localhost:9090/-/reload`.
