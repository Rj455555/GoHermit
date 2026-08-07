import { expect, test } from '@playwright/test'

test.beforeEach(async ({ request }) => {
  await request.post('/__test__/reset')
})

test('Agent shows only owner conversations while Reports projects the Weixin exchange', async ({
  page,
  request,
}) => {
  const sessionsResponse = await request.get('/api/sessions?limit=100&kind=interactive')
  expect(sessionsResponse.status()).toBe(200)
  const sessionProjection = await sessionsResponse.json()
  expect(sessionProjection.sessions.every((session: { kind: string }) => session.kind === 'interactive')).toBe(true)

  await page.goto('/agent')
  const drawerTrigger = page.getByRole('button', { name: /打开会话抽屉/u })
  if (await drawerTrigger.isVisible()) await drawerTrigger.click()
  await expect(page.getByRole('link', { name: /Phase 3 Session/u })).toBeVisible()
  await expect(page.getByText('Scheduled Employee Session')).toHaveCount(0)

  await page.goto('/reports')
  await page.getByRole('tab', { name: /微信对话/u }).click()
  await expect(page.getByText('微信里发来的任务请求')).toBeVisible()
  await expect(page.getByText('GoHermit 已完成并回传结果')).toBeVisible()
  await expect(page.getByText('peer-secret-1234')).toHaveCount(0)
  await expect(page.getByRole('link', { name: /task-queued/u })).toHaveCount(2)
})
