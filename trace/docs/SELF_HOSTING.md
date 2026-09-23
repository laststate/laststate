# Self-Hosting LastState

LastState is designed to run entirely on your infrastructure. No cloud required. No data egress. Air-gapped deployments supported.

## Quick Start (60 seconds)

```bash
# Clone the stack
git clone https://github.com/laststate/trace.git
cd trace

# Initialize deployment
docker compose up --build

# Done! Open http://localhost:8080
```

Bootstrap credentials are written to:
- `data/bootstrap-token.txt` — Ingest bearer token (for Relay)
- `data/bootstrap-admin.txt` — Admin email + password

## Using the CLI

The `laststate` CLI manages your deployment:

```bash
# Build the CLI
cd trace/cmd/laststate
go build -o laststate .

# Initialize a new deployment
./laststate init

# Start all services
./laststate up

# Check status
./laststate status

# Follow logs
./laststate logs trace
./laststate logs relay

# Stop services
./laststate down

# Show configuration
./laststate config

# Generate new bootstrap tokens
./laststate bootstrap
```

## Architecture

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Latch     │────▶│   Relay     │────▶│   Trace     │
│  (device)   │     │ (gateway)   │     │ (backend)   │
│  C11/Rust   │     │   Go        │     │   Go        │
└─────────────┘     └─────────────┘     └─────────────┘
                         │                   │
                    SQLite spool       PostgreSQL
```

### Services

| Service | Port | Description |
|---------|------|-------------|
| Trace | 8080 | Observability backend (API + workers + UI) |
| Relay | 8081 | Offline-first gateway (optional) |
| PostgreSQL | 5432 | Trace backend database |

### Environment Variables

```bash
# Trace
TRACE_MODE=all              # all | api | worker
TRACE_QUEUE=postgres        # postgres | redis | memory
TRACE_DB_URL=postgres://... # PostgreSQL connection
TRACE_OBJECT_DIR=/data/objects
TRACE_OPEN_UI=true          # Enable web UI (default: true for self-hosted)
TRACE_ALLOW_PUBLIC_REGISTER=false
TRACE_OIDC_AUTO_JOIN=false
TRACE_SECRETS_KEY=...       # Encrypt channel/alert secrets
TRACE_TRUSTED_PROXIES=...   # Trusted proxy IPs for X-Forwarded-For

# Relay
RELAY_DATA_DIR=/data
RELAY_LOG_LEVEL=info
```

## Docker Compose

### Minimal (Trace only)

```yaml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: trace
      POSTGRES_USER: trace
      POSTGRES_PASSWORD: trace
    volumes:
      - pgdata:/var/lib/postgresql/data

  trace:
    image: laststate/trace:latest
    environment:
      TRACE_MODE: all
      TRACE_QUEUE: postgres
      TRACE_DB_URL: postgres://trace:trace@postgres:5432/trace?sslmode=disable
      TRACE_OBJECT_DIR: /data/objects
      TRACE_OPEN_UI: "true"
    volumes:
      - trace-data:/data
    ports:
      - "8080:8080"
    depends_on:
      postgres:
        condition: service_healthy

volumes:
  pgdata:
  trace-data:
```

### Full stack (Trace + Relay)

See `docker-compose.laststate.yml` for the complete stack.

## Production Checklist

1. **TLS** — Put Relay and Trace behind a reverse proxy with TLS
2. **Backups** — Run `./scripts/backup.sh` daily
3. **Monitoring** — Scrape `/metrics` with Prometheus
4. **Retention** — Tune per-project retention in Settings
5. **Auth** — Set `TRACE_ALLOW_PUBLIC_REGISTER=false`
6. **OIDC** — Configure OIDC provider for SSO
7. **SAML** — Configure SAML for enterprise SSO (experimental)
8. **Secrets** — Use `TRACE_SECRETS_KEY` for alert secrets encryption
9. **Proxies** — Set `TRACE_TRUSTED_PROXIES` behind a load balancer

## Scaling

For larger deployments:

```bash
# Run API and workers separately
TRACE_MODE=api trace        # Stateless HTTP
TRACE_MODE=worker trace     # Job consumers
TRACE_QUEUE=redis           # Use Redis for higher throughput
```

See [SCALE.md](./SCALE.md) for horizontal scaling details.

## Self-Hosted vs. SaaS

| Feature | Self-Hosted | SaaS |
|---------|-------------|------|
| Data location | Your infrastructure | LastState cloud |
| Cost | Free (infrastructure only) | Subscription |
| Updates | Manual | Automatic |
| Support | Community | Priority (Team+) |
| SLA | N/A | Yes (Enterprise) |

## Troubleshooting

### "Connection refused" on port 8080
```bash
./laststate status
# Check if postgres is healthy: docker compose ps
```

### "No bootstrap token"
```bash
./laststate bootstrap
# Or generate manually:
openssl rand -hex 24
```

### "Database migration failed"
```bash
./laststate down
./laststate up
# Or run migrations manually:
docker compose exec trace go run ./cmd/trace --migrate
```

## Support

- **Documentation**: https://laststate.dev/docs
- **GitHub Issues**: https://github.com/laststate/trace/issues
- **Community**: https://github.com/laststate/trace/discussions
