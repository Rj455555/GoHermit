import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../../api/errors'
import type { EmployeeTask, TaskBoardCard, TaskBoardView } from '../../../api/types'
import { i18n } from '../../../i18n/i18n'
import { UIProvider, useUI } from '../../../state/UIContext'
import { TaskBoardGrid } from './TaskBoardGrid'
import { useTaskBoard } from './useTaskBoard'

const api = vi.hoisted(() => ({
  getEmployeeTask: vi.fn(),
  getTaskBoard: vi.fn(),
  startEmployeeTask: vi.fn(),
  resumeEmployeeTask: vi.fn(),
  updateTaskBoardCard: vi.fn(),
}))

vi.mock('../../../api/endpoints', () => api)
vi.mock('../../../components/ConnectivityProvider', () => ({
  useConnectivity: () => ({ status: 'online', generation: 0, canMutate: true }),
}))

const now = '2026-08-04T08:00:00Z'
const longTitle = 'Refactor the ingestion pipeline to stream large workspace snapshots without blocking the event loop'

const sessionCard: TaskBoardCard = {
  id: 'task-a',
  task_id: 'task-a',
  kind: 'task',
  title: 'Review release evidence',
  column_id: 'todo',
  rank: 0,
  labels: [],
  priority: 0,
  pinned: false,
  blocked: false,
  depends_on: [],
  employee_id: 'employee-ada',
  employee_name: 'Ada',
  state: 'running',
  projection_reason: 'run_running',
  authoritative_updated_at: now,
  session_id: 'session-1',
  run_id: 'run-1',
  session_event_sequence: 4,
  session_count: 1,
  approval_status: 'none',
  verification_status: 'none',
  stale: false,
}

const plainCard: TaskBoardCard = {
  id: 'task-b',
  task_id: 'task-b',
  kind: 'task',
  title: longTitle,
  column_id: 'todo',
  rank: 1,
  labels: ['release', 'ingestion', 'backend'],
  priority: 0,
  pinned: false,
  blocked: false,
  depends_on: [],
  employee_id: 'employee-grace',
  employee_name: 'Grace',
  state: 'queued',
  projection_reason: 'queued_task',
  authoritative_updated_at: now,
  loop_id: 'loop-1',
  session_event_sequence: 0,
  session_count: 0,
  approval_status: 'none',
  verification_status: 'none',
  stale: false,
}

const noteCard: TaskBoardCard = {
  id: 'note-1',
  kind: 'note',
  title: 'Capture rollout',
  body: 'Record the release evidence before shipping.',
  column_id: 'backlog',
  rank: 0,
  labels: [],
  priority: 0,
  pinned: false,
  blocked: false,
  depends_on: [],
  projection_reason: 'note',
  authoritative_updated_at: now,
  session_event_sequence: 0,
  session_count: 0,
  approval_status: 'none',
  verification_status: 'none',
  stale: false,
}

const board: TaskBoardView = {
  schema_version: 1,
  definition: {
    id: 'default',
    name: 'Task workspace',
    columns: [
      { id: 'backlog', title: 'Backlog', color: '#64748b', hidden: false },
      { id: 'todo', title: 'Todo', color: '#2563eb', hidden: false },
      { id: 'in_progress', title: 'In progress', color: '#0891b2', hidden: false },
      { id: 'done', title: 'Done', color: '#16a34a', hidden: false },
    ],
  },
  cards: [sessionCard, plainCard, noteCard],
  view: { view: 'board', wip_enabled: false },
  filters: { states: [], labels: [] },
  updated_at: now,
  projection_generated_at: now,
}

const queuedTask: EmployeeTask = {
  schema_version: 1,
  id: 'task-b',
  employee_id: 'employee-grace',
  employee_revision: 1,
  prompt: longTitle,
  state: 'queued',
  created_at: now,
  updated_at: now,
  employee_snapshot: {
    schema_version: 1,
    employee_id: 'employee-grace',
    revision: 1,
    captured_at: now,
    digest: 'e'.repeat(64),
  },
  skills: [],
  knowledge: [],
  memory_facts: [],
  project_binding: {
    id: 'project-main',
    label: 'GoHermit',
    workspace_fingerprint: 'f'.repeat(64),
    read_allowed: true,
    mutation_allowed: true,
    allowed_tool_capabilities: ['read'],
    network_allowed: false,
  },
  policy: {
    allowed_capabilities: ['read'],
    network_allowed: false,
    budget: { max_model_calls: 4, max_tokens: 4_000, timeout_seconds: 600 },
  },
  snapshot_digest: 'a'.repeat(64),
  session_id: '',
  run_id: '',
  artifacts: [],
}

