# LastState Billing — Pricing & Plans

## Overview

Four tiers, one vertical (AUV/Industrial), 12-month horizon. You pay for
qualified HIL, SLA and the billing service — never for closing code.
Self-hosting stays free and unlimited (see `LICENSING.md`).

## Plans

| Tier | Price | Devices | Boards qualified | Retention | SLA | License |
|------|-------|---------|------------------|-----------|-----|---------|
| **Local** | **$0** (self-host) | unlimited | 5 (self) | unlimited | community | AGPL auditable — `docker compose -f docker-compose.appliance.yml up` |
| **Pilot** | **$499 / month** | up to 100 | 1 | 90 days | SLO free month* | AGPL auditable + signed updates |
| **Fleet** | **$1,999 / month** | up to 1,000 | 3 | 1 year | 99.5% ingest | AGPL auditable + ELF/DWARF symbolication + postmortem LLM |
| **Enterprise** | **$7,999 / month** | unlimited | 5 (signed matrix) | unlimited | 99.9% + credits | Commercial Trace (no §13) + indemnity |

* **SLO "crash reproduzido":** if a HardFault/panic reproduced in lab on a
qualified board is not promoted (`latch-dump` without envelope), the month
is free.

**Paid POC:** 30 days, $499, 1 board: `hil/esp32_relay` on the customer
bench. No durable ACK in 1 week → refund.

## Pricing Details

### Local ($0)
Evaluate and run everything self-hosted: full crash reporting,
symbolication and issue tracking. No card, no quotas.

### Pilot ($499/mo)
One qualified board in light production: 100 devices, 90-day retention,
signed updates and the crash-reproduced SLO above.

### Fleet ($1,999/mo)
Controlled rollout: 1,000 devices, 3 qualified boards, 1-year retention,
99.5% ingest SLA, full symbolication and postmortem analysis.

### Enterprise ($7,999/mo)
Unlimited scale on a 5-board signed matrix: unlimited retention, 99.9% SLA
with credits, commercial Trace license (no AGPL §13), indemnity, FAE and
on-site workshop.
## Payment Methods

- **Stripe** — Global credit/debit cards (USD, EUR, GBP)
- **Mercado Pago** — Pix, boleto, credit cards (BRL, USD)
- **Crypto** — Bitcoin, Ethereum, USDC via Coinbase Commerce / NOWPayments

## Self-Hosted

All plans include self-hosted deployment. Run the full stack locally:

```bash
docker compose up --build
```

Or use the `laststate` CLI:

```bash
laststate init
laststate up
```

## Trial

New organizations start on the free tier with a 14-day trial (see
ENTITLEMENTS.md for trial scope). No credit card required.

## Upgrades & Downgrades

- **Upgrade**: Immediate effect with proration. You'll be charged the prorated difference.
- **Downgrade**: Takes effect at the end of the current billing period.
- **Cancel**: At any time. Access continues until the end of the billing period.

## HIL qualification services (one-time, per board)

Separate from subscriptions: paid hardware qualification sold through the
site (`/design-partner`), not metered or enforced by billing-service.
Buying a service never changes entitlements by itself — cloud quotas still
come from the subscription tier above.

| Service | Price (USD, one-time) | Scope |
|---------|----------------------|-------|
| **POC** | $499 | 1 board, 30 days, HIL rig + EVIDENCE.md + case study |
| **Fleet** | $1,999 | 3 boards, 90 days, partial matrix, SLO month free |
| **Enterprise HIL** | Custom | 5-board signed matrix, FAE + on-site workshop, commercial Trace license |

## API Reference

See the [billing API docs](./API.md) for the complete endpoint reference.

## Contact

For custom pricing or enterprise inquiries, contact us at billing@laststate.dev.
