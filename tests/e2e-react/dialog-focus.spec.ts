import { expect, test } from '@playwright/test'

test('ConfirmDialog isolates the shell and cleans every close path', async ({ page }) => {
  await page.goto('/__test__/dialog/')
  const trigger = page.getByRole('button', { name: '打开确认框' })
  const shell = page.getByTestId('shell-background')

  await page.getByRole('button', { name: '显示通知' }).click()
  await trigger.click()
  await expect(page.getByRole('dialog', { name: '确认操作' })).toBeVisible()
  await expect(shell).toHaveAttribute('inert')
  await expect(page.getByRole('status').filter({ hasText: '已保存' })).toContainText('已保存')
  await expect(page.getByRole('button', { name: '取消' })).toBeFocused()
  await expect.poll(() => page.evaluate(() => document.body.style.overflow)).toBe('hidden')
  await page.keyboard.press('Shift+Tab')
  await expect(page.getByRole('button', { name: '确认', exact: true })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog', { name: '确认操作' })).toHaveCount(0)
  await expect(shell).not.toHaveAttribute('inert')
  await expect(trigger).toBeFocused()
  await expect.poll(() => page.evaluate(() => document.body.style.overflow)).toBe('')

  await trigger.click()
  await page.getByRole('button', { name: '取消' }).click()
  await expect(shell).not.toHaveAttribute('inert')
  await expect(trigger).toBeFocused()

  await trigger.click()
  await page.getByTestId('confirm-dialog-overlay').click({ position: { x: 8, y: 8 } })
  await expect(shell).not.toHaveAttribute('inert')
  await expect(trigger).toBeFocused()

  await trigger.click()
  await page.getByRole('button', { name: '确认', exact: true }).click()
  await expect(page.getByLabel('确认次数')).toHaveText('1')
  await expect(shell).not.toHaveAttribute('inert')
  await expect(trigger).toBeFocused()
  await expect.poll(() => page.evaluate(() => document.body.style.overflow)).toBe('')
})