const onBoardChange = vi.fn()

function GridLocationProbe() {
  const location = useLocation()
  return <output data-testid="grid-location">{location.pathname}</output>
}

function ToastProbe() {
  const { state } = useUI()
  if (!state.toast) return null
  return <output data-testid="grid-toast">{`${state.toast.tone}:${state.toast.messageKey}`}</output>
}

interface GridOptions {
  onUseNoteAsTask?: (note: TaskBoardCard) => void
  resolveTask?: (taskId: string) => Promise<EmployeeTask | undefined> | EmployeeTask | undefined
  board?: TaskBoardView
}

function renderGrid(options: GridOptions = {}) {
  return render(
    <I18nextProvider i18n={i18n}>
      <UIProvider>
        <MemoryRouter initialEntries={['/']}>
          <GridLocationProbe />
          <ToastProbe />
          <Routes>
            <Route path="*" element={<TaskBoardGrid board={options.board ?? board} onBoardChange={onBoardChange} resolveTask={options.resolveTask} {...(options.onUseNoteAsTask ? { onUseNoteAsTask: options.onUseNoteAsTask } : {})} />} />
          </Routes>
        </MemoryRouter>
      </UIProvider>
    </I18nextProvider>,
  )
}

function boardWithTaskState(state: TaskBoardCard['state']): TaskBoardView {
  return { ...board, cards: [sessionCard, { ...plainCard, state }, noteCard] }
}

const hopperCard: TaskBoardCard = {
  ...plainCard,
  id: 'task-c',
  task_id: 'task-c',
  title: 'Audit the release checklist',
  rank: 2,
  employee_id: 'employee-hopper',
  employee_name: 'Hopper',
}

const twoTaskBoard: TaskBoardView = { ...board, cards: [sessionCard, plainCard, hopperCard, noteCard] }

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

async function flushAsync() {
  await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)) })
}

let probeBoardApi: ReturnType<typeof useTaskBoard> | null = null

function BoardApiProbe() {
  probeBoardApi = useTaskBoard({ board, onBoardChange })
  return null
}

function renderProbe() {
  return render(
    <UIProvider>
      <BoardApiProbe />
    </UIProvider>,
  )
}

function stubHitTarget(element: Element | null) {
  return vi.spyOn(document, 'elementFromPoint').mockReturnValue(element)
}

function pointerDragCardTo(card: HTMLElement, target: Element | null) {
  stubHitTarget(target)
  fireEvent.pointerDown(card, { button: 0, isPrimary: true, pointerId: 1, clientX: 10, clientY: 10 })
  fireEvent.pointerMove(window, { pointerId: 1, clientX: 40, clientY: 40 })
  fireEvent.pointerUp(window, { pointerId: 1, clientX: 40, clientY: 40 })
}

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.setItem('gohermit.ui.locale', 'en-US')
  void i18n.changeLanguage('en-US')
  api.getEmployeeTask.mockResolvedValue(queuedTask)
  api.getTaskBoard.mockResolvedValue(board)
  api.updateTaskBoardCard.mockResolvedValue(board)
  api.startEmployeeTask.mockResolvedValue({ ...queuedTask, state: 'running', session_id: 'session-2', run_id: 'run-2' })
  api.resumeEmployeeTask.mockResolvedValue({ ...queuedTask, state: 'running', session_id: 'session-3', run_id: 'run-3' })
})

