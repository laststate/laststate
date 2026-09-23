# Observability

## Metrics

`GET /metrics` exposes Prometheus text:

- `trace_ingest_*`
- `trace_jobs_*`
- `trace_artifact_uploads_total`
- `trace_webhook_*`
- `trace_gc_deleted_total`
- `trace_up`, `trace_start_time_seconds`

## Tracing

HTTP responses include `X-Trace-ID` and `X-Span-ID` (generated if missing). Propagate these IDs in Relay clients and logs.

Structured logs are JSON via `log/slog` (`job failed`, `gc`, `bootstrap`, etc.).

## Suggested Grafana panels

1. Ingest accept vs reject rate  
2. Job fail rate  
3. Webhook delivery success  
4. Process uptime  

## Dashboards

Ship Prometheus scrape config:

```yaml
scrape_configs:
  - job_name: trace
    static_configs:
      - targets: ["trace:8080"]
```
