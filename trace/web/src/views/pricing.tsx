import React from 'react'
import { Link } from 'react-router-dom'
import { BrandLogo } from '../Loading'
import './styles.css'

export default function PricingPage() {
  const plans = [
    {
      id: 'free', name: 'Free', price: '$0', period: '/mo',
      devices: '5', events: '1k/day', retention: '30 days',
      tokens: '1', alerts: '—',
      features: ['Symbolication', 'Basic crash capture'],
      highlight: false,
    },
    {
      id: 'hobbyist', name: 'Hobbyist', price: '$9', period: '/mo',
      devices: '100', events: '50k/day', retention: '90 days',
      tokens: '5', alerts: '5',
      features: [
        'Everything in Free',
        'Custom alert rules',
        'Analytics export',
        'Breadcrumb replay',
      ],
      highlight: true,
    },
    {
      id: 'team', name: 'Team', price: '$49', period: '/mo',
      devices: '1k', events: '500k/day', retention: '1 year',
      tokens: '20', alerts: '50',
      features: [
        'Everything in Hobbyist',
        'SSO / SAML',
        'On-call schedules',
        'Escalation policies',
        'Audit logs',
        'Custom integrations',
      ],
      highlight: false,
    },
    {
      id: 'enterprise', name: 'Enterprise', price: '$199', period: '/mo',
      devices: '∞', events: '∞', retention: '∞',
      tokens: '∞', alerts: '∞',
      features: [
        'Everything in Team',
        'On-premise / air-gapped',
        'SLA guarantee',
        'Priority support',
        'Custom integrations',
        'Dedicated account manager',
      ],
      highlight: false,
    },
  ]

  return (
    <div className="pricing-page">
      <Link to="/overview" className="page-back">← Dashboard</Link>
      {/* Header */}
      <header className="pricing-header">
        <div className="brand">
          <BrandLogo size={32} />
          <span>LastState</span>
        </div>
        <h1>Pricing</h1>
        <p className="meta">Start free. Scale when you need it. All plans include self-hosted deployment.</p>
      </header>

      {/* Plans */}
      <div className="pricing-grid">
        {plans.map(plan => (
          <div
            key={plan.id}
            className={`pricing-card ${plan.highlight ? 'pricing-highlight' : ''}`}
          >
            {plan.highlight && <div className="pricing-badge">Most popular</div>}
            <h2>{plan.name}</h2>
            <div className="pricing-price">
              {plan.price}<span className="pricing-period">{plan.period}</span>
            </div>
            <ul className="pricing-features">
              <li><strong>{plan.devices}</strong> devices</li>
              <li><strong>{plan.events}</strong> events/day</li>
              <li><strong>{plan.retention}</strong> retention</li>
              <li><strong>{plan.tokens}</strong> API tokens</li>
              <li><strong>{plan.alerts}</strong> alert rules</li>
            </ul>
            <hr />
            <ul className="pricing-includes">
              {plan.features.map(f => (
                <li key={f}>✓ {f}</li>
              ))}
            </ul>
            <a href="/billing" className="btn primary" style={{ width: '100%', textAlign: 'center' }}>
              {plan.id === 'free' ? 'Get started' : 'Start trial'}
            </a>
          </div>
        ))}
      </div>

      {/* FAQ */}
      <section className="pricing-faq">
        <h2>Frequently asked questions</h2>
        <div className="faq-list">
          <details>
            <summary>What happens when I exceed my limits?</summary>
            <p>New events are still ingested but not processed. New devices are registered but not tracked. You'll be notified via email and in the UI.</p>
          </details>
          <details>
            <summary>Can I downgrade mid-cycle?</summary>
            <p>Yes. Downgrades take effect at the end of the current billing period. You'll keep access to your current tier until then.</p>
          </details>
          <details>
            <summary>Is there a free trial?</summary>
            <p>New organizations get a 14-day trial on the Team plan. No credit card required.</p>
          </details>
          <details>
            <summary>What payment methods do you accept?</summary>
            <p>Stripe (credit/debit cards), Mercado Pago (Pix, boleto, credit cards), and Crypto (Bitcoin, Ethereum, USDC).</p>
          </details>
          <details>
            <summary>Can I self-host for free?</summary>
            <p>Yes! All plans include self-hosted deployment. The Free plan gives you 5 devices and 1k events/day at zero cost.</p>
          </details>
          <details>
            <summary>What is on-premise deployment?</summary>
            <p>Enterprise plan supports air-gapped, on-premise deployment where all data stays within your infrastructure. No cloud connectivity required.</p>
          </details>
        </div>
      </section>

      {/* CTA */}
      <section className="pricing-cta">
        <h2>Ready to get started?</h2>
        <p className="meta">Self-host in 60 seconds or start a 14-day trial on the cloud.</p>
        <div className="pricing-actions">
          <a href="/docs" className="btn primary">Read the docs</a>
          <a href="/billing" className="btn secondary">View billing</a>
        </div>
      </section>

      {/* Footer */}
      <footer className="pricing-footer">
        <p className="meta">© 2026 LastState. Apache 2.0 & AGPL-3.0.</p>
      </footer>
    </div>
  )
}
