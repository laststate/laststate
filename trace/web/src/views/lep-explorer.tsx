// LEP Explorer — Interactive playground for decoding LEP envelopes.
// Client-side only: paste hex, visualize TLVs, compare envelopes.
import React, { useState, useCallback } from 'react'
import { decodeLEP, encodeLEP, toHex, type LEPEnvelope, type TLVRecord, parseCrashInfo, FLAG_AUTH, FLAG_ENC, FLAG_COMPRESSED, FLAG_TRUNCATED, TLV_CRASH, TLV_BREADCRUMB, TLV_METRIC, TLV_ANOMALY } from '../lep/codec'
import { Download, ExternalLink, Share2, Trash2, Upload } from '../icons'

interface ComparePair {
  left: LEPEnvelope | null
  right: LEPEnvelope | null
}

export default function LEPExplorer() {
  const [hexInput, setHexInput] = useState('')
  const [envelope, setEnvelope] = useState<LEPEnvelope | null>(null)
  const [compareHex, setCompareHex] = useState('')
  const [compareEnv, setCompareEnv] = useState<LEPEnvelope | null>(null)
  const [activeTab, setActiveTab] = useState<'tlvs' | 'stack' | 'hex'>('tlvs')
  const [crashInfo, setCrashInfo] = useState<any>(null)

  const decode = useCallback((hex: string) => {
    const env = decodeLEP(hex)
    return env
  }, [])

  const handleDecode = useCallback(() => {
    const env = decode(hexInput.trim())
    setEnvelope(env)
    if (env.valid && env.tlvs.length > 0) {
      const crashTLV = env.tlvs.find(t => t.type === TLV_CRASH)
      if (crashTLV) {
        setCrashInfo(parseCrashInfo(crashTLV))
      }
    }
  }, [hexInput, decode])

  const handleCompareDecode = useCallback(() => {
    const env = decode(compareHex.trim())
    setCompareEnv(env)
  }, [compareHex, decode])

  const handleClear = useCallback(() => {
    setHexInput('')
    setEnvelope(null)
    setCrashInfo(null)
    setActiveTab('tlvs')
  }, [])

  const handleCompareClear = useCallback(() => {
    setCompareHex('')
    setCompareEnv(null)
  }, [])

  const handleExportJSON = useCallback(() => {
    if (!envelope) return
    const data = {
      header: envelope.header,
      tlvs: envelope.tlvs.map(t => ({ ...t, hex: t.hex })),
      valid: envelope.valid,
    }
    const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'lep-envelope.json'
    a.click()
    URL.revokeObjectURL(url)
  }, [envelope])

  const handleExportCSV = useCallback(() => {
    if (!envelope || envelope.tlvs.length === 0) return
    const headers = 'Type,Type Name,Length (bytes),Hex Preview\n'
    const rows = envelope.tlvs.map(t =>
      `${t.type},"${t.typeName}",${t.length},"${t.hex.slice(0, 80)}..."`
    ).join('\n')
    const blob = new Blob([headers + rows], { type: 'text/csv' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'lep-tlvs.csv'
    a.click()
    URL.revokeObjectURL(url)
  }, [envelope])

  const handleCopyHex = useCallback(() => {
    if (!envelope) return
    navigator.clipboard.writeText(toHex(envelope.raw))
  }, [envelope])

  const flagLabels: Record<number, string> = {
    [FLAG_AUTH]: 'Authenticated',
    [FLAG_ENC]: 'Encrypted',
    [FLAG_COMPRESSED]: 'Compressed',
    [FLAG_TRUNCATED]: 'Truncated',
  }

  return (
    <div className="lep-explorer">
      <h2>LEP Explorer</h2>
      <p className="meta">
        Paste a LEP envelope hex string to decode and visualize. Supports LEP v1 and v2.
        <a href="/docs" target="_blank" rel="noreferrer" style={{ marginLeft: 8 }}>LEP spec</a>
      </p>

      <div className="grid-2">
        {/* Left panel — primary envelope */}
        <div className="panel">
          <div className="panel-head">
            <h3 style={{ margin: 0 }}>Primary Envelope</h3>
            <div className="row gap">
              <button type="button" className="btn ghost" onClick={handleClear}>
                <Trash2 size={14} /> Clear
              </button>
              <button type="button" className="btn ghost" onClick={handleExportJSON}>
                <Download size={14} /> JSON
              </button>
              <button type="button" className="btn ghost" onClick={handleExportCSV}>
                <Download size={14} /> CSV
              </button>
              <button type="button" className="btn ghost" onClick={handleCopyHex}>
                <Share2 size={14} /> Copy Hex
              </button>
            </div>
          </div>

          <textarea
            value={hexInput}
            onChange={e => setHexInput(e.target.value)}
            placeholder="Paste LEP hex here... e.g. 4C5354500100..."
            style={{
              width: '100%',
              minHeight: 120,
              fontFamily: 'monospace',
              fontSize: '0.8rem',
              padding: 8,
              background: 'hsl(0 0% 4%)',
              border: '1px solid hsl(0 0% 12%)',
              borderRadius: 4,
              color: 'hsl(0 0% 80%)',
              resize: 'vertical',
            }}
          />

          <div className="row gap" style={{ marginTop: 8 }}>
            <button type="button" className="btn" onClick={handleDecode}>Decode</button>
            <button type="button" className="btn secondary" onClick={() => {
              // Generate a sample envelope for demo
              const sampleTLV = { type: TLV_BREADCRUMB, value: new Uint8Array([72, 101, 108, 108, 111]).buffer }
              const sample = encodeLEP([sampleTLV])
              setHexInput(toHex(sample))
              setEnvelope(decode(toHex(sample)))
            }}>Load Sample</button>
          </div>

          {envelope && (
            <div style={{ marginTop: 12 }}>
              {envelope.error && (
                <div className="empty" style={{ borderColor: 'hsl(0 84% 50%)' }}>
                  <strong>Decode error:</strong> {envelope.error}
                </div>
              )}
              {envelope.valid && (
                <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', fontSize: '0.8rem' }}>
                  <span className="tag" style={{ background: 'hsl(143 70% 30%)' }}>Valid</span>
                  <span>Magic: <code>{envelope.header.magic}</code></span>
                  <span>Version: <code>{envelope.header.version}</code></span>
                  <span>Payload: <code>{envelope.header.payloadLength} bytes</code></span>
                  <span>CRC: <code>0x{envelope.header.crc.toString(16)}</code></span>
                  <span>TLVs: <code>{envelope.tlvs.length}</code></span>
                  <span>Flags: {Object.entries(flagLabels).map(([bit, label]) =>
                    (envelope.header.flags & Number(bit)) ? <span key={bit} className="tag" style={{ fontSize: '0.65rem' }}>{label}</span> : null
                  )}</span>
                </div>
              )}
              {!envelope.valid && !envelope.error && (
                <div className="empty">Invalid LEP envelope — check hex format</div>
              )}
            </div>
          )}

          {/* Tabs for decoded content */}
          {envelope && envelope.valid && envelope.tlvs.length > 0 && (
            <div style={{ marginTop: 16 }}>
              <div className="row" style={{ gap: 4, marginBottom: 8 }}>
                {(['tlvs', 'stack', 'hex'] as const).map(tab => (
                  <button
                    key={tab}
                    type="button"
                    className={`btn ${activeTab === tab ? '' : 'ghost'}`}
                    style={{ fontSize: '0.75rem' }}
                    onClick={() => setActiveTab(tab)}
                  >
                    {tab === 'tlvs' ? 'TLVs' : tab === 'stack' ? 'Stack Trace' : 'Hex Dump'}
                  </button>
                ))}
              </div>

              {activeTab === 'tlvs' && (
                <div className="table-wrap">
                  <table>
                    <thead>
                      <tr><th>Type</th><th>Name</th><th>Length</th><th>Hex Preview</th></tr>
                    </thead>
                    <tbody>
                      {envelope.tlvs.map((tlv, idx) => (
                        <tr key={idx} style={{ animationDelay: `${idx * 0.03}s` }}>
                          <td className="mono">{tlv.type.toString(16).padStart(4, '0')}</td>
                          <td>{tlv.typeName}</td>
                          <td>{tlv.length}</td>
                          <td className="mono" style={{ fontSize: '0.7rem', maxWidth: 300, overflow: 'hidden', textOverflow: 'ellipsis' }}>
                            {tlv.hex}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}

              {activeTab === 'stack' && crashInfo && (
                <div className="panel" style={{ background: 'hsl(0 0% 4%)', border: '1px solid hsl(0 0% 12%)' }}>
                  <h4>Crash Information</h4>
                  <div className="kv" style={{ marginTop: 8 }}>
                    <div className="kv-row"><span className="k">Exception</span><span className="v">{crashInfo.exceptionTypeName} (#{crashInfo.exceptionType})</span></div>
                    <div className="kv-row"><span className="k">PC</span><span className="v mono">0x{crashInfo.pc.toString(16)}</span></div>
                    <div className="kv-row"><span className="k">SP</span><span className="v mono">0x{crashInfo.sp.toString(16)}</span></div>
                    <div className="kv-row"><span className="k">LR</span><span className="v mono">0x{crashInfo.lr.toString(16)}</span></div>
                    <div className="kv-row"><span className="k">R0</span><span className="v mono">0x{crashInfo.r0.toString(16)}</span></div>
                    <div className="kv-row"><span className="k">R1</span><span className="v mono">0x{crashInfo.r1.toString(16)}</span></div>
                    <div className="kv-row"><span className="k">R2</span><span className="v mono">0x{crashInfo.r2.toString(16)}</span></div>
                    <div className="kv-row"><span className="k">R3</span><span className="v mono">0x{crashInfo.r3.toString(16)}</span></div>
                    <div className="kv-row"><span className="k">xPSR</span><span className="v mono">0x{crashInfo.xPSR.toString(16)}</span></div>
                    <div className="kv-row"><span className="k">Fault Status</span><span className="v mono">0x{crashInfo.faultStatus.toString(16)}</span></div>
                    <div className="kv-row"><span className="k">Fault Address</span><span className="v mono">0x{crashInfo.faultAddress?.toString(16) || '—'}</span></div>
                  </div>
                </div>
              )}

              {activeTab === 'hex' && (
                <pre className="hex" style={{ fontSize: '0.7rem', maxHeight: 400, overflow: 'auto' }}>
                  {(() => {
                    const bytes = new Uint8Array(envelope.raw)
                    const lines: string[] = []
                    for (let i = 0; i < bytes.length; i += 16) {
                      const chunk = bytes.slice(i, i + 16)
                      const hex = Array.from(chunk).map(b => b.toString(16).padStart(2, '0')).join(' ')
                      const asc = Array.from(chunk).map(b => (b >= 32 && b < 127 ? String.fromCharCode(b) : '.')).join('')
                      lines.push(i.toString(16).padStart(8, '0') + '  ' + hex.padEnd(48) + '  ' + asc)
                    }
                    return lines.join('\n')
                  })()}
                </pre>
              )}
            </div>
          )}
        </div>

        {/* Right panel — compare */}
        <div className="panel">
          <div className="panel-head">
            <h3 style={{ margin: 0 }}>Compare Envelope</h3>
            <button type="button" className="btn ghost" onClick={handleCompareClear}>
              <Trash2 size={14} /> Clear
            </button>
          </div>

          <textarea
            value={compareHex}
            onChange={e => setCompareHex(e.target.value)}
            placeholder="Paste second LEP hex here for comparison..."
            style={{
              width: '100%',
              minHeight: 120,
              fontFamily: 'monospace',
              fontSize: '0.8rem',
              padding: 8,
              background: 'hsl(0 0% 4%)',
              border: '1px solid hsl(0 0% 12%)',
              borderRadius: 4,
              color: 'hsl(0 0% 80%)',
              resize: 'vertical',
            }}
          />

          <div className="row gap" style={{ marginTop: 8 }}>
            <button type="button" className="btn" onClick={handleCompareDecode}>Compare</button>
          </div>

          {compareEnv && (
            <div style={{ marginTop: 12 }}>
              {compareEnv.error && (
                <div className="empty" style={{ borderColor: 'hsl(0 84% 50%)' }}>
                  <strong>Error:</strong> {compareEnv.error}
                </div>
              )}
              {compareEnv.valid && (
                <div style={{ fontSize: '0.8rem' }}>
                  <div className="meta" style={{ marginBottom: 8 }}>Comparison results:</div>
                  <table style={{ width: '100%', fontSize: '0.8rem' }}>
                    <thead><tr><th>Property</th><th>Primary</th><th>Compare</th></tr></thead>
                    <tbody>
                      <tr><td>Valid</td><td>{envelope?.valid ? '✓' : '✗'}</td><td>{compareEnv.valid ? '✓' : '✗'}</td></tr>
                      <tr><td>Magic</td><td className="mono">{envelope?.header.magic || '—'}</td><td className="mono">{compareEnv.header.magic}</td></tr>
                      <tr><td>Version</td><td className="mono">{envelope?.header.version || '—'}</td><td className="mono">{compareEnv.header.version}</td></tr>
                      <tr><td>Payload</td><td className="mono">{envelope?.header.payloadLength || '—'}</td><td className="mono">{compareEnv.header.payloadLength}</td></tr>
                      <tr><td>TLVs</td><td className="mono">{envelope?.tlvs.length || 0}</td><td className="mono">{compareEnv.tlvs.length}</td></tr>
                      <tr><td>CRC</td><td className="mono">{envelope?.header.crc?.toString(16) || '—'}</td><td className="mono">0x{compareEnv.header.crc.toString(16)}</td></tr>
                    </tbody>
                  </table>

                  {compareEnv.tlvs.length > 0 && (
                    <div style={{ marginTop: 12 }}>
                      <h4>Compare TLVs</h4>
                      <div className="table-wrap">
                        <table>
                          <thead><tr><th>Type</th><th>Name</th><th>Length</th></tr></thead>
                          <tbody>
                            {compareEnv.tlvs.map((tlv, idx) => (
                              <tr key={idx}>
                                <td className="mono">{tlv.type.toString(16).padStart(4, '0')}</td>
                                <td>{tlv.typeName}</td>
                                <td>{tlv.length}</td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    </div>
                  )}
                </div>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Footer info */}
      <div className="panel" style={{ marginTop: 16 }}>
        <h4 style={{ marginTop: 0 }}>LEP v1 / v2 Format Reference</h4>
        <div className="grid-3">
          <div>
            <strong>Header (24 bytes, LE)</strong>
            <ul style={{ fontSize: '0.8rem', paddingLeft: 16 }}>
              <li>Magic: <code>LSTP</code></li>
              <li>Version: u8 (1 or 2)</li>
              <li>Type: u8</li>
              <li>Architecture: u8</li>
              <li>Flags: u8</li>
              <li>Sequence: u32</li>
              <li>Event ID: u32</li>
              <li>Payload length: u32</li>
              <li>Header CRC-32/IEEE: u32</li>
            </ul>
          </div>
          <div>
            <strong>TLV Types</strong>
            <ul style={{ fontSize: '0.8rem', paddingLeft: 16 }}>
              <li>0x01: Identity</li>
              <li>0x03: Event</li>
              <li>0x04: CPU</li>
              <li>0x05: Fault / Crash</li>
              <li>0x06: Breadcrumb</li>
              <li>0x07: Metric</li>
              <li>0x09: Health</li>
              <li>0x0C: Log</li>
            </ul>
          </div>
          <div>
            <strong>Flags</strong>
            <ul style={{ fontSize: '0.8rem', paddingLeft: 16 }}>
              <li>0x01: Authenticated</li>
              <li>0x02: Encrypted</li>
              <li>0x04: AEAD</li>
              <li>0x08: Truncated</li>
              <li>0x10: Compressed</li>
            </ul>
          </div>
        </div>
      </div>
    </div>
  )
}
