/**
 * Mock API + screenshot of Nightwatch-style Overview for visual preview.
 * Usage: node scripts/preview-shot.mjs  (server must be on :4173)
 */
import { chromium } from 'playwright'
import { mkdirSync } from 'fs'
import { dirname, join } from 'path'
import { fileURLToPath } from 'url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const outDir = join(__dirname, '..', 'preview-shots')
mkdirSync(outDir, { recursive: true })

function hourLabels(n = 24) {
  const out = []
  const now = new Date('2025-11-03T18:00:00Z')
  for (let i = n - 1; i >= 0; i--) {
    const d = new Date(now.getTime() - i * 3600_000)
    out.push(d.toISOString())
  }
  return out
}

function dayLabels(n = 14) {
  const out = []
  const now = new Date('2025-11-03T00:00:00Z')
  for (let i = n - 1; i >= 0; i--) {
    const d = new Date(now.getTime() - i * 86400_000)
    out.push(d.toISOString().slice(0, 10))
  }
  return out
}

const hours = hourLabels(24)
const days = dayLabels(14)

// Realistic Nightwatch-ish waves
const wave = (i, base, amp, phase = 0) =>
  Math.max(0, Math.round(base + amp * Math.sin((i + phase) * 0.55) + (i % 5 === 0 ? amp * 0.4 : 0) + Math.random() * amp * 0.15))

const severity_hourly = hours.map((hour, i) => {
  const ok = wave(i, 40, 28, 0)
  const warn = wave(i, 6, 8, 1.2)
  const err = wave(i, 4, 10, 2.1)
  return { hour, ok, warn, err }
})

const exception_hourly = hours.map((hour, i) => ({
  hour,
  handled: wave(i, 18, 14, 0.5),
  unhandled: wave(i, 8, 12, 1.8),
}))

const events_hourly = hours.map((hour, i) => ({
  hour,
  count: wave(i, 55, 35, 0.3),
}))

const events_trend = days.map((date, i) => ({ date, count: wave(i, 900, 400, 0.2) }))
const fatal_trend = days.map((date, i) => ({ date, count: wave(i, 40, 30, 1.4) }))
const issues_trend = days.map((date, i) => ({ date, count: wave(i, 12, 9, 0.7) }))

const overview = {
  project: { name: 'Firmware Fleet', slug: 'firmware-fleet' },
  devices: 1842,
  open_issues: 128,
  events_today: 124200,
  events_total: 4_820_331,
  issues_total: 961,
  regressions: 7,
  resolved_issues: 128,
  fatal_open: 12,
  unhealthy_devices: 23,
  events_trend,
  fatal_trend,
  issues_trend,
  events_hourly,
  severity_hourly,
  exception_hourly,
  severity_24h: {
    ok: severity_hourly.reduce((s, h) => s + h.ok, 0),
    warn: severity_hourly.reduce((s, h) => s + h.warn, 0),
    err: severity_hourly.reduce((s, h) => s + h.err, 0),
  },
  by_severity: [
    { severity: 'fatal', count: 42 },
    { severity: 'error', count: 186 },
    { severity: 'warning', count: 94 },
    { severity: 'info', count: 310 },
  ],
  by_architecture: [
    { name: 'cortex-m', count: 420 },
    { name: 'riscv', count: 188 },
    { name: 'xtensa', count: 96 },
    { name: 'linux', count: 54 },
  ],
  by_pipeline: [
    { pipeline: 'issue', count: 510 },
    { pipeline: 'breadcrumb', count: 120 },
    { pipeline: 'metric', count: 88 },
  ],
  by_status: [
    { status: 'open', count: 128 },
    { status: 'resolved', count: 128 },
    { status: 'ignored', count: 14 },
  ],
  top_issues: [
    {
      id: 'a1b2c3d4-0001-4000-8000-000000000001',
      title: 'HardFault: Null pointer dereference in flight_data_insert',
      severity: 'fatal',
      status: 'open',
      event_count: 482,
      affected_devices: 91,
      last_seen: new Date().toISOString(),
    },
    {
      id: 'a1b2c3d4-0002-4000-8000-000000000002',
      title: 'Watchdog reset: task_wdt_timeout on radio_worker',
      severity: 'fatal',
      status: 'open',
      event_count: 211,
      affected_devices: 44,
      last_seen: new Date(Date.now() - 120_000).toISOString(),
    },
    {
      id: 'a1b2c3d4-0003-4000-8000-000000000003',
      title: 'Stack overflow in mqtt_publish_batch',
      severity: 'error',
      status: 'open',
      event_count: 156,
      affected_devices: 28,
      last_seen: new Date(Date.now() - 300_000).toISOString(),
    },
    {
      id: 'a1b2c3d4-0004-4000-8000-000000000004',
      title: 'I2C NACK: sensor bus hung on BME280',
      severity: 'error',
      status: 'open',
      event_count: 98,
      affected_devices: 17,
      last_seen: new Date(Date.now() - 600_000).toISOString(),
    },
    {
      id: 'a1b2c3d4-0005-4000-8000-000000000005',
      title: 'OOM: heap fragmentation after OTA chunk',
      severity: 'warning',
      status: 'resolved',
      event_count: 64,
      affected_devices: 9,
      last_seen: new Date(Date.now() - 3_600_000).toISOString(),
    },
    {
      id: 'a1b2c3d4-0006-4000-8000-000000000006',
      title: 'Assert failed: ring_buf_put returned -ENOSPC',
      severity: 'error',
      status: 'open',
      event_count: 41,
      affected_devices: 12,
      last_seen: new Date(Date.now() - 7200_000).toISOString(),
    },
  ],
}

