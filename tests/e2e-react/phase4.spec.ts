import { expect, test } from '@playwright/test'

test.beforeEach(async ({ request }) => {
  await request.post('/__test__/reset')
})

test('archived Employee is direct-loadable, structured, non-empty, and fully read-only', async ({ page }) => {
  await page.goto('/employees/employee-ada')
  await expect(page.getByTestId('employee-status')).toBeVisible()
  await expect(page.getByRole('button', { name: /保存|Save|停用|Disable|启用|Enable|归档|Archive/u })).toHaveCount(0)

  await page.getByRole('button', { name: /技能|Skills/u }).click()
  await expect(page.getByText(/native-review@1\.0\.0/u)).toBeVisible()
  await expect(page.getByText(/Digest 已过期|Digest drift/u)).toBeVisible()

  await page.getByRole('button', { name: /知识|Knowledge/u }).click()
  await expect(page.getByTestId('knowledge-source')).toContainText('Release handbook')
  await expect(page.getByTestId('knowledge-citation')).toContainText('docs/handbook.md:1-3')

  await page.getByRole('button', { name: /记忆|Memory/u }).click()
  await expect(page.getByTestId('memory-candidate')).toContainText('Run the bounded verification recipe.')
  await expect(page.locator('input[value="Always retain release evidence."]')).toBeVisible()

  await page.getByRole('button', { name: /任务|Tasks/u }).click()
  await expect(page.getByRole('link', { name: 'Prepare release.' })).toBeVisible()
  await page.getByRole('button', { name: /活动|Activity/u }).click()
  await expect(page.getByText(/Employee 已归档|Employee archived/u)).toBeVisible()

  await page.reload()
  await expect(page.getByText(/Employee 已归档|Employee archived/u)).toBeVisible()
})

test('quick create persists a conservative Employee from only a Chinese name', async ({ page }) => {
  await page.goto('/employees')
  await page.getByRole('button', { name: '创建 Employee' }).click()

  await expect(page.getByRole('heading', { name: '几秒钟创建电子员工' })).toBeVisible()
  await page.getByLabel('名称').fill('档案管理员')
  const createRequest = page.waitForRequest((request) =>
    request.url().endsWith('/api/employees') && request.method() === 'POST')
  await page.getByRole('button', { name: '立即创建' }).click()
  const body = (await createRequest).postDataJSON() as {
    employee: Record<string, unknown>
    project_bindings: Array<Record<string, unknown>>
  }

  expect(body.employee.id).toMatch(/^employee-[a-z0-9._-]+$/u)
  expect(body.employee.name).toBe('档案管理员')
  expect(body.employee.job_title).toBe('岗位待配置')
  expect(body.employee.charter).toBe('角色细节尚未配置。请在分配工作前完善这位电子员工。')
  expect(body.employee.skill_bindings).toEqual([])
  expect(body.employee.permission_policy).toEqual({
    allowed_capabilities: ['read'],
    network_allowed: false,
  })
  expect(body.employee.memory_policy).toEqual({
    candidate_generation: false,
    promotion: 'disabled',
    max_context_facts: 0,
    max_context_bytes: 0,
  })
  expect(body.project_bindings).toEqual([
    expect.objectContaining({
      mutation_allowed: false,
      network_allowed: false,
      allowed_tool_capabilities: ['read'],
    }),
  ])
  await expect(page).toHaveURL(/\/employees\/employee-[a-z0-9._-]+$/u)
  await expect(page.getByRole('heading', { name: '档案管理员' })).toBeVisible()
})