describe('TaskBoardGrid card activation', () => {
  it('navigates when clicking a task card body area outside the title', async () => {
    renderGrid()

    const card = await screen.findByRole('link', { name: `Open task detail: ${longTitle}` })
    fireEvent.click(card.querySelector('.task-board-card__meta')!)

    expect(screen.getByTestId('grid-location')).toHaveTextContent('/tasks/task-b')
  })

  it('navigates a task card with a session to exactly the session route', async () => {
    renderGrid()

    fireEvent.click(await screen.findByRole('link', { name: 'Open task session: Review release evidence' }))

    expect(screen.getByTestId('grid-location')).toHaveTextContent('/agent/sessions/session-1')
  })

  it('navigates a task card without a session to the task detail route', async () => {
    renderGrid()

    fireEvent.click(await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }))

    expect(screen.getByTestId('grid-location')).toHaveTextContent('/tasks/task-b')
  })

  it('clamps long titles without a native tooltip and keeps the full title in the aria-label', async () => {
    renderGrid()

    const card = await screen.findByRole('link', { name: `Open task detail: ${longTitle}` })
    const title = card.querySelector('.task-board-card__title')!
    expect(title).not.toHaveAttribute('title')
    expect(title).toHaveTextContent(longTitle)
    expect(card).toHaveAttribute('aria-label', `Open task detail: ${longTitle}`)
    fireEvent.mouseOver(title)
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()

    fireEvent.click(title)
    expect(screen.getByTestId('grid-location')).toHaveTextContent('/tasks/task-b')
  })

  it('activates a focused card with Enter and Space like a click', async () => {
    renderGrid()

    fireEvent.keyDown(await screen.findByRole('link', { name: 'Open task session: Review release evidence' }), { key: 'Enter' })
    expect(screen.getByTestId('grid-location')).toHaveTextContent('/agent/sessions/session-1')

    fireEvent.keyDown(screen.getByRole('link', { name: `Open task detail: ${longTitle}` }), { key: ' ' })
    expect(screen.getByTestId('grid-location')).toHaveTextContent('/tasks/task-b')
  })

  it('keeps inner loop links and note actions from triggering card activation', async () => {
    const user = userEvent.setup()
    const onUseNoteAsTask = vi.fn()
    renderGrid({ onUseNoteAsTask })

    await user.click(await screen.findByRole('link', { name: 'Loop: loop-1' }))
    expect(screen.getByTestId('grid-location')).toHaveTextContent('/loops/loop-1')

    await user.click(screen.getByRole('button', { name: 'Convert to Task draft' }))
    expect(onUseNoteAsTask).toHaveBeenCalledWith(noteCard)
    expect(screen.queryByText('Note details')).not.toBeInTheDocument()
    expect(screen.getByTestId('grid-location')).toHaveTextContent('/loops/loop-1')
  })
})

describe('TaskBoardGrid pointer dragging', () => {
  it('activates the drag only past the threshold and hit-tests the target column', async () => {
    renderGrid()

    const card = await screen.findByRole('link', { name: `Open task detail: ${longTitle}` })
    const column = screen.getByTestId('task-board-column-done')
    stubHitTarget(column)
    fireEvent.pointerDown(card, { button: 0, isPrimary: true, pointerId: 1, clientX: 10, clientY: 10 })
    fireEvent.pointerMove(window, { pointerId: 1, clientX: 13, clientY: 13 })
    expect(card).not.toHaveClass('is-dragging')

    fireEvent.pointerMove(window, { pointerId: 1, clientX: 40, clientY: 40 })
    expect(card).toHaveClass('is-dragging')
    expect(column).toHaveClass('is-drop-target')
    expect(document.body).toHaveClass('is-board-dragging')

    fireEvent.pointerUp(window, { pointerId: 1, clientX: 40, clientY: 40 })
    expect(card).not.toHaveClass('is-dragging')
    expect(column).not.toHaveClass('is-drop-target')
    expect(document.body).not.toHaveClass('is-board-dragging')
    await waitFor(() => expect(api.updateTaskBoardCard).toHaveBeenCalledWith(
      'task-b',
      expect.objectContaining({ column_id: 'done' }),
    ))
  })

  it('ignores non-primary presses and presses that start on interactive elements', async () => {
    renderGrid()

    const card = await screen.findByRole('link', { name: `Open task detail: ${longTitle}` })
    fireEvent.pointerDown(card, { button: 2, isPrimary: true, pointerId: 1, clientX: 10, clientY: 10 })
    fireEvent.pointerMove(window, { pointerId: 1, clientX: 60, clientY: 60 })
    fireEvent.pointerUp(window, { pointerId: 1, clientX: 60, clientY: 60 })
    expect(card).not.toHaveClass('is-dragging')

    const loopLink = screen.getByRole('link', { name: 'Loop: loop-1' })
    fireEvent.pointerDown(loopLink, { button: 0, isPrimary: true, pointerId: 1, clientX: 10, clientY: 10 })
    fireEvent.pointerMove(window, { pointerId: 1, clientX: 60, clientY: 60 })
    fireEvent.pointerUp(window, { pointerId: 1, clientX: 60, clientY: 60 })
    expect(card).not.toHaveClass('is-dragging')
    expect(api.updateTaskBoardCard).not.toHaveBeenCalled()
  })

  it('cancels an active drag on pointercancel without dropping', async () => {
    renderGrid()

    const card = await screen.findByRole('link', { name: `Open task detail: ${longTitle}` })
    stubHitTarget(screen.getByTestId('task-board-column-done'))
    fireEvent.pointerDown(card, { button: 0, isPrimary: true, pointerId: 1, clientX: 10, clientY: 10 })
    fireEvent.pointerMove(window, { pointerId: 1, clientX: 40, clientY: 40 })
    expect(card).toHaveClass('is-dragging')

    fireEvent.pointerCancel(window, { pointerId: 1 })
    expect(card).not.toHaveClass('is-dragging')
    expect(document.body).not.toHaveClass('is-board-dragging')
    expect(api.updateTaskBoardCard).not.toHaveBeenCalled()
  })

  it('moves a task to a different normal column and ignores same-column drops', async () => {
    renderGrid()

    const card = await screen.findByRole('link', { name: `Open task detail: ${longTitle}` })
    pointerDragCardTo(card, screen.getByTestId('task-board-column-todo'))
    expect(api.updateTaskBoardCard).not.toHaveBeenCalled()

    pointerDragCardTo(card, screen.getByTestId('task-board-column-done'))
    await waitFor(() => expect(api.updateTaskBoardCard).toHaveBeenCalledWith(
      'task-b',
      expect.objectContaining({ column_id: 'done' }),
    ))
    expect(api.startEmployeeTask).not.toHaveBeenCalled()
    expect(api.resumeEmployeeTask).not.toHaveBeenCalled()
  })

  it('suppresses a click fired immediately after drag end', async () => {
    renderGrid()

    const card = await screen.findByRole('link', { name: `Open task detail: ${longTitle}` })
    pointerDragCardTo(card, null)
    fireEvent.click(card)

    expect(screen.getByTestId('grid-location')).toHaveTextContent('/')
    expect(screen.queryByText('Note details')).not.toBeInTheDocument()
  })

  it('moves a note into In progress without starting anything and opens the note modal on click', async () => {
    const user = userEvent.setup()
    renderGrid()

    const note = await screen.findByRole('link', { name: 'Open note: Capture rollout' })
    pointerDragCardTo(note, screen.getByTestId('task-board-column-in_progress'))
    await waitFor(() => expect(api.updateTaskBoardCard).toHaveBeenCalledWith(
      'note-1',
      expect.objectContaining({ column_id: 'in_progress' }),
    ))
    expect(api.startEmployeeTask).not.toHaveBeenCalled()
    expect(api.resumeEmployeeTask).not.toHaveBeenCalled()
    expect(screen.queryByText('Confirm Task start')).not.toBeInTheDocument()

    // The drop marks the drag end; clicks inside the 300ms suppression window are ignored.
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 350)) })
    await user.click(note)
    const dialog = await screen.findByRole('dialog', { name: 'Note details' })
    expect(dialog).toHaveTextContent('Record the release evidence before shipping.')
    expect(screen.getByTestId('grid-location')).toHaveTextContent('/')
  })
})