const base = process.env.PREVIEW_URL || 'http://127.0.0.1:4173'

const browser = await chromium.launch({ headless: true })
const page = await browser.newPage({ viewport: { width: 1440, height: 1100 } })

await page.route('**/api/**', async (route) => {
  const url = route.request().url()
  if (url.includes('/api/me')) {
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ email: 'demo@laststate.io', role: 'admin' }),
    })
  }
  if (url.includes('/api/overview')) {
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(overview),
    })
  }
  // default empty ok so shell doesn't hard-fail on side calls
  return route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ items: [], total: 0 }),
  })
})

await page.addInitScript(() => {
  localStorage.setItem('trace_session', 'demo-preview-token')
  localStorage.setItem('trace_project_id', '00000000-0000-4000-8000-000000000099')
})

// Capture boot splash briefly (route is mocked so it should flash then yield)
const splashPath = join(outDir, 'boot-splash.png')
await page.goto(`${base}/overview`, { waitUntil: 'domcontentloaded', timeout: 60_000 })
try {
  await page.waitForSelector('[data-testid="boot-splash"]', { timeout: 3_000 })
  await page.screenshot({ path: splashPath, fullPage: false })
} catch {
  // splash may be too fast
}

await page.waitForSelector('[data-testid="overview"]', { timeout: 30_000 })
// let charts paint + min boot delay
await page.waitForTimeout(900)

const full = join(outDir, 'overview-full.png')
const top = join(outDir, 'overview-top.png')
const mid = join(outDir, 'overview-exceptions.png')

await page.screenshot({ path: full, fullPage: true })
await page.screenshot({ path: top, fullPage: false })

// scroll to exceptions feed
await page.locator('.nw-exception-list, .nw-exc-hero').first().scrollIntoViewIfNeeded()
await page.waitForTimeout(250)
await page.screenshot({ path: mid, fullPage: false })

// issue detail with mock
await page.route('**/api/issues/**', async (route) => {
  if (route.request().method() !== 'GET') {
    return route.fulfill({ status: 200, contentType: 'application/json', body: '{}' })
  }
  const issueDetail = {
    issue: {
      id: 'a1b2c3d4-0001-4000-8000-000000000001',
      title: 'HardFault: Null pointer dereference in flight_data_insert',
      severity: 'fatal', status: 'open', fingerprint: 'fp-demo',
      event_count: 482, affected_devices: 91, regression_count: 1,
      probable_cause: 'Null deref in flight_data_insert',
      first_seen: new Date().toISOString(), last_seen: new Date().toISOString(),
    },
    events: [{
      id: '11111111-1111-1111-1111-111111111111',
      event_id: 'evt-demo-1', severity: 'fatal',
      frames: JSON.stringify([
        { address: 0x08001234, function: 'flight_data_insert', file: 'flight.c', line: 88 },
        { address: 0x08001000, function: 'main', file: 'main.c', line: 42 },
      ]),
      analysis: JSON.stringify({
        summary: 'Null deref', unwind_method: 'dwarf-cfi',
        breadcrumbs: [
          { type: 'log', message: 'boot complete', timestamp: new Date().toISOString(), category: 'system' },
          { type: 'log', message: 'sensor init', timestamp: new Date().toISOString(), category: 'sensor' },
          { type: 'error', message: 'null ptr', timestamp: new Date().toISOString(), category: 'fault' },
        ],
      }),
    }],
    activity: [{ id: '1', action: 'created', body: 'new issue', created_at: new Date().toISOString() }],
    comments: [],
  }
  return route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(issueDetail),
  })
})

await page.goto(`${base}/issues/a1b2c3d4-0001-4000-8000-000000000001`, { waitUntil: 'networkidle', timeout: 60_000 })
await page.waitForSelector('[data-testid="issue-detail"]', { timeout: 30_000 })
await page.waitForTimeout(500)
const issueShot = join(outDir, 'issue-detail.png')
await page.screenshot({ path: issueShot, fullPage: true })

console.log('Wrote:')
console.log(' ', splashPath)
console.log(' ', full)
console.log(' ', top)
console.log(' ', mid)
console.log(' ', issueShot)
await browser.close()
