import { expect, test } from '@playwright/test'

test.beforeEach(async ({ request }) => {
  await request.post('/__test__/reset')
})

test('archived Employee is direct-loadable and fully read-only', async ({ page }) => {
  await page.goto('/employees/employee-ada')
  await expect(page.getByTestId('employee-status')).toBeVisible()
  await expect(page.getByRole('button', { name: /保存|Save|停用|Disable|启用|Enable|归档|Archive/u })).toHaveCount(0)
  await page.reload()
  await expect(page.getByTestId('employee-status')).toBeVisible()
})

test('locale switching translates Phase 4 status metadata but preserves authoritative text', async ({ page }) => {
  await page.goto('/employees/employee-ada')
  await expect(page.getByTestId('employee-status')).toHaveText('已归档')
  await expect(page.getByRole('heading', { name: 'Ada' })).toBeVisible()
  await expect(page.getByText(/employee-ada/u)).toBeVisible()
  await page.getByRole('button', { name: /English/u }).click()
  await expect(page.getByTestId('employee-status')).toHaveText('Archived')
  await expect(page.getByRole('heading', { name: 'Ada' })).toBeVisible()
  await expect(page.getByText(/employee-ada/u)).toBeVisible()

  await page.goto('/tasks/task-queued')
  await expect(page.getByRole('heading', { name: 'Prepare release.' })).toBeVisible()
  await expect(page.getByTestId('task-status')).toHaveText('Queued')
})

test('queued Task requires explicit Prepare then Start and restores through history', async ({ page }) => {
  await page.goto('/tasks/task-queued')
  await expect(page.getByTestId('task-status')).toBeVisible()
  await expect(page.getByRole('button', { name: /启动|Start/u })).toHaveCount(0)
  await page.getByRole('button', { name: /准备|Prepare/u }).click()
  await page.getByRole('button', { name: /启动|Start/u }).click()
  await expect(page.getByTestId('task-status')).not.toContainText(/queued|排队/u)
  await page.goBack()
  await page.goForward()
  await expect(page).toHaveURL(/\/tasks\/task-queued$/)
})

test('Loop Definition keeps revision, requires Dry Run, and restores Invocation timeline', async ({ page }) => {
  await page.goto('/loops/daily-review')
  await expect(page.locator('input[value="Daily review"]')).toBeVisible()
  await expect(page.getByRole('button', { name: /启动|Start/u })).toBeDisabled()
  await page.getByRole('button', { name: 'Dry Run' }).click()
  await expect(page.getByTestId('loop-dry-run')).toContainText('"ready": true')
  await expect(page.getByRole('button', { name: /启动|Start/u })).toBeEnabled()
  await page.goto('/loops/daily-review/invocations/invocation-1')
  await expect(page.getByTestId('loop-timeline')).toContainText('invocation-1')
  await page.reload()
  await expect(page.getByTestId('loop-timeline')).toContainText('session-1')
})