test('nine-step Employee wizard persists exact configuration and uses real Dry Run', async ({ page }) => {
  await page.goto('/employees')
  await page.getByRole('button', { name: /English/u }).click()
  await page.getByRole('button', { name: 'Create Employee' }).click()
  await page.getByRole('button', { name: 'Advanced setup' }).click()

  await expect(page.getByTestId('employee-wizard-step')).toContainText('Step 1 of 9')
  await page.getByLabel('Employee ID').fill('employee-gate')
  await page.getByLabel('Name', { exact: true }).fill('Gate Engineer')
  await page.getByLabel('Job title').fill('Verification Engineer')
  await page.getByRole('button', { name: 'Next' }).click()

  await expect(page.getByTestId('employee-wizard-step')).toContainText('Step 2 of 9')
  await page.getByRole('button', { name: 'Next' }).click()
  await page.getByLabel('Charter').fill('Keep the Phase 4 gate reproducible.')
  await page.getByLabel('Responsibilities').fill('Verify bounded changes.')
  await page.getByLabel('Behavior boundaries').fill('Never expose credentials.')
  await page.getByRole('button', { name: 'Next' }).click()

  await page.getByText(/native-review@1\.0\.0/u).click()
  await page.getByRole('button', { name: 'Next' }).click()
  await page.getByRole('button', { name: 'Next' }).click()
  await page.getByRole('button', { name: 'Next' }).click()
  await page.getByRole('button', { name: 'Next' }).click()
  await page.getByRole('button', { name: 'Next' }).click()

  await expect(page.getByTestId('employee-wizard-step')).toContainText('Step 9 of 9')
  await expect(page.getByTestId('employee-readiness')).toHaveText('Ready')
  await expect(page.getByText('Gate Engineer')).toBeVisible()
  await expect(page.getByText('GoHermit', { exact: true })).toBeVisible()
})

test('guided Employee setup generates a safe draft and sends only writable DTO fields', async ({ page }) => {
  await page.goto('/employees')
  await page.getByRole('button', { name: /English/u }).click()
  await page.getByRole('button', { name: 'Create Employee' }).click()
  await page.getByRole('button', { name: 'Advanced setup' }).click()

  await page.getByLabel('Employee name').fill('Mina')
  await page.getByLabel('What should this Employee take care of?').fill(
    'Maintain GoHermit and verify every change',
  )
  await page.getByRole('button', { name: 'Generate recommended draft' }).click()
  await expect(page.getByLabel('Job title')).toHaveValue('Software Engineer')
  await expect(page.getByText('Draft generated. You can edit every field before creating.')).toBeVisible()
  await page.getByLabel('Employee ID').fill('档案管理员')
  await expect(page.getByText(/generate a safe system ID/u)).toBeVisible()
  await page.getByRole('button', { name: 'Use recommendation and review' }).click()

  const createRequest = page.waitForRequest((request) =>
    request.url().endsWith('/api/employees') && request.method() === 'POST')
  await page.getByRole('button', { name: 'Next' }).click()
  const body = (await createRequest).postDataJSON() as {
    employee: Record<string, unknown>
  }
  expect(body.employee.project_count).toBeUndefined()
  expect(body.employee.name).toBe('Mina')
  expect(body.employee.id).toMatch(/^mina-[a-z0-9._-]+$/u)
  expect(body.employee.id).not.toContain('档案管理员')
  await expect(page.getByTestId('employee-readiness')).toHaveText('Ready')
})

test('locale switching translates Phase 4 status metadata but preserves authoritative text', async ({ page }) => {
  await page.goto('/employees/employee-ada')
  const authoritative = ['Ada', 'employee-ada', 'Ship verified releases.']
  for (const value of authoritative) await expect(page.getByText(value, { exact: false }).first()).toBeVisible()

  await page.getByRole('button', { name: /English/u }).click()
  await expect(page.getByTestId('employee-status')).toHaveText('Archived')
  for (const value of authoritative) await expect(page.getByText(value, { exact: false }).first()).toBeVisible()

  await page.goto('/tasks/task-queued')
  await expect(page.getByRole('heading', { name: 'Prepare release.' })).toBeVisible()
  await expect(page.getByTestId('task-status')).toHaveText('Queued')
  await expect(page.getByText('reports/release.txt', { exact: false })).toBeVisible()
})

