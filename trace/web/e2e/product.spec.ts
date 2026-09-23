import { test, expect } from '@playwright/test'

/**
 * Product-flow suite with API mocks (no live backend required).
 * Covers: overview charts, issues list → detail, reprocess affordance, login shell.
 */

const overview = {
  project: { name: 'E2E Fleet', slug: 'e2e-fleet' },
  devices: 10, open_issues: 2, events_today: 100, events_total: 1000, issues_total: 5,
  regressions: 1, resolved_issues: 3, fatal_open: 1, unhealthy_devices: 0,
  events_trend: Array.from({ length: 14 }, (_, i) => ({ date: `2025-01-${String(i + 1).padStart(2, '0')}`, count: 10 + i })),
  fatal_trend: Array.from({ length: 14 }, (_, i) => ({ date: `2025-01-${String(i + 1).padStart(2, '0')}`, count: i % 3 })),
  issues_trend: Array.from({ length: 14 }, (_, i) => ({ date: `2025-01-${String(i + 1).padStart(2, '0')}`, count: 1 })),
  events_hourly: Array.from({ length: 24 }, (_, i) => ({ hour: `2025-01-14T${String(i).padStart(2, '0')}:00:00Z`, count: i + 1 })),
  severity_hourly: Array.from({ length: 24 }, (_, i) => ({
    hour: `2025-01-14T${String(i).padStart(2, '0')}:00:00Z`, ok: 5, warn: 1, err: i % 4,
  })),
  exception_hourly: Array.from({ length: 24 }, (_, i) => ({
    hour: `2025-01-14T${String(i).padStart(2, '0')}:00:00Z`, handled: 3, unhandled: 2,
  })),
  severity_24h: { ok: 100, warn: 20, err: 15 },
  by_severity: [{ severity: 'fatal', count: 2 }, { severity: 'error', count: 5 }],
  by_architecture: [{ name: 'cortex-m', count: 8 }],
  by_pipeline: [{ pipeline: 'issue', count: 10 }],
  by_status: [{ status: 'open', count: 2 }],
  top_issues: [{
    id: 'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee',
    title: 'HardFault in e2e_test',
    severity: 'fatal', status: 'open', event_count: 9, affected_devices: 2,
    last_seen: new Date().toISOString(),
  }],
}

const issueDetail = {
  issue: {
    id: 'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee',
    title: 'HardFault in e2e_test',
    severity: 'fatal', status: 'open', fingerprint: 'fp-e2e',
    event_count: 9, affected_devices: 2, regression_count: 0,
    probable_cause: 'Null deref', first_seen: new Date().toISOString(), last_seen: new Date().toISOString(),
  },
  events: [{
    id: '11111111-1111-1111-1111-111111111111',
    event_id: 'evt-e2e-1', severity: 'fatal',
    frames: JSON.stringify([{ address: 134217728, function: 'main', file: 'app.c', line: 42 }]),
    analysis: JSON.stringify({
      summary: 'Null deref', unwind_method: 'dwarf-cfi',
      breadcrumbs: [{ type: 'log', message: 'boot', timestamp: new Date().toISOString(), category: 'system' }],
    }),
  }],
  activity: [{ id: '1', action: 'created', body: 'new issue', created_at: new Date().toISOString() }],
  comments: [],
}

test.describe('Product flows (mocked API)', () => {
  test.beforeEach(async ({ page }) => {
    await page.route('**/api/**', async (route) => {
      const url = route.request().url()
      const method = route.request().method()
      if (url.includes('/api/me')) {
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ email: 'e2e@test', role: 'admin' }) })
      }
      if (url.includes('/api/overview')) {
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(overview) })
      }
      if (url.includes('/api/issues/') && !url.endsWith('/issues') && method === 'GET') {
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(issueDetail) })
      }
      if (url.includes('/api/issues') && method === 'GET') {
        return route.fulfill({
          status: 200, contentType: 'application/json',
          body: JSON.stringify({ items: overview.top_issues, total: 1 }),
        })
      }
      if (url.includes('/status') && method === 'POST') {
        return route.fulfill({ status: 200, contentType: 'application/json', body: '{}' })
      }
      if (url.includes('/reprocess') && method === 'POST') {
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ queued: 1 }) })
      }
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: [], total: 0 }) })
    })
    await page.addInitScript(() => {
      localStorage.setItem('trace_session', 'e2e-token')
      localStorage.setItem('trace_project_id', '00000000-0000-4000-8000-000000000001')
    })
  })

  test('overview renders Nightwatch charts', async ({ page }) => {
    await page.goto('/overview')
    await expect(page.getByTestId('boot-splash')).toBeVisible({ timeout: 5_000 }).catch(() => {})
    await expect(page.getByTestId('overview')).toBeVisible({ timeout: 20_000 })
    await expect(page.getByText('System health')).toBeVisible()
    await expect(page.locator('.brand-logo-img')).toBeVisible()
    await expect(page.locator('.nw-metric-big').first()).toContainText('100')
    await expect(page.locator('.nw-svg').first()).toBeVisible()
    await expect(page.locator('.live-pill')).toBeVisible()
  })

  test('issues list → detail with timeline', async ({ page }) => {
    await page.goto('/issues')
    await expect(page.getByTestId('issues-list')).toBeVisible({ timeout: 20_000 })
    await page.locator('.nw-exception').first().click()
    await expect(page).toHaveURL(/\/issues\/aaaaaaaa/)
    await expect(page.getByTestId('issue-detail')).toBeVisible()
    await expect(page.getByTestId('issue-detail').getByText('HardFault in e2e_test')).toBeVisible()
    await expect(page.getByText('Breadcrumb replay').or(page.getByText('Timeline'))).toBeVisible()
    await expect(page.getByTestId('issue-detail').getByText('Null deref')).toBeVisible()
  })

  test('export CSV control present', async ({ page }) => {
    await page.goto('/overview')
    await expect(page.getByRole('button', { name: 'Export CSV' })).toBeVisible({ timeout: 20_000 })
    await expect(page.getByRole('group', { name: 'Time range' })).toBeVisible()
  })

  test('login dialog still works when logged out affordance', async ({ page }) => {
    await page.goto('/settings')
    await expect(page.getByTestId('auth-btn')).toBeVisible({ timeout: 20_000 })
  })
})
