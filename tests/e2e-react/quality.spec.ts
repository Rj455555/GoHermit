import { expect, test } from '@playwright/test'

test('200% zoom keeps controls accessible and shell copy is clean', async ({ page }) => {
  await page.goto('/dashboard')
  await page.evaluate(() => {
    document.documentElement.style.zoom = '2'
  })
  await expect(page.getByRole('button', { name: '收起主导航' })).toBeVisible()
  const text = await page.locator('body').innerText()
  expect(text).not.toContain('undefined')
  expect(text).not.toContain('V0.6')
  expect(text).not.toMatch(/\b(?:navigation|routes|actions|connectivity)\.[a-zA-Z.]+\b/)
})

test('React test server exposes only the Phase 3 fixture API surface', async ({ request }) => {
  expect((await request.get('/api/health')).status()).toBe(200)
  expect((await request.get('/api/sessions/session-1')).status()).toBe(200)
  expect((await request.get('/api/run')).status()).toBe(404)
  expect((await request.get('/api/unknown')).status()).toBe(404)
})
