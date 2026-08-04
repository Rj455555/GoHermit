import { expect, test, type APIRequestContext, type Locator, type Page } from '@playwright/test'

test.beforeEach(async ({ request }) => {
  await request.post('/__test__/reset')
})

interface BoardRequestRecord {
  method: string
  path: string
  action?: string
  task_id?: string
  body?: { column_id?: string }
}

async function boardRequests(request: APIRequestContext): Promise<BoardRequestRecord[]> {
  const response = await request.get('/__test__/task-board-requests')
  return (await response.json()).requests
}

function boardCard(page: Page, columnTestId: string, title: string) {
  return page.getByTestId(columnTestId).locator('.task-board-card').filter({ hasText: title })
}

// The board uses pointer-based dragging: pointerdown on the card arms a press,
// crossing a 6px threshold activates the drag, columns are hit-tested with
// elementFromPoint, and pointerup drops. These helpers drive real mouse input.
async function pointerDownOnCard(page: Page, card: Locator) {
  await card.scrollIntoViewIfNeeded()
  const box = await card.boundingBox()
  if (!box) throw new Error('card is not visible')
  // Press on the card head: never an interactive child (which would be ignored).
  const x = box.x + box.width / 2
  const y = box.y + 12
  await page.mouse.move(x, y)
  await page.mouse.down()
  await page.mouse.move(x + 12, y + 12, { steps: 4 })
  await expect(card).toHaveClass(/is-dragging/u)
  await expect(page.locator('body')).toHaveClass(/is-board-dragging/u)
}

async function pointerToColumn(page: Page, column: Locator) {
  const box = await column.boundingBox()
  if (!box) throw new Error('column is not visible')
  await page.mouse.move(box.x + box.width / 2, box.y + Math.min(box.height / 2, 140), { steps: 8 })
}

async function realDragCardToColumn(page: Page, source: Locator, targetColumn: Locator) {
  await pointerDownOnCard(page, source)
  await pointerToColumn(page, targetColumn)
  await expect(targetColumn).toHaveClass(/is-drop-target/u)
  await page.mouse.up()
  await expect(page.locator('body')).not.toHaveClass(/is-board-dragging/u)
}

async function clickCardBody(card: Locator) {
  // Center the card inside the horizontally scrolling board first; otherwise
  // mobile sticky surfaces above the board can cover the raw click point.
  await card.evaluate((element) => element.scrollIntoView({ block: 'center', inline: 'center' }))
  const box = await card.boundingBox()
  await card.click({ position: { x: 8, y: Math.max((box?.height ?? 48) - 8, 4) } })
}

test.describe('Task Board card activation', () => {
  test('dashboard card with a Session navigates to the Session page from a plain body click', async ({ page }) => {
    await page.goto('/dashboard')
    const card = boardCard(page, 'dashboard-task-board-column-in_progress', 'Stream board execution updates')
    await expect(card).toBeVisible()
    await clickCardBody(card)
    await expect(page).toHaveURL(/\/agent\/sessions\/session-board$/u)
    await expect(page.getByRole('heading', { name: 'Board Linked Session' })).toBeVisible()
  })

  test('dashboard card without a Session navigates to the Task detail page', async ({ page }) => {
    await page.goto('/dashboard')
    const card = boardCard(page, 'dashboard-task-board-column-backlog', 'Triage workspace lint warnings')
    await expect(card).toBeVisible()
    await clickCardBody(card)
    await expect(page).toHaveURL(/\/tasks\/board-task-01$/u)
    await expect(page.getByTestId('task-status')).toBeVisible()
  })

  test('long clamped titles remain clickable and navigate', async ({ page }) => {
    test.skip(
      (page.viewportSize()?.width ?? 1440) < 768,
      'Below 768px the create-task form overlays the horizontally scrolled board click point.',
    )
    await page.goto('/tasks?view=board')
    const chinese = boardCard(page, 'task-board-column-backlog', '整理并归档过去三十天')
    await expect(chinese).toBeVisible()
    await chinese.locator('.task-board-card__title').click()
    await expect(page).toHaveURL(/\/tasks\/board-task-long-zh$/u)
    await expect(page.getByTestId('task-status')).toBeVisible()

    await page.goto('/tasks?view=board')
    const english = boardCard(page, 'task-board-column-todo', 'Consolidate every orphaned deployment')
    await expect(english).toBeVisible()
    await english.locator('.task-board-card__title').click()
    await expect(page).toHaveURL(/\/tasks\/board-task-long-en$/u)
    await expect(page.getByTestId('task-status')).toBeVisible()
  })

  test('note card opens the note modal without navigating', async ({ page }) => {
    test.skip(
      (page.viewportSize()?.width ?? 1440) < 768,
      'Below 768px the create-task form overlays the horizontally scrolled board click point.',
    )
    await page.goto('/tasks?view=board')
    const note = boardCard(page, 'task-board-column-todo', 'Release cadence note')
    await expect(note).toBeVisible()
    await clickCardBody(note)
    const dialog = page.getByRole('dialog', { name: /笔记详情|Note details/u })
    await expect(dialog).toBeVisible()
    await expect(dialog).toContainText('Ship the verified release every Friday')
    await expect(page).toHaveURL(/\/tasks\?view=board$/u)
    await dialog.getByRole('button', { name: /关\s*闭|Dismiss/u }).click()
    await expect(dialog).toHaveCount(0)
    await expect(page).toHaveURL(/\/tasks\?view=board$/u)
  })
})

