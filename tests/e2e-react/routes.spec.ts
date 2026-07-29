import { expect, test } from '@playwright/test'

const declaredRoutes = [
  '/dashboard',
  '/employees',
  '/employees/employee-ada',
  '/tasks',
  '/tasks/task-queued',
  '/agent',
  '/agent/sessions/session-1',
  '/loops',
  '/loops/daily-review',
  '/loops/daily-review/invocations/invocation-1',
  '/settings',
]

test('all declared React routes support direct access and refresh', async ({ page }) => {
  for (const route of declaredRoutes) {
    await page.goto(route)
    await expect(page.getByTestId('placeholder-page')).toHaveCount(0)
    await expect(page.locator('main')).not.toBeEmpty()
    await page.reload()
    await expect(page.locator('main')).not.toBeEmpty()
  }
})

test('root redirect and browser back/forward follow the URL', async ({ page }) => {
  await page.goto('/')
  await expect(page).toHaveURL(/\/dashboard$/)
  await page.locator('a[href="/employees"]').click()
  await page.locator('a[href="/tasks"]').click()
  await page.goBack()
  await expect(page).toHaveURL(/\/employees$/)
  await expect(page.locator('a[href="/employees"]')).toHaveAttribute('aria-current', 'page')
  await page.goForward()
  await expect(page).toHaveURL(/\/tasks$/)
})

test('unknown React routes render localized Not Found', async ({ page }) => {
  await page.goto('/not-a-declared-route')
  await expect(page.locator('main h1')).toBeVisible()
  await page.getByRole('button', { name: /English/u }).click()
  await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible()
})
