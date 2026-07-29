import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { i18n } from '../../i18n/i18n'
import { ApiError } from '../../api/errors'
import { AgentDataProvider } from './AgentDataContext'
import { AgentLandingPage, AgentSessionPage } from './AgentPage'

const api = vi.hoisted(() => ({
  getInfo: vi.fn(),
  listSessions: vi.fn(),
  createSession: vi.fn(),
  getSession: vi.fn(),
  startRun: vi.fn(),
  cancelRun: vi.fn(),
  resumeRun: vi.fn(),
  approvePlan: vi.fn(),
  listApprovals: vi.fn(),
  decideApproval: vi.fn(),
}))

vi.mock('../../api/endpoints', () => api)
vi.mock('../../hooks/useSessionEvents', () => ({
  useSessionEvents: () => ({
    events: [],
    streamingText: '',
    status: 'connected',
    fatal: false,
    reconnect: vi.fn(),
  }),
}))
vi.mock('../../components/ConnectivityProvider', () => ({
  useConnectivity: () => ({ status: 'online', generation: 0, canMutate: true, reconnect: vi.fn() }),
}))

function renderAgent(path = '/agent') {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={[path]}>
        <AgentDataProvider active>
          <Routes>
            <Route path="/agent" element={<AgentLandingPage />} />
            <Route path="/agent/sessions/:sessionId" element={<AgentSessionPage />} />
          </Routes>
        </AgentDataProvider>
      </MemoryRouter>
    </I18nextProvider>,
  )
}

function sessionDetail(
  run: Record<string, unknown> | undefined,
  approvals: Record<string, unknown>[] = [],
) {
  return {
    session: {
      id: 'session-1',
      title: 'Session',
      selection: { company: 'openai', access: 'openai-codex', model: 'gpt-5.6', agent: 'coding' },
      next_event_sequence: 0,
      active_run_id: run?.id,
      runs: run === undefined ? [] : [run],
      tool_calls: [],
      test_results: [],
      approval_requests: approvals,
    },
    messages: [],
  }
}

const pendingApproval = {
  request_id: 'approval-1',
  session_id: 'session-1',
  run_id: 'run-1',
  tool: 'write_file',
  resource_paths: ['safe/file.go'],
  args_summary: 'Write one file',
  args_digest: 'digest',
  policy_fingerprint: 'policy',
  plan_revision: 1,
  created_at: '2026-07-29T08:00:00Z',
  expires_at: '2099-07-29T08:00:00Z',
  status: 'pending',
}

beforeEach(() => {
  api.getInfo.mockResolvedValue({
    available_companies: [{
      id: 'openai',
      label: 'OpenAI',
      access: [{
        id: 'openai-codex',
        label: 'Codex',
        auth_type: 'oauth_external',
        supported: true,
        models: [{ id: 'gpt-5.6', label: 'GPT-5.6', provider: 'openai-codex' }],
      }],
    }],
    agents: [{ id: 'coding', label: 'Coding', description: '', read_only: false, tool_policy: 'workspace' }],
  })
  api.listSessions.mockResolvedValue({ sessions: [] })
  api.listApprovals.mockResolvedValue({ approvals: [] })
})

