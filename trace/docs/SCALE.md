# Scaling

## Process model

| Mode | Use |
|------|-----|
| `TRACE_MODE=all` | Development and small fleets |
| `TRACE_MODE=api` | Stateless HTTP (ingest + UI) |
| `TRACE_MODE=worker` | N job consumers |

Helm deploys API and worker separately under `deploy/helm/trace`.

## Queue backends

| Backend | When to use |
|---------|-------------|
| `postgres` | Default durable mid-scale |
| `redis` | Higher claim throughput; set `TRACE_QUEUE_URL` |
| `memory` / `nats` (sim) | Local tests only |

Ingest and workers must share the same `TRACE_QUEUE` configuration. Soft backpressure rejects enqueue when ready depth exceeds the process limit (default 50 000).

## Horizontal scaling checklist

1. Run multiple workers against the same queue.
2. Use S3-compatible object storage (not local disk) for multi-node.
3. Put PgBouncer in front of Postgres for API fan-out.
4. Scope tokens per project (`X-Project-ID` when multi-project).
5. Tune per-project retention; GC runs in the worker process.
6. Set `TRACE_TRUSTED_PROXIES` behind a real load balancer.

## Not claimed

- Sub-second analytics on trillions of events (no external warehouse required; optional NDJSON export exists).
- Multi-region active-active without an external design.

Scrape `GET /metrics` with Prometheus; alert on job backlog growth.
