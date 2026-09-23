# Entitlements

Feature access per subscription tier. When a subscription is activated or
upgraded, Billing Service calls the Trace Admin API to provision or deprovision
features for the organization.

> Scope: subscriptions only. One-time HIL qualification services
> (POC / Fleet / Enterprise HIL, see PRICING.md) are sold through the site
> and never change entitlements by themselves.

## Tier features

| Feature | Free | Hobbyist | Team | Enterprise |
|---------|------|----------|------|------------|
| Symbolication | Basic | Full | Full | Full |
| Device count | 5 | 100 | 1,000 | Unlimited |
| Events/day | 1,000 | 50,000 | 500,000 | Unlimited |
| Data retention | 30 days | 90 days | 1 year | Unlimited |
| API tokens | 1 | 5 | 20 | Unlimited |
| Alert rules | 0 | 5 | 50 | Unlimited |
| Analytics export | — | ✓ | ✓ | ✓ |
| Custom integrations | — | — | ✓ | ✓ |
| SSO (OIDC/SAML) | — | — | ✓ | ✓ |
| Audit logs | — | — | ✓ | ✓ |
| On-call management | — | — | ✓ | ✓ |
| Priority support | — | — | ✓ | ✓ |
| SLA | — | — | — | ✓ |
| On-prem deployment | — | — | — | ✓ |

## Provisioning flow

```
1. Payment webhook received → subscription activated
2. Billing calls Trace Admin API: POST /v1/entitlements/{org}/provision
   Body: {"tier": "team"}
3. Trace activates features for the organization
4. Billing records the provisioning in the local database
```

## Deprovisioning flow

```
1. Subscription canceled or expired
2. Billing calls Trace Admin API: POST /v1/entitlements/{org}/deprovision
   Body: {"tier": "free"}
3. Trace removes elevated features, downgrades to free tier
4. Billing records the deprovisioning in the local database
```

## Trial period

New organizations start on the free tier with a 14-day trial:

- Trial features match the free tier.
- Trial expires automatically unless a subscription is activated.
- On trial expiry without subscription, features are downgraded to free.
- Trial can be extended by admin: `POST /v1/trials/{org}/extend?days=14`

## Proration

Mid-cycle plan changes calculate proration:

```
proration = (days_remaining / total_days) × (new_price - old_price)
```

- Positive proration: customer is charged the difference.
- Negative proration: customer receives a credit.
- Proration is calculated at the time of the change and applied to the next
  invoice.

## Quota enforcement

Quotas are checked at runtime against the `tier_quotas` table:

```go
// Check if an organization has exceeded its device limit
quota, _ := db.GetTierQuota(tier)
if quota.MaxDevices > 0 && deviceCount >= quota.MaxDevices {
    return fmt.Errorf("device limit reached for tier %s", tier)
}
```

## API reference

### Provision features

```
POST /v1/entitlements/{org_id}/provision
Content-Type: application/json

{"tier": "team"}
```

Response: `200 OK`

### Deprovision features

```
POST /v1/entitlements/{org_id}/deprovision
Content-Type: application/json

{"tier": "free"}
```

Response: `200 OK`

### Get entitlements

```
GET /v1/entitlements/{org_id}
```

Response:

```json
{
  "tier": "team",
  "features": {
    "symbolication": true,
    "analytics_export": true,
    "custom_integrations": true,
    "sso": true,
    "audit_logs": true,
    "oncall": true
  },
  "quotas": {
    "max_devices": 1000,
    "max_events_per_day": 500000,
    "retention_days": 365,
    "max_api_tokens": 20,
    "max_alert_rules": 50
  }
}
```
