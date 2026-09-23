# Trace Architecture

Detailed architecture documentation for Trace, the crash analytics and
observability backend.

## System overview

```
┌───────────┐     ┌───────────┐     ┌─────────────────────────────────┐
│   Latch   │────▶│   Relay   │────▶│              Trace               │
│ (device)  │     │ (gateway) │     │  ┌───────────┐  ┌─────────────┐  │
│  C11/Rust │     │   Go      │     │  │ API       │  │ Web UI      │  │
└───────────┘     └───────────┘     │  │ Server    │  │ (React SPA) │  │
                          ┌─────────┤  │           │  └─────────────┘  │
                          │         │  │ Workers   │         │           │
                          ▼         │  │ Pipeline  │         ▼           │
                    PostgreSQL    │  │           │    ┌─────────────┐  │
                    (events)      │  │           │    │ Analytics   │  │
                    S3/MinIO      │  └───────────┘    │ Engine      │  │
                    (artifacts)   │                   └─────────────┘  │
                                  │                   ┌─────────────┐  │
                                  │                   │ Webhook     │  │
                                  │                   │ Delivery    │  │
                                  │                   └─────────────┘  │
                                  └────────────────────────────────────┘
```

## Components

### API Server

The API server handles all HTTP requests. It is stateless and can be
horizontally scaled.

- **Endpoints**: REST API for crash data, analytics, billing, and administration.
- **Auth**: Bearer tokens with scoped permissions. OIDC and SAML SSO supported.
- **Rate limiting**: Per-IP and per-token rate limits.
- **CORS**: Configurable allowed origins.
- **OpenAPI**: Full API specification generated from code.

### Worker Pipeline

Workers consume events from the queue and run analysis pipelines.

1. **Ingest** — Validate and normalize incoming crash data.
2. **Symbolicate** — Resolve addresses to symbols using LLVM or addr2line.
3. **Analyze** — Detect anomaly patterns, device DNA, health scores.
4. **Store** — Persist results to PostgreSQL and artifacts to S3.
5. **Notify** — Send alerts via webhooks (PagerDuty, Slack, Discord).

### Analytics Engine

- **Anomaly detection** — Statistical analysis of crash patterns.
- **Device DNA** — Fingerprint devices based on crash signatures.
- **Health scoring** — Aggregate health metrics per fleet.
- **Trending** — Identify rising and falling crash rates.

### Queue System

Supports multiple backend implementations:

| Backend | Use case | Throughput |
|---------|----------|------------|
| `memory` | Development, testing | Low |
| `postgres` | Single-node production | Medium |
| `redis` | Multi-node production | High |
| `nats` | High-throughput production | Very high |

### Object Storage

Crash dumps and symbol files are stored in S3-compatible object storage:

- **S3** (AWS)
- **MinIO** (self-hosted)
- **Local filesystem** (development)

### Database

PostgreSQL is the primary data store:

- Events and metadata
- User accounts and authentication
- Billing and subscription data
- Alert rules and on-call schedules
- Export history

## Data flow

```
Device ──▶ Latch (capture) ──▶ LEP encode ──▶ Relay (ingest)
                                                      │
                                                      ▼
                                                Trace API
                                                      │
                                    ┌─────────────────┼─────────────────┐
                                    ▼                 ▼                 ▼
                              Analyze          Store            Notify
                                    │                 │                 │
                                    ▼                 ▼                 ▼
                              PostgreSQL      S3/MinIO          Webhooks
```

## Security model

- **Transport**: TLS 1.2+ for all external communication.
- **Auth**: Scoped bearer tokens with constant-time comparison.
- **Encryption**: AES-GCM for sensitive data at rest (channels, alerts).
- **SSR protection**: Outbound webhooks validate URLs.
- **Audit**: All admin actions logged with timestamps and actor identity.

## Scaling

### Horizontal scaling

Run API and workers separately:

```bash
# Stateless API (scale horizontally)
TRACE_MODE=api TRACE_QUEUE=redis trace

# Workers (scale independently)
TRACE_MODE=worker TRACE_QUEUE=redis trace
```

### Vertical scaling

For single-node deployments:

```bash
TRACE_MODE=all trace
```

### Database scaling

- **Read replicas**: API can read from replicas.
- **Connection pooling**: Use PgBouncer for connection management.
- **Partitioning**: Consider partitioning events by date for large datasets.

## Deployment topologies

### Single-node (development/staging)

```
Trace (all-in-one) + PostgreSQL + MinIO
```

### Multi-node (production)

```
Trace API (n nodes) + Workers (m nodes) + PostgreSQL (primary + replica) + Redis + MinIO
```

### Air-gapped

All services run on internal infrastructure. No cloud dependencies.

## Configuration

All configuration is via environment variables. See `.env.example` for the
complete reference. Key sections:

- **Core**: Database, server port, log level
- **Auth**: JWT secret, OIDC, SAML
- **Queue**: Backend selection, connection strings
- **Storage**: S3/MinIO configuration
- **Features**: Chaos engineering, public API, soundboard, DNA detection
- **Billing**: Stripe, Mercado Pago, Crypto gateway keys
- **Monitoring**: ClickHouse, BigQuery, metrics

## File structure

```
trace/
├── cmd/trace/main.go          # Entry point
├── internal/
│   ├── api/                   # HTTP handlers, middleware, auth
│   ├── worker/                # Event processing pipeline
│   ├── analysis/              # Crash analysis engine
│   ├── analytics/             # Analytics warehouse (ClickHouse)
│   ├── anomaly/               # Anomaly detection
│   ├── billing/               # Plan and quota management
│   ├── config/                # Configuration loading
│   ├── db/                    # Database layer + migrations
│   ├── decode/                # LEP decoding
│   ├── dna/                   # Device fingerprinting
│   ├── fingerprint/           # Crash fingerprinting
│   ├── gc/                    # Garbage collection
│   ├── health/                # Health scoring
│   ├── lep/                   # LEP protocol implementation
│   ├── mailer/                # Email notifications
│   ├── metrics/               # Prometheus metrics
│   ├── notify/                # Alert delivery (PagerDuty, Slack)
│   ├── objects/               # Object storage (S3)
│   ├── oidc/                  # OIDC authentication
│   ├── postmortem/            # Postmortem generation
│   ├── pr/                    # PR creation integration
│   ├── protocol/              # LEP protocol
│   ├── public/                # Public API
│   ├── queue/                 # Message queue backends
│   ├── region/                # Regional routing
│   ├── saml/                  # SAML authentication
│   ├── secretbox/             # Encryption
│   ├── soundboard/            # Soundboard features
│   ├── store/                 # Database operations
│   ├── symbolicate/           # Symbol resolution
│   ├── tracectx/              # Trace context
│   └── webhook/               # Outbound webhooks
├── web/                       # React SPA
│   ├── src/                   # Source code
│   ├── public/                # Static assets
│   └── e2e/                   # End-to-end tests
├── sdk/                       # Client SDKs
│   ├── js/                    # JavaScript SDK
│   └── python/                # Python SDK
├── deploy/helm/trace/         # Helm charts
├── docs/                      # Documentation
├── scripts/                   # Utility scripts
└── .env.example               # Environment variables reference
```
