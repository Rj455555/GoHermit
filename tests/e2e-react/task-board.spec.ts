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

async function fireBoardDragEvent(target: Locator, type: 'dragstart' | 'dragover' | 'drop' | 'dragend') {
  await target.evaluate((element, eventType) => {
    const win = window as unknown as { __boardDragTransfer?: DataTransfer }
    if (eventType === 'dragstart') win.__boardDragTransfer = new DataTransfer()
    const dataTransfer = win.__boardDragTransfer ?? new DataTransfer()
    element.dispatchEvent(new DragEvent(eventType, { bubbles: true, cancelable: true, dataTransfer }))
  }, type)
}

async function dragCardToColumn(source: Locator, target: Locator) {
  await fireBoardDragEvent(source, 'dragstart')
  await expect(source).toHaveClass(/is-dragging/u)
  await fireBoardDragEvent(target, 'dragover')
  await expect(target).toHaveClass(/is-drop-target/u)
  // A successful cross-column drop replaces the board from the PUT response and
  // detaches the source node; dropCard clears the drag state synchronously, so
  // no dragend dispatch is required here.
  await fireBoardDragEvent(target, 'drop')
}

async function clickCardBody(card: Locator) {
  // Center the card inside the horizontally scrolling board first; otherwise
  // mobile sticky surfaces above the board can cover the raw click point.
  await card.evaluate((element) => element.scrollIntoView({ block: 'center', inline: 'center' }))
  const box = await card.boundingBox()
  await card.click({ position: { x: 8, y: Math.max((box?.height ?? 48) - 8, 4) } })
}

test.describe('Task Board card activation', () => {
  test('dashboard card with a Session navigates to the Session page from a body click', async ({ page }) => {
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

test.describe('Task Board drag and drop', () => {
  test('dragging a task card between columns on the Tasks board issues a card PUT', async ({ page, request }) => {
    await page.goto('/tasks?view=board')
    const source = boardCard(page, 'task-board-column-backlog', 'Refactor board fixture helpers')
    await expect(source).toBeVisible()
    await dragCardToColumn(source, page.getByTestId('task-board-column-todo'))

    await expect(boardCard(page, 'task-board-column-todo', 'Refactor board fixture helpers')).toBeVisible()
    const puts = (await boardRequests(request)).filter((entry) => entry.method === 'PUT')
    expect(puts).toHaveLength(1)
    expect(puts[0].path).toBe('/api/task-board/cards/board-task-drag')
    expect(puts[0].body?.column_id).toBe('todo')
  })

  test('dropping a queued task on In progress asks for Start confirmation and cancel records nothing', async ({ page, request }) => {
    await page.goto('/dashboard')
    const source = boardCard(page, 'dashboard-task-board-column-todo', 'Execute queued migration rehearsal')
    await expect(source).toBeVisible()
    await dragCardToColumn(source, page.getByTestId('dashboard-task-board-column-in_progress'))

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
    await dragCardToColumn(source, page.getByTestId('dashboard-task-board-column-in_progress'))

    const dialog = page.getByRole('dialog', { name: /确认启动 Task|Confirm Task start/u })
    await expect(dialog).toBeVisible()
    await dialog.getByRole('button', { name: /启\s*动|Start/u }).click()
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

  test('mid-drag state marks the source card and target column and a same-column drop records nothing', async ({ page, request }) => {
    await page.goto('/tasks?view=board')
    const source = boardCard(page, 'task-board-column-backlog', 'Refactor board fixture helpers')
    const target = page.getByTestId('task-board-column-todo')
    await expect(source).toBeVisible()

    await fireBoardDragEvent(source, 'dragstart')
    await expect(source).toHaveClass(/is-dragging/u)
    await fireBoardDragEvent(target, 'dragover')
    await expect(target).toHaveClass(/is-drop-target/u)

    await fireBoardDragEvent(page.getByTestId('task-board-column-backlog'), 'drop')
    await fireBoardDragEvent(source, 'dragend')
    await expect(page.getByTestId('task-board-column-todo')).not.toHaveClass(/is-drop-target/u)
    expect(await boardRequests(request)).toEqual([])
    await expect(boardCard(page, 'task-board-column-backlog', 'Refactor board fixture helpers')).toBeVisible()
  })

  test('dragging a note to another column issues a card PUT without a start request', async ({ page, request }) => {
    await page.goto('/tasks?view=board')
    const note = boardCard(page, 'task-board-column-todo', 'Release cadence note')
    await expect(note).toBeVisible()
    await dragCardToColumn(note, page.getByTestId('task-board-column-review'))

    await expect(boardCard(page, 'task-board-column-review', 'Release cadence note')).toBeVisible()
    const records = await boardRequests(request)
    expect(records.filter((entry) => entry.method === 'POST')).toEqual([])
    const puts = records.filter((entry) => entry.method === 'PUT')
    expect(puts).toHaveLength(1)
    expect(puts[0].path).toBe('/api/task-board/cards/board-note-01')
    expect(puts[0].body?.column_id).toBe('review')
  })

  test('the Dashboard board is not read-only and issues the same card PUT', async ({ page, request }) => {
    await page.goto('/dashboard')
    const source = boardCard(page, 'dashboard-task-board-column-backlog', 'Refactor board fixture helpers')
    await expect(source).toBeVisible()
    await dragCardToColumn(source, page.getByTestId('dashboard-task-board-column-todo'))

    await expect(boardCard(page, 'dashboard-task-board-column-todo', 'Refactor board fixture helpers')).toBeVisible()
    const puts = (await boardRequests(request)).filter((entry) => entry.method === 'PUT')
    expect(puts).toHaveLength(1)
    expect(puts[0].path).toBe('/api/task-board/cards/board-task-drag')
    expect(puts[0].body?.column_id).toBe('todo')
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
