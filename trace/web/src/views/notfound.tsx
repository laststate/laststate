// Dedicated 404 page for unknown routes.
import React from 'react'
import { Link } from 'react-router-dom'
import { BrandLogo } from '../Loading'
import { Button } from '../components/ui/button'

export default function NotFoundPage() {
  return (
    <div className="auth-page" data-testid="not-found">
      <div className="auth-card" style={{ textAlign: 'center' }}>
        <div className="auth-brand">
          <BrandLogo size={36} />
          <span className="auth-brand-text">Last State <em>Trace</em></span>
        </div>
        <div className="nf-code mono">404</div>
        <h1>Page not found</h1>
        <p className="auth-subtitle">
          The route you followed doesn't exist. It may have been renamed, or the
          link is stale.
        </p>
        <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'center', flexWrap: 'wrap' }}>
          <Link to="/overview"><Button>Go to dashboard</Button></Link>
          <Link to="/landing"><Button variant="secondary">About Trace</Button></Link>
        </div>
      </div>
    </div>
  )
}
