import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { act, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { DashboardPage } from './DashboardPage'
import { i18n } from '../../i18n/i18n'

const api = vi.hoisted(() => ({
  getInfo: vi.fn(),
  listLoops: vi.fn(),
  listSessions: vi.fn(),
  listLoopInvocations: vi.fn(),
}))

vi.mock('../../api/endpoints', () => api)
vi.mock('../../components/ConnectivityProvider', () => ({
  useConnectivity: () => ({ status: 'online', generation: 0, canMutate: true, reconnect: vi.fn() }),
}))

describe('DashboardPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    void i18n.changeLanguage('zh-CN')
  })

  function renderDashboard() {
    return render(
      <I18nextProvider i18n={i18n}>
        <MemoryRouter>
          <DashboardPage />
        </MemoryRouter>
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
    expect(screen.getByRole('heading', { name: 'Nightly' }).closest('section')).toHaveTextContent(/Nightly.*失败/u)
    await act(() => i18n.changeLanguage('en-US'))
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Nightly' }).closest('section')).toHaveTextContent(/Nightly.*Failed/u),
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
    expect(screen.getByRole('heading', { name: 'Literal Loop' }).closest('section')).toHaveTextContent(
      /Literal Loop.*未知状态/u,
    )
    expect(screen.queryByText(/invocationStatus|future_state/u)).not.toBeInTheDocument()
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
})
