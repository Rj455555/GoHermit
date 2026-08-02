import { mkdir } from 'node:fs/promises'
import { join } from 'node:path'

import { expect, test } from '@playwright/test'

const output = process.env.ROUND2_SCREENSHOT_DIR
test.skip(!output, 'Set ROUND2_SCREENSHOT_DIR for the non-committed visual acceptance run.')

async function selectAntOption(page: import('@playwright/test').Page, selector: string, option: RegExp) {
  await page.locator(selector).click()
  const optionContent = page.locator('.ant-select-dropdown:visible .ant-select-item-option-content').filter({ hasText: option })
  await expect(optionContent).toBeVisible()
  await optionContent.dispatchEvent('click')
  await page.keyboard.press('Escape')
  await expect(page.locator('.ant-select-dropdown:visible')).toHaveCount(0)
}

async function expectNoPageOverflow(page: import('@playwright/test').Page) {
  await expect.poll(() => page.evaluate(() =>
    document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(1)
}

test.beforeEach(async ({ request }) => {
  await request.post('/__test__/reset')
})

test('captures Round 2 responsive acceptance surfaces', async ({ page }) => {
  if (!output) return
  await mkdir(output, { recursive: true })
  const sizes = [
    [1440, 900], [1280, 800], [1024, 768], [768, 1024],
    [430, 932], [390, 844], [375, 812], [360, 800],
  ] as const

  for (const [width, height] of sizes) {
    await page.setViewportSize({ width, height })
    await page.goto('/dashboard')
    await expect(page.getByRole('heading', { name: /仪表盘|Dashboard/u })).toBeVisible()
    await expectNoPageOverflow(page)
    await page.screenshot({ path: join(output, `dashboard-${width}x${height}.png`), fullPage: true })
  }

  await page.setViewportSize({ width: 390, height: 844 })
  const capture = async (name: string) => page.screenshot({ path: join(output, `${name}-390x844.png`), fullPage: true })
  await page.goto('/dashboard')
  await page.getByRole('button', { name: /主导航|Main navigation/u }).click()
  const navigationDrawer = page.getByRole('dialog', { name: /主导航|Main navigation/u })
  await expect(navigationDrawer).toBeVisible()
  await navigationDrawer.screenshot({ path: join(output, 'navigation-drawer-390x844.png'), animations: 'disabled' })
  await navigationDrawer.getByRole('button', { name: /Close|关闭/u }).click()
  await expect(navigationDrawer).toBeHidden()

  const employeeDetailRoute = (url: URL) => url.pathname === '/api/employees/employee-ada'
  let releaseEmployeeDetail = () => undefined
  const employeeDetailGate = new Promise<void>((resolve) => { releaseEmployeeDetail = resolve })
  const holdEmployeeDetail = async (route: import('@playwright/test').Route) => {
    await employeeDetailGate
    await route.continue()
  }
  await page.route(employeeDetailRoute, holdEmployeeDetail)
  await page.goto('/employees/employee-ada')
  await expect(page.locator('.ant-skeleton').first()).toBeVisible()
  await capture('loading')
  const employeeDetailResponse = page.waitForResponse((response) => response.url().includes('/api/employees/employee-ada'))
  releaseEmployeeDetail()
  await employeeDetailResponse
  await expect(page.locator('.ant-skeleton')).toHaveCount(0)
  await page.unroute(employeeDetailRoute, holdEmployeeDetail)

  const employeeListRoute = (url: URL) => url.pathname === '/api/employees'
  const emptyEmployeeList = async (route: import('@playwright/test').Route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ employees: [], next_cursor: '' }) })
  }
  await page.route(employeeListRoute, emptyEmployeeList)
  await page.goto('/employees')
  await expect(page.locator('.ant-empty')).toBeVisible()
  await capture('empty')
  await page.unroute(employeeListRoute, emptyEmployeeList)

  await page.goto('/employees')
  await capture('employee-list')
  await page.goto('/employees/employee-ada?tab=overview')
  await capture('employee-overview-archived')
  for (const [name, tab] of [
    ['employee-settings', /设置|Settings/u],
    ['employee-skills', /Skills/u],
    ['employee-knowledge', /Knowledge/u],
    ['employee-memory', /Memory/u],
    ['employee-projects', /Projects/u],
    ['employee-tasks', /Tasks/u],
    ['employee-activity', /Activity/u],
  ] as const) {
    await selectAntOption(page, '.employee-mobile-tab-select', tab)
    await expectNoPageOverflow(page)
    await capture(name)
  }
  await page.goto('/tasks')
  await capture('global-tasks')
  await page.goto('/tasks/task-queued')
  await capture('task-detail')
  await page.goto('/loops')
  await capture('loop-list')
  await page.goto('/loops/daily-review')
  await page.getByRole('tab', { name: /高级设置|Advanced settings/u }).click()
  await capture('team-template-editor-and-assignment')
  await page.goto('/loops/daily-review/invocations/invocation-1')
  await capture('mission-workitems')
  await selectAntOption(page, '.loop-mobile-tab-select', /审批|Approvals/u)
  await capture('approval')
  await selectAntOption(page, '.loop-mobile-tab-select', /验证|Verification/u)
  await capture('verification')
  await page.goto('/employees/missing-employee')
  await capture('error-result')
})
