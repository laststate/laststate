// Device DNA view — shows hardware fingerprinting and similarity comparisons.
import React, { useState, useEffect } from 'react'
import { api } from '../api'
import { Fingerprint, AlertTriangle } from '../icons'

interface DNADevice {
  device_id: string
  mcu_uuid: string
  boot_time_avg: number
  clock_freq: number
  flash_wear: number
  bootloader_sig: string
  fingerprint: string
}

interface ClonePair {
  device_1_id: string
  device_2_id: string
  similarity: number
  match: boolean
}

export default function DeviceDNAView() {
  const [devices, setDevices] = useState<DNADevice[]>([])
  const [clones, setClones] = useState<ClonePair[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    async function load() {
      try {
        setLoading(true)
        const [devicesRes, clonesRes] = await Promise.all([
          api('/api/devices/dna').catch(() => ({ items: [] })),
          api('/api/devices/dna/clones').catch(() => ({ items: [] })),
        ])
        setDevices(devicesRes.items || [])
        setClones(clonesRes.items || [])
      } catch (e: any) {
        setError(e.message)
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [])

  if (loading) return <div className="panel"><p className="meta">Loading device DNA...</p></div>
  if (error) return <div className="panel" style={{ borderColor: 'hsl(0 84% 50%)' }}><p style={{ color: 'hsl(0 84% 50%)' }}>{error}</p></div>

  return (
    <div className="device-dna">
      <h2>
        <Fingerprint size={20} style={{ marginRight: 8, verticalAlign: 'middle' }} />
        Device DNA — Hardware Fingerprinting
      </h2>
      <p className="meta">
        Each device has a unique DNA based on hardware characteristics. Similar DNAs may indicate clones,
        replaced components, or defective batches. Threshold: 95% similarity.
      </p>

      {/* Clone alerts */}
      {clones.length > 0 && (
        <div className="panel" style={{ marginBottom: '1.5rem', borderColor: 'hsl(0 84% 50%)' }}>
          <div className="panel-head">
            <h3 style={{ margin: 0 }}>
              <AlertTriangle size={16} style={{ marginRight: 8, color: 'hsl(0 84% 50%)' }} />
              Potential Clones Detected ({clones.length})
            </h3>
          </div>
          <div className="table-wrap" style={{ marginTop: 8 }}>
            <table>
              <thead><tr><th>Device 1</th><th>Device 2</th><th>Similarity</th><th>Action</th></tr></thead>
              <tbody>
                {clones.map((c, i) => (
                  <tr key={i}>
                    <td className="mono">{c.device_1_id}</td>
                    <td className="mono">{c.device_2_id}</td>
                    <td><span className="tag error">{(c.similarity * 100).toFixed(1)}%</span></td>
                    <td><button type="button" className="btn ghost" style={{ fontSize: '0.7rem' }}>Investigate</button></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Device DNA table */}
      <div className="panel">
        <h3 style={{ marginTop: 0 }}>Device Fingerprints</h3>
        {devices.length ? (
          <div className="table-wrap">
            <table>
              <thead><tr><th>Device</th><th>MCU UUID</th><th>Boot Time (ms)</th><th>Clock (MHz)</th><th>Flash Wear</th><th>Signature</th></tr></thead>
              <tbody>
                {devices.map((d, i) => (
                  <tr key={i}>
                    <td className="mono">{d.device_id}</td>
                    <td className="mono">{d.mcu_uuid}</td>
                    <td>{d.boot_time_avg.toFixed(1)}</td>
                    <td>{d.clock_freq.toFixed(2)}</td>
                    <td>{d.flash_wear}%</td>
                    <td className="mono">{d.bootloader_sig}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : <div className="panel empty">No device DNA data</div>}
      </div>
    </div>
  )
}
