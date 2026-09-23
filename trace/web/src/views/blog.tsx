import React from 'react'
import { Link } from 'react-router-dom'
import { BrandLogo } from '../Loading'
import './styles.css'

const posts = [
  {
    title: "Introducing LastState: Embedded Observability That Survives the Crash",
    date: "2026-08-15",
    excerpt: "When an embedded device crashes, the evidence is usually lost with the reboot. LastState captures crash state across reboots, collects it from any transport, and correlates issues across your fleet — all open source, all self-hosted.",
    anchor: "post-introducing",
  },
  {
    title: "LEP v1: An Open Protocol for Firmware Diagnostics",
    date: "2026-07-29",
    excerpt: "We're releasing LEP v1 as an open, versioned, transport-independent binary contract for firmware diagnostics. No vendor lock-in. No proprietary formats. Just bytes.",
    anchor: "post-lep-v1",
  },
  {
    title: "How Latch Captures Crash State in 3 Calls",
    date: "2026-07-15",
    excerpt: "Three function calls. That's all it takes to capture CPU context, breadcrumbs, metrics, and stack traces before a hard fault. No heap allocation. No scheduler dependency. Pure C11.",
    anchor: "post-latch",
  },
]

export default function BlogPage() {
  return (
    <div className="blog-page">
      <Link to="/overview" className="page-back">← Dashboard</Link>
      <header className="blog-header">
        <div className="brand">
          <BrandLogo size={32} />
          <span>LastState</span>
        </div>
        <h1>Blog</h1>
        <p className="meta">Engineering updates, protocol specs, and embedded observability insights.</p>
      </header>

      <div className="blog-list">
        {posts.map((post, i) => (
          <article key={i} className="blog-post">
            <div className="blog-post-meta">
              <time>{post.date}</time>
            </div>
            <h2 id={post.anchor}>{post.title}</h2>
            <p className="blog-post-excerpt">{post.excerpt}</p>
            
          </article>
        ))}
      </div>

      <footer className="blog-footer">
        <p className="meta">© 2026 LastState. Apache 2.0 & AGPL-3.0.</p>
      </footer>
    </div>
  )
}