describe('TaskBoardGrid Start confirmation', () => {
  it('cancelling the Start confirmation performs no start, resume, or card move', async () => {
    const user = userEvent.setup()
    renderGrid()

    pointerDragCardTo(
      await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
      screen.getByTestId('task-board-column-in_progress'),
    )
    await screen.findByText('Confirm Task start')
    await user.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(api.startEmployeeTask).not.toHaveBeenCalled()
    expect(api.resumeEmployeeTask).not.toHaveBeenCalled()
    expect(api.updateTaskBoardCard).not.toHaveBeenCalled()
  })

  it('rolls back with a board refetch and error toast when starting the dropped task fails', async () => {
    const user = userEvent.setup()
    api.startEmployeeTask.mockRejectedValue(new ApiError('http_error', 409))
    renderGrid()

    pointerDragCardTo(
      await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
      screen.getByTestId('task-board-column-in_progress'),
    )
    await screen.findByText('Confirm Task start')
    await user.click(screen.getByRole('button', { name: 'Start' }))

    await waitFor(() => expect(api.startEmployeeTask).toHaveBeenCalledWith('task-b'))
    expect(api.updateTaskBoardCard).not.toHaveBeenCalled()
    await waitFor(() => expect(api.getTaskBoard).toHaveBeenCalled())
    expect(screen.getByTestId('grid-toast')).toHaveTextContent('error:mutation.conflict')
  })

  it('opens the Start confirmation with content immediately when the task resolves synchronously', async () => {
    renderGrid({ resolveTask: () => queuedTask })

    pointerDragCardTo(
      await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
      screen.getByTestId('task-board-column-in_progress'),
    )

    expect(screen.getByText('Confirm Task start')).toBeInTheDocument()
    expect(screen.queryByTestId('task-board-start-loading')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Start' })).toBeEnabled()
  })

  it('shows a spinner with a disabled confirm until an async task load resolves', async () => {
    let resolveLoad!: (task: EmployeeTask) => void
    api.getEmployeeTask.mockImplementation(() => new Promise<EmployeeTask>((resolve) => { resolveLoad = resolve }))
    renderGrid()

    pointerDragCardTo(
      await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
      screen.getByTestId('task-board-column-in_progress'),
    )

    const dialog = await screen.findByRole('dialog')
    expect(screen.getByTestId('task-board-start-loading')).toBeInTheDocument()
    const startButton = screen.getByRole('button', { name: 'Start' })
    expect(startButton).toBeDisabled()
    expect(dialog).not.toHaveTextContent('Workspace')

    act(() => { resolveLoad(queuedTask) })
    await waitFor(() => expect(startButton).toBeEnabled())
    expect(screen.queryByTestId('task-board-start-loading')).not.toBeInTheDocument()
    expect(dialog).toHaveTextContent('Workspace')
  })

  it('closes the modal, toasts, and refetches when the task load 404s', async () => {
    api.getEmployeeTask.mockRejectedValue(new ApiError('http_error', 404))
    renderGrid()

    pointerDragCardTo(
      await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
      screen.getByTestId('task-board-column-in_progress'),
    )

    await waitFor(() => expect(screen.getByTestId('grid-toast')).toHaveTextContent('error:mutation.failed'))
    await waitFor(() => expect(api.getTaskBoard).toHaveBeenCalled())
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(api.startEmployeeTask).not.toHaveBeenCalled()
    expect(api.updateTaskBoardCard).not.toHaveBeenCalled()
  })

  it('closes the modal, toasts offline, and refetches when the task load hits a network error', async () => {
    api.getEmployeeTask.mockRejectedValue(new ApiError('network_error'))
    renderGrid()

    pointerDragCardTo(
      await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
      screen.getByTestId('task-board-column-in_progress'),
    )

    await waitFor(() => expect(screen.getByTestId('grid-toast')).toHaveTextContent('error:mutation.offline'))
    await waitFor(() => expect(api.getTaskBoard).toHaveBeenCalled())
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(api.startEmployeeTask).not.toHaveBeenCalled()
    expect(api.updateTaskBoardCard).not.toHaveBeenCalled()
  })
})

describe('TaskBoardGrid Start gating by task state', () => {
  for (const state of ['queued', 'prepared'] as const) {
    it(`asks for confirmation and starts a ${state} task dropped on In progress`, async () => {
      const user = userEvent.setup()
      api.getEmployeeTask.mockResolvedValue({ ...queuedTask, state })
      renderGrid({ board: boardWithTaskState(state) })

      pointerDragCardTo(
        await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
        screen.getByTestId('task-board-column-in_progress'),
      )
      await screen.findByText('Confirm Task start')
      await user.click(screen.getByRole('button', { name: 'Start' }))

      await waitFor(() => expect(api.startEmployeeTask).toHaveBeenCalledWith('task-b'))
      expect(api.resumeEmployeeTask).not.toHaveBeenCalled()
      await waitFor(() => expect(api.updateTaskBoardCard).toHaveBeenCalledWith(
        'task-b',
        expect.objectContaining({ column_id: 'in_progress' }),
      ))
    })
  }

  it('asks for confirmation and resumes an interrupted task dropped on In progress', async () => {
    const user = userEvent.setup()
    api.getEmployeeTask.mockResolvedValue({ ...queuedTask, state: 'interrupted' })
    renderGrid({ board: boardWithTaskState('interrupted') })

    pointerDragCardTo(
      await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
      screen.getByTestId('task-board-column-in_progress'),
    )
    await screen.findByText('Confirm Task start')
    await user.click(screen.getByRole('button', { name: 'Start' }))

    await waitFor(() => expect(api.resumeEmployeeTask).toHaveBeenCalledWith('task-b'))
    expect(api.startEmployeeTask).not.toHaveBeenCalled()
    await waitFor(() => expect(api.updateTaskBoardCard).toHaveBeenCalledWith(
      'task-b',
      expect.objectContaining({ column_id: 'in_progress' }),
    ))
  })

  for (const state of ['running', 'verifying', 'waiting_owner', 'completed', 'failed', 'cancelled'] as const) {
    it(`refuses to start a ${state} task dropped on In progress with no mutation at all`, async () => {
      renderGrid({ board: boardWithTaskState(state) })

      pointerDragCardTo(
        await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
        screen.getByTestId('task-board-column-in_progress'),
      )

      await waitFor(() => expect(screen.getByTestId('grid-toast')).toHaveTextContent('info:tasks.startNotAllowed'))
      expect(screen.queryByText('Confirm Task start')).not.toBeInTheDocument()
      expect(api.startEmployeeTask).not.toHaveBeenCalled()
      expect(api.resumeEmployeeTask).not.toHaveBeenCalled()
      expect(api.updateTaskBoardCard).not.toHaveBeenCalled()
      expect(api.getEmployeeTask).not.toHaveBeenCalled()
    })
  }

  it('still moves a non-startable task into other columns without Start semantics', async () => {
    renderGrid({ board: boardWithTaskState('completed') })

    pointerDragCardTo(
      await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
      screen.getByTestId('task-board-column-done'),
    )

    await waitFor(() => expect(api.updateTaskBoardCard).toHaveBeenCalledWith(
      'task-b',
      expect.objectContaining({ column_id: 'done' }),
    ))
    expect(api.startEmployeeTask).not.toHaveBeenCalled()
    expect(api.resumeEmployeeTask).not.toHaveBeenCalled()
  })
})

describe('TaskBoardGrid Start request lifecycle', () => {
  it('does not reopen the Start modal when a cancelled load later resolves', async () => {
    const load = deferred<EmployeeTask>()
    api.getEmployeeTask.mockImplementation(() => load.promise)
    const user = userEvent.setup()
    renderGrid()

    pointerDragCardTo(
      await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
      screen.getByTestId('task-board-column-in_progress'),
    )
    await screen.findByRole('dialog')
    expect(screen.getByTestId('task-board-start-loading')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())

    await act(async () => { load.resolve(queuedTask); await load.promise })
    await flushAsync()

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.queryByTestId('grid-toast')).not.toBeInTheDocument()
    expect(api.startEmployeeTask).not.toHaveBeenCalled()
    expect(api.resumeEmployeeTask).not.toHaveBeenCalled()
    expect(api.updateTaskBoardCard).not.toHaveBeenCalled()
  })

  it('shows no late toast or refetch when a cancelled load later rejects', async () => {
    const load = deferred<EmployeeTask>()
    api.getEmployeeTask.mockImplementation(() => load.promise)
    const user = userEvent.setup()
    renderGrid()

    pointerDragCardTo(
      await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
      screen.getByTestId('task-board-column-in_progress'),
    )
    await screen.findByRole('dialog')
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())

    await act(async () => {
      load.reject(new ApiError('http_error', 404))
      await load.promise.catch(() => undefined)
    })
    await flushAsync()

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.queryByTestId('grid-toast')).not.toBeInTheDocument()
    expect(api.getTaskBoard).not.toHaveBeenCalled()
    expect(api.updateTaskBoardCard).not.toHaveBeenCalled()
  })

  it('keeps only the newest Start candidate when an earlier load resolves last', async () => {
    const loadA = deferred<EmployeeTask>()
    const loadB = deferred<EmployeeTask>()
    api.getEmployeeTask.mockImplementation((taskId: string) => (taskId === 'task-c' ? loadB.promise : loadA.promise))
    const user = userEvent.setup()
    renderGrid({ board: twoTaskBoard })

    pointerDragCardTo(
      await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
      screen.getByTestId('task-board-column-in_progress'),
    )
    await screen.findByRole('dialog')
    pointerDragCardTo(
      screen.getByRole('link', { name: 'Open task detail: Audit the release checklist' }),
      screen.getByTestId('task-board-column-in_progress'),
    )

    await act(async () => { loadA.resolve(queuedTask); await loadA.promise })
    await flushAsync()
    const dialog = screen.getByRole('dialog')
    expect(dialog).not.toHaveTextContent('Grace')

    await act(async () => { loadB.resolve({ ...queuedTask, id: 'task-c', employee_id: 'employee-hopper' }); await loadB.promise })
    await waitFor(() => expect(within(dialog).getByText('Hopper')).toBeInTheDocument())
    expect(within(dialog).queryByText('Grace')).not.toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: 'Start' }))
    await waitFor(() => expect(api.startEmployeeTask).toHaveBeenCalledWith('task-c'))
    expect(api.startEmployeeTask).toHaveBeenCalledTimes(1)
    expect(api.resumeEmployeeTask).not.toHaveBeenCalled()
    expect(screen.queryByTestId('grid-toast')).not.toBeInTheDocument()
  })

  it('ignores an in-flight Start load that resolves after unmount', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const load = deferred<EmployeeTask>()
    api.getEmployeeTask.mockImplementation(() => load.promise)
    const view = renderGrid()

    pointerDragCardTo(
      await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
      screen.getByTestId('task-board-column-in_progress'),
    )
    await screen.findByRole('dialog')

    view.unmount()
    await act(async () => { load.resolve(queuedTask); await load.promise })
    await flushAsync()

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(api.getTaskBoard).not.toHaveBeenCalled()
    expect(api.startEmployeeTask).not.toHaveBeenCalled()
    expect(api.resumeEmployeeTask).not.toHaveBeenCalled()
    expect(api.updateTaskBoardCard).not.toHaveBeenCalled()
    expect(consoleError).not.toHaveBeenCalled()
  })
})