test.describe('Task Board pointer drag and drop', () => {
  test.beforeEach(async ({}, testInfo) => {
    test.skip(testInfo.project.name !== 'desktop-chrome', 'Real pointer drag proofs run on desktop-chrome; smaller boards scroll horizontally with offscreen targets.')
  })

  test('real cross-column drag on the Tasks board issues exactly one card PUT and never navigates', async ({ page, request }) => {
    await page.goto('/tasks?view=board')
    const source = boardCard(page, 'task-board-column-backlog', 'Refactor board fixture helpers')
    await expect(source).toBeVisible()
    await realDragCardToColumn(page, source, page.getByTestId('task-board-column-todo'))

    await expect(boardCard(page, 'task-board-column-todo', 'Refactor board fixture helpers')).toBeVisible()
    await expect(page).toHaveURL(/\/tasks\?view=board$/u)
    const puts = (await boardRequests(request)).filter((entry) => entry.method === 'PUT')
    expect(puts).toHaveLength(1)
    expect(puts[0].path).toBe('/api/task-board/cards/board-task-drag')
    expect(puts[0].body?.column_id).toBe('todo')
  })

  test('real cross-column drag on the Dashboard board is not read-only and never navigates', async ({ page, request }) => {
    await page.goto('/dashboard')
    const source = boardCard(page, 'dashboard-task-board-column-backlog', 'Refactor board fixture helpers')
    await expect(source).toBeVisible()
    await realDragCardToColumn(page, source, page.getByTestId('dashboard-task-board-column-todo'))

    await expect(boardCard(page, 'dashboard-task-board-column-todo', 'Refactor board fixture helpers')).toBeVisible()
    await expect(page).toHaveURL(/\/dashboard$/u)
    const puts = (await boardRequests(request)).filter((entry) => entry.method === 'PUT')
    expect(puts).toHaveLength(1)
    expect(puts[0].path).toBe('/api/task-board/cards/board-task-drag')
    expect(puts[0].body?.column_id).toBe('todo')
  })

  test('real note drag to another column issues a card PUT and zero start requests', async ({ page, request }) => {
    await page.goto('/tasks?view=board')
    const note = boardCard(page, 'task-board-column-todo', 'Release cadence note')
    await expect(note).toBeVisible()
    await realDragCardToColumn(page, note, page.getByTestId('task-board-column-review'))

    await expect(boardCard(page, 'task-board-column-review', 'Release cadence note')).toBeVisible()
    const records = await boardRequests(request)
    expect(records.filter((entry) => entry.method === 'POST')).toEqual([])
    const puts = records.filter((entry) => entry.method === 'PUT')
    expect(puts).toHaveLength(1)
    expect(puts[0].path).toBe('/api/task-board/cards/board-note-01')
    expect(puts[0].body?.column_id).toBe('review')
  })

  test('dropping a queued task on In progress asks for Start confirmation and cancel records nothing', async ({ page, request }) => {
    await page.goto('/dashboard')
    const source = boardCard(page, 'dashboard-task-board-column-todo', 'Execute queued migration rehearsal')
    await expect(source).toBeVisible()
    await realDragCardToColumn(page, source, page.getByTestId('dashboard-task-board-column-in_progress'))

    const dialog = page.getByRole('dialog', { name: /确认启动 Task|Confirm Task start/u })
    await expect(dialog).toBeVisible()
    await expect(dialog).toContainText(/启动会创建或复用真实 Session|Start creates or reuses the real Session/u)
    await dialog.getByRole('button', { name: /取\s*消|Cancel/u }).click()
    await expect(dialog).toHaveCount(0)

    expect(await boardRequests(request)).toEqual([])
    await expect(boardCard(page, 'dashboard-task-board-column-todo', 'Execute queued migration rehearsal')).toBeVisible()
    await expect(boardCard(page, 'dashboard-task-board-column-in_progress', 'Execute queued migration rehearsal')).toHaveCount(0)
  })

  test('confirming Start records the start POST and lands the card in In progress', async ({ page, request }) => {
    await page.goto('/dashboard')
    const source = boardCard(page, 'dashboard-task-board-column-todo', 'Execute queued migration rehearsal')
    await expect(source).toBeVisible()
    await realDragCardToColumn(page, source, page.getByTestId('dashboard-task-board-column-in_progress'))

    const dialog = page.getByRole('dialog', { name: /确认启动 Task|Confirm Task start/u })
    await expect(dialog).toBeVisible()
    const confirm = dialog.getByRole('button', { name: /启\s*动|Start/u })
    // The OK button stays disabled until the async Task load resolves.
    await expect(confirm).toBeEnabled()
    await confirm.click()
    await expect(dialog).toHaveCount(0)

    await expect(boardCard(page, 'dashboard-task-board-column-in_progress', 'Execute queued migration rehearsal')).toBeVisible()
    const records = await boardRequests(request)
    const starts = records.filter((entry) => entry.method === 'POST')
    expect(starts).toHaveLength(1)
    expect(starts[0]).toMatchObject({
      path: '/api/employee-tasks/board-task-start/start',
      action: 'start',
      task_id: 'board-task-start',
    })
    const puts = records.filter((entry) => entry.method === 'PUT')
    expect(puts).toHaveLength(1)
    expect(puts[0].path).toBe('/api/task-board/cards/board-task-start')
    expect(puts[0].body?.column_id).toBe('in_progress')
  })

  test('mid-drag marks source and target, and dropping back on the origin records nothing', async ({ page, request }) => {
    await page.goto('/tasks?view=board')
    const source = boardCard(page, 'task-board-column-backlog', 'Refactor board fixture helpers')
    const target = page.getByTestId('task-board-column-todo')
    await expect(source).toBeVisible()

    await pointerDownOnCard(page, source)
    await pointerToColumn(page, target)
    await expect(target).toHaveClass(/is-drop-target/u)
    await expect(source).toHaveClass(/is-dragging/u)

    // Drag back over the origin column and release: same-column drops are a no-op.
    await pointerToColumn(page, page.getByTestId('task-board-column-backlog'))
    await page.mouse.up()
    await expect(page.locator('body')).not.toHaveClass(/is-board-dragging/u)
    await expect(target).not.toHaveClass(/is-drop-target/u)

    expect(await boardRequests(request)).toEqual([])
    await expect(page).toHaveURL(/\/tasks\?view=board$/u)
    await expect(boardCard(page, 'task-board-column-backlog', 'Refactor board fixture helpers')).toBeVisible()
  })

  test('dropping a waiting_owner task on In progress shows the not-allowed toast and records nothing', async ({ page, request }) => {
    await page.goto('/tasks?view=board')
    const source = boardCard(page, 'task-board-column-todo', 'Consolidate every orphaned deployment')
    await expect(source).toBeVisible()
    await realDragCardToColumn(page, source, page.getByTestId('task-board-column-in_progress'))

    await expect(page.locator('.toast')).toContainText(/不能通过拖拽启动|cannot be started from the board/u)
    expect(await boardRequests(request)).toEqual([])
    await expect(boardCard(page, 'task-board-column-todo', 'Consolidate every orphaned deployment')).toBeVisible()
    await expect(page.getByRole('dialog')).toHaveCount(0)
  })
})

