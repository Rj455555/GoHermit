import { expect, test } from '@playwright/test'

const routes = [
  '/dashboard',
  '/employees',
  '/employees/employee-1',
  '/tasks',
  '/tasks/task-1',
  '/agent',
  '/agent/sessions/session-1',
  '/loops',
  '/loops/loop-1',
  '/loops/loop-1/invocations/invocation-1',
  '/settings',
]

test('all declared React routes support direct access and refresh', async ({ page }) => {
  for (const route of routes) {
    await page.goto(route)
    await expect(page.getByTestId('placeholder-page')).toBeVisible()
    await page.reload()
    await expect(page.getByTestId('placeholder-page')).toBeVisible()
  }
})

test('root redirect and browser back/forward follow the URL', async ({ page }) => {
  await page.goto('/')
  await expect(page).toHaveURL(/\/dashboard$/)
  await page.getByRole('link', { name: '电子员工' }).click()
  await page.getByRole('link', { name: '任务' }).click()
  await page.goBack()
  await expect(page).toHaveURL(/\/employees$/)
  await expect(page.getByRole('link', { name: '电子员工' })).toHaveAttribute(
    'aria-current',
    'page',
  )
  await page.goForward()
  await expect(page).toHaveURL(/\/tasks$/)
})

test('unknown React routes render localized Not Found', async ({ page }) => {
  await page.goto('/not-a-declared-route')
  await expect(page.getByRole('heading', { name: '页面未找到' })).toBeVisible()
})