describe('TaskBoardGrid Start re-validation against the loaded Task', () => {
  it('rejects the Start when a queued-looking card loads as waiting_owner', async () => {
    api.getEmployeeTask.mockResolvedValue({ ...queuedTask, state: 'waiting_owner' })
    renderGrid()

    pointerDragCardTo(
      await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
      screen.getByTestId('task-board-column-in_progress'),
    )

    await waitFor(() => expect(screen.getByTestId('grid-toast')).toHaveTextContent('info:tasks.startNotAllowed'))
    await waitFor(() => expect(api.getTaskBoard).toHaveBeenCalled())
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(api.startEmployeeTask).not.toHaveBeenCalled()
    expect(api.resumeEmployeeTask).not.toHaveBeenCalled()
    expect(api.updateTaskBoardCard).not.toHaveBeenCalled()
  })

  for (const state of ['completed', 'failed', 'cancelled'] as const) {
    it(`rejects the Start when a queued-looking card loads as ${state}`, async () => {
      api.getEmployeeTask.mockResolvedValue({ ...queuedTask, state })
      renderGrid()

      pointerDragCardTo(
        await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
        screen.getByTestId('task-board-column-in_progress'),
      )

      await waitFor(() => expect(screen.getByTestId('grid-toast')).toHaveTextContent('info:tasks.startNotAllowed'))
      await waitFor(() => expect(api.getTaskBoard).toHaveBeenCalled())
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      expect(api.startEmployeeTask).not.toHaveBeenCalled()
      expect(api.resumeEmployeeTask).not.toHaveBeenCalled()
      expect(api.updateTaskBoardCard).not.toHaveBeenCalled()
    })
  }

  it('resumes instead of starting when a queued-looking card loads as interrupted', async () => {
    const user = userEvent.setup()
    api.getEmployeeTask.mockResolvedValue({ ...queuedTask, state: 'interrupted' })
    renderGrid()

    pointerDragCardTo(
      await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
      screen.getByTestId('task-board-column-in_progress'),
    )
    await screen.findByText('Confirm Task start')
    await user.click(screen.getByRole('button', { name: 'Start' }))

    await waitFor(() => expect(api.resumeEmployeeTask).toHaveBeenCalledWith('task-b'))
    expect(api.startEmployeeTask).not.toHaveBeenCalled()
    await waitFor(() => expect(api.updateTaskBoardCard).toHaveBeenCalledWith(
      'task-b',
      expect.objectContaining({ column_id: 'in_progress' }),
    ))
  })
})

