import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { i18n } from '../../i18n/i18n'
import { ApiError } from '../../api/errors'
import { ToastRegion } from '../../components/ToastRegion'
import { UIProvider } from '../../state/UIContext'
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
const sessionEventMock = vi.hoisted(() => ({
  events: [] as Array<Record<string, unknown>>,
  streamingText: '',
  status: 'connected',
  fatal: false,
  truncated: false,
  reconnect: vi.fn(),
}))

vi.mock('../../api/endpoints', () => api)
vi.mock('../../hooks/useSessionEvents', () => ({
  useSessionEvents: () => sessionEventMock,
}))
vi.mock('../../components/ConnectivityProvider', () => ({
  useConnectivity: () => ({ status: 'online', generation: 0, canMutate: true, reconnect: vi.fn() }),
}))

function renderAgent(path = '/agent') {
  return render(
    <I18nextProvider i18n={i18n}>
      <UIProvider>
        <MemoryRouter initialEntries={[path]}>
          <AgentDataProvider active>
            <Routes>
              <Route path="/agent" element={<AgentLandingPage />} />
              <Route path="/agent/sessions/:sessionId" element={<AgentSessionPage />} />
            </Routes>
          </AgentDataProvider>
        </MemoryRouter>
        <ToastRegion />
      </UIProvider>
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
  vi.clearAllMocks()
  window.localStorage.clear()
  void i18n.changeLanguage('zh-CN')
  sessionEventMock.events = []
  sessionEventMock.streamingText = ''
  sessionEventMock.status = 'connected'
  sessionEventMock.fatal = false
  sessionEventMock.truncated = false
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
  api.getSession.mockResolvedValue(sessionDetail(undefined))
})

