// Anomaly detection view — shows detected anomalies and thresholds.
import React, { useState, useEffect } from 'react'
import { api } from '../api'
import { Radar, Settings } from '../icons'

interface AnomalyEvent {
  id: string
  metric: string
  value: number
  threshold: { min: number; max: number }
  device_id: string
  timestamp: string
}

interface Thresholds {
  [key: string]: { metric: string; min: number; max: number; enabled: boolean }
}

export default function AnomalyView() {
  const [events, setEvents] = useState<AnomalyEvent[]>([])
  const [thresholds, setThresholds] = useState<Thresholds>({})
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    async function load() {
      try {
        setLoading(true)
        const [eventsRes, thresholdsRes] = await Promise.all([
          api('/api/anomaly/events').catch(() => ({ events: [] })),
          api('/api/anomaly/thresholds').catch(() => ({})),
        ])
        setEvents(eventsRes.events || [])
        setThresholds(thresholdsRes)
      } catch (e: any) {
        console.error('Anomaly load error:', e)
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [])

  if (loading) return <div className="panel"><p className="meta">Loading anomaly data...</p></div>

  return (
    <div className="anomaly">
      <h2>
        <Radar size={20} style={{ marginRight: 8, verticalAlign: 'middle' }} />
        Anomaly Detection
      </h2>
      <p className="meta">
        On-device and server-side anomaly detection. Thresholds configurable per metric.
        When enabled (LS_ENABLE_ANOMALY=ON), Latch adds anomaly breadcrumbs and flags LEP envelopes.
      </p>

      {/* Thresholds */}
      <div className="panel" style={{ marginBottom: '1.5rem' }}>
        <div className="panel-head">
          <h3 style={{ margin: 0 }}>
            <Settings size={16} style={{ marginRight: 8 }} />
            Detection Thresholds
          </h3>
        </div>
        <div className="table-wrap" style={{ marginTop: 8 }}>
          <table>
            <thead><tr><th>Metric</th><th>Min</th><th>Max</th><th>Status</th></tr></thead>
            <tbody>
              {Object.entries(thresholds).map(([key, t]) => (
                <tr key={key}>
                  <td><strong>{t.metric}</strong></td>
                  <td className="mono">{t.min}</td>
                  <td className="mono">{t.max}</td>
                  <td><span className={`tag ${t.enabled ? 'ok' : 'warn'}`}>{t.enabled ? 'Enabled' : 'Disabled'}</span></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {/* Events */}
      <div className="panel">
        <h3 style={{ marginTop: 0 }}>Recent Anomalies</h3>
        {events.length ? (
          <div className="table-wrap">
            <table>
              <thead><tr><th>Event</th><th>Metric</th><th>Value</th><th>Range</th><th>Device</th><th>Time</th></tr></thead>
              <tbody>
                {events.map((e, i) => (
                  <tr key={i}>
                    <td className="mono">{e.id}</td>
                    <td>{e.metric}</td>
                    <td className="mono">{e.value}</td>
                    <td className="mono">[{e.threshold.min}, {e.threshold.max}]</td>
                    <td className="mono">{e.device_id}</td>
                    <td className="meta">{new Date(e.timestamp).toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : <div className="panel empty">No anomalies detected</div>}
      </div>
    </div>
  )
}
