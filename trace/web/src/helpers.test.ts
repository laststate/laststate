// Lightweight unit tests for Trace frontend
// Run with: npx tsx src/helpers.test.ts

import { isView } from './nav'

// Test helpers
function assert(cond: unknown, msg: string) {
  if (!cond) throw new Error(`Assertion failed: ${msg}`)
}

function assertEq<T>(actual: T, expected: T, msg: string) {
  if (actual !== expected) {
    throw new Error(`Assertion failed: ${msg} (expected ${expected}, got ${actual})`)
  }
}

// Test isView
assert(isView('overview'), 'isView overview')
assert(isView('issues'), 'isView issues')
assert(isView('events'), 'isView events')
assert(isView('devices'), 'isView devices')
assert(isView('releases'), 'isView releases')
assert(isView('artifacts'), 'isView artifacts')
assert(isView('alerts'), 'isView alerts')
assert(isView('channels'), 'isView channels')
assert(isView('relays'), 'isView relays')
assert(isView('projects'), 'isView projects')
assert(isView('hardware'), 'isView hardware')
assert(isView('boots'), 'isView boots')
assert(isView('dead'), 'isView dead')
assert(isView('audit'), 'isView audit')
assert(isView('settings'), 'isView settings')
assert(isView('analytics'), 'isView analytics')
assert(isView('compliance'), 'isView compliance')
assert(isView('billing'), 'isView billing')
assert(isView('blog'), 'isView blog')
assert(isView('faq'), 'isView faq')
assert(isView('docs'), 'isView docs')
assert(isView('pricing'), 'isView pricing')
assert(isView('fleet-health'), 'isView fleet-health')
assert(isView('device-dna'), 'isView device-dna')
assert(isView('pr'), 'isView pr')
assert(isView('chaos'), 'isView chaos')
assert(isView('lep-explorer'), 'isView lep-explorer')
assert(isView('memorial-wall'), 'isView memorial-wall')
assert(isView('public-api'), 'isView public-api')
assert(isView('anomaly'), 'isView anomaly')
assert(!isView('xyz'), 'isView xyz returns false')
assert(!isView(''), 'isView empty returns false')
assert(!isView(undefined), 'isView undefined returns false')

// Test pathFromParts
function pathFromParts(view: string, id?: string): string {
  if (!isView(view)) return '/overview'
  return id ? `/${view}/${id}` : `/${view}`
}

assertEq(pathFromParts('issues', 'abc'), '/issues/abc', 'path detail')
assertEq(pathFromParts('overview'), '/overview', 'path list')
assertEq(pathFromParts('bad'), '/overview', 'path fallback')
assertEq(pathFromParts('events', 'evt-123'), '/events/evt-123', 'path events detail')
assertEq(pathFromParts('issues'), '/issues', 'path issues list')

// Test hexDump
function hexDump(s: string): string {
  const bytes = Array.from(s).map(c => c.charCodeAt(0))
  const lines: string[] = []
  for (let i = 0; i < bytes.length; i += 16) {
    const chunk = bytes.slice(i, i + 16)
    const hex = chunk.map(b => b.toString(16).padStart(2, '0')).join(' ')
    const asc = chunk.map(b => (b >= 32 && b < 127 ? String.fromCharCode(b) : '.')).join('')
    lines.push(i.toString(16).padStart(4, '0') + '  ' + hex.padEnd(48) + '  ' + asc)
  }
  return lines.join('\n')
}

const hexResult = hexDump('AB')
assert(hexResult.includes('41 42'), 'hex dump AB')
assert(hexResult.includes('41 42'), 'hex dump AB offset')

const hexHello = hexDump('Hello')
assert(hexHello.includes('48 65 6c 6c 6f'), 'hex dump Hello')

// Test buildQuery
function buildQuery(params: Record<string, string | number | undefined>): string {
  const qs = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== '') qs.set(k, String(v))
  }
  return qs.toString()
}

