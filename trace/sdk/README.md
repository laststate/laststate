# Trace client SDKs

Minimal official-ish clients for **device Relay / CI** talking to Trace.

| Language | Path | Install |
|----------|------|---------|
| JavaScript | `sdk/js/trace.js` | ESM import / copy into firmware tooling |
| Python | `sdk/python/trace_client.py` | `PYTHONPATH=sdk/python` |

## Auth

- Header `Authorization: Bearer <ingest-token>`
- Optional `X-Project-ID: <uuid>` for multi-project tokens

## Endpoints covered

- `POST /v1/ingest` — raw LEP binary
- `POST /v1/events:batch`
- `POST /v1/artifacts`
- `POST /v1/relay/heartbeat`
- `GET /v1/relay/capabilities`

These are **not** full browser SDKs (no auto error capture). They exist so fleets that are not the C Relay can still speak Trace without reverse-engineering HTTP.
