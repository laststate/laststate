// Public Crash API documentation view.
import React from 'react'
import { ExternalLink, Shield } from '../icons'

export default function PublicAPI() {
  return (
    <div className="public-api">
      <h2>Public Crash API</h2>
      <p className="meta">
        Aggregate crash statistics from the global embedded device fleet.
        All data is anonymized and aggregated — no device IDs or personal information.
      </p>

      <div className="grid-2" style={{ marginTop: '1.5rem' }}>
        {/* API Docs */}
        <div className="panel">
          <h3 style={{ marginTop: 0 }}>
            <ExternalLink size={16} /> API Endpoints
          </h3>
          <div className="table-wrap">
            <table>
              <thead>
                <tr><th>Method</th><th>Endpoint</th><th>Description</th></tr>
              </thead>
              <tbody>
                <tr>
                  <td><code>GET</code></td>
                  <td className="mono">/api/public/crashes</td>
                  <td>Aggregate crash statistics</td>
                </tr>
                <tr>
                  <td><code>GET</code></td>
                  <td className="mono">/api/public/crashes/by-arch</td>
                  <td>Crash counts by architecture</td>
                </tr>
                <tr>
                  <td><code>GET</code></td>
                  <td className="mono">/api/public/crashes/by-region</td>
                  <td>Crash counts by region (country)</td>
                </tr>
                <tr>
                  <td><code>GET</code></td>
                  <td className="mono">/api/public/crashes/top-fingerprints</td>
                  <td>Top N most common crash fingerprints</td>
                </tr>
                <tr>
                  <td><code>GET</code></td>
                  <td className="mono">/api/public/crashes/trend</td>
                  <td>Daily crash trend (last 30 days)</td>
                </tr>
              </tbody>
            </table>
          </div>

          <h4 style={{ marginTop: '1rem' }}>Query Parameters</h4>
          <div className="table-wrap">
            <table>
              <thead><tr><th>Param</th><th>Type</th><th>Default</th><th>Description</th></tr></thead>
              <tbody>
                <tr><td className="mono">days</td><td>int</td><td>30</td><td>Number of days to look back</td></tr>
                <tr><td className="mono">limit</td><td>int</td><td>10</td><td>Max results (top-N queries)</td></tr>
                <tr><td className="mono">arch</td><td>string</td><td>—</td><td>Filter by architecture</td></tr>
                <tr><td className="mono">region</td><td>string</td><td>—</td><td>Filter by ISO 3166-1 alpha-2 country code</td></tr>
                <tr><td className="mono">severity</td><td>string</td><td>—</td><td>Filter by severity (fatal, error, warn)</td></tr>
              </tbody>
            </table>
          </div>

          <h4 style={{ marginTop: '1rem' }}>Example Response</h4>
          <pre>{`{
  "total_crashes": 14523,
  "unique_fingerprints": 342,
  "affected_devices": 8921,
  "period": "2026-07-16 to 2026-08-15",
  "by_severity": {
    "fatal": 234,
    "error": 1892,
    "warn": 12400
  },
  "top_architectures": [
    { "arch": "cortex-m4", "count": 5421 },
    { "arch": "riscv64", "count": 3200 },
    { "arch": "esp32", "count": 2800 }
  ]
}`}</pre>
        </div>

        {/* Privacy */}
        <div className="panel">
          <h3 style={{ marginTop: 0 }}>
            <Shield size={16} /> Privacy Policy
          </h3>
          <div style={{ fontSize: '0.85rem', lineHeight: 1.6 }}>
            <h4>Data Collected</h4>
            <ul style={{ paddingLeft: 16 }}>
              <li>Crash type (HardFault, MemManage, BusFault, etc.)</li>
              <li>Architecture (Cortex-M, RISC-V, Xtensa, etc.)</li>
              <li>Timestamp (UTC)</li>
              <li>Aggregate counts per region (country-level, not city-level)</li>
            </ul>

            <h4>Data NOT Collected</h4>
            <ul style={{ paddingLeft: 16 }}>
              <li>Device ID</li>
              <li>Build ID</li>
              <li>Stack trace contents</li>
              <li>Breadcrumb messages</li>
              <li>Personal information</li>
              <li>IP addresses</li>
            </ul>

            <h4>Usage</h4>
            <ul style={{ paddingLeft: 16 }}>
              <li>Aggregated analytics for the embedded community</li>
              <li>Research and benchmarking</li>
              <li>NOT for identifying individual devices or users</li>
            </ul>

            <h4>Opt-out</h4>
            <ul style={{ paddingLeft: 16 }}>
              <li>Device owners: disable via Latch config <code>LS_PUBLIC_TELEMETRY=false</code></li>
              <li>Organizations: disable via Trace settings <code>PUBLIC_CRASH_API=false</code></li>
            </ul>

            <h4>Rate Limiting</h4>
            <p className="meta">
              100 requests per hour per IP. Results cached for 1 hour.
            </p>

            <h4>Contact</h4>
            <p className="meta">
              privacy@laststate.dev
            </p>
          </div>
        </div>
      </div>

      {/* Rate limit notice */}
      <div className="panel" style={{ marginTop: '1.5rem' }}>
        <h4 style={{ marginTop: 0 }}>API Usage Notes</h4>
        <div className="grid-2">
          <div>
            <strong>Rate Limit:</strong>
            <p className="meta">100 requests/hour per IP address. Exceeding the limit returns HTTP 429 with a Retry-After header.</p>
          </div>
          <div>
            <strong>Caching:</strong>
            <p className="meta">Responses are cached for 1 hour. For real-time data, use the authenticated Trace API instead.</p>
          </div>
        </div>
      </div>
    </div>
  )
}