assertEq(
  buildQuery({ limit: 25, offset: 0, status: 'open' }),
  'limit=25&offset=0&status=open',
  'buildQuery basic'
)
assertEq(
  buildQuery({ limit: 50, severity: 'fatal' }),
  'limit=50&severity=fatal',
  'buildQuery severity'
)
assertEq(
  buildQuery({ q: undefined, limit: 10 }),
  'limit=10',
  'buildQuery undefined'
)
assertEq(
  buildQuery({}),
  '',
  'buildQuery empty'
)

// Test pageCount
function pageCount(total: number, limit: number): number {
  if (limit <= 0) return 1
  return Math.max(1, Math.ceil(total / limit))
}

assertEq(pageCount(100, 25), 4, 'pageCount 100/25')
assertEq(pageCount(101, 25), 5, 'pageCount 101/25')
assertEq(pageCount(0, 25), 1, 'pageCount 0/25')
assertEq(pageCount(25, 25), 1, 'pageCount 25/25')
assertEq(pageCount(1, 25), 1, 'pageCount 1/25')
assertEq(pageCount(100, 0), 1, 'pageCount limit 0')

// Test fmtCompact (format large numbers)
function fmtCompact(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M'
  if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K'
  return String(n)
}

assertEq(fmtCompact(0), '0', 'fmtCompact 0')
assertEq(fmtCompact(100), '100', 'fmtCompact 100')
assertEq(fmtCompact(1000), '1.0K', 'fmtCompact 1000')
assertEq(fmtCompact(1500), '1.5K', 'fmtCompact 1500')
assertEq(fmtCompact(1000000), '1.0M', 'fmtCompact 1M')
assertEq(fmtCompact(2500000), '2.5M', 'fmtCompact 2.5M')

// Test sevClass
function sevClass(status: string): string {
  switch (status) {
    case 'open':
    case 'investigating':
      return 'warn'
    case 'resolved':
      return 'ok'
    case 'ignored':
    case 'archived':
      return 'muted'
    default:
      return ''
  }
}

assertEq(sevClass('open'), 'warn', 'sevClass open')
assertEq(sevClass('investigating'), 'warn', 'sevClass investigating')
assertEq(sevClass('resolved'), 'ok', 'sevClass resolved')
assertEq(sevClass('ignored'), 'muted', 'sevClass ignored')
assertEq(sevClass('archived'), 'muted', 'sevClass archived')
assertEq(sevClass('unknown'), '', 'sevClass unknown')

// Test issueCode (short issue ID display)
function issueCode(id: string): string {
  if (!id) return '—'
  if (id.length <= 12) return id
  return id.slice(0, 8) + '...'
}

assertEq(issueCode('abc'), 'abc', 'issueCode short')
assertEq(issueCode('a1b2c3d4e5f6a1b2'), 'a1b2c3d4...', 'issueCode long')
assertEq(issueCode(''), '—', 'issueCode empty')

// Test fmtTime
function fmtTime(v: any): string {
  if (!v) return '—'
  try { return new Date(v).toLocaleString() } catch { return String(v) }
}

assert(fmtTime('2026-08-15T10:30:00Z') !== '—', 'fmtTime valid')
assertEq(fmtTime(''), '—', 'fmtTime empty')
assertEq(fmtTime(null), '—', 'fmtTime null')
assertEq(fmtTime(undefined), '—', 'fmtTime undefined')

// Test safeJSON
function safeJSON(v: any): any {
  if (typeof v === 'string') {
    try { return JSON.parse(v) } catch { return v }
  }
  return v
}

assertEq(JSON.stringify(safeJSON('{"a":1}')), JSON.stringify({ a: 1 }), 'safeJSON valid')
assertEq(safeJSON('not json'), 'not json', 'safeJSON invalid')
assertEq(safeJSON(42), 42, 'safeJSON number')
assertEq(safeJSON(null), null, 'safeJSON null')

console.log('helpers.test.ts: all tests passed')
