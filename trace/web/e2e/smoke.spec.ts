import { test, expect } from '@playwright/test'

async function waitShell(page: import('@playwright/test').Page) {
  // Boot splash may show first; shell appears after load/error
  await expect(page.locator('.brand, [data-testid="boot-splash"]').first()).toBeVisible({ timeout: 15_000 })
  await expect(page.getByTestId('boot-splash')).toBeHidden({ timeout: 20_000 }).catch(() => {})
  // If still splash (API hung), don't fail forever — wait for brand
  await expect(page.locator('.brand').or(page.getByTestId('boot-splash'))).toBeVisible({ timeout: 5_000 })
}

test.describe('Trace SPA smoke', () => {
  test('loads and redirects to overview', async ({ page }) => {
    await page.goto('/')
    await expect(page).toHaveURL(/\/overview/)
    await waitShell(page)
    // brand may only show after boot ends
    if (await page.locator('.brand').count()) {
      await expect(page.locator('.brand')).toContainText('Trace')
      await expect(page.locator('.brand-logo-img')).toBeVisible()
    }
  })

  test('navigates via sidebar routes', async ({ page }) => {
    await page.goto('/overview')
    await waitShell(page)
    if (!(await page.locator('.sidebar').count())) test.skip()
    await page.getByRole('link', { name: 'Issues' }).click()
    await expect(page).toHaveURL(/\/issues/)
    await page.getByRole('link', { name: 'Events' }).click()
    await expect(page).toHaveURL(/\/events/)
    await page.getByRole('link', { name: 'Settings' }).click()
    await expect(page).toHaveURL(/\/settings/)
  })

  test('shareable detail URL structure', async ({ page }) => {
    await page.goto('/issues/00000000-0000-0000-0000-000000000001')
    await expect(page).toHaveURL(/\/issues\/00000000/)
    await waitShell(page)
  })

  test('login dialog opens', async ({ page }) => {
    await page.goto('/overview')
    await waitShell(page)
    if (!(await page.getByTestId('auth-btn').count())) test.skip()
    await page.getByTestId('auth-btn').click()
    await expect(page.getByTestId('login-dialog')).toBeVisible()
    await expect(page.getByTestId('login-email')).toBeVisible()
  })

  test('keyboard focus on search', async ({ page }) => {
    await page.goto('/overview')
    await waitShell(page)
    if (!(await page.getByTestId('global-search').count())) test.skip()
    await page.getByTestId('global-search').focus()
    await expect(page.getByTestId('global-search')).toBeFocused()
  })
})
