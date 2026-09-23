/** Nightwatch / Sentry-style chart primitives (pure SVG, no deps). */

const COLORS = {
  purple: '#8b5cf6',
  cyan: '#22d3ee',
  pink: '#f472b6',
  green: '#34d399',
  yellow: '#fbbf24',
  orange: '#fb923c',
  red: '#f87171',
  blue: '#60a5fa',
  gray: '#6b7280',
  graySoft: '#4b5563',
  grid: 'rgba(255,255,255,0.06)',
  text: '#8b93a7',
  /** Nightwatch palette from product screenshots */
  nwOk: '#5c6370',
  nwWarn: '#e8a317',
  nwErr: '#d4546a',
  nwAvg: '#8b93a7',
  nwMax: '#e8913a',
  nwHandled: '#5c6370',
  nwUnhandled: '#b84a5a',
}

const PALETTE = [COLORS.purple, COLORS.cyan, COLORS.pink, COLORS.green, COLORS.yellow, COLORS.orange, COLORS.blue, COLORS.red]

export type SeriesPoint = { x: string; y: number }
type Pt = { x: number; y: number; label: string; value: number }

/** One bar with stacked segments (Nightwatch REQUESTS / EXCEPTIONS style). */
export type StackedBar = { x: string; segments: { key: string; y: number }[] }

function padSeries(data: SeriesPoint[], min = 14): SeriesPoint[] {
  if (data.length >= min) return data
  const out = [...data]
  while (out.length < min) out.unshift({ x: '', y: 0 })
  return out
}

function toPts(data: SeriesPoint[], padL: number, padT: number, innerW: number, innerH: number, max: number): Pt[] {
  return data.map((d, i) => ({
    x: padL + (data.length === 1 ? innerW / 2 : (i / (data.length - 1)) * innerW),
    y: padT + innerH - (d.y / max) * innerH,
    label: d.x,
    value: d.y,
  }))
}

function smoothPath(pts: { x: number; y: number }[]) {
  if (!pts.length) return ''
  if (pts.length < 2) return `M ${pts[0].x} ${pts[0].y}`
  let d = `M ${pts[0].x} ${pts[0].y}`
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[i === 0 ? i : i - 1]
    const p1 = pts[i]
    const p2 = pts[i + 1]
    const p3 = pts[i + 2] || p2
    const cp1x = p1.x + (p2.x - p0.x) / 6
    const cp1y = p1.y + (p2.y - p0.y) / 6
    const cp2x = p2.x - (p3.x - p1.x) / 6
    const cp2y = p2.y - (p3.y - p1.y) / 6
    d += ` C ${cp1x} ${cp1y}, ${cp2x} ${cp2y}, ${p2.x} ${p2.y}`
  }
  return d
}

function fmtCompact(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1).replace(/\.0$/, '') + 'M'
  if (n >= 10_000) return (n / 1000).toFixed(1).replace(/\.0$/, '') + 'K'
  if (n >= 1000) return (n / 1000).toFixed(1).replace(/\.0$/, '') + 'K'
  return String(Math.round(n))
}