describe('TaskBoardGrid committed Start transaction', () => {
  it('locks cancel, close, mask, and Escape while the Start POST is in flight', async () => {
    const post = deferred<EmployeeTask>()
    api.startEmployeeTask.mockImplementation(() => post.promise)
    const user = userEvent.setup()
    renderGrid()

    pointerDragCardTo(
      await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
      screen.getByTestId('task-board-column-in_progress'),
    )
    await screen.findByText('Confirm Task start')
    await user.click(screen.getByRole('button', { name: 'Start' }))
    await waitFor(() => expect(api.startEmployeeTask).toHaveBeenCalledWith('task-b'))

    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled()
    expect(document.querySelector('.ant-modal-close')).not.toBeInTheDocument()

    await user.keyboard('{Escape}')
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    const wrap = document.querySelector('.ant-modal-wrap')
    expect(wrap).not.toBeNull()
    fireEvent.click(wrap!)
    expect(screen.getByRole('dialog')).toBeInTheDocument()

    await act(async () => {
      post.resolve({ ...queuedTask, state: 'running', session_id: 'session-2', run_id: 'run-2' })
      await post.promise
    })
    await waitFor(() => expect(api.updateTaskBoardCard).toHaveBeenCalledWith(
      'task-b',
      expect.objectContaining({ column_id: 'in_progress' }),
    ))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(api.startEmployeeTask).toHaveBeenCalledTimes(1)
    expect(api.updateTaskBoardCard).toHaveBeenCalledTimes(1)
  })

  it('ignores a programmatic cancelStartCandidate while the Start POST is in flight', async () => {
    const post = deferred<EmployeeTask>()
    api.startEmployeeTask.mockImplementation(() => post.promise)
    renderProbe()
    const boardApi = () => {
      expect(probeBoardApi).not.toBeNull()
      return probeBoardApi!
    }

    act(() => { boardApi().dropCard(plainCard, 'in_progress') })
    await waitFor(() => expect(boardApi().startCandidate?.task?.id).toBe('task-b'))
    const candidate = boardApi().startCandidate
    act(() => { void boardApi().confirmStartCandidate() })
    await waitFor(() => expect(api.startEmployeeTask).toHaveBeenCalledWith('task-b'))
    expect(boardApi().startBusy).toBe(true)

    act(() => { boardApi().cancelStartCandidate() })
    expect(boardApi().startCandidate).toBe(candidate)
    expect(boardApi().startBusy).toBe(true)

    await act(async () => { post.resolve({ ...queuedTask, state: 'running' }); await post.promise })
    await waitFor(() => expect(api.updateTaskBoardCard).toHaveBeenCalledWith(
      'task-b',
      expect.objectContaining({ column_id: 'in_progress' }),
    ))
    await waitFor(() => expect(boardApi().startCandidate).toBeNull())
    expect(api.startEmployeeTask).toHaveBeenCalledTimes(1)
  })

  it('issues exactly one Start POST followed by exactly one card PUT on the happy path', async () => {
    const user = userEvent.setup()
    renderGrid()

    pointerDragCardTo(
      await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
      screen.getByTestId('task-board-column-in_progress'),
    )
    await screen.findByText('Confirm Task start')
    await user.click(screen.getByRole('button', { name: 'Start' }))

    await waitFor(() => expect(api.startEmployeeTask).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(api.updateTaskBoardCard).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('closes the interaction with a single POST total when the card PUT fails after a committed Start', async () => {
    const user = userEvent.setup()
    api.updateTaskBoardCard.mockRejectedValue(new ApiError('http_error', 409))
    renderGrid()

    pointerDragCardTo(
      await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
      screen.getByTestId('task-board-column-in_progress'),
    )
    await screen.findByText('Confirm Task start')
    await user.click(screen.getByRole('button', { name: 'Start' }))

    await waitFor(() => expect(screen.getByTestId('grid-toast')).toHaveTextContent('error:mutation.conflict'))
    await waitFor(() => expect(api.getTaskBoard).toHaveBeenCalled())
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(api.startEmployeeTask).toHaveBeenCalledTimes(1)
    expect(api.resumeEmployeeTask).not.toHaveBeenCalled()
    expect(api.updateTaskBoardCard).toHaveBeenCalledTimes(1)
  })

  it('keeps the modal open after a failed Start POST and allows an explicit retry', async () => {
    const user = userEvent.setup()
    api.startEmployeeTask.mockRejectedValueOnce(new ApiError('http_error', 409))
    renderGrid()

    pointerDragCardTo(
      await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
      screen.getByTestId('task-board-column-in_progress'),
    )
    await screen.findByText('Confirm Task start')
    await user.click(screen.getByRole('button', { name: 'Start' }))

    await waitFor(() => expect(screen.getByTestId('grid-toast')).toHaveTextContent('error:mutation.conflict'))
    expect(api.updateTaskBoardCard).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeEnabled()

    await user.click(screen.getByRole('button', { name: 'Start' }))
    await waitFor(() => expect(api.startEmployeeTask).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(api.updateTaskBoardCard).toHaveBeenCalledWith(
      'task-b',
      expect.objectContaining({ column_id: 'in_progress' }),
    ))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('allows cancelling after a failed Start POST because nothing committed', async () => {
    const user = userEvent.setup()
    api.startEmployeeTask.mockRejectedValue(new ApiError('http_error', 409))
    renderGrid()

    pointerDragCardTo(
      await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }),
      screen.getByTestId('task-board-column-in_progress'),
    )
    await screen.findByText('Confirm Task start')
    await user.click(screen.getByRole('button', { name: 'Start' }))

    await waitFor(() => expect(screen.getByTestId('grid-toast')).toHaveTextContent('error:mutation.conflict'))
    expect(api.updateTaskBoardCard).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(api.startEmployeeTask).toHaveBeenCalledTimes(1)
    expect(api.resumeEmployeeTask).not.toHaveBeenCalled()
  })
})
