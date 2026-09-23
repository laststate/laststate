# Go-live checklist — from code-ready to first real charge

Single source of truth for everything that still needs a human with access.
Nothing here requires code changes; every item names its owner and how to
verify it. Until an item is done, the service fails closed by design.

## 1. Legal entity + inboxes (owner: founder)

- [ ] Registered company name + CNPJ + address → fill `[COMPANY_LEGAL_NAME]`,
      `[CNPJ]`, `[ADDRESS]`, `[JURISDICTION]`, `[FORUM]` on website
      `/terms` (search the repo for `a preencher`)
- [ ] Mailboxes receiving mail: `support@`, `sales@`, `legal@`,
      `privacy@`, `billing@` → fill `[SUPPORT_EMAIL]`, `[LEGAL_EMAIL]`,
      `[PRIVACY_EMAIL]` on `/terms`, `/refund`, `/dpa`
- [ ] Verify: send a test email to each; keep the DPA countersign flow in
      `/dpa` (countersign as-is or negotiate)

## 2. npm publish (owner: founder, 5 min)

- [ ] Create an **automation** token at npmjs.com (package `laststate-cli`,
      publish permission) → repo secret `NPM_TOKEN` on `laststate/cli`
      (the release workflow falls back to it; no code change needed)
- [ ] Re-push the tag: `git tag v1.2.0 <commit> && git push origin v1.2.0`
      (the old tag was deleted after the failed 404 run)
- [ ] Verify: `npm view laststate-cli version` prints `1.2.0`, then
      `npm i -g laststate-cli && laststate --help`

## 3. Stripe test mode (owner: founder)

- [ ] Dashboard (test mode ON): 4 prices — POC one-time $499, Pilot
      $499/mo, Fleet $1,999/mo, Enterprise $7,999/mo
- [ ] Webhook endpoint (test mode): `https://<billing-host>/webhooks/stripe`
      with `checkout.session.completed`, `invoice.paid`,
      `invoice.payment_failed`, `customer.subscription.deleted`
- [ ] Fill `.env` from `.env.example`: `STRIPE_KEY=sk_test_…`,
      `STRIPE_WEBHOOK_SECRET=whsec_…`, `BILLING_PRICE_*`, redirect URLs
- [ ] Run `docs/CHECKOUT.md` §2 end-to-end (4242 card, no real charge)

## 4. Stripe live (owner: founder, after §3 green)

- [ ] Business profile + ToS/refund URLs (`/terms`, `/refund`) submitted in
      the dashboard; activation approved
- [ ] Recreate the 4 prices in live mode; swap `sk_test_`/`whsec_(test)`
      for live values (never commit them)
- [ ] First real charge: POC $499 on a test org, then refund it via
      `POST /v1/invoices/{id}/refund` to prove the money loop both ways

## 5. Funnel sinks (owner: founder/ops)

- [ ] `WAITLIST_WEBHOOK_URL` set (Vercel env) — Discord/Slack/CRM endpoint;
      send a test signup and confirm it arrives
- [ ] PostHog + Sentry keys in prod; confirm `waitlist_submitted` events
- [ ] Decide invite SLA: who approves waitlist → Trace invite, and how fast

## 6. HIL → first POC (owner: founder + lab)

- [ ] Qualify STM32 Nucleo (closes `latch#17`): physical run + EVIDENCE.md +
      `qualification-matrix.json` entry
- [ ] Tag Latch v1.0.0 stable + Relay v1.0.0 per their gates
- [ ] Sell the first $499 POC through §3–§4 above — that day this file is done