test('queued Task requires explicit Prepare then Start and restores through history', async ({ page }) => {
  await page.goto('/tasks/task-queued')
  await expect(page.getByTestId('task-status')).toBeVisible()
  await expect(page.getByRole('button', { name: /启动|Start/u })).toHaveCount(0)
  await page.getByRole('button', { name: /准备|Prepare/u }).click()
  await page.getByRole('button', { name: /启动|Start/u }).click()
  await expect(page.getByTestId('task-status')).not.toContainText(/queued|排队/u)
  await expect(page.getByRole('heading', { name: /计划|Plan/u })).toBeVisible()
  await expect(page.getByRole('heading', { name: /工具|Tools/u })).toBeVisible()
  await expect(page.getByRole('heading', { name: /验证|Verification/u })).toBeVisible()
  await expect(page.getByRole('heading', { name: /审批|Approvals/u })).toBeVisible()
  await page.goBack()
  await page.goForward()
  await expect(page).toHaveURL(/\/tasks\/task-queued$/)
  await expect(page.getByTestId('task-status')).toBeVisible()
})

test('Loop Definition, Team, Dry Run, and Invocation use structured authoritative projections', async ({ page }) => {
  await page.goto('/loops/daily-review')
  await expect(page.getByRole('heading', { name: 'Daily review' })).toBeVisible()
  await expect(page.getByRole('heading', { name: /循环契约|Loop contract/u })).toBeVisible()
  await page.getByText(/高级设置|Advanced settings/u).click()
  await expect(page.locator('input[value="Daily review"]')).toBeVisible()
  await expect(page.getByRole('heading', { name: /验证检查|Verification checks/u })).toBeVisible()
  await expect(page.getByTestId('team-role-builder')).toHaveValue('employee-builder')
  await expect(page.getByText(/Builder · r2 · (就绪|Ready)/u)).toBeVisible()
  await expect(page.getByRole('button', { name: /立即运行|Run now/u })).toBeDisabled()

  await page.getByRole('button', { name: 'Dry Run' }).click()
  await expect(page.getByRole('heading', { name: /Dry Run 结果|Dry Run result/u })).toBeVisible()
  await expect(page.getByRole('button', { name: /立即运行|Run now/u })).toBeEnabled()

  await page.goto('/loops/daily-review/invocations/invocation-1')
  await expect(page.getByRole('heading', { name: /Definition 快照|Definition snapshot/u })).toBeVisible()
  await expect(page.getByRole('heading', { name: /计划|Plan/u })).toBeVisible()
  await expect(page.getByRole('heading', { name: /工具|Tools/u })).toBeVisible()
  await expect(page.getByTestId('loop-timeline')).toContainText('invocation-1')
  await page.reload()
  await expect(page.getByTestId('loop-timeline')).toContainText('invocation-1')
})

test('rapid Employee and Loop A/B route switches cannot commit stale responses', async ({ page }) => {
  await page.goto('/employees/employee-alpha')
  await page.evaluate(() => {
    window.history.pushState({}, '', '/employees/employee-beta')
    window.dispatchEvent(new PopStateEvent('popstate'))
  })
  await expect(page.getByRole('heading', { name: 'Beta Employee' })).toBeVisible()
  await page.waitForTimeout(400)
  await expect(page.getByRole('heading', { name: 'Beta Employee' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Alpha Employee' })).toHaveCount(0)

  await page.goto('/loops/loop-alpha')
  await page.evaluate(() => {
    window.history.pushState({}, '', '/loops/loop-beta')
    window.dispatchEvent(new PopStateEvent('popstate'))
  })
  await expect(page.getByRole('heading', { name: 'Beta Loop' })).toBeVisible()
  await page.waitForTimeout(400)
  await expect(page.getByRole('heading', { name: 'Beta Loop' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Alpha Loop' })).toHaveCount(0)
})
