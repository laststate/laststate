# Stack overview

Relay sits between Latch devices and Trace:

```
Latch → Relay sources → durable spool → destinations (Trace HTTP)
```

## Related repositories

| Repo | Role |
|------|------|
| [latch](https://github.com/laststate/latch) | Device SDK / LEP producer |
| [protocol](https://github.com/laststate/protocol) | LEP v1 specification |
| [trace](https://github.com/laststate/trace) | Cloud/self-hosted backend |

## Trace destination

Configure a destination of type `trace` with:

- `url`: Trace base URL (e.g. `http://127.0.0.1:8080`)
- `auth.type: bearer` and ingest token from Trace bootstrap

Relay uses:

- `GET /v1/relay/capabilities`
- `POST /v1/ingest` (and optional batch endpoints)

Idempotent success is HTTP **2xx** (including duplicates with the same payload hash).
Same `event_id` with a **different** payload is **422** `conflict` — not delivered.

See [trace-api.md](../spec/trace-api.md) and [latch-integration.md](latch-integration.md).