/** Smooth cubic area + line chart (Nightwatch-style). */
export function AreaChart({
  series,
  height = 180,
  color = COLORS.purple,
  fillId = 'areaGrad',
  showDots = false,
  yLabel,
}: {
  series: SeriesPoint[]
  height?: number
  color?: string
  fillId?: string
  showDots?: boolean
  yLabel?: string
}) {
  const data = padSeries(series)
  if (!data.length) return <EmptyChart />
  const w = 640
  const h = height
  const padL = 28
  const padR = 4
  const padT = 8
  const padB = 20
  const max = Math.max(1, ...data.map(d => d.y))
  const innerW = w - padL - padR
  const innerH = h - padT - padB
  const pts = toPts(data, padL, padT, innerW, innerH, max)
  const line = smoothPath(pts)
  const area = `${line} L ${pts[pts.length - 1].x} ${padT + innerH} L ${pts[0].x} ${padT + innerH} Z`
  const gridYs = [0, 0.25, 0.5, 0.75, 1].map(t => padT + innerH * (1 - t))
  const step = Math.max(1, Math.ceil(pts.length / 7))

  return (
    <div className="nw-chart" style={{ ['--chart-h' as string]: `${height}px` }}>
      <svg
        viewBox={`0 0 ${w} ${h}`}
        className="nw-svg"
        preserveAspectRatio="none"
        role="img"
        aria-label={yLabel || 'Area chart'}
      >
        <defs>
          <linearGradient id={fillId} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={color} stopOpacity="0.35" />
            <stop offset="70%" stopColor={color} stopOpacity="0.08" />
            <stop offset="100%" stopColor={color} stopOpacity="0" />
          </linearGradient>
        </defs>
        {gridYs.map((gy, i) => (
          <g key={i}>
            <line x1={padL} x2={w - padR} y1={gy} y2={gy} stroke={COLORS.grid} strokeWidth="1" />
            <text x={padL - 6} y={gy + 3} textAnchor="end" fill={COLORS.text} fontSize="10">
              {Math.round(max * (1 - i / 4))}
            </text>
          </g>
        ))}
        <path d={area} fill={`url(#${fillId})`} />
        <path d={line} fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        {/* Hit targets + tooltips (native title) */}
        {pts.map((p, i) => (
          <circle key={`h${i}`} cx={p.x} cy={p.y} r={showDots ? 2.5 : 6} fill={showDots ? '#0c0c0e' : 'transparent'} stroke={showDots ? color : 'transparent'} strokeWidth="1.5" className="nw-hit">
            <title>{(p.label || i) + ': ' + p.value}</title>
          </circle>
        ))}
        {pts.filter((_, i) => i % step === 0 || i === pts.length - 1).map((p, i) => (
          <text key={i} x={p.x} y={h - 6} textAnchor="middle" fill={COLORS.text} fontSize="10">
            {(p.label || '').toString().slice(-5)}
          </text>
        ))}
      </svg>
    </div>
  )
}

/** Export series as CSV (download helper for charts). */
export function seriesToCSV(series: SeriesPoint[], name = 'series'): string {
  const lines = ['label,value']
  for (const p of series) lines.push(`${JSON.stringify(p.x)},${p.y}`)
  return lines.join('\n')
}

