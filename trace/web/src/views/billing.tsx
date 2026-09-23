import React, { useCallback, useEffect, useState } from 'react'
import { api } from '../api'
import { Button } from '../components/ui/button'
import { Badge } from '../components/ui/badge'
import { Check, CreditCard, Globe, Lock, Sparkles } from '../icons'

interface Plan {
  name: string
  priceCents: number
  currency: string
  maxDevices: number
  maxEventsPerDay: number
  retentionDays: number
  maxApiTokens: number
  maxAlertRules: number
  features: string[]
  isUnlimitedDevices?: boolean
  isUnlimitedEvents?: boolean
  isUnlimitedRetention?: boolean
  id: string
}

interface Subscription {
  id: string
  planId: string
  status: string
  currentPeriodEnd: string
  provider: string
}

interface BillingData {
  subscription?: Subscription
  tiers: Plan[]
  usage?: { events: number; devices: number; retentionDays: number }
  deployment?: string
  billingEnabled?: boolean
}

export default function BillingView() {
  const [data, setData] = useState<BillingData | null>(null)
  const [loading, setLoading] = useState(true)
  const [checkoutUrl, setCheckoutUrl] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    ;(async () => {
      try {
        const [tiersRes, subRes, usageRes] = await Promise.all([
          api('/v1/billing/tiers').catch(() => ({ items: [], deployment: 'enterprise', billing_enabled: true })),
          api('/v1/billing/subscription').catch(() => ({})),
          api('/v1/usage').catch(() => ({})),
        ])
        setData({
          tiers: tiersRes.items || [],
          subscription: subRes,
          deployment: tiersRes.deployment,
          billingEnabled: tiersRes.billing_enabled,
          usage: {
            events: (usageRes.metrics || []).reduce((sum: number, m: any) => sum + (m.value || 0), 0),
            devices: usageRes.metrics?.find((m: any) => m.metric_name === 'devices')?.value || 0,
            retentionDays: 30,
          },
        })
      } catch (e: any) {
        setError(e.message)
      } finally {
        setLoading(false)
      }
    })()
  }, [])

  const handleUpgrade = useCallback(async (planId: string) => {
    try {
      const res = await api('/v1/billing/checkout', {
        method: 'POST',
        body: { plan_id: planId, provider: 'stripe' },
      })
      if (res.url) {
        window.location.href = res.url
      } else {
        setCheckoutUrl(res.url || null)
      }
    } catch (e: any) {
      setError(e.message)
    }
  }, [])

  const handleCancel = useCallback(async () => {
    if (!confirm('Cancel your subscription? You will be downgraded to Free at the end of the billing period.')) return
    try {
      await api('/v1/billing/subscription', { method: 'DELETE' })
      setData(d => d ? { ...d, subscription: undefined } : d)
    } catch (e: any) {
      setError(e.message)
    }
  }, [])

  const fmtPrice = (cents: number) => {
    if (cents === 0) return 'Free'
    return `$${(cents / 100).toFixed(0)}/mo`
  }

  const fmtCount = (n: number) => {
    if (n < 0) return '∞'
    return n.toLocaleString()
  }

  const fmtDays = (n: number) => {
    if (n < 0) return '∞'
    if (n >= 365) return `${Math.round(n / 365)} year${n >= 730 ? 's' : ''}`
    return `${n} days`
  }

  if (loading) {
    return <div className="panel"><p className="meta">Loading billing...</p></div>
  }

  if (error) {
    return <div className="panel" style={{ borderColor: 'hsl(0 84% 50%)' }}><p style={{ color: 'hsl(0 84% 50%)' }}>Error: {error}</p></div>
  }

  // Local deployment (self-hosted): everything unlocked, no paywall.
  if (data?.billingEnabled === false || data?.deployment === 'local') {
    const usage = data?.usage
    return (
      <div className="billing-view">
        <div className="panel" style={{ marginBottom: '1.5rem', borderColor: 'hsl(152 60% 45%)' }}>
          <div className="panel-head">
            <h2 style={{ margin: 0 }}>
              <Sparkles size={16} style={{ marginRight: 8 }} />
              Everything unlocked — self-hosted
            </h2>
            <Badge>local</Badge>
          </div>
          <p className="meta" style={{ marginTop: '0.75rem' }}>
            This server runs in local deployment mode. All features are enabled with no limits and no subscription required.
          </p>
          <div className="grid-3" style={{ marginTop: '1rem' }}>
            <div>
              <div className="meta">Events this period</div>
              <div style={{ fontSize: '1.5rem', fontWeight: 700 }}>{fmtCount(usage?.events || 0)}</div>
            </div>
            <div>
              <div className="meta">Devices</div>
              <div style={{ fontSize: '1.5rem', fontWeight: 700 }}>{fmtCount(usage?.devices || 0)}</div>
            </div>
            <div>
              <div className="meta">Quotas</div>
              <div style={{ fontWeight: 600 }}>Unlimited</div>
            </div>
          </div>
          <div style={{ marginTop: '1rem' }}>
            <ul style={{ paddingLeft: '1.25rem', fontSize: '0.85rem', display: 'grid', gap: '0.35rem' }}>
              <li><Check size={12} style={{ marginRight: 6, verticalAlign: 'middle' }} /> No mandatory authentication</li>
              <li><Check size={12} style={{ marginRight: 6, verticalAlign: 'middle' }} /> No event/device quotas</li>
              <li><Check size={12} style={{ marginRight: 6, verticalAlign: 'middle' }} /> No billing or payment required</li>
            </ul>
          </div>
        </div>

        <div className="panel">
          <h2 style={{ marginTop: 0 }}>Usage</h2>
          <div className="grid-2">
            <div>
              <div className="meta">Events this period</div>
              <div style={{ fontSize: '1.5rem', fontWeight: 700 }}>{fmtCount(usage?.events || 0)}</div>
              <div className="meta">unlimited</div>
            </div>
            <div>
              <div className="meta">Devices</div>
              <div style={{ fontSize: '1.5rem', fontWeight: 700 }}>{fmtCount(usage?.devices || 0)}</div>
              <div className="meta">unlimited</div>
            </div>
          </div>
        </div>

        <div className="panel" style={{ marginTop: '1.5rem' }}>
          <h2 style={{ marginTop: 0 }}>Billing</h2>
          <p className="meta">
            Billing is disabled in local deployment mode. Subscriptions, quotas and payment methods apply only to
            managed LastState (enterprise) deployments.
          </p>
        </div>
      </div>
    )
  }

  const currentPlan = data?.tiers.find(t => t.id === data?.subscription?.planId)
  const featuresMap: Record<string, string> = {
    symbolication: 'Crash symbolication',
    analytics_export: 'Analytics export',
    custom_alerts: 'Custom alert rules',
    custom_integrations: 'Custom integrations',
    oncall: 'On-call schedules',
    escalation: 'Escalation policies',
    audit_logs: 'Audit logs',
    sso: 'SSO / SAML',
    priority_support: 'Priority support',
    sla: 'SLA guarantee',
    on_prem: 'On-premise / air-gapped',
  }

  return (
    <div className="billing-view">
      {/* Current status */}
      {data?.subscription && data.subscription.status === 'active' && (
        <div className="panel" style={{ marginBottom: '1.5rem', borderColor: 'hsl(262 70% 50%)' }}>
          <div className="panel-head">
            <h2 style={{ margin: 0 }}>
              <Sparkles size={16} style={{ marginRight: 8 }} />
              Current Plan: {currentPlan?.name || data.subscription.planId}
            </h2>
            <Badge>{data.subscription.status}</Badge>
          </div>
          <div className="grid-3" style={{ marginTop: '1rem' }}>
            <div>
              <div className="meta">Billing period ends</div>
              <div style={{ fontWeight: 600 }}>{new Date(data.subscription.currentPeriodEnd).toLocaleDateString()}</div>
            </div>
            <div>
              <div className="meta">Provider</div>
              <div style={{ fontWeight: 600 }}>{data.subscription.provider === 'stripe' ? '💳 Card' : data.subscription.provider === 'mercado_pago' ? '🇧🇷 Pix/Card' : '₿ Crypto'}</div>
            </div>
            <div>
              <div className="meta">Usage this period</div>
              <div style={{ fontWeight: 600 }}>
                {fmtCount(data.usage?.events || 0)} events · {fmtCount(data.usage?.devices || 0)} devices
              </div>
            </div>
          </div>
          <div style={{ marginTop: '1rem' }}>
            <Button type="button" variant="secondary" onClick={handleCancel}>
              Cancel subscription
            </Button>
          </div>
        </div>
      )}

      {/* Usage metering */}
      <div className="panel" style={{ marginBottom: '1.5rem' }}>
        <h2 style={{ marginTop: 0 }}>Usage</h2>
        <div className="grid-2">
          <div>
            <div className="meta">Events this period</div>
            <div style={{ fontSize: '1.5rem', fontWeight: 700 }}>
              {fmtCount(data?.usage?.events || 0)}
            </div>
            {data?.subscription && currentPlan && (
              <div className="meta">
                of {fmtCount(currentPlan.maxEventsPerDay)} events/day limit
              </div>
            )}
          </div>
          <div>
            <div className="meta">Devices</div>
            <div style={{ fontSize: '1.5rem', fontWeight: 700 }}>
              {fmtCount(data?.usage?.devices || 0)}
            </div>
            {data?.subscription && currentPlan && (
              <div className="meta">
                of {fmtCount(currentPlan.maxDevices)} devices limit
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Plans comparison */}
      <h2 style={{ marginBottom: '1rem' }}>Choose a plan</h2>
      <div className="plans-grid">
        {(data?.tiers || []).map(tier => {
          const isCurrent = data?.subscription?.planId === tier.id && data?.subscription?.status === 'active'
          return (
            <div
              key={tier.id}
              className={`panel plan-card ${isCurrent ? 'plan-active' : ''}`}
              style={{
                borderColor: isCurrent ? 'hsl(262 70% 50%)' : 'hsl(0 0% 12%)',
                position: 'relative',
              }}
            >
              {isCurrent && (
                <div style={{
                  position: 'absolute', top: -12, left: '50%', transform: 'translateX(-50%)',
                  background: 'hsl(262 70% 50%)', color: 'white', padding: '2px 12px',
                  borderRadius: '10px', fontSize: '0.7rem', fontWeight: 600,
                }}>
                  CURRENT PLAN
                </div>
              )}
              <h3 style={{ marginTop: '0.5rem', fontSize: '1.25rem' }}>{tier.name}</h3>
              <div style={{ fontSize: '2rem', fontWeight: 700, margin: '0.5rem 0' }}>
                {fmtPrice(tier.priceCents)}
                {tier.priceCents > 0 && <span className="meta" style={{ fontSize: '0.85rem' }}>/mo</span>}
              </div>
              <ul style={{ paddingLeft: '1.25rem', margin: '1rem 0', fontSize: '0.85rem' }}>
                <li>{fmtCount(tier.maxDevices)} devices {tier.isUnlimitedDevices ? '(unlimited)' : ''}</li>
                <li>{fmtCount(tier.maxEventsPerDay)} events/day {tier.isUnlimitedEvents ? '(unlimited)' : ''}</li>
                <li>{fmtDays(tier.retentionDays)} retention {tier.isUnlimitedRetention ? '(unlimited)' : ''}</li>
                <li>{tier.maxApiTokens > 0 ? `${tier.maxApiTokens} API tokens` : '—'}</li>
                <li>{tier.maxAlertRules > 0 ? `${tier.maxAlertRules} alert rules` : '—'}</li>
              </ul>
              <hr style={{ border: 'none', borderTop: '1px solid hsl(0 0% 12%)', margin: '1rem 0' }} />
              <ul style={{ paddingLeft: '1.25rem', margin: '0.5rem 0', fontSize: '0.8rem' }}>
                {tier.features.map(f => (
                  <li key={f}>
                    <Check size={12} style={{ marginRight: 6, verticalAlign: 'middle' }} />
                    {featuresMap[f] || f.replace(/_/g, ' ')}
                  </li>
                ))}
              </ul>
              <div style={{ marginTop: '1.5rem' }}>
                {isCurrent ? (
                  <Button type="button" variant="secondary" fullWidth disabled>
                    Current plan
                  </Button>
                ) : data?.subscription?.status === 'active' ? (
                  <Button type="button" variant="default" fullWidth onClick={() => handleUpgrade(tier.id)}>
                    Upgrade to {tier.name}
                  </Button>
                ) : tier.id !== 'free' ? (
                  <Button type="button" variant="default" fullWidth onClick={() => handleUpgrade(tier.id)}>
                    Subscribe
                  </Button>
                ) : (
                  <Button type="button" variant="ghost" fullWidth disabled>
                    Free tier
                  </Button>
                )}
              </div>
            </div>
          )
        })}
      </div>

      {/* Payment methods */}
      <div className="panel" style={{ marginTop: '2rem' }}>
        <h2 style={{ marginTop: 0 }}>Payment methods</h2>
        <div className="grid-3">
          <div style={{ padding: '1rem', border: '1px solid hsl(0 0% 12%)', borderRadius: '0.5rem' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.5rem' }}>
              <CreditCard size={18} />
              <strong>Stripe</strong>
            </div>
            <p className="meta">Credit & debit cards. Global (USD, EUR, GBP).</p>
          </div>
          <div style={{ padding: '1rem', border: '1px solid hsl(0 0% 12%)', borderRadius: '0.5rem' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.5rem' }}>
              <Globe size={18} />
              <strong>Mercado Pago</strong>
            </div>
            <p className="meta">Pix, boleto, credit cards. Brazil & LatAm (BRL, USD).</p>
          </div>
          <div style={{ padding: '1rem', border: '1px solid hsl(0 0% 12%)', borderRadius: '0.5rem' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.5rem' }}>
              <Lock size={18} />
              <strong>Crypto</strong>
            </div>
            <p className="meta">Bitcoin, Ethereum, USDC via Coinbase Commerce.</p>
          </div>
        </div>
      </div>

      {/* FAQ */}
      <div className="panel" style={{ marginTop: '2rem' }}>
        <h2 style={{ marginTop: 0 }}>FAQ</h2>
        <details style={{ marginBottom: '0.75rem' }}>
          <summary style={{ cursor: 'pointer', fontWeight: 500 }}>What happens when I exceed my limits?</summary>
          <p className="meta" style={{ marginTop: '0.5rem' }}>
            When you exceed your daily event limit, new events are still ingested but not processed.
            When you exceed your device limit, new devices are registered but not tracked.
            You'll be notified via email and in the UI.
          </p>
        </details>
        <details style={{ marginBottom: '0.75rem' }}>
          <summary style={{ cursor: 'pointer', fontWeight: 500 }}>Can I downgrade mid-cycle?</summary>
          <p className="meta" style={{ marginTop: '0.5rem' }}>
            Yes. Downgrades take effect at the end of the current billing period.
            You'll keep access to your current tier until then.
          </p>
        </details>
        <details style={{ marginBottom: '0.75rem' }}>
          <summary style={{ cursor: 'pointer', fontWeight: 500 }}>Is there a free trial?</summary>
          <p className="meta" style={{ marginTop: '0.5rem' }}>
            New organizations get a 14-day trial on the Team plan. No credit card required.
          </p>
        </details>
        <details>
          <summary style={{ cursor: 'pointer', fontWeight: 500 }}>What is the Team trial?</summary>
          <p className="meta" style={{ marginTop: '0.5rem' }}>
            The Team plan trial gives you full access to SSO, on-call, audit logs, and 20 API tokens for 14 days.
          </p>
        </details>
      </div>
    </div>
  )
}