describe('Agent pages', () => {
  it('shows only ready Access and prevents duplicate Session creation', async () => {
    api.createSession.mockResolvedValue({ id: 'session-new' })
    const user = userEvent.setup()
    renderAgent()

    await screen.findByRole('option', { name: 'Codex' })
    expect(screen.queryByRole('option', { name: /unconfigured/u })).not.toBeInTheDocument()
    await user.type(screen.getByLabelText(/标题|Title/u), 'New Session')
    const submit = screen.getByRole('button', { name: /新建会话|Create Session/u })
    await Promise.all([user.click(submit), user.click(submit)])

    await waitFor(() => expect(api.createSession).toHaveBeenCalledOnce())
    expect(api.createSession).toHaveBeenCalledWith(
      expect.objectContaining({
        title: 'New Session',
        company: 'openai',
        access: 'openai-codex',
        model: 'gpt-5.6',
        agent: 'coding',
      }),
    )
  })

  it('restores Session detail from the URL and preserves untranslated message content', async () => {
    api.getSession.mockResolvedValue({
      session: {
        id: 'session-1',
        title: '保持原文',
        selection: { company: 'openai', access: 'openai-codex', model: 'gpt-5.6', agent: 'coding' },
        next_event_sequence: 3,
        active_run_id: 'run-1',
        runs: [{ id: 'run-1', status: 'completed', plan_mode: 'auto', plan_approved: false }],
        tool_calls: [],
        test_results: [],
        approval_requests: [],
      },
      messages: [{
        id: 'message-1',
        run_id: 'run-1',
        role: 'user',
        content: '<script>literal user text</script>',
        created_at: '2026-07-29T08:00:00Z',
      }],
    })
    renderAgent('/agent/sessions/session-1')

    expect(await screen.findByRole('heading', { name: '保持原文' })).toBeVisible()
    expect(screen.getByText('<script>literal user text</script>')).toBeVisible()
    expect(document.querySelector('script')).toBeNull()
    expect(api.getSession).toHaveBeenCalledWith('session-1', expect.anything())
  })

  it('keeps Composer input after failure and clears it after a successful explicit Run start', async () => {
    api.getSession.mockResolvedValue({
      session: {
        id: 'session-1',
        title: 'Session',
        selection: { company: 'openai', access: 'openai-codex', model: 'gpt-5.6', agent: 'coding' },
        next_event_sequence: 0,
        runs: [],
        tool_calls: [],
        test_results: [],
        approval_requests: [],
      },
      messages: [],
    })
    api.startRun.mockRejectedValueOnce(new Error('failure')).mockResolvedValueOnce({
      session_id: 'session-1',
      run_id: 'run-1',
    })
    const user = userEvent.setup()
    renderAgent('/agent/sessions/session-1')
    const composer = await screen.findByLabelText(/消息|Message/u)

    await user.type(composer, 'do not translate')
    await user.keyboard('{Control>}{Enter}{/Control}')
    await waitFor(() => expect(composer).toHaveValue('do not translate'))
    await user.keyboard('{Control>}{Enter}{/Control}')
    await waitFor(() => expect(composer).toHaveValue(''))
    expect(api.startRun).toHaveBeenCalledTimes(2)
  })

  it.each([
    ['running', 'auto', true, 'cancelRun', 'cancelRun'],
    ['interrupted', 'auto', true, 'resumeRun', 'resumeRun'],
    ['queued', 'review', false, 'approvePlan', 'approvePlan'],
  ] as const)(
    'submits the authoritative %s Run action once',
    async (status, planMode, planApproved, translationKey, method) => {
      api.getSession.mockResolvedValue(sessionDetail({
        id: 'run-1',
        status,
        plan_mode: planMode,
        plan_approved: planApproved,
      }))
      api[method].mockResolvedValue({ session_id: 'session-1', run_id: 'run-1' })
      const user = userEvent.setup()
      renderAgent('/agent/sessions/session-1')

      await user.click(await screen.findByRole('button', {
        name: i18n.t(`session.${translationKey}`),
      }))
      await waitFor(() => expect(api[method]).toHaveBeenCalledOnce())
      expect(api[method]).toHaveBeenCalledWith('session-1', 'run-1')
    },
  )

  it('renders Plan, Tool, and Verification data as authoritative text projections', async () => {
    const detail = sessionDetail({
      id: 'run-1',
      status: 'completed',
      plan_mode: 'auto',
      plan_approved: true,
      plan: {
        revision: 2,
        steps: [{
          id: 'step-1',
          status: 'in_progress',
          title: 'Do not translate this plan step',
          detail: 'literal detail',
        }],
      },
    })
    Object.assign(detail.session, {
      tool_calls: [{
        call_id: 'call-1',
        time: '2026-07-29T08:00:00Z',
        name: 'workspace_tool',
        status: 'uncertain',
        summary: 'literal tool summary',
      }],
      test_results: [{
        command: 'go test ./...',
        time: '2026-07-29T08:00:00Z',
        passed: true,
        summary: 'literal verification summary',
      }],
    })
    api.getSession.mockResolvedValue(detail)
    renderAgent('/agent/sessions/session-1')

    expect(await screen.findByText('Do not translate this plan step')).toBeVisible()
    expect(screen.getByText(/literal tool summary/u)).toBeVisible()
    expect(screen.getByText(/go test \.\/\.\.\./u)).toBeVisible()
    expect(screen.getByText(/literal verification summary/u)).toBeVisible()
  })

  it('decides an Approval once and refreshes after a 409 conflict', async () => {
    api.getSession.mockResolvedValue(sessionDetail(undefined, [pendingApproval]))
    api.listApprovals.mockResolvedValue({ approvals: [pendingApproval] })
    api.decideApproval.mockRejectedValue(new ApiError('http_error', 409))
    const user = userEvent.setup()
    renderAgent('/agent/sessions/session-1')

    await user.click(await screen.findByRole('button', { name: i18n.t('approval.approve') }))
    await waitFor(() => expect(api.decideApproval).toHaveBeenCalledOnce())
    expect(api.decideApproval).toHaveBeenCalledWith('session-1', 'approval-1', 'approve')
    await waitFor(() => expect(api.listApprovals).toHaveBeenCalledTimes(2))
  })

  it('disables decisions for an expired Approval', async () => {
    const expired = { ...pendingApproval, expires_at: '2020-01-01T00:00:00Z' }
    api.getSession.mockResolvedValue(sessionDetail(undefined, [expired]))
    api.listApprovals.mockResolvedValue({ approvals: [expired] })
    renderAgent('/agent/sessions/session-1')

    expect(await screen.findByRole('button', { name: i18n.t('approval.approve') })).toBeDisabled()
    expect(screen.getByRole('button', { name: i18n.t('approval.deny') })).toBeDisabled()
    expect(api.decideApproval).not.toHaveBeenCalled()
  })
})
