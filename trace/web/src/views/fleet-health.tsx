// Fleet Health Score view — shows device health rankings and trend.
import React, { useState, useEffect } from 'react'
import { api } from '../api'
import { Gauge, TrendingDown, TrendingUp } from '../icons'

interface HealthData {
  average_score: number
  median_score: number
  healthy_count: number
  degraded_count: number
  critical_count: number
  top_healthy: any[]
  bottom_dead: any[]
  trend: any[]
}

export default function FleetHealthView() {
  const [data, setData] = useState<HealthData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    async function load() {
      try {
        setLoading(true)
        const res = await api('/api/fleet/health')
        setData(res)
      } catch (e: any) {
        setError(e.message)
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [])

  if (loading) return <div className="panel"><p className="meta">Loading fleet health...</p></div>
  if (error) return <div className="panel" style={{ borderColor: 'hsl(0 84% 50%)' }}><p style={{ color: 'hsl(0 84% 50%)' }}>{error}</p></div>

  const avgScore = data?.average_score || 0
  const scoreColor = avgScore >= 80 ? 'hsl(143 70% 40%)' : avgScore >= 50 ? 'hsl(45 90% 50%)' : 'hsl(0 84% 50%)'

  return (
    <div className="fleet-health">
      <h2>
        <Gauge size={20} style={{ marginRight: 8, verticalAlign: 'middle' }} />
        Fleet Health Score
      </h2>
      <p className="meta">
        Device health score 0-100 based on crash frequency (40%), uptime (25%), battery (15%), OTA (10%), temperature (10%).
        Alert when score &lt; 30.
      </p>

      {/* Score overview */}
      <div className="panel" style={{ marginBottom: '1.5rem', borderColor: scoreColor }}>
        <div className="grid-4" style={{ textAlign: 'center' }}>
          <div>
            <div style={{ fontSize: '2.5rem', fontWeight: 700, color: scoreColor }}>{avgScore}</div>
            <div className="meta">Average Score</div>
          </div>
          <div>
            <div style={{ fontSize: '2rem', fontWeight: 700, color: 'hsl(143 70% 40%)' }}>{data?.healthy_count || 0}</div>
            <div className="meta">Healthy</div>
          </div>
          <div>
            <div style={{ fontSize: '2rem', fontWeight: 700, color: 'hsl(45 90% 50%)' }}>{data?.degraded_count || 0}</div>
            <div className="meta">Degraded</div>
          </div>
          <div>
            <div style={{ fontSize: '2rem', fontWeight: 700, color: 'hsl(0 84% 50%)' }}>{data?.critical_count || 0}</div>
            <div className="meta">Critical (&lt;30)</div>
          </div>
        </div>
      </div>

      <div className="grid-2">
        {/* Top healthy devices */}
        <div className="panel">
          <h3 style={{ marginTop: 0 }}>
            <TrendingUp size={16} style={{ marginRight: 8 }} />Top Healthy Devices
          </h3>
          {data?.top_healthy?.length ? (
            <div className="table-wrap">
              <table>
                <thead><tr><th>Device</th><th>Score</th><th>Status</th></tr></thead>
                <tbody>
                  {data.top_healthy.map((d: any, i: number) => (
                    <tr key={i}>
                      <td className="mono">{d.device_id}</td>
                      <td>{d.score}</td>
                      <td><span className={`tag ${d.status === 'healthy' ? 'ok' : d.status === 'degraded' ? 'warn' : 'error'}`}>{d.status}</span></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : <div className="panel empty">No healthy devices</div>}
        </div>

        {/* Bottom dead devices */}
        <div className="panel">
          <h3 style={{ marginTop: 0 }}>
            <TrendingDown size={16} style={{ marginRight: 8 }} />Bottom Dead Devices
          </h3>
          {data?.bottom_dead?.length ? (
            <div className="table-wrap">
              <table>
                <thead><tr><th>Device</th><th>Score</th><th>Status</th></tr></thead>
                <tbody>
                  {data.bottom_dead.map((d: any, i: number) => (
                    <tr key={i}>
                      <td className="mono">{d.device_id}</td>
                      <td>{d.score}</td>
                      <td><span className="tag error">{d.status}</span></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : <div className="panel empty">No critical devices</div>}
        </div>
      </div>

      {/* Trend chart placeholder */}
      <div className="panel" style={{ marginTop: '1.5rem' }}>
        <h3 style={{ marginTop: 0 }}>Health Score Trend</h3>
        {data?.trend?.length ? (
          <div className="table-wrap">
            <table>
              <thead><tr><th>Date</th><th>Average Score</th></tr></thead>
              <tbody>
                {data.trend.map((t: any, i: number) => (
                  <tr key={i}>
                    <td>{t.date}</td>
                    <td>{t.score}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : <div className="panel empty">No trend data</div>}
      </div>
    </div>
  )
}