export function downloadText(filename: string, text: string, mime = 'text/csv') {
  const blob = new Blob([text], { type: mime })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

export function DualAreaChart({
  a, b, labelA = 'A', labelB = 'B', height = 180,
  colorA = COLORS.purple, colorB = COLORS.cyan,
}: {
  a: SeriesPoint[]
  b: SeriesPoint[]
  labelA?: string
  labelB?: string
  height?: number
  colorA?: string
  colorB?: string
}) {
  const n = Math.max(a.length, b.length, 14)
  const sa = padSeries(a, n)
  const sb = padSeries(b, n)
  const w = 640
  const h = height
  const padL = 4, padR = 4, padT = 8, padB = 4
  const max = Math.max(1, ...sa.map(d => d.y), ...sb.map(d => d.y))
  const innerW = w - padL - padR
  const innerH = h - padT - padB
  const pa = toPts(sa, padL, padT, innerW, innerH, max)
  const pb = toPts(sb, padL, padT, innerW, innerH, max)
  const pathA = smoothPath(pa)
  const pathB = smoothPath(pb)
  const areaA = `${pathA} L ${pa[pa.length - 1].x} ${padT + innerH} L ${pa[0].x} ${padT + innerH} Z`
  const areaB = `${pathB} L ${pb[pb.length - 1].x} ${padT + innerH} L ${pb[0].x} ${padT + innerH} Z`
  const idA = 'dualA' + Math.random().toString(36).slice(2, 6)
  const idB = 'dualB' + Math.random().toString(36).slice(2, 6)

  return (
    <div className="nw-chart nw-chart-stack" style={{ ['--chart-h' as string]: `${height + 24}px` }}>
      <div className="nw-legend">
        <span><i style={{ background: colorA }} />{labelA}</span>
        <span><i style={{ background: colorB }} />{labelB}</span>
      </div>
      <svg viewBox={`0 0 ${w} ${h}`} className="nw-svg" preserveAspectRatio="none" role="img">
        <defs>
          <linearGradient id={idA} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={colorA} stopOpacity="0.32" />
            <stop offset="100%" stopColor={colorA} stopOpacity="0" />
          </linearGradient>
          <linearGradient id={idB} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={colorB} stopOpacity="0.28" />
            <stop offset="100%" stopColor={colorB} stopOpacity="0" />
          </linearGradient>
        </defs>
        {[0, 0.5, 1].map((t, i) => (
          <line key={i} x1={padL} x2={w - padR} y1={padT + innerH * (1 - t)} y2={padT + innerH * (1 - t)} stroke={COLORS.grid} />
        ))}
        <path d={areaA} fill={`url(#${idA})`} />
        <path d={areaB} fill={`url(#${idB})`} />
        <path d={pathA} fill="none" stroke={colorA} strokeWidth="2" />
        <path d={pathB} fill="none" stroke={colorB} strokeWidth="2" />
      </svg>
    </div>
  )
}

/**
 * Nightwatch-style dual line chart (e.g. AVG gray + MAX orange).
 * No fill — clean lines matching https://i.imgur.com/wiCFura.png right panel.
 */
export function DualLineChart({
  a, b, labelA = 'AVG', labelB = 'MAX', height = 140,
  colorA = COLORS.nwAvg, colorB = COLORS.nwMax,
}: {
  a: SeriesPoint[]
  b: SeriesPoint[]
  labelA?: string
  labelB?: string
  height?: number
  colorA?: string
  colorB?: string
}) {
  const n = Math.max(a.length, b.length, 24)
  const sa = padSeries(a, n)
  const sb = padSeries(b, n)
  const w = 640
  const h = height
  const padL = 2, padR = 2, padT = 8, padB = 18
  const max = Math.max(1, ...sa.map(d => d.y), ...sb.map(d => d.y))
  const innerW = w - padL - padR
  const innerH = h - padT - padB
  const pa = toPts(sa, padL, padT, innerW, innerH, max)
  const pb = toPts(sb, padL, padT, innerW, innerH, max)
  const labels = sa.filter(d => d.x).length ? sa : sb
  const step = Math.max(1, Math.ceil(labels.length / 4))

  return (
    <div className="nw-chart" style={{ ['--chart-h' as string]: `${height}px` }}>
      <svg viewBox={`0 0 ${w} ${h}`} className="nw-svg" preserveAspectRatio="none" role="img" aria-label={`${labelA} / ${labelB}`}>
        <path d={smoothPath(pa)} fill="none" stroke={colorA} strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" />
        <path d={smoothPath(pb)} fill="none" stroke={colorB} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        {labels.filter((_, i) => i % step === 0 || i === labels.length - 1).map((d, i) => {
          const idx = labels.indexOf(d)
          const px = padL + (labels.length === 1 ? innerW / 2 : (idx / (labels.length - 1)) * innerW)
          return (
            <text key={i} x={px} y={h - 4} textAnchor="middle" fill={COLORS.text} fontSize="9">
              {(d.x || '').toString().slice(-11)}
            </text>
          )
        })}
      </svg>
    </div>
  )
}

/**
 * Stacked vertical bars — Nightwatch signature chart
 * (REQUESTS 1xx/4xx/5xx, EXCEPTIONS handled/unhandled).
 */
export function StackedBarChart({
  bars,
  colors,
  height = 140,
  gap = 2.5,
}: {
  bars: StackedBar[]
  /** segment key → color */
  colors: Record<string, string>
  height?: number
  gap?: number
}) {
  const data = bars.length >= 12 ? bars : (() => {
    const out = [...bars]
    while (out.length < 24) out.unshift({ x: '', segments: Object.keys(colors).map(k => ({ key: k, y: 0 })) })
    return out
  })()
  if (!data.length) return <EmptyChart />

  const w = 640
  const h = height
  const padL = 0, padR = 0, padT = 4, padB = 16
  const innerW = w - padL - padR
  const innerH = h - padT - padB
  const max = Math.max(1, ...data.map(b => b.segments.reduce((s, seg) => s + seg.y, 0)))
  const barW = Math.max(2, (innerW / data.length) - gap)
  const keys = Object.keys(colors)
  const step = Math.max(1, Math.ceil(data.length / 4))

  return (
    <div className="nw-chart" style={{ ['--chart-h' as string]: `${height}px` }}>
      <svg viewBox={`0 0 ${w} ${h}`} className="nw-svg" preserveAspectRatio="none" role="img" aria-label="Stacked bar chart">
        {data.map((bar, i) => {
          const total = bar.segments.reduce((s, seg) => s + seg.y, 0)
          const bx = padL + (i / data.length) * innerW + gap / 2
          let yCursor = padT + innerH
          const segs = keys.map(k => {
            const found = bar.segments.find(s => s.key === k)
            return { key: k, y: found?.y || 0 }
          })
          return (
            <g key={i}>
              {segs.map((seg) => {
                const bh = (seg.y / max) * innerH
                yCursor -= bh
                if (bh < 0.5) return null
                return (
                  <rect
                    key={seg.key}
                    x={bx}
                    y={yCursor}
                    width={barW}
                    height={Math.max(bh, 1)}
                    rx={0}
                    fill={colors[seg.key] || COLORS.gray}
                  >
                    <title>{`${bar.x} ${seg.key}: ${seg.y}`}</title>
                  </rect>
                )
              })}
              {total === 0 && (
                <rect x={bx} y={padT + innerH - 1} width={barW} height={1} fill="rgba(255,255,255,0.04)" />
              )}
              {(i % step === 0 || i === data.length - 1) && bar.x && (
                <text x={bx + barW / 2} y={h - 3} textAnchor="middle" fill={COLORS.text} fontSize="9">
                  {bar.x.toString().slice(-11)}
                </text>
              )}
            </g>
          )
        })}
      </svg>
    </div>
  )
}

/** Thin multi-color bar sparkline (JOB ATTEMPTS style). */
export function MiniBarSpark({
  values,
  colors = [COLORS.nwOk, COLORS.nwWarn, COLORS.nwErr],
}: {
  values: number[]
  colors?: string[]
}) {
  if (!values?.length) return null
  const w = 120, h = 36
  const max = Math.max(1, ...values)
  const gap = 1.5
  const barW = Math.max(1.5, (w / values.length) - gap)
  return (
    <svg viewBox={`0 0 ${w} ${h}`} className="nw-spark" preserveAspectRatio="none" aria-hidden>
      {values.map((v, i) => {
        const bh = Math.max(1, (v / max) * (h - 2))
        return (
          <rect
            key={i}
            x={i * (barW + gap)}
            y={h - bh}
            width={barW}
            height={bh}
            rx={0.5}
            fill={colors[i % colors.length]}
            opacity={0.9}
          />
        )
      })}
    </svg>
  )
}

export { fmtCompact }

export function Sparkline({ data, color = COLORS.purple }: { data: number[]; color?: string }) {
  if (!data?.length) return null
  const w = 120, h = 36
  const max = Math.max(1, ...data)
  const pts = data.map((v, i) => ({
    x: data.length === 1 ? w / 2 : (i / (data.length - 1)) * w,
    y: h - 2 - (v / max) * (h - 4),
  }))
  const line = smoothPath(pts)
  const area = `${line} L ${pts[pts.length - 1].x} ${h} L ${pts[0].x} ${h} Z`
  const id = 'sp' + Math.random().toString(36).slice(2, 7)
  return (
    <svg viewBox={`0 0 ${w} ${h}`} className="nw-spark" preserveAspectRatio="none" aria-hidden>
      <defs>
        <linearGradient id={id} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity="0.4" />
          <stop offset="100%" stopColor={color} stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d={area} fill={`url(#${id})`} />
      <path d={line} fill="none" stroke={color} strokeWidth="1.75" />
    </svg>
  )
}

export function HBarList({ items, nameKey, valueKey }: { items: any[]; nameKey: string; valueKey: string }) {
  if (!items?.length) return <EmptyChart />
  const max = Math.max(1, ...items.map(i => Number(i[valueKey]) || 0))
  return (
    <div className="hbar">
      {items.map((it, i) => {
        const v = Number(it[valueKey]) || 0
        return (
          <div key={i} className="hbar-row">
            <span className="meta" style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{it[nameKey]}</span>
            <div className="hbar-track">
              <div className="hbar-fill" style={{ width: `${(v / max) * 100}%`, background: PALETTE[i % PALETTE.length] }} />
            </div>
            <span className="mono">{v}</span>
          </div>
        )
      })}
    </div>
  )
}

export function Donut({ items, nameKey, valueKey }: { items: any[]; nameKey: string; valueKey: string }) {
  if (!items?.length) return <EmptyChart />
  const total = items.reduce((s, it) => s + (Number(it[valueKey]) || 0), 0) || 1
  let acc = 0
  const stops = items.map((it, i) => {
    const v = Number(it[valueKey]) || 0
    const start = (acc / total) * 100
    acc += v
    const end = (acc / total) * 100
    return `${PALETTE[i % PALETTE.length]} ${start}% ${end}%`
  }).join(', ')
  return (
    <div className="donut-row">
      <div className="nw-donut" style={{ background: `conic-gradient(${stops})` }} role="img" aria-label="Distribution">
        <div className="nw-donut-hole">
          <strong>{total}</strong>
          <span>total</span>
        </div>
      </div>
      <div className="donut-legend">
        {items.map((it, i) => (
          <div key={i} className="legend-item">
            <span className="swatch" style={{ background: PALETTE[i % PALETTE.length] }} />
            <span>{it[nameKey]}</span>
            <strong style={{ marginLeft: 'auto', color: 'var(--text)' }}>{it[valueKey]}</strong>
          </div>
        ))}
      </div>
    </div>
  )
}

export function BarChart({ data, labelKey = 'date', valueKey = 'count', color }: {
  data: any[]; labelKey?: string; valueKey?: string; color?: string
}) {
  const series = (data || []).map(d => ({ x: String(d[labelKey] || ''), y: Number(d[valueKey]) || 0 }))
  const fid = 'g' + Math.random().toString(36).slice(2, 6)
  return <AreaChart series={series} color={color === 'cyan' ? COLORS.cyan : COLORS.purple} fillId={fid} />
}

function EmptyChart() {
  return <div className="nw-empty">No data in this window yet</div>
}

export function sevClass(s: string) {
  const x = (s || '').toLowerCase()
  if (x === 'fatal') return 'fatal'
  if (x === 'error') return 'error'
  if (x === 'warning') return 'warning'
  if (x === 'info' || x === 'debug') return 'info'
  if (x === 'open') return 'open'
  if (x === 'resolved') return 'resolved'
  if (x === 'unhealthy') return 'unhealthy'
  if (x === 'healthy') return 'healthy'
  return ''
}

export function issueCode(id: string) {
  const s = (id || '').replace(/-/g, '').slice(0, 5).toUpperCase()
  return s ? `TRC-${s}` : 'TRC-?????'
}

export { COLORS, PALETTE }
