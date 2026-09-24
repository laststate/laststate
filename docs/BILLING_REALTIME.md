# Billing realtime (stack-wide)

`billing-service` is the source of truth. Everything else is connected live —
no nightly sync, no manual tier edits.

```text
billing-service (REST + webhooks v2 + SSE stream :8081 locally)
  ├─push→ trace        POST /v1/billing/webhook (HMAC) + admin entitlements API
  ├─tail← relay        cached GET /v1/entitlements + POST /v1/usage every 60s
  ├─tail← simulator    POST /v1/usage per minute (idempotent hour keys)
  ├─use← CLI           laststate billing status|checkout|portal|watch
  └─use← website       POST /api/billing/checkout, POST /api/billing/webhook
```

## Bring-up

```bash
cp .env.example .env
# fill BILLING_API_KEY, BILLING_ORG_ID, TRACE_BILLING_HMAC_SECRET + provider keys
docker compose up -d
curl -N 'http://localhost:8081/v1/billing/events/stream' -H "Authorization: Bearer $BILLING_API_KEY"
```

## Register receivers

```bash
# trace
curl -X POST localhost:8081/v1/webhook-endpoints \
  -H "Authorization: Bearer $BILLING_API_KEY" -H 'Content-Type: application/json' -d "{
    \"organization_id\": \"$BILLING_ORG_ID\",
    \"url\": \"http://trace:8080/v1/billing/webhook\",
    \"secret\": \"$TRACE_BILLING_HMAC_SECRET\", \"events\": []
  }"
```

Per-component details live next to the code: `billing-service/docs/REALTIME.md`,
`trace/docs/BILLING_REALTIME.md`, `relay/docs/BILLING_REALTIME.md`,
`simulator/docs/BILLING_REALTIME.md`, `cli/docs/BILLING_REALTIME.md`,
`website/docs/BILLING_REALTIME.md`, `latch/docs/BILLING_REALTIME.md`,
`protocol/spec/lep-billing-events.md`, `developer-platform/docs/BILLING_REALTIME.md`.
