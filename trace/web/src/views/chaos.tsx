// Chaos Engineering view — inject faults and verify crash capture.
import React, { useState, useEffect } from 'react'
import { api } from '../api'
import { Bug, Play, RefreshCw } from '../icons'

interface ChaosStatus {
  enabled: boolean
  adapter: string
  types: string[]
  total: number
}

interface InjectionResult {
  id: string
  type: string
  device_id: string
  status: string
  captured: boolean
  duration_ms: number
  timestamp: string
}

export default function ChaosView() {
  const [status, setStatus] = useState<ChaosStatus | null>(null)
  const [results, setResults] = useState<InjectionResult[]>([])
  const [selectedDevice, setSelectedDevice] = useState('')
  const [selectedType, setSelectedType] = useState('')
  const [injecting, setInjecting] = useState(false)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    async function load() {
      try {
        setLoading(true)
        const [statusRes, resultsRes] = await Promise.all([
          api('/api/chaos/status').catch(() => null),
          api('/api/chaos/results').catch(() => ({ results: [] })),
        ])
        setStatus(statusRes)
        setResults(resultsRes.results || [])
      } catch (e: any) {
        console.error('Chaos load error:', e)
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [])

  const handleInject = async () => {
    if (!selectedDevice || !selectedType) return
    setInjecting(true)
    try {
      const res = await api('/api/chaos/inject', {
        method: 'POST',
        body: { device_id: selectedDevice, injection: selectedType },
      })
      setResults(prev => [res, ...prev])
    } catch (e: any) {
      alert('Injection failed: ' + e.message)
    } finally {
      setInjecting(false)
    }
  }

  if (loading) return <div className="panel"><p className="meta">Loading chaos status...</p></div>

  return (
    <div className="chaos">
      <h2>
        <Bug size={20} style={{ marginRight: 8, verticalAlign: 'middle' }} />
        Chaos Engineering
      </h2>
      <p className="meta">
        Controlled fault injection to verify Latch crash capture. {status?.enabled ? 'Enabled' : 'Disabled'}.
        Adapter: {status?.adapter || 'none'}.
      </p>

      {/* Status */}
      <div className="panel" style={{ marginBottom: '1.5rem' }}>
        <div className="grid-3">
          <div>
            <div className="meta">Status</div>
            <div><span className={`tag ${status?.enabled ? 'ok' : 'error'}`}>{status?.enabled ? 'Enabled' : 'Disabled'}</span></div>
          </div>
          <div>
            <div className="meta">Adapter</div>
            <div className="mono">{status?.adapter || '—'}</div>
          </div>
          <div>
            <div className="meta">Total Injections</div>
            <div>{status?.total || 0}</div>
          </div>
        </div>
      </div>

      {/* Injection form */}
      <div className="panel" style={{ marginBottom: '1.5rem' }}>
        <h3 style={{ marginTop: 0 }}>Inject Fault</h3>
        <div className="grid-3">
          <div>
            <label>Device ID
              <input value={selectedDevice} onChange={e => setSelectedDevice(e.target.value)} placeholder="DEV-001" className="input" style={{ width: '100%', marginTop: 4 }} />
            </label>
          </div>
          <div>
            <label>Injection Type
              <select value={selectedType} onChange={e => setSelectedType(e.target.value)} className="input" style={{ width: '100%', marginTop: 4 }}>
                <option value="">Select type...</option>
                {status?.types?.map((t: string) => <option key={t} value={t}>{t}</option>)}
              </select>
            </label>
          </div>
          <div style={{ display: 'flex', alignItems: 'flex-end' }}>
            <button type="button" className="btn" onClick={handleInject} disabled={!selectedDevice || !selectedType || !status?.enabled || injecting}>
              <Play size={14} style={{ marginRight: 4 }} /> {injecting ? 'Injecting...' : 'Inject'}
            </button>
          </div>
        </div>
        {!status?.enabled && (
          <div className="empty" style={{ marginTop: 8, borderColor: 'hsl(0 84% 50%)' }}>
            Chaos engineering is disabled. Set CHAOS_ENABLED=true to enable.
          </div>
        )}
      </div>

      {/* Results */}
      <div className="panel">
        <div className="panel-head">
          <h3 style={{ margin: 0 }}>Recent Injections</h3>
          <button type="button" className="btn ghost" onClick={() => window.location.reload()}>
            <RefreshCw size={14} /> Refresh
          </button>
        </div>
        {results.length ? (
          <div className="table-wrap">
            <table>
              <thead><tr><th>ID</th><th>Type</th><th>Device</th><th>Status</th><th>Captured</th><th>Duration</th><th>Time</th></tr></thead>
              <tbody>
                {results.map((r, i) => (
                  <tr key={i}>
                    <td className="mono">{r.id.slice(0, 8)}</td>
                    <td>{r.type}</td>
                    <td className="mono">{r.device_id}</td>
                    <td><span className="tag">{r.status}</span></td>
                    <td>{r.captured ? '✓' : '✗'}</td>
                    <td>{r.duration_ms}ms</td>
                    <td className="meta">{new Date(r.timestamp).toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : <div className="panel empty">No injections yet</div>}
      </div>
    </div>
  )
}
