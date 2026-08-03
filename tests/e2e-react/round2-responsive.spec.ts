import { expect, test, type Page } from '@playwright/test'

test.beforeEach(async ({ request }) => {
  await request.post('/__test__/reset')
})

function captureUnexpectedConsole(page: Page) {
  const failures: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') failures.push(message.text())
  })
  page.on('pageerror', (error) => failures.push(error.message))
  return failures
}

async function expectNoPageOverflow(page: Page) {
  await expect.poll(() => page.evaluate(() =>
    document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(1)
}

async function selectAntOption(page: Page, selector: string, option: RegExp) {
  await page.locator(selector).click()
  const optionContent = page.locator('.ant-select-dropdown:visible .ant-select-item-option-content').filter({ hasText: option })
  await expect(optionContent).toBeVisible()
  // Ant Design portals the popup to the document body. During rapid URL-owned
  // tab changes its placement can settle outside the visual viewport for one
  // animation frame even though the option is visible and enabled. Dispatching
  // the same semantic click avoids scrolling the portal and the resulting
  // detach/recreate race; the URL assertion below still proves selection.
  await optionContent.dispatchEvent('click')
  await page.keyboard.press('Escape')
  await expect(page.locator('.ant-select-dropdown:visible')).toHaveCount(0)
}

test('Employee deep tabs restore through refresh and remain operable at every breakpoint', async ({ page }) => {
  const consoleFailures = captureUnexpectedConsole(page)
  await page.goto('/employees/employee-ada?tab=overview')
  await expect(page.getByRole('heading', { name: 'Ada' })).toBeVisible()

  const compact = (page.viewportSize()?.width ?? 1440) < 768
  for (const [tab, key] of [
    [/设置|Settings/u, 'settings'], [/Skills/u, 'skills'], [/Knowledge/u, 'knowledge'],
    [/Memory/u, 'memory'], [/Projects/u, 'projects'], [/Tasks/u, 'tasks'], [/Activity/u, 'activity'],
  ] as const) {
    if (compact) await selectAntOption(page, '.employee-mobile-tab-select', tab)
    else await page.getByRole('tab', { name: tab }).click()
    await expect(page).toHaveURL(new RegExp(`tab=${key}`, 'u'))
  }
  await expect(page).toHaveURL(/tab=activity/u)
  await page.reload()
  if (compact) await expect(page.locator('.employee-mobile-tab-select')).toContainText('Activity')
  else await expect(page.getByRole('tab', { name: /Activity/u })).toHaveAttribute('aria-selected', 'true')
  await expectNoPageOverflow(page)

  if (compact) {
    await expect(page.getByRole('combobox', { name: /Employee.*section|Employee 页面分区/u })).toBeVisible()
  }
  expect(consoleFailures).toEqual([])
})

test('Task detail preserves execution order and uses a mobile-safe action surface', async ({ page }) => {
  const consoleFailures = captureUnexpectedConsole(page)
  await page.goto('/tasks/task-queued')
  await expect(page.getByTestId('task-status')).toBeVisible()

  const sections = [
    /权威上下文|Task Summary|Authoritative context/iu,
    /Activity/iu,
    /计划|Plan/iu,
    /工具|Tools/iu,
    /审批|Approvals/iu,
    /验证|Verification/iu,
    /Artifacts/iu,
  ]
  const positions: number[] = []
  for (const name of sections) {
    const heading = page.getByRole('heading', { name })
    await expect(heading).toBeVisible()
    positions.push(await heading.evaluate((element) => element.getBoundingClientRect().top + window.scrollY))
  }
  expect(positions).toEqual([...positions].sort((left, right) => left - right))
  await expectNoPageOverflow(page)
  if ((page.viewportSize()?.width ?? 1440) < 768) await expect(page.locator('.task-sticky-action-bar')).toBeVisible()
  expect(consoleFailures).toEqual([])
})

test('Loop advanced Team and Mission projections stay bounded and hide worker sessions', async ({ page }) => {
  const consoleFailures = captureUnexpectedConsole(page)
  await page.goto('/loops/daily-review')
  await page.getByRole('tab', { name: /高级设置|Advanced settings/u }).click()
  await expect(page.locator('.team-role-card')).toHaveCount(5)
  await expect(page.getByText(/Builder · r2 · (就绪|Ready)/u)).toBeVisible()
  await expectNoPageOverflow(page)

  await page.goto('/loops/daily-review/invocations/invocation-1')
  if ((page.viewportSize()?.width ?? 1440) < 768) {
    await selectAntOption(page, '.loop-mobile-tab-select', /时间线|Timeline/u)
  } else {
    await page.getByRole('tab', { name: /时间线|Timeline/u }).last().click()
  }
  await expect(page.getByTestId('loop-timeline')).toBeVisible()
  await expect(page.getByRole('link', { name: /hidden|worker session/iu })).toHaveCount(0)
  await expect(page.getByText(/private memory sentinel/iu)).toHaveCount(0)
  await expectNoPageOverflow(page)
  expect(consoleFailures).toEqual([])
})

test('mobile navigation uses a modal Drawer and closes after route selection', async ({ page }) => {
  test.skip((page.viewportSize()?.width ?? 1440) > 900, 'Mobile navigation is only rendered below the shell breakpoint.')
  const consoleFailures = captureUnexpectedConsole(page)
  await page.goto('/dashboard')
  const trigger = page.getByRole('button', { name: /主导航|Main navigation/u })
  await expect(trigger).toBeVisible()
  await trigger.click()
  const drawer = page.getByRole('dialog', { name: /主导航|Main navigation/u })
  await expect(drawer).toBeVisible()
  await drawer.getByRole('link', { name: /电子员工|Employees/u }).click()
  await expect(page).toHaveURL(/\/employees$/u)
  await expect(drawer).toBeHidden()
  await expectNoPageOverflow(page)
  expect(consoleFailures).toEqual([])
})

test('Employee directory keeps Ant Design grid ownership and equal-height cards', async ({ page }) => {
  await page.goto('/employees')
  const grid = page.locator('.employee-directory-grid')
  await expect(grid).toBeVisible()
  const geometry = await grid.evaluate((element) => {
    const cards = [...element.querySelectorAll<HTMLElement>('.employee-card')]
    const links = [...element.querySelectorAll<HTMLElement>('.employee-card-anchor')]
    const rows = new Map<number, number[]>()
    for (const card of cards) {
      const rect = card.getBoundingClientRect()
      const key = Math.round(rect.top)
      rows.set(key, [...(rows.get(key) ?? []), Math.round(rect.height)])
    }
    return {
      display: getComputedStyle(element).display,
      maxColumns: Math.max(...[...rows.values()].map((heights) => heights.length), 0),
      equalRows: [...rows.values()].every((heights) => Math.max(...heights) - Math.min(...heights) <= 2),
      linkPadding: links.map((link) => getComputedStyle(link).padding).filter((value) => value !== '0px'),
    }
  })
  expect(geometry.display).toBe('flex')
  expect(geometry.maxColumns).toBeLessThanOrEqual(4)
  expect(geometry.equalRows).toBe(true)
  expect(geometry.linkPadding).toEqual([])
  await expectNoPageOverflow(page)
})

test('Dashboard has one unified vertical stack and responsive hero surface', async ({ page }) => {
  await page.goto('/dashboard')
  await expect(page.locator('.dashboard-content-stack')).toBeVisible()
  await expect(page.locator('.dashboard-hero-card')).toHaveCount(1)
  await expect(page.locator('.dashboard-page > .hero')).toHaveCount(0)
  const layout = await page.locator('.dashboard-content-stack').evaluate((element) => ({
    display: getComputedStyle(element).display,
    gap: getComputedStyle(element).gap,
    width: Math.round(element.getBoundingClientRect().width),
  }))
  expect(layout.display).toBe('flex')
  expect(layout.gap).toBe('16px')
  expect(layout.width).toBeGreaterThan(0)
  await expectNoPageOverflow(page)
})

test('Task prompts wrap instead of forcing a page-level horizontal scroll', async ({ page }) => {
  await page.goto('/employees/employee-ada?tab=tasks')
  const prompt = page.locator('.task-prompt-text').first()
  await expect(prompt).toBeVisible()
  const style = await prompt.evaluate((element) => ({
    whiteSpace: getComputedStyle(element).whiteSpace,
    width: element.getBoundingClientRect().width,
    scrollWidth: element.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }))
  expect(style.whiteSpace).toBe('normal')
  expect(style.width).toBeLessThanOrEqual(style.clientWidth)
  expect(style.scrollWidth).toBeLessThanOrEqual(style.width + 1)
  await expectNoPageOverflow(page)
})
