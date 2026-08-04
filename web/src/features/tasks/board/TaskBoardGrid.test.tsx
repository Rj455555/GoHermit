import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../../api/errors'
import type { EmployeeTask, TaskBoardCard, TaskBoardView } from '../../../api/types'
import { i18n } from '../../../i18n/i18n'
import { UIProvider, useUI } from '../../../state/UIContext'
import { TaskBoardGrid } from './TaskBoardGrid'

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

function renderGrid(options?: { onUseNoteAsTask?: (note: TaskBoardCard) => void }) {
  return render(
    <I18nextProvider i18n={i18n}>
      <UIProvider>
        <MemoryRouter initialEntries={['/']}>
          <GridLocationProbe />
          <ToastProbe />
          <Routes>
            <Route path="*" element={<TaskBoardGrid board={board} onBoardChange={onBoardChange} {...(options ?? {})} />} />
          </Routes>
        </MemoryRouter>
      </UIProvider>
    </I18nextProvider>,
  )
}

function dragCardToColumn(card: HTMLElement, column: HTMLElement) {
  const dataTransfer = { setData: vi.fn(), getData: vi.fn(), effectAllowed: '', dropEffect: '' }
  fireEvent.dragStart(card, { dataTransfer })
  fireEvent.dragOver(column, { dataTransfer })
  fireEvent.drop(column, { dataTransfer })
  return dataTransfer
}

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.setItem('gohermit.ui.locale', 'en-US')
  void i18n.changeLanguage('en-US')
  api.getEmployeeTask.mockResolvedValue(queuedTask)
  api.getTaskBoard.mockResolvedValue(board)
  api.updateTaskBoardCard.mockResolvedValue(board)
  api.startEmployeeTask.mockResolvedValue({ ...queuedTask, state: 'running', session_id: 'session-2', run_id: 'run-2' })
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

  it('clamps long titles with a native title attribute and no tooltip overlay blocking navigation', async () => {
    renderGrid()

    const title = (await screen.findByRole('link', { name: `Open task detail: ${longTitle}` }))
      .querySelector('.task-board-card__title')!
    expect(title).toHaveAttribute('title', longTitle)
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

describe('TaskBoardGrid drag and drop', () => {
  it('moves a task to a different normal column and ignores same-column drops', async () => {
    renderGrid()

    const card = await screen.findByRole('link', { name: `Open task detail: ${longTitle}` })
    const dataTransfer = dragCardToColumn(card, screen.getByTestId('task-board-column-todo'))
    expect(dataTransfer.setData).toHaveBeenCalledWith('text/plain', 'task-b')
    expect(api.updateTaskBoardCard).not.toHaveBeenCalled()

    dragCardToColumn(card, screen.getByTestId('task-board-column-done'))
    await waitFor(() => expect(api.updateTaskBoardCard).toHaveBeenCalledWith(
      'task-b',
      expect.objectContaining({ column_id: 'done' }),
    ))
    expect(api.startEmployeeTask).not.toHaveBeenCalled()
    expect(api.resumeEmployeeTask).not.toHaveBeenCalled()
  })

  it('cancelling the Start confirmation performs no start, resume, or card move', async () => {
    const user = userEvent.setup()
    renderGrid()

    dragCardToColumn(
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

    dragCardToColumn(
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

  it('suppresses a click fired immediately after drag end', async () => {
    renderGrid()

    const card = await screen.findByRole('link', { name: `Open task detail: ${longTitle}` })
    fireEvent.dragStart(card)
    fireEvent.dragEnd(card)
    fireEvent.click(card)

    expect(screen.getByTestId('grid-location')).toHaveTextContent('/')
    expect(screen.queryByText('Note details')).not.toBeInTheDocument()
  })

  it('moves a note into In progress without starting anything and opens the note modal on click', async () => {
    const user = userEvent.setup()
    renderGrid()

    const note = await screen.findByRole('link', { name: 'Open note: Capture rollout' })
    dragCardToColumn(note, screen.getByTestId('task-board-column-in_progress'))
    await waitFor(() => expect(api.updateTaskBoardCard).toHaveBeenCalledWith(
      'note-1',
      expect.objectContaining({ column_id: 'in_progress' }),
    ))
    expect(api.startEmployeeTask).not.toHaveBeenCalled()
    expect(api.resumeEmployeeTask).not.toHaveBeenCalled()
    expect(screen.queryByText('Confirm Task start')).not.toBeInTheDocument()

    await user.click(note)
    const dialog = await screen.findByRole('dialog', { name: 'Note details' })
    expect(dialog).toHaveTextContent('Record the release evidence before shipping.')
    expect(screen.getByTestId('grid-location')).toHaveTextContent('/')
  })
})
