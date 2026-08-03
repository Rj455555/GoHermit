import { expect, test } from '@playwright/test'

const declaredRoutes = [
  '/dashboard',
  '/employees',
  '/employees/employee-docker',
  '/tasks',
  '/agent',
  '/loops',
  '/loops/loop-docker',
  '/settings',
]

test('container serves the localized React workbench on every declared route', async ({ page }) => {
  for (const route of declaredRoutes) {
    await page.goto(route)
    await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
    await expect(page.locator('main > *').first(), route).toBeAttached()
    await expect(page.getByTestId('placeholder-page')).toHaveCount(0)
    await page.reload()
    await expect(page.locator('main > *').first(), `${route} after refresh`).toBeAttached()
  }

  await page.getByRole('button', { name: /English/u }).click()
  await expect(page.locator('html')).toHaveAttribute('lang', 'en-US')
  await page.getByRole('button', { name: /中文/u }).click()
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
})

test('container CSP permits Ant Design styles without weakening scripts', async ({ page }) => {
  const violations: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error' && message.text().includes('Content Security Policy')) {
      violations.push(message.text())
    }
  })

  const response = await page.goto('/dashboard')
  await expect(page.locator('main > *').first()).toBeAttached()
  const csp = response?.headers()['content-security-policy'] ?? ''
  expect(csp).toContain("script-src 'self'")
  expect(csp).not.toContain("script-src 'self' 'unsafe-inline'")
  expect(csp).not.toContain("'unsafe-eval'")
  expect(csp).toContain("style-src 'self' 'unsafe-inline'")
  expect(violations).toEqual([])
})

test('container navigation restores URL state through browser history', async ({ page }) => {
  await page.goto('/dashboard')
  await page.locator('a[href="/employees"]').click()
  await page.locator('a[href="/tasks"]').click()
  await page.goBack()
  await expect(page).toHaveURL(/\/employees$/)
  await page.goForward()
  await expect(page).toHaveURL(/\/tasks$/)
})

test('container keeps API and path failures outside the SPA boundary', async ({ request }) => {
  for (const path of [
    '/api',
    '/api/',
    '/api/unknown',
    '/unknown',
    '/employees/employee-docker/extra',
    '/assets/missing.js',
    '/dist/index.html',
  ]) {
    const response = await request.get(path, { maxRedirects: 0 })
    expect(response.status(), path).toBe(404)
    expect(response.headers()['content-type'] ?? '', path).not.toContain('text/html')
    expect(await response.text(), path).not.toContain('<div id="root"></div>')
  }
})
