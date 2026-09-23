# Trace agents

Scoped instructions for AI assistants that operate on specific subsystems.
Read the top-level [AGENT.md](AGENT.md) first.

## Agents

| Agent | Scope | Instructions |
|-------|-------|--------------|
| API | `internal/api/` | HTTP handlers, auth, middleware, OpenAPI |
| Worker | `internal/worker/` | Event processing pipeline |
| Analytics | `internal/analysis/` | Crash analysis, symbolication, anomaly detection |
| Store | `internal/store/` | Database layer, migrations |
| Queue | `internal/queue/` | Message queue backends |
| Billing | `internal/billing/` | Plans, quotas, Stripe integration |
| Webhook | `internal/webhook/` | Outbound webhook delivery |
| Docs | `docs/`, `web/` | Documentation and web UI |

Each agent directory contains its own `AGENTS.md` with detailed instructions.
Read the relevant one before editing files in that scope.
