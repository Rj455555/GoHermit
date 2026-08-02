import { expect, test } from '@playwright/test'

test('locale and desktop rail preferences switch immediately and survive refresh', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-chrome', 'desktop navigation rail contract')
  await page.goto('/dashboard')
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
  await expect(page.getByText('GOHERMIT · 工作流')).toBeVisible()
  await page.getByRole('button', { name: '切换到 English' }).click()
  await expect(page.getByRole('link', { name: 'Dashboard' })).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('lang', 'en-US')
  await page.getByRole('button', { name: 'Collapse navigation' }).click()
  await expect(page.getByRole('navigation', { name: 'Primary navigation' })).toHaveAttribute(
    'data-collapsed',
    'true',
  )
  await page.reload()
  await expect(page.getByRole('link', { name: 'Dashboard' })).toBeVisible()
  await expect(page.getByRole('navigation', { name: 'Primary navigation' })).toHaveAttribute(
    'data-collapsed',
    'true',
  )
})

test('Agent Session sidebar has an independent persisted desktop preference', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-chrome', 'desktop Session sidebar contract')
  await page.goto('/agent')
  await expect(page.getByRole('complementary', { name: '会话' })).toBeVisible()
  await page.getByRole('button', { name: '收起会话栏' }).click()
  await expect(page.getByRole('button', { name: '展开会话栏' })).toBeFocused()
  await page.reload()
  await expect(page.getByRole('button', { name: '展开会话栏' })).toBeVisible()
  await expect(page.getByRole('button', { name: '展开会话栏' })).not.toBeFocused()
  await page.goto('/settings')
  await expect(page.getByRole('complementary', { name: '会话' })).toHaveCount(0)
})

test('stored collapsed state never steals focus on entry or refresh', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-chrome', 'desktop Session sidebar contract')
  await page.addInitScript(() => {
    localStorage.setItem('gohermit.ui.sessionSidebarCollapsed', 'true')
  })
  await page.goto('/settings')
  const agentLink = page.getByRole('link', { name: '智能体' })
  await agentLink.click()
  await expect(page.getByRole('button', { name: '展开会话栏' })).toBeVisible()
  await expect(agentLink).toBeFocused()

  await page.reload()
  await expect(page.getByRole('button', { name: '展开会话栏' })).toBeVisible()
  await expect(page.getByRole('button', { name: '展开会话栏' })).not.toBeFocused()
})

test('returning to desktop restores collapsed state without stealing focus', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('gohermit.ui.sessionSidebarCollapsed', 'true')
  })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/agent')
  await expect(page.getByRole('button', { name: '打开会话抽屉' })).toBeVisible()

  await page.setViewportSize({ width: 1280, height: 800 })
  await expect(page.getByRole('button', { name: '展开会话栏' })).toBeVisible()
  await expect(page.getByRole('button', { name: '展开会话栏' })).not.toBeFocused()
})

test('mobile Session drawer traps focus, closes safely, and has no horizontal overflow', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/agent')
  const trigger = page.getByRole('button', { name: '打开会话抽屉' })
  await trigger.click()
  await expect(page.getByRole('dialog', { name: '会话' })).toBeVisible()
  await expect(page.getByRole('button', { name: '关闭会话抽屉' })).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(page.locator('.session-list__item').last()).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog', { name: '会话' })).toHaveCount(0)
  await expect(trigger).toBeFocused()
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
  )
  expect(overflow).toBe(false)
})
