import { expect, test } from '@playwright/test'

test.beforeEach(async ({ request }) => {
  await request.post('/__test__/reset')
})

test('Dashboard renders authoritative readiness, loops, and active projections', async ({ page }) => {
  await page.goto('/dashboard')

  await expect(page.getByRole('heading', { name: '仪表盘' })).toBeVisible()
  await expect(page.getByText('/test/workspace')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Daily review' })).toBeVisible()
})

test('Settings keeps credentials transient and requires confirmation to delete', async ({ page }) => {
  await page.goto('/settings')

  await expect(page.getByRole('heading', { name: '设置' })).toBeVisible()
  await expect(page.getByLabel('显示名称')).toHaveValue('Phase 3 Owner')
  await page.locator('.settings-provider-option').filter({ hasText: 'OpenAI API' }).click()
  const password = page.getByLabel('OpenAI API API Key')
  await password.fill('e2e-transient-secret')
  await page.getByRole('button', { name: '保存 API Key' }).click()
  await expect(password).toHaveValue('')
  expect(await page.evaluate(() => Object.keys(localStorage).every((key) => !/key|token|credential/i.test(key)))).toBe(true)

  const deleteButtons = page.getByRole('button', { name: '删除凭据' })
  await deleteButtons.last().click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await page.getByRole('button', { name: '确认' }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
})

test('Settings completes one Codex login poll and refreshes readiness', async ({ page, request }) => {
  await request.post('/__test__/codex-unconfigured')
  await page.goto('/settings')

  const codexOption = page.locator('.settings-provider-option').filter({ hasText: 'Codex' })
  await codexOption.click()
  const codexPanel = page.locator('.settings-provider-config')
  await codexPanel.locator('button.button--primary').click()
  await expect.poll(
    async () => (await (await request.get('/__test__/state')).json()).loginPolls,
    { timeout: 8_000 },
  ).toBe(1)
  await expect(codexPanel.locator('button.button--danger')).toBeVisible()
})

test('Agent creates a Session only from an explicit submission and restores it through the URL', async ({
  page,
}) => {
  await page.goto('/agent')
  await expect(page.getByLabel('接入方式')).toHaveValue('openai-codex')
  await page.getByLabel('标题').fill('URL-owned Session')
  await page.getByRole('button', { name: '新建会话' }).last().click()

  await expect(page).toHaveURL(/\/agent\/sessions\/session-\d+$/u)
  await expect(page.getByRole('heading', { name: 'URL-owned Session' })).toBeVisible()
  expect(await page.evaluate(() => localStorage.getItem('gohermit.session'))).toBeNull()
})

test('Agent restores from URL, shares one Session EventSource, and streams model deltas', async ({
  page,
  request,
}) => {
  await page.goto('/agent/sessions/session-1')
  await expect(page.getByRole('heading', { name: 'Phase 3 Session' })).toBeVisible()
  await expect.poll(async () => (await (await request.get('/__test__/state')).json()).activeSSE).toBe(1)

  const composer = page.getByLabel('消息')
  await composer.fill('preserve this user text')
  await composer.press('Control+Enter')
  await expect(composer).toHaveValue('')
  await expect(page.getByTestId('streaming-bubble')).toContainText('streamed answer')
  const timeline = page.locator('.message-timeline')
  await expect(timeline.getByText('preserve this user text').last()).toBeVisible()
  await expect(timeline.getByText('streamed answer', { exact: true }).first()).toBeVisible()

  const state = await (await request.get('/__test__/state')).json()
  expect(state.maxSSE).toBe(1)
  expect(state.runStarts).toBe(1)
  expect(state.urls.every((url: string) => /\/api\/sessions\/session-1\/events\?after=\d+$/u.test(url))).toBe(true)
})

test('queued Review Plan binds approval and cancellation to the active Run and disables Composer', async ({
  page,
  request,
}) => {
  await request.post('/__test__/queued-review')
  await page.goto('/agent/sessions/session-1')

  await expect(page.getByRole('heading', { name: 'Queued Review Session' })).toBeVisible()
  await expect(page.getByLabel('消息')).toBeDisabled()
  await expect(page.getByRole('button', { name: '批准计划' })).toBeVisible()
  await expect(page.getByRole('button', { name: '取消运行' })).toBeVisible()

  await page.getByRole('button', { name: '批准计划' }).click()
  await expect(page.getByRole('button', { name: '批准计划' })).toHaveCount(0)
  const approved = await (await request.get('/api/sessions/session-1')).json()
  expect(approved.session.runs[0].plan_approved).toBe(true)

  await request.post('/__test__/queued-review')
  await page.reload()
  await page.getByRole('button', { name: '取消运行' }).click()
  await expect(page.getByLabel('消息')).toBeEnabled()
  const cancelled = await (await request.get('/api/sessions/session-1')).json()
  expect(cancelled.session.active_run_id).toBeUndefined()
  expect(cancelled.session.runs[0].status).toBe('cancelled')
})

test('terminal Run history never exposes mutation actions', async ({ page, request }) => {
  await request.post('/__test__/terminal-history')
  await page.goto('/agent/sessions/session-1')

  await expect(page.getByRole('heading', { name: 'Terminal History Session' })).toBeVisible()
  await expect(page.getByText('已完成').first()).toBeVisible()
  await expect(page.getByRole('button', { name: '批准计划' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '取消运行' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '恢复运行' })).toHaveCount(0)
  await expect(page.getByLabel('消息')).toBeEnabled()
})

test('Composer enforces the exact 16 KiB UTF-8 byte boundary', async ({ page }) => {
  await page.goto('/agent/sessions/session-1')
  const composer = page.getByLabel('消息')
  const send = page.getByRole('button', { name: '发送' })

  await composer.fill('a'.repeat(16 << 10))
  await expect(page.getByText(`${16 << 10} / ${16 << 10}`)).toBeVisible()
  await expect(send).toBeEnabled()

  await composer.fill(`中${'a'.repeat((16 << 10) - 2)}`)
  await expect(page.getByText(`${(16 << 10) + 1} / ${16 << 10}`)).toBeVisible()
  await expect(send).toBeDisabled()
})

test('refresh resumes the Session high-water without creating Run-scoped EventSources', async ({
  page,
  request,
}) => {
  await page.goto('/agent/sessions/session-1')
  await expect(page.getByRole('heading', { name: 'Phase 3 Session' })).toBeVisible()
  await expect.poll(async () => (await (await request.get('/__test__/state')).json()).activeSSE).toBe(1)
  const composer = page.getByLabel('消息')
  await composer.fill('advance the durable cursor')
  await composer.press('Control+Enter')
  await expect(page.locator('.message-timeline').getByText('streamed answer', { exact: true })).toBeVisible()
  await expect.poll(
    async () => page.evaluate(() => localStorage.getItem('gohermit.ui.sseSequence.session-1')),
  ).toBe('2')

  await page.reload()
  await expect(page.getByRole('heading', { name: 'Phase 3 Session' })).toBeVisible()
  await expect.poll(async () => (await (await request.get('/__test__/state')).json()).activeSSE).toBe(1)
  const state = await (await request.get('/__test__/state')).json()
  expect(state.urls.at(-1)).toMatch(/\/events\?after=2$/u)
  expect(state.urls.some((url: string) => url.includes('run_id='))).toBe(false)
})

test('an active SSE disconnect reconnects once and preserves the Session projection', async ({
  page,
  request,
}) => {
  await page.goto('/agent/sessions/session-1')
  await expect(page.getByRole('heading', { name: 'Phase 3 Session' })).toBeVisible()
  await expect.poll(async () => (await (await request.get('/__test__/state')).json()).activeSSE).toBe(1)
  const composer = page.getByLabel('消息')
  await composer.fill('reconnect from the current high-water')
  await composer.press('Control+Enter')
  await expect(page.locator('.message-timeline').getByText('streamed answer', { exact: true })).toBeVisible()
  await expect.poll(
    async () => page.evaluate(() => localStorage.getItem('gohermit.ui.sseSequence.session-1')),
  ).toBe('2')

  await request.post('/__test__/disconnect/session-1')
  await expect.poll(
    async () => (await (await request.get('/__test__/state')).json()).urls.length,
    { timeout: 8_000 },
  ).toBeGreaterThanOrEqual(2)
  await expect.poll(async () => (await (await request.get('/__test__/state')).json()).activeSSE).toBe(1)
  await expect(page.getByRole('heading', { name: 'Phase 3 Session' })).toBeVisible()

  const state = await (await request.get('/__test__/state')).json()
  expect(state.urls.at(-1)).toMatch(/\/events\?after=\d+$/u)
  expect(state.lastEventIDs.at(-1)).toBe('2')
  expect(state.maxSSE).toBe(1)
})

test('an approval decision is submitted once and refreshed out of the pending projection', async ({
  page,
  request,
}) => {
  await page.goto('/agent/sessions/session-1')
  const approval = page.locator('.approval-card')
  await expect(approval).toBeVisible()

  await approval.locator('button.button--primary').click()
  await expect(approval).toHaveCount(0)
  const state = await (await request.get('/__test__/state')).json()
  expect(state.approvalDecisions).toBe(1)
})
