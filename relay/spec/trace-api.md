# Trace ingestion contract used by Relay

## Capabilities

`GET /v1/relay/capabilities`

A `404` or `405` means legacy single-event ingestion. A successful response can advertise API version, LEP versions, event-size limits, batch support, compression, and artifact support.

## Single event

`POST /v1/ingest`

Headers:

```text
Content-Type: application/octet-stream
Idempotency-Key: evt_<stable-id>
X-Last-State-Event-ID: evt_<stable-id>
Authorization: Bearer <token>   # when configured
```

### Status codes (single event)

| Status | Meaning | Relay action |
|--------|---------|--------------|
| `2xx` | Accepted or **true duplicate** (same `event_id` + same payload hash). Body may include `"duplicate": true` / `"status":"duplicate"`. | Mark delivered |
| `422` + `error.code=conflict` | Same `event_id`, **different** payload hash | Permanent failure (do not ACK as delivered) |
| `409` | Legacy / ambiguous. Success **only** if body marks duplicate; otherwise treat as conflict (permanent) | Inspect body |
| `408`, `425`, `429`, `5xx` | Temporary | Retry |
| `401`, `403`, `413`, `422` (other codes) | Operator or payload | Permanent / fix config |

Duplicates must use **2xx**, not 409. Conflicts must use **422** with `error.code: "conflict"`.


## Batch ingestion

`POST /v1/events:batch` accepts JSON matching `internal/traceapi.BatchRequest`.
Binary event payloads are base64-encoded by standard JSON encoding. The response
contains independent `accepted`, `duplicates`, and `rejected` arrays. A rejected
item includes `retryable`, allowing Relay to retry only temporary failures.

Servers advertise batch support and limits through `GET /v1/relay/capabilities`.
Relay falls back to `POST /v1/ingest` when the endpoint is absent or disabled.
