import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { act, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { DashboardPage } from './DashboardPage'
import { i18n } from '../../i18n/i18n'
import { UIProvider } from '../../state/UIContext'

const api = vi.hoisted(() => ({
  getInfo: vi.fn(),
  listLoops: vi.fn(),
  listSessions: vi.fn(),
  listLoopInvocations: vi.fn(),
  getTaskBoard: vi.fn(),
}))

vi.mock('../../api/endpoints', () => api)
vi.mock('../../components/ConnectivityProvider', () => ({
  useConnectivity: () => ({ status: 'online', generation: 0, canMutate: true, reconnect: vi.fn() }),
}))

describe('DashboardPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.getTaskBoard.mockResolvedValue(null)
    void i18n.changeLanguage('zh-CN')
  })

  function renderDashboard() {
    return render(
      <I18nextProvider i18n={i18n}>
        <UIProvider>
          <MemoryRouter>
            <DashboardPage />
          </MemoryRouter>
        </UIProvider>
      </I18nextProvider>,
    )
  }

  it('renders real readiness, bounded invocation summaries, and active Session hints', async () => {
    api.getInfo.mockResolvedValue({
      workspace: '/workspace/gohermit',
      available_companies: [{ id: 'openai', access: [{ id: 'openai-codex' }] }],
      auth_status: { 'openai-codex': { configured: true, detail: 'ready', source: 'Codex' } },
    })
    api.listLoops.mockResolvedValue({
      loops: [{ id: 'loop-1', name: 'Nightly', enabled: true }],
    })
    api.listSessions.mockResolvedValue({
      sessions: [{ id: 'session-1', title: 'Keep this title', active_run_id: 'run-1' }],
    })
    api.listLoopInvocations.mockResolvedValue({
      invocations: [
        { id: 'inv-1', loop_id: 'loop-1', status: 'attached', created_at: '2026-07-29T08:00:00Z' },
        { id: 'inv-2', loop_id: 'loop-1', status: 'completed', created_at: '2026-07-29T09:00:00Z' },
        { id: 'inv-3', loop_id: 'loop-1', status: 'failed', created_at: '2026-07-29T10:00:00Z' },
      ],
    })

    renderDashboard()

    expect(await screen.findByText('/workspace/gohermit')).toBeVisible()
    expect(screen.getByText('Keep this title')).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Nightly' })).toBeVisible()
    expect(screen.getByTestId('invocation-completed')).toHaveTextContent('1')
    expect(screen.getByTestId('invocation-failed')).toHaveTextContent('1')
    expect(screen.getByRole('heading', { name: 'Nightly' }).closest('.dashboard-hero-card')).toHaveTextContent(/Nightly.*失败/u)
    await act(() => i18n.changeLanguage('en-US'))
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Nightly' }).closest('.dashboard-hero-card')).toHaveTextContent(/Nightly.*Failed/u),
    )
    expect(screen.getByText('Keep this title')).toBeVisible()
    expect(api.listLoopInvocations).toHaveBeenCalledTimes(1)
  })

  it('uses a localized fallback for an unknown Invocation status', async () => {
    api.getInfo.mockResolvedValue({
      workspace: '/workspace/gohermit',
      available_companies: [],
      auth_status: {},
    })
    api.listLoops.mockResolvedValue({
      loops: [{ id: 'loop-1', name: 'Literal Loop', enabled: true }],
    })
    api.listSessions.mockResolvedValue({ sessions: [] })
    api.listLoopInvocations.mockResolvedValue({
      invocations: [{ id: 'inv-1', loop_id: 'loop-1', status: 'future_state', created_at: '2026-07-29T08:00:00Z' }],
    })

    renderDashboard()

    await screen.findByRole('heading', { name: 'Literal Loop' })
    expect(screen.getByRole('heading', { name: 'Literal Loop' }).closest('.dashboard-hero-card')).toHaveTextContent(
      /Literal Loop.*未知状态/u,
    )
    expect(screen.queryByText(/invocationStatus|future_state/u)).not.toBeInTheDocument()
  })

  it('surfaces the authoritative Task Board on Dashboard', async () => {
    api.getInfo.mockResolvedValue({ workspace: '/workspace/gohermit', available_companies: [], auth_status: {} })
    api.listLoops.mockResolvedValue({ loops: [] })
    api.listSessions.mockResolvedValue({ sessions: [] })
    api.listLoopInvocations.mockResolvedValue({ invocations: [] })
    api.getTaskBoard.mockResolvedValue({
      schema_version: 1,
      definition: { id: 'software', name: 'Software development', columns: [
        { id: 'todo', title: 'Todo', color: '#2563eb', hidden: false },
        { id: 'in_progress', title: 'In progress', color: '#0891b2', hidden: false },
      ] },
      cards: [{
        id: 'task-1', task_id: 'task-1', kind: 'task', title: 'Review the board placement', column_id: 'todo', rank: 1,
        labels: ['product'], priority: 1, pinned: false, blocked: false, depends_on: [], projection_reason: 'authoritative',
        authoritative_updated_at: '2026-08-04T08:00:00Z', session_event_sequence: 0, session_count: 0,
        approval_status: 'none', verification_status: 'none', stale: false, state: 'queued', employee_name: 'Planner',
      }],
      view: { view: 'board', wip_enabled: true },
      filters: { states: [], labels: [] },
      updated_at: '2026-08-04T08:00:00Z', projection_generated_at: '2026-08-04T08:00:00Z',
    })

    renderDashboard()

    expect(await screen.findByTestId('dashboard-task-board')).toBeVisible()
    expect(screen.getByText('Review the board placement')).toBeVisible()
    expect(screen.getByTestId('dashboard-task-board-column-todo')).toHaveTextContent('Todo')
    expect(screen.getByRole('link', { name: i18n.t('dashboard.openTaskBoard') })).toHaveAttribute('href', '/tasks?view=board')
  })

  it('renders draggable task cards from every Employee through the shared board grid', async () => {
    api.getInfo.mockResolvedValue({ workspace: '/workspace/gohermit', available_companies: [], auth_status: {} })
    api.listLoops.mockResolvedValue({ loops: [] })
    api.listSessions.mockResolvedValue({ sessions: [] })
    api.listLoopInvocations.mockResolvedValue({ invocations: [] })
    api.getTaskBoard.mockResolvedValue({
      schema_version: 1,
      definition: { id: 'software', name: 'Software development', columns: [
        { id: 'todo', title: 'Todo', color: '#2563eb', hidden: false },
        { id: 'in_progress', title: 'In progress', color: '#0891b2', hidden: false },
      ] },
      cards: [{
        id: 'task-1', task_id: 'task-1', kind: 'task', title: 'Review the board placement', column_id: 'todo', rank: 1,
        labels: [], priority: 0, pinned: false, blocked: false, depends_on: [], projection_reason: 'authoritative',
        authoritative_updated_at: '2026-08-04T08:00:00Z', session_event_sequence: 0, session_count: 0,
        approval_status: 'none', verification_status: 'none', stale: false, state: 'queued',
        employee_id: 'employee-ada', employee_name: 'Ada',
      }, {
        id: 'task-2', task_id: 'task-2', kind: 'task', title: 'Audit the release checklist', column_id: 'todo', rank: 2,
        labels: [], priority: 0, pinned: false, blocked: false, depends_on: [], projection_reason: 'authoritative',
        authoritative_updated_at: '2026-08-04T08:00:00Z', session_event_sequence: 0, session_count: 0,
        approval_status: 'none', verification_status: 'none', stale: false, state: 'queued',
        employee_id: 'employee-grace', employee_name: 'Grace',
      }],
      view: { view: 'board', wip_enabled: true },
      filters: { states: [], labels: [] },
      updated_at: '2026-08-04T08:00:00Z', projection_generated_at: '2026-08-04T08:00:00Z',
    })

    renderDashboard()

    const grid = await screen.findByTestId('dashboard-task-board')
    // Shared grid structure: same card class and column testid scheme as the Tasks board.
    expect(screen.getByTestId('dashboard-task-board-column-todo')).toBeInTheDocument()
    const cards = grid.querySelectorAll('.task-board-card')
    expect(cards).toHaveLength(2)
    for (const card of cards) expect(card).toHaveAttribute('draggable', 'true')
    expect(grid).toHaveTextContent('Ada')
    expect(grid).toHaveTextContent('Grace')
  })

  it('keeps the authoritative workspace visible when supporting history fails', async () => {
    api.getInfo.mockResolvedValue({
      workspace: '/workspace/gohermit',
      available_companies: [],
      auth_status: {},
      active: false,
    })
    api.listLoops.mockRejectedValue(new Error('legacy loop store'))
    api.listSessions.mockRejectedValue(new Error('legacy session summary'))

    renderDashboard()

    expect(await screen.findByText('/workspace/gohermit')).toBeVisible()
    expect(screen.queryByText(i18n.t('dashboard.errorTitle'))).not.toBeInTheDocument()
    expect(screen.getByText(i18n.t('connectivity.stale'))).toBeVisible()
  })

  it('aborts route-owned reads on unmount', async () => {
    let capturedSignal: AbortSignal | undefined
    api.getInfo.mockImplementation(({ signal }: { signal: AbortSignal }) => {
      capturedSignal = signal
      return new Promise(() => {})
    })
    api.listLoops.mockReturnValue(new Promise(() => {}))
    api.listSessions.mockReturnValue(new Promise(() => {}))

    const view = renderDashboard()
    view.unmount()

    await waitFor(() => expect(capturedSignal?.aborted).toBe(true))
  })

  it('keeps one vertical stack and one hero surface', async () => {
    api.getInfo.mockResolvedValue({ workspace: '/workspace/gohermit', available_companies: [], auth_status: {} })
    api.listLoops.mockResolvedValue({ loops: [] })
    api.listSessions.mockResolvedValue({ sessions: [] })
    api.listLoopInvocations.mockResolvedValue({ invocations: [] })

    renderDashboard()

    await screen.findByRole('heading', { name: /仪表盘|Dashboard/u })
    expect(document.querySelectorAll('.dashboard-hero-card')).toHaveLength(1)
    expect(document.querySelectorAll('.dashboard-content-stack')).toHaveLength(1)
    expect(document.querySelectorAll('.dashboard-page > .hero')).toHaveLength(0)
  })
})
