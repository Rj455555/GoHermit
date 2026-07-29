import { I18nextProvider } from 'react-i18next'
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

    render(<I18nextProvider i18n={i18n}><DashboardPage /></I18nextProvider>)

    expect(await screen.findByText('/workspace/gohermit')).toBeVisible()
    expect(screen.getByText('Keep this title')).toBeVisible()
    expect(screen.getByText('Nightly')).toBeVisible()
    expect(screen.getByTestId('invocation-completed')).toHaveTextContent('1')
    expect(screen.getByTestId('invocation-failed')).toHaveTextContent('1')
    expect(screen.getByRole('heading', { name: i18n.t('dashboard.recent') }).parentElement).toHaveTextContent('Nightly · 失败')
    await act(() => i18n.changeLanguage('en-US'))
    await waitFor(() => expect(screen.getByRole('heading', { name: i18n.t('dashboard.recent') }).parentElement).toHaveTextContent('Nightly · Failed'))
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

    render(<I18nextProvider i18n={i18n}><DashboardPage /></I18nextProvider>)

    await screen.findByRole('heading', { name: i18n.t('dashboard.recent') })
    expect(screen.getByRole('heading', { name: i18n.t('dashboard.recent') }).parentElement).toHaveTextContent('Literal Loop · 未知状态')
    expect(screen.queryByText(/invocationStatus|future_state/u)).not.toBeInTheDocument()
  })

  it('aborts route-owned reads on unmount', async () => {
    let capturedSignal: AbortSignal | undefined
    api.getInfo.mockImplementation(({ signal }: { signal: AbortSignal }) => {
      capturedSignal = signal
      return new Promise(() => {})
    })
    api.listLoops.mockReturnValue(new Promise(() => {}))
    api.listSessions.mockReturnValue(new Promise(() => {}))

    const view = render(<I18nextProvider i18n={i18n}><DashboardPage /></I18nextProvider>)
    view.unmount()

    await waitFor(() => expect(capturedSignal?.aborted).toBe(true))
  })
})
