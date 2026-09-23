// Device Memorial Wall — honors devices that have gone silent.
// Shows anonymized device data with virtual candles (localStorage).
import React, { useState, useEffect, useCallback } from 'react'
import { api } from '../api'
import { Flame as Candle, Download, Search } from '../icons'

interface MemorialDevice {
  id: string
  arch: string
  mfr: string
  batch: string
  firmwareVersion: string
  firstSeen: string
  lastSeen: string
  lastCrash: string
  daysSinceLastSeen: number
  candlesLit: number
}

export default function MemorialWall() {
  const [devices, setDevices] = useState<MemorialDevice[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [filter, setFilter] = useState({ arch: '', mfr: '', search: '' })
  const [candleState, setCandleState] = useState<Record<string, boolean>>({})
  const [totalCandles, setTotalCandles] = useState(0)

  // Load candle state from localStorage
  useEffect(() => {
    try {
      const saved = localStorage.getItem('memorial_candles')
      if (saved) {
        setCandleState(JSON.parse(saved))
      }
    } catch { /* ignore */ }
  }, [])

  // Fetch memorial devices
  useEffect(() => {
    async function load() {
      try {
        setLoading(true)
        const res = await api('/api/memorial/devices?days=30')
        setDevices(res.items || [])
        // Calculate total candles
        const saved = localStorage.getItem('memorial_candles')
        if (saved) {
          const state = JSON.parse(saved)
          const count = Object.values(state).filter(Boolean).length
          setTotalCandles(count)
        }
      } catch (e: any) {
        setError(e.message)
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [])

  const toggleCandle = useCallback((deviceId: string) => {
    setCandleState(prev => {
      const next = { ...prev, [deviceId]: !prev[deviceId] }
      localStorage.setItem('memorial_candles', JSON.stringify(next))
      const count = Object.values(next).filter(Boolean).length
      setTotalCandles(count)
      return next
    })
  }, [])

  const handleExport = useCallback(() => {
    if (!devices.length) return
    const headers = 'Architecture,Manufacturer,Batch,Firmware,First Seen,Last Seen,Candles\n'
    const rows = devices.map(d =>
      `"${d.arch}","${d.mfr}","${d.batch}","${d.firmwareVersion}","${d.firstSeen}","${d.lastSeen}",${d.candlesLit}`
    ).join('\n')
    const blob = new Blob([headers + rows], { type: 'text/csv' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'memorial-wall.csv'
    a.click()
    URL.revokeObjectURL(url)
  }, [devices])

  const filteredDevices = devices.filter(d => {
    if (filter.arch && d.arch !== filter.arch) return false
    if (filter.mfr && d.mfr !== filter.mfr) return false
    if (filter.search && !d.firmwareVersion.toLowerCase().includes(filter.search.toLowerCase())) return false
    return true
  })

  const uniqueArchs = [...new Set(devices.map(d => d.arch))]
  const uniqueMfrs = [...new Set(devices.map(d => d.mfr))]

  return (
    <div className="memorial-wall">
      <h2>Device Memorial Wall</h2>
      <p className="meta">
        Honoring devices that have gone silent. Each candle represents a moment of remembrance.
        Data is anonymized — no device IDs or organization names shown.
      </p>

      {/* Stats */}
      <div className="grid-3" style={{ marginBottom: '1.5rem' }}>
        <div className="panel" style={{ textAlign: 'center' }}>
          <div style={{ fontSize: '2rem', fontWeight: 700 }}>{devices.length}</div>
          <div className="meta">Devices Memorialized</div>
        </div>
        <div className="panel" style={{ textAlign: 'center' }}>
          <div style={{ fontSize: '2rem', fontWeight: 700 }}>{totalCandles}</div>
          <div className="meta">Candles Lit</div>
        </div>
        <div className="panel" style={{ textAlign: 'center' }}>
          <div style={{ fontSize: '2rem', fontWeight: 700 }}>
            {devices.length > 0 ? devices.reduce((sum, d) => sum + d.daysSinceLastSeen, 0) / devices.length : 0}
          </div>
          <div className="meta">Avg Days Silent</div>
        </div>
      </div>

      {/* Filters */}
      <div className="filters" style={{ marginBottom: '1rem' }}>
        <label>
          Architecture
          <select value={filter.arch} onChange={e => setFilter(f => ({ ...f, arch: e.target.value }))}>
            <option value="">All</option>
            {uniqueArchs.map(a => <option key={a} value={a}>{a}</option>)}
          </select>
        </label>
        <label>
          Manufacturer
          <select value={filter.mfr} onChange={e => setFilter(f => ({ ...f, mfr: e.target.value }))}>
            <option value="">All</option>
            {uniqueMfrs.map(m => <option key={m} value={m}>{m}</option>)}
          </select>
        </label>
        <label>
          Search
          <input value={filter.search} onChange={e => setFilter(f => ({ ...f, search: e.target.value }))} placeholder="Firmware version..." />
        </label>
        <button type="button" className="btn secondary" onClick={handleExport}>
          <Download size={14} /> Export CSV
        </button>
      </div>

      {/* Device grid */}
      {loading && <div className="panel"><p className="meta">Loading memorial wall...</p></div>}
      {error && <div className="panel" style={{ borderColor: 'hsl(0 84% 50%)' }}><p style={{ color: 'hsl(0 84% 50%)' }}>{error}</p></div>}

      {!loading && !error && filteredDevices.length === 0 && (
        <div className="panel empty">No memorialized devices found</div>
      )}

      {!loading && !error && filteredDevices.length > 0 && (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))', gap: '1rem' }}>
          {filteredDevices.map((device, idx) => (
            <div
              key={device.id}
              className="panel"
              style={{
                animationDelay: `${idx * 0.05}s`,
                border: candleState[device.id] ? '1px solid hsl(45 90% 50%)' : '1px solid hsl(0 0% 12%)',
              }}
            >
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
                <div>
                  <h4 style={{ margin: 0 }}>
                    {device.arch}#{device.batch.slice(0, 4)}
                    <span className="meta" style={{ marginLeft: 8, fontSize: '0.75rem' }}>
                      {device.mfr}
                    </span>
                  </h4>
                  <div className="meta" style={{ marginTop: 4 }}>
                    Firmware: <code>{device.firmwareVersion}</code>
                  </div>
                </div>
                <button
                  type="button"
                  className={`btn ${candleState[device.id] ? '' : 'ghost'}`}
                  style={{ fontSize: '1.2rem', padding: '0.2rem 0.4rem' }}
                  onClick={() => toggleCandle(device.id)}
                  title={candleState[device.id] ? 'Extinguish candle' : 'Light a candle'}
                >
                  <Candle size={20} style={{ color: candleState[device.id] ? 'hsl(45 90% 50%)' : 'hsl(0 0% 40%)' }} />
                </button>
              </div>

              <div className="kv" style={{ marginTop: 8, fontSize: '0.8rem' }}>
                <div className="kv-row">
                  <span className="k">In memoriam</span>
                  <span className="v">
                    {device.firstSeen} — {device.lastSeen}
                  </span>
                </div>
                <div className="kv-row">
                  <span className="k">Last seen</span>
                  <span className="v">{device.daysSinceLastSeen} days ago</span>
                </div>
                <div className="kv-row">
                  <span className="k">Last crash</span>
                  <span className="v mono">{device.lastCrash}</span>
                </div>
                <div className="kv-row">
                  <span className="k">Candles</span>
                  <span className="v">
                    {candleState[device.id] ? '🕯️' : ''} {device.candlesLit + (candleState[device.id] ? 1 : 0)}
                  </span>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Footer */}
      <div className="panel" style={{ marginTop: '2rem' }}>
        <h4 style={{ marginTop: 0 }}>About the Memorial Wall</h4>
        <p className="meta">
          This wall honors embedded devices that have gone silent. When a device stops reporting
          for over 30 days, it appears here with anonymized information — architecture, manufacturer,
          batch, firmware version, and dates. No device IDs, organization names, or personal data
          is shown.
        </p>
        <p className="meta">
          Click the candle to light a virtual flame in remembrance. Your candles are stored locally
          in your browser and persist across visits.
        </p>
      </div>
    </div>
  )
}