test.describe('Task Board responsive layout', () => {
  test.beforeEach(async ({}, testInfo) => {
    test.skip(testInfo.project.name !== 'desktop-chrome', 'Viewport-explicit layout checks run once on desktop-chrome.')
  })

  const businessColumns = ['backlog', 'todo', 'in_progress', 'review', 'done'] as const

  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 1280, height: 800 },
    { width: 1024, height: 768 },
  ]) {
    test(`dashboard board fits all columns without horizontal scroll at ${viewport.width}px`, async ({ page }) => {
      await page.setViewportSize(viewport)
      await page.goto('/dashboard')
      const board = page.getByTestId('dashboard-task-board')
      await expect(board).toBeVisible()

      await expect.poll(() => page.evaluate(() =>
        document.documentElement.scrollWidth === document.documentElement.clientWidth)).toBe(true)
      const boardOverflow = await board.evaluate((element) => element.scrollWidth - element.clientWidth)
      expect(boardOverflow).toBeLessThanOrEqual(1)
      for (const column of businessColumns) {
        const box = await page.getByTestId(`dashboard-task-board-column-${column}`).boundingBox()
        expect(box).not.toBeNull()
        expect(box?.x).toBeGreaterThanOrEqual(-1)
        expect((box?.x ?? 0) + (box?.width ?? 0)).toBeLessThanOrEqual(viewport.width + 1)
        expect(box?.width).toBeGreaterThanOrEqual(155)
      }

      // At ≤1279px the Sider auto-collapses to its 68px rail; above it the
      // default expanded 228px rail applies.
      const siderWidth = Math.round((await page.locator('.app-shell__sider').boundingBox())?.width ?? 0)
      const rail = page.locator('.navigation-rail')
      if (viewport.width <= 1279) {
        await expect(rail).toHaveAttribute('data-collapsed', 'true')
        expect(siderWidth).toBe(68)
      } else {
        await expect(rail).toHaveAttribute('data-collapsed', 'false')
        expect(siderWidth).toBe(228)
      }
    })
  }

  test('at 375px the page never overflows and horizontal scrolling stays inside the board', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 })
    await page.goto('/dashboard')
    const board = page.getByTestId('dashboard-task-board')
    await expect(board).toBeVisible()

    await expect.poll(() => page.evaluate(() =>
      document.documentElement.scrollWidth === document.documentElement.clientWidth)).toBe(true)
    const contained = await board.evaluate((element) => ({
      scrollWidth: element.scrollWidth,
      clientWidth: element.clientWidth,
    }))
    expect(contained.scrollWidth).toBeGreaterThan(contained.clientWidth)
  })
})
