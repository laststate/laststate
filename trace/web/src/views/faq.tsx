import React from 'react'
import { Link } from 'react-router-dom'
import { BrandLogo } from '../Loading'
import './styles.css'

const faqs = [
  {
    q: "What is LastState?",
    a: "LastState is an open-source observability platform for embedded firmware and hardware fleets. It captures crash state across device reboots, collects it from any transport, and correlates issues across your fleet.",
  },
  {
    q: "What devices are supported?",
    a: "Latch supports Cortex-M (STM32, nRF, etc.), RISC-V (RV32/RV64), Xtensa (ESP32), and Linux signal capture. We maintain a hardware compatibility matrix with board-specific qualification reports.",
  },
  {
    q: "Is this free for hobbyists?",
    a: "Yes! The Free tier gives you 5 devices, 1k events/day, and 30-day retention at zero cost. No credit card required. The Hobbyist plan ($9/mo) adds 100 devices and 50k events/day.",
  },
  {
    q: "Can I self-host everything?",
    a: "Absolutely. LastState is designed for self-hosted deployment. Run the full stack with `docker compose up` or use the `laststate` CLI. Your data stays on your infrastructure.",
  },
  {
    q: "What is the LEP protocol?",
    a: "LEP (LastState Event Protocol) v1 is an open, versioned, transport-independent binary contract for firmware diagnostics. It defines the wire format for crash envelopes, identity, encryption, and transport framing.",
  },
  {
    q: "How does crash capture work?",
    a: "Latch captures a minimal snapshot of CPU context, breadcrumbs, metrics, and stack traces in retained RAM before a hard fault. After reboot, the snapshot is promoted to a durable spool and delivered via your preferred transport.",
  },
  {
    q: "Do I need a Relay?",
    a: "Relay is optional but recommended for production. It provides offline-first collection, LEP validation, local analysis, and reliable delivery to Trace. For simple setups, Latch can send directly via UART or TCP.",
  },
  {
    q: "What about security and compliance?",
    a: "Latch supports XChaCha20-Poly1305 envelope encryption, HKDF-SHA-256 key derivation, replay windows, and hardware-backed key contracts. Trace provides audit logs, SSO/SAML, and SOC 2 readiness. Enterprise plan includes SLA and on-premise deployment.",
  },
  {
    q: "How do I contribute?",
    a: "We welcome contributions! Start with `good first issue` labels on GitHub. Integration experience, board qualification reports, documentation fixes, and test vectors are all valuable. See CONTRIBUTING.md in each repo.",
  },
  {
    q: "What about support?",
    a: "Community support is available via GitHub Discussions. Team plan includes email support. Enterprise plan includes priority support with SLA guarantees.",
  },
]

export default function FAQPage() {
  return (
    <div className="faq-page">
      <Link to="/overview" className="page-back">← Dashboard</Link>
      <header className="faq-header">
        <div className="brand">
          <BrandLogo size={32} />
          <span>LastState</span>
        </div>
        <h1>Frequently Asked Questions</h1>
      </header>

      <div className="faq-list">
        {faqs.map((faq, i) => (
          <details key={i} className="faq-item">
            <summary>{faq.q}</summary>
            <p>{faq.a}</p>
          </details>
        ))}
      </div>

      <section className="faq-cta">
        <h2>Still have questions?</h2>
        <p className="meta">
          Open a discussion on GitHub or contact us at hello@laststate.dev
        </p>
        <a href="https://github.com/laststate/trace/discussions" className="btn primary">
          GitHub Discussions
        </a>
      </section>

      <footer className="faq-footer">
        <p className="meta">© 2026 LastState. Apache 2.0 & AGPL-3.0.</p>
      </footer>
    </div>
  )
}
