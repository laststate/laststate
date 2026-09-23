import React from 'react'
import { Link } from 'react-router-dom'
import { BrandLogo } from '../Loading'
import './styles.css'

export default function DocsPage() {
  const sections = [
    {
      title: "Getting Started",
      items: [
        { label: "Installation", href: "/docs/install" },
        { label: "Quick Start", href: "/docs/quickstart" },
        { label: "Configuration", href: "/docs/config" },
        { label: "Self-Hosting", href: "/docs/self-hosting" },
      ],
    },
    {
      title: "Protocol",
      items: [
        { label: "LEP v1 Specification", href: "/docs/protocol/lep-v1" },
        { label: "TLV Types", href: "/docs/protocol/tlv-types" },
        { label: "Framing", href: "/docs/protocol/framing" },
        { label: "Encryption", href: "/docs/protocol/encryption" },
        { label: "Transports", href: "/docs/protocol/transports" },
      ],
    },
    {
      title: "Latch (Device SDK)",
      items: [
        { label: "Integration Guide", href: "/docs/latch/integration" },
        { label: "API Reference", href: "/docs/latch/api" },
        { label: "Platform Support", href: "/docs/latch/platforms" },
        { label: "Hardware Compatibility", href: "/docs/latch/compatibility" },
        { label: "Rust SDK", href: "/docs/latch/rust" },
      ],
    },
    {
      title: "Relay (Gateway)",
      items: [
        { label: "Setup Guide", href: "/docs/relay/setup" },
        { label: "Sources", href: "/docs/relay/sources" },
        { label: "Delivery", href: "/docs/relay/delivery" },
        { label: "CLI Reference", href: "/docs/relay/cli" },
        { label: "Production Checklist", href: "/docs/relay/production" },
      ],
    },
    {
      title: "Trace (Backend)",
      items: [
        { label: "Overview", href: "/docs/trace/overview" },
        { label: "API Reference", href: "/docs/trace/api" },
        { label: "Pipelines", href: "/docs/trace/pipelines" },
        { label: "Scaling", href: "/docs/trace/scale" },
        { label: "Backup & Restore", href: "/docs/trace/backup" },
      ],
    },
    {
      title: "Administration",
      items: [
        { label: "Authentication", href: "/docs/admin/auth" },
        { label: "Organizations", href: "/docs/admin/orgs" },
        { label: "Billing", href: "/docs/admin/billing" },
        { label: "Security", href: "/docs/admin/security" },
        { label: "Compliance", href: "/docs/admin/compliance" },
      ],
    },
  ]

  return (
    <div className="docs-page">
      <Link to="/overview" className="page-back">← Dashboard</Link>
      <header className="docs-header">
        <div className="brand">
          <BrandLogo size={32} />
          <span>LastState</span>
        </div>
        <h1>Documentation</h1>
      </header>

      <div className="docs-grid">
        {sections.map(section => (
          <div key={section.title} className="docs-section">
            <h2>{section.title}</h2>
            <ul>
              {section.items.map(item => (
                <li key={item.label}>
                  <a href={item.href}>{item.label}</a>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>

      <footer className="docs-footer">
        <p className="meta">© 2026 LastState. Apache 2.0 & AGPL-3.0.</p>
      </footer>
    </div>
  )
}