describe('Agent pages', () => {
  it('shows a bounded empty selection panel alongside the Session form', async () => {
    renderAgent()

    await screen.findByRole('button', { name: i18n.t('agent.createSession') })
    expect(screen.getByText(i18n.t('agent.selectSessionDescription'))).toBeVisible()
    expect(document.querySelector('.agent-empty-panel button')).toBeVisible()
  })

  it('keeps Agent configuration usable when legacy Session history fails', async () => {
    api.listSessions.mockRejectedValue(new ApiError('invalid_response', 200))
    renderAgent()

    await screen.findByRole('button', { name: i18n.t('agent.createSession') })
    fireEvent.mouseDown(screen.getByRole('combobox', { name: i18n.t('agent.access') }))
    expect(await screen.findByRole('option', { name: 'Codex' })).toBeInTheDocument()
    expect(screen.queryByText(i18n.t('agent.loadError'))).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: i18n.t('agent.createSession') })).toBeEnabled()
  })

  it('shows only ready Access and prevents duplicate Session creation', async () => {
    let resolveCreation: (value: { id: string }) => void = () => undefined
    api.createSession.mockReturnValue(new Promise((resolve) => {
      resolveCreation = resolve
    }))
    const user = userEvent.setup()
    renderAgent()

    await screen.findByRole('button', { name: i18n.t('agent.createSession') })
    fireEvent.mouseDown(screen.getByRole('combobox', { name: i18n.t('agent.access') }))
    await screen.findByRole('option', { name: 'Codex' })
    expect(screen.queryByRole('option', { name: /unconfigured/u })).not.toBeInTheDocument()
    await user.type(screen.getByLabelText(/标题|Title/u), 'New Session')
    const submit = screen.getByRole('button', { name: /新建会话|Create Session/u })
    await Promise.all([user.click(submit), user.click(submit)])

    expect(api.createSession).toHaveBeenCalledOnce()
    expect(api.createSession).toHaveBeenCalledWith(
      expect.objectContaining({
        title: 'New Session',
        company: 'openai',
        access: 'openai-codex',
        model: 'gpt-5.6',
        agent: 'coding',
      }),
    )
    resolveCreation({ id: 'session-new' })
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Session' })).toBeVisible())
  })

  it('shows a sanitized Session creation failure and permits an explicit retry', async () => {
    api.createSession
      .mockRejectedValueOnce(new ApiError('http_error', 500))
      .mockResolvedValueOnce({ id: 'session-new' })
    const user = userEvent.setup()
    renderAgent()

    const submit = await screen.findByRole('button', { name: i18n.t('agent.createSession') })
    await user.click(submit)
    expect(await screen.findByRole('alert')).toHaveTextContent(i18n.t('mutation.failed'))
    expect(screen.queryByText(/http_error|500/u)).not.toBeInTheDocument()
    expect(submit).toBeEnabled()

    await user.click(submit)
    await waitFor(() => expect(api.createSession).toHaveBeenCalledTimes(2))
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

  it('exposes the event-stream recovery action whenever the Hook reports fatal status', async () => {
    sessionEventMock.status = 'fatal'
    sessionEventMock.fatal = true
    const user = userEvent.setup()
    renderAgent('/agent/sessions/session-1')

    const reconnect = await screen.findByRole('button', {
      name: i18n.t('session.reconnectEvents'),
    })
    await user.click(reconnect)
    expect(sessionEventMock.reconnect).toHaveBeenCalledOnce()
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
    api.startRun.mockRejectedValueOnce(new ApiError('network_error')).mockResolvedValueOnce({
      session_id: 'session-1',
      run_id: 'run-1',
    })
    const user = userEvent.setup()
    renderAgent('/agent/sessions/session-1')
    const composer = await screen.findByLabelText(/消息|Message/u)

    await user.type(composer, 'do not translate')
    await user.keyboard('{Control>}{Enter}{/Control}')
    await waitFor(() => expect(composer).toHaveValue('do not translate'))
    expect(await screen.findByRole('alert')).toHaveTextContent(i18n.t('mutation.offline'))
    await user.keyboard('{Control>}{Enter}{/Control}')
    await waitFor(() => expect(composer).toHaveValue(''))
    expect(api.startRun).toHaveBeenCalledTimes(2)
  })

  it('prevents repeated keyboard submission while a Run start is in flight', async () => {
    let resolveRun: (value: { session_id: string; run_id: string }) => void = () => undefined
    api.startRun.mockReturnValue(new Promise((resolve) => {
      resolveRun = resolve
    }))
    const user = userEvent.setup()
    renderAgent('/agent/sessions/session-1')
    const composer = await screen.findByLabelText(i18n.t('session.message'))

    await user.type(composer, 'single submission')
    await user.keyboard('{Control>}{Enter}{/Control}{Control>}{Enter}{/Control}')
    expect(api.startRun).toHaveBeenCalledOnce()
    expect(composer).toBeDisabled()

    resolveRun({ session_id: 'session-1', run_id: 'run-1' })
    await waitFor(() => expect(composer).toHaveValue(''))
  })

  it.each([
    ['running', 'auto', true, 'cancelRun', 'cancelRun'],
    ['interrupted', 'auto', true, 'resumeRun', 'resumeRun'],
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

  it('binds queued Review Plan approval and cancellation to the true active Run and disables Composer', async () => {
    api.getSession.mockResolvedValue(sessionDetail({
      id: 'run-active',
      status: 'queued',
      plan_mode: 'review',
      plan_approved: false,
      plan: {
        revision: 1,
        steps: [{ id: 'step-1', status: 'pending', title: 'Literal plan', detail: '' }],
      },
    }))
    api.approvePlan.mockResolvedValue({ session_id: 'session-1', run_id: 'run-active' })
    api.cancelRun.mockResolvedValue({ session_id: 'session-1', run_id: 'run-active' })
    const user = userEvent.setup()
    renderAgent('/agent/sessions/session-1')

    const approve = await screen.findByRole('button', { name: i18n.t('session.approvePlan') })
    const cancel = screen.getByRole('button', { name: i18n.t('session.cancelRun') })
    expect(screen.getByLabelText(i18n.t('session.message'))).toBeDisabled()

    await user.click(approve)
    await waitFor(() => expect(api.approvePlan).toHaveBeenCalledWith('session-1', 'run-active'))
    await user.click(cancel)
    await waitFor(() => expect(api.cancelRun).toHaveBeenCalledWith('session-1', 'run-active'))
  })

  it('shows latest terminal Run history without exposing mutation actions', async () => {
    const detail = sessionDetail(undefined)
    detail.session.runs = [{
      id: 'run-history',
      status: 'completed',
      plan_mode: 'review',
      plan_approved: false,
      plan: {
        revision: 1,
        steps: [{ id: 'step-1', status: 'completed', title: 'Historical plan', detail: '' }],
      },
    }]
    api.getSession.mockResolvedValue(detail)
    renderAgent('/agent/sessions/session-1')

    expect((await screen.findAllByText(i18n.t('runStatus.completed'))).length).toBeGreaterThan(0)
    expect(screen.queryByRole('button', { name: i18n.t('session.approvePlan') })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: i18n.t('session.cancelRun') })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: i18n.t('session.resumeRun') })).not.toBeInTheDocument()
    expect(screen.getByLabelText(i18n.t('session.message'))).toBeEnabled()
  })

  it('enforces the exact 16 KiB UTF-8 Composer boundary', async () => {
    api.startRun.mockResolvedValue({ session_id: 'session-1', run_id: 'run-1' })
    const user = userEvent.setup()
    renderAgent('/agent/sessions/session-1')
    const composer = await screen.findByLabelText(i18n.t('session.message'))
    const send = screen.getByRole('button', { name: i18n.t('session.send') })

    fireEvent.change(composer, { target: { value: 'a'.repeat(16 << 10) } })
    expect(screen.getByText(`${16 << 10} / ${16 << 10}`)).toBeVisible()
    expect(send).toBeEnabled()
    await user.click(send)
    await waitFor(() => expect(api.startRun).toHaveBeenCalledOnce())

    fireEvent.change(composer, { target: { value: `中${'a'.repeat((16 << 10) - 2)}` } })
    expect(screen.getByText(`${(16 << 10) + 1} / ${16 << 10}`)).toBeVisible()
    expect(send).toBeDisabled()
  })

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

  it('translates dynamic status metadata while preserving authoritative content byte-for-byte', async () => {
    const detail = sessionDetail(undefined)
    const localizedDetail = {
      ...detail,
      messages: [{
        id: 'message-1',
        run_id: 'run-1',
        role: 'user',
        content: '用户 raw 😀 /workspace/file.go',
        created_at: '2026-07-29T08:00:00Z',
      }],
    }
    Object.assign(detail.session, {
      mission: {
        goal: 'Mission raw content',
        work_items: [{ id: 'item-1', title: 'WorkItem raw content', status: 'running' }],
        handoffs: [],
      },
      tool_calls: [{
        call_id: 'call-1',
        time: '2026-07-29T08:00:00Z',
        name: 'literal_tool_name',
        status: 'uncertain',
        summary: 'Tool raw summary /workspace/file.go',
      }],
    })
    sessionEventMock.events = [{
      sequence: 1,
      type: 'tool_started',
      tool: 'literal_tool_name',
    }]
    api.getSession.mockResolvedValue(localizedDetail)
    renderAgent('/agent/sessions/session-1')

    expect(await screen.findByText('用户')).toBeVisible()
    expect(screen.getByText(/WorkItem raw content/u)).toHaveTextContent('运行中')
    expect(screen.getByText('literal_tool_name').closest('li')).toHaveTextContent('状态不确定')
    expect(screen.getByText(/工具已开始/u)).toBeVisible()
    await i18n.changeLanguage('en-US')
    expect(await screen.findByText('User')).toBeVisible()
    expect(screen.getByText(/WorkItem raw content/u)).toHaveTextContent('Running')
    expect(screen.getByText('literal_tool_name').closest('li')).toHaveTextContent('Uncertain')
    expect(screen.getByText(/Tool started/u)).toBeVisible()

    for (const content of [
      '用户 raw 😀 /workspace/file.go',
      'Mission raw content',
      'WorkItem raw content',
      'literal_tool_name',
      'Tool raw summary /workspace/file.go',
    ]) {
      expect(screen.getAllByText((_, element) => element?.textContent?.includes(content) === true).length).toBeGreaterThan(0)
    }
  })

  it('surfaces a sanitized Run mutation conflict and releases the in-flight guard', async () => {
    api.getSession.mockResolvedValue(sessionDetail({
      id: 'run-1',
      status: 'running',
      plan_mode: 'auto',
      plan_approved: true,
    }))
    api.cancelRun
      .mockRejectedValueOnce(new ApiError('http_error', 409))
      .mockResolvedValueOnce({ session_id: 'session-1', run_id: 'run-1' })
    const user = userEvent.setup()
    renderAgent('/agent/sessions/session-1')

    const cancel = await screen.findByRole('button', { name: i18n.t('session.cancelRun') })
    await user.click(cancel)
    expect(await screen.findByRole('alert')).toHaveTextContent(i18n.t('mutation.conflict'))
    expect(cancel).toBeEnabled()
    await user.click(cancel)
    await waitFor(() => expect(api.cancelRun).toHaveBeenCalledTimes(2))
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
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows a sanitized non-conflict Approval failure and allows retry', async () => {
    api.getSession.mockResolvedValue(sessionDetail(undefined, [pendingApproval]))
    api.listApprovals.mockResolvedValue({ approvals: [pendingApproval] })
    api.decideApproval
      .mockRejectedValueOnce(new ApiError('network_error'))
      .mockResolvedValueOnce({ request_id: 'approval-1', status: 'approved' })
    const user = userEvent.setup()
    renderAgent('/agent/sessions/session-1')

    const approve = await screen.findByRole('button', { name: i18n.t('approval.approve') })
    await user.click(approve)
    expect(await screen.findByRole('alert')).toHaveTextContent(i18n.t('mutation.offline'))
    await user.click(approve)
    await waitFor(() => expect(api.decideApproval).toHaveBeenCalledTimes(2))
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
