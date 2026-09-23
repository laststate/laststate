# Billing Service agents

Scoped instructions for AI assistants that operate on specific subsystems.
Read the top-level [AGENT.md](AGENT.md) first.

## Agents

| Agent | Scope | Instructions |
|-------|-------|--------------|
| API | `internal/api/` | HTTP handlers, auth, rate limiting, CORS |
| Billing | `internal/billing/` | Subscriptions, invoices, proration, dunning |
| Entitlement | `internal/entitlement/` | Feature provisioning via Trace |
| Store | `internal/store/` | PostgreSQL data access |
| Webhook | `internal/webhook/` | Provider webhook processing |
| Schema | `internal/schema/` | Database schema and migrations |
| Docs | `docs/` | API reference, architecture, webhooks |

Each agent directory contains its own `AGENTS.md` with detailed instructions.
Read the relevant one before editing files in that scope.
