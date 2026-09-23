import React from 'react'
import { Link } from 'react-router-dom'
import { BrandLogo } from '../Loading'
import './styles.css'

export default function LandingPage() {
  return (
    <div className="landing-page">
      <Link to="/overview" className="page-back">← Dashboard</Link>
      {/* Hero */}
      <section className="landing-hero">
        <div className="landing-hero-content">
          <div className="landing-brand">
            <BrandLogo size={40} />
            <span className="landing-brand-text">LastState</span>
          </div>
          <h1 className="landing-hero-title">
            Embedded firmware observability
            <br />
            <span className="landing-gradient">that survives the crash.</span>
          </h1>
          <p className="landing-hero-sub">
            Latch captures crash state across reboots. Relay collects from any transport.
            Trace analyzes and correlates. All open source. All self-hosted.
          </p>
          <div className="landing-hero-actions">
            <a href="/pricing" className="btn primary">View pricing</a>
            <a href="/docs" className="btn secondary">Documentation</a>
            <a href="https://github.com/laststate" className="btn ghost" target="_blank" rel="noreferrer">
              GitHub →
            </a>
          </div>
          <div className="landing-hero-meta">
            <span className="tag">Apache 2.0</span>
            <span className="tag">Self-hosted</span>
            <span className="tag">14-day free trial</span>
          </div>
        </div>
        <div className="landing-hero-visual">
          <pre className="landing-code-block">
{`Latch (device) → Relay → Trace
  ↓           ↓       ↓
 Crash       Collect  Analyze
 Capture     Store    Correlate
              ACK      Alert`}
          </pre>
        </div>
      </section>

      {/* Features */}
      <section className="landing-section">
        <h2>Why LastState?</h2>
        <div className="landing-features">
          <div className="landing-feature">
            <h3>🔒 Crash state survives reboot</h3>
            <p>Latch captures CPU context, breadcrumbs, metrics, and stack traces in a retained snapshot before the device resets. No heap allocation. No scheduler dependency. Pure C11.</p>
          </div>
          <div className="landing-feature">
            <h3>📡 Any transport, any MCU</h3>
            <p>Relay accepts LEP envelopes over serial, TCP, HTTP, MQTT, BLE, LoRa, CAN, or files. Cortex-M, ESP32, RISC-V, Xtensa, and more. Open protocol — no vendor lock-in.</p>
          </div>
          <div className="landing-feature">
            <h3>🔍 Symbolication out of the box</h3>
            <p>Trace automatically matches ELF files, resolves DWARF symbols, and presents readable stack traces. Suspect commit detection links crashes to code changes.</p>
          </div>
          <div className="landing-feature">
            <h3>🏠 Self-hosted, always</h3>
            <p>Run the full stack with <code>docker compose up</code>. Your data stays on your infrastructure. No cloud dependency. No data egress. Air-gapped deployments supported.</p>
          </div>
          <div className="landing-feature">
            <h3>🌐 Open protocol, open ecosystem</h3>
            <p>LEP v1 is an open, versioned, transport-independent binary contract. Third-party implementations welcome. Community-maintained board qualification reports.</p>
          </div>
          <div className="landing-feature">
            <h3>💰 Free for hobbyists</h3>
            <p>Start with 5 devices and 1k events/day for free. Upgrade when your fleet grows. No credit card required for the 14-day trial.</p>
          </div>
        </div>
      </section>

      {/* Stack */}
      <section className="landing-section landing-stack">
        <h2>The stack</h2>
        <div className="landing-stack-grid">
          <div className="landing-stack-card">
            <h3>Protocol (LEP v1)</h3>
            <p>Binary, versioned, transport-independent contract for firmware diagnostics. Apache 2.0.</p>
            <a href="https://github.com/laststate/protocol" target="_blank" rel="noreferrer">View repo →</a>
          </div>
          <div className="landing-stack-card">
            <h3>Latch</h3>
            <p>Heap-free C11 + Rust no_std SDK. Captures fault state across reboots. Apache 2.0.</p>
            <a href="https://github.com/laststate/latch" target="_blank" rel="noreferrer">View repo →</a>
          </div>
          <div className="landing-stack-card">
            <h3>Relay</h3>
            <p>Offline-first gateway. Collects from devices, validates LEP, forwards to Trace. Apache 2.0.</p>
            <a href="https://github.com/laststate/relay" target="_blank" rel="noreferrer">View repo →</a>
          </div>
          <div className="landing-stack-card landing-stack-highlight">
            <h3>Trace</h3>
            <p>Observability backend. Ingest, analyze, dashboard, alerts. AGPL-3.0.</p>
            <a href="https://github.com/laststate/trace" target="_blank" rel="noreferrer">View repo →</a>
          </div>
        </div>
      </section>

      {/* Pricing preview */}
      <section className="landing-section">
        <h2>Simple pricing</h2>
        <p className="meta">Start free. Scale when you need it. All plans include self-hosted deployment.</p>
        <div className="landing-pricing">
          <div className="landing-price-card">
            <h3>Free</h3>
            <div className="landing-price">$0<span className="landing-price-period">/mo</span></div>
            <ul>
              <li>5 devices</li>
              <li>1k events/day</li>
              <li>30-day retention</li>
              <li>Symbolication</li>
            </ul>
            <a href="/pricing" className="btn secondary">Get started</a>
          </div>
          <div className="landing-price-card landing-price-highlight">
            <div className="landing-price-badge">Most popular</div>
            <h3>Hobbyist</h3>
            <div className="landing-price">$9<span className="landing-price-period">/mo</span></div>
            <ul>
              <li>100 devices</li>
              <li>50k events/day</li>
              <li>90-day retention</li>
              <li>Custom alerts</li>
              <li>Analytics export</li>
              <li>5 API tokens</li>
            </ul>
            <a href="/pricing" className="btn primary">Start trial</a>
          </div>
          <div className="landing-price-card">
            <h3>Team</h3>
            <div className="landing-price">$49<span className="landing-price-period">/mo</span></div>
            <ul>
              <li>1k devices</li>
              <li>500k events/day</li>
              <li>1-year retention</li>
              <li>SSO / SAML</li>
              <li>On-call schedules</li>
              <li>Audit logs</li>
              <li>20 API tokens</li>
            </ul>
            <a href="/pricing" className="btn secondary">Start trial</a>
          </div>
          <div className="landing-price-card">
            <h3>Enterprise</h3>
            <div className="landing-price">$199<span className="landing-price-period">/mo</span></div>
            <ul>
              <li>Unlimited everything</li>
              <li>On-premise / air-gapped</li>
              <li>SLA guarantee</li>
              <li>Priority support</li>
              <li>Custom integrations</li>
            </ul>
            <a href="/pricing" className="btn secondary">Contact sales</a>
          </div>
        </div>
      </section>

      {/* CTA */}
      <section className="landing-section landing-cta">
        <h2>Start debugging in minutes</h2>
        <pre className="landing-code-block landing-code-cta">
{`# Self-hosted in 60 seconds
docker compose up --build

# Or with the CLI
laststate init
laststate up`}
        </pre>
        <div className="landing-hero-actions">
          <a href="/docs" className="btn primary">Read the docs</a>
          <a href="/pricing" className="btn secondary">View pricing</a>
        </div>
      </section>

      {/* Footer */}
      <footer className="landing-footer">
        <div className="landing-footer-content">
          <div>
            <div className="landing-brand">
              <BrandLogo size={24} />
              <span>LastState</span>
            </div>
            <p className="meta">Embedded firmware observability for everyone.</p>
          </div>
          <div className="landing-footer-links">
            <div>
              <h4>Product</h4>
              <a href="/pricing">Pricing</a>
              <a href="/docs">Documentation</a>
              <a href="/changelog">Changelog</a>
            </div>
            <div>
              <h4>Community</h4>
              <a href="https://github.com/laststate" target="_blank" rel="noreferrer">GitHub</a>
              <a href="https://github.com/laststate/protocol" target="_blank" rel="noreferrer">Protocol</a>
              <a href="https://github.com/laststate/latch" target="_blank" rel="noreferrer">Latch</a>
              <a href="https://github.com/laststate/relay" target="_blank" rel="noreferrer">Relay</a>
              <a href="https://github.com/laststate/trace" target="_blank" rel="noreferrer">Trace</a>
            </div>
            <div>
              <h4>Legal</h4>
              <a href="/privacy">Privacy</a>
              <a href="/terms">Terms</a>
              <a href="/security">Security</a>
            </div>
          </div>
        </div>
        <div className="landing-footer-bottom">
          <p className="meta">© 2026 LastState. Apache 2.0 & AGPL-3.0.</p>
        </div>
      </footer>
    </div>
  )
}
