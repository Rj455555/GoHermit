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
  expect(text).not.toMatch(/\b(?:navigation|routes|actions)\.[a-zA-Z.]+\b/)
})

test('React test server is independent from business APIs and Session SSE', async ({
  page,
  request,
}) => {
  const businessRequests: string[] = []
  page.on('request', (entry) => {
    if (entry.url().includes('/api/') || entry.url().includes('/events')) {
      businessRequests.push(entry.url())
    }
  })
  await page.goto('/agent/sessions/session-1')
  await expect(page.getByTestId('placeholder-page')).toBeVisible()
  expect(businessRequests).toEqual([])
  expect((await request.get('/api/health')).status()).toBe(404)
  expect((await request.get('/api/sessions/session-1/events')).status()).toBe(404)
})
