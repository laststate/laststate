# Deploying Trace

Step-by-step deployment guide for self-hosted Trace.

## Prerequisites

- Docker 24+ and Docker Compose V2, or Kubernetes 1.28+
- PostgreSQL 15+ (for production)
- At least 2 vCPU and 4 GB RAM for a single-node deployment
- 10 GB free disk for the first month of crash data

## Quick Start (Docker Compose)

### Minimal — Trace only with in-memory backend

```bash
git clone https://github.com/laststate/trace.git
cd trace

# Start with mock data (no database required)
TRACE_MOCK=true docker compose up --build

# Open http://localhost:8080
```

Bootstrap credentials are written to:
- `data/bootstrap-token.txt` — Ingest bearer token (for Relay)
- `data/bootstrap-admin.txt` — Admin email + password

### Production — Trace with PostgreSQL

```bash
# Create .env from example and adjust values
cp .env.example .env
# Edit .env: set TRACE_DATABASE_URL, TRACE_JWT_SECRET, etc.

# Run migrations and start
docker compose up --build -d

# Verify
docker compose ps
curl -s http://localhost:8080/health
```

### Full stack — Trace + Relay + PostgreSQL

```bash
docker compose -f docker-compose.laststate.yml up --build -d
```

This starts Trace, Relay, and PostgreSQL. Relay ships crash data from
devices to Trace automatically.

## Kubernetes (Helm)

```bash
# Add the Helm repo
helm repo add laststate https://laststate.github.io/charts
helm repo update

# Create a values override file
cat > values.yaml <<EOF
trace:
  replicaCount: 2
  persistence:
    size: 50Gi
  ingress:
    enabled: true
    hostname: trace.example.com
    tls: true
postgres:
  enabled: true
  persistence:
    size: 50Gi
EOF

# Install
helm install trace laststate/trace -f values.yaml -n laststate --create-namespace

# Get bootstrap token
kubectl get secret trace-bootstrap -n laststate -o jsonpath='{.data.token}' | base64 -d
```

## Configuration

All configuration is via environment variables. See `.env.example` for the
complete list. Key variables:

| Variable | Required | Description |
|----------|----------|-------------|
| `TRACE_DATABASE_URL` | production | PostgreSQL connection string |
| `TRACE_JWT_SECRET` | production | JWT signing secret (change in production!) |
| `TRACE_QUEUE` | production | Queue backend: `postgres`, `redis`, or `memory` |
| `TRACE_OBJECT_DIR` | production | Path for crash dump storage |
| `TRACE_ALLOW_PUBLIC_REGISTER` | recommended | Set to `false` in production |
| `TRACE_OIDC_AUTO_JOIN` | recommended | Set to `false` in production |
| `TRACE_TRUSTED_PROXIES` | recommended | Trusted proxy IPs for XFF |
| `TRACE_SECRETS_KEY` | recommended | AES-GCM key for alert/channel encryption |

## Migrations

Trace manages its own database migrations automatically on startup. The
migration files are in `internal/db/migrations/`.

```bash
# Check current migration status
docker compose exec trace go run ./cmd/trace --migrate status

# Run migrations manually if needed
docker compose exec trace go run ./cmd/trace --migrate up
```

**Never** manually edit migration files in production. Always add new
migrations as new files and run them in order.

## TLS and HTTPS

Trace should always run behind a reverse proxy with TLS in production:

```nginx
server {
    listen 443 ssl;
    server_name trace.example.com;

    ssl_certificate /etc/ssl/certs/trace.crt;
    ssl_certificate_key /etc/ssl/private/trace.key;

    location / {
        proxy_pass http://trace:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Set `TRACE_TRUSTED_PROXIES` to your proxy's IP range (e.g.,
`TRACE_TRUSTED_PROXIES=10.0.0.0/8`) so Trace correctly resolves client IPs.

## Backup and Restore

```bash
# Backup
./scripts/backup.sh

# Restore
./scripts/restore.sh <backup-file>
```

Run backups daily. The backup script handles database dump and object store
sync.

## Monitoring

- **Health**: `GET /health` returns `200 OK` when healthy.
- **Metrics**: `GET /metrics` exposes Prometheus metrics.
- **Logs**: Structured JSON logs at `info` level by default. Use
  `TRACE_LOG_LEVEL=debug` for verbose output.

Recommended Prometheus scrape config:

```yaml
scrape_configs:
  - job_name: 'trace'
    static_configs:
      - targets: ['trace:8080']
    metrics_path: '/metrics'
```

## Scaling

For high-traffic deployments, run API and worker separately:

```bash
# Stateless API (horizontal scale)
TRACE_MODE=api TRACE_QUEUE=redis docker compose up trace-api

# Worker (consumes events)
TRACE_MODE=worker TRACE_QUEUE=redis docker compose up trace-worker
```

See [SCALE.md](./SCALE.md) for detailed horizontal scaling patterns.

## Troubleshooting

- **"Connection refused" on port 8080**: Check if PostgreSQL is healthy:
  `docker compose ps`. Ensure `TRACE_DATABASE_URL` is correct.
- **"No bootstrap token"**: Run `./laststate bootstrap` or check
  `data/bootstrap-token.txt`.
- **"Database migration failed"**: Run migrations manually:
  `docker compose exec trace go run ./cmd/trace --migrate up`.
- **High memory usage**: Reduce `TRACE_OBJECT_DIR` retention or enable
  automatic GC: set `TRACE_GC_ENABLED=true`.

For more help, see [SUPPORT.md](../SUPPORT.md).
