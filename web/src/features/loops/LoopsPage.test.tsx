import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { i18n } from '../../i18n/i18n'
import { UIProvider } from '../../state/UIContext'
import type { LoopDefinition, TeamTemplate } from '../../api/types'
import { LoopDetailPage, LoopInvocationPage, LoopsPage } from './LoopsPage'

const api = vi.hoisted(() => ({
  listLoops: vi.fn(),
  getLoop: vi.fn(),
  createLoop: vi.fn(),
  updateLoop: vi.fn(),
  importLoop: vi.fn(),
  dryRunLoop: vi.fn(),
  listLoopInvocations: vi.fn(),
  startLoopInvocation: vi.fn(),
  getLoopInvocation: vi.fn(),
  cancelLoopInvocation: vi.fn(),
  getInfo: vi.fn(),
  listEmployees: vi.fn(),
  getTeamTemplate: vi.fn(),
  importTeamTemplate: vi.fn(),
  dryRunEmployee: vi.fn(),
  getSession: vi.fn(),
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
    truncated: false,
    reconnect: vi.fn(),
  }),
}))
vi.mock('../../components/ConnectivityProvider', () => ({
  useConnectivity: () => ({ status: 'online', generation: 0, canMutate: true }),
}))

const now = '2026-07-29T08:00:00Z'
const definition = {
  id: 'daily-review',
  schema_version: 1,
  name: 'Daily review',
  description: '',
  workspace_identity: '/workspace/gohermit',
  enabled: true,
  task_source: { type: 'fixed_prompt', prompt: 'Review the repository.' },
  agent_selection: { company: 'openai', access: 'codex', model: 'gpt', agent: 'team' },
  team_template_ref: 'default',
  plan_mode: 'review',
  verification_recipe: {
    checks: [{
      id: 'unit',
      command: ['go', 'test', '-run', 'Test Name', './...'],
      required: true,
      timeout_seconds: 90,
    }],
    independent_verifier: true,
    max_repair_attempts: 0,
  },
  budget: { max_model_calls: 12, max_tokens: 120_000, timeout_seconds: 1_200 },
  approval_policy: { require_for_mutation: false },
  workspace_policy: { read_only: true, require_clean_git: false },
  output_policy: { include_diff: false, max_report_bytes: 65_536 },
  created_at: now,
  updated_at: now,
  revision: 2,
}
const invocation = {
  id: 'invocation-1',
  loop_id: definition.id,
  definition_revision: 2,
  definition_snapshot: definition,
  trigger: 'manual',
  task_snapshot: definition.task_source.prompt,
  session_id: 'session-1',
  run_id: 'run-1',
  status: 'attached',
  created_at: now,
  started_at: now,
}
const employee = {
  id: 'employee.builder',
  revision: 4,
  state: 'active',
  name: 'Builder',
  job_title: 'Engineer',
  agent_profile: 'builder',
  project_count: 1,
  created_at: now,
  updated_at: now,
}
const team = {
  schema_version: 2,
  name: 'default',
  default: { company: 'openai', access: 'codex', model: 'gpt-5' },
  roles: {
    builder: {
      company: '',
      access: '',
      model: '',
      employee_id: employee.id,
    },
  },
}

function renderLoops(path = '/loops') {
  return render(
    <I18nextProvider i18n={i18n}>
      <UIProvider>
        <MemoryRouter initialEntries={[path]}>
          <LoopNavigationProbe />
          <Routes>
            <Route path="/loops" element={<LoopsPage />} />
            <Route path="/loops/:loopId" element={<LoopDetailPage />} />
            <Route path="/loops/:loopId/invocations/:invocationId" element={<LoopInvocationPage />} />
          </Routes>
        </MemoryRouter>
      </UIProvider>
    </I18nextProvider>,
  )
}

function LoopNavigationProbe() {
  const navigate = useNavigate()
  return <button type="button" onClick={() => void navigate('/loops/second-loop')}>Go second loop</button>
}

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.setItem('gohermit.ui.locale', 'en-US')
  void i18n.changeLanguage('en-US')
  api.listLoops.mockResolvedValue({ loops: [definition] })
  api.getLoop.mockResolvedValue(definition)
  api.listLoopInvocations.mockResolvedValue({ invocations: [invocation], limit: 50 })
  api.getLoopInvocation.mockResolvedValue(invocation)
  api.getInfo.mockResolvedValue({
    version: 'test',
    workspace: '/workspace/gohermit',
    model: {
      provider: 'openai',
      protocol: 'responses',
      base_url: '',
      model: 'gpt-5',
      api_key_env: 'OPENAI_API_KEY',
      api_key_configured: true,
    },
    selection: { company: 'openai', access: 'codex', model: 'gpt-5', agent: 'team' },
    companies: [{
      id: 'openai',
      label: 'OpenAI',
      access: [{
        id: 'codex',
        label: 'Codex',
        auth_type: 'oauth_external',
        description: '',
        supported: true,
        models: [{ id: 'gpt-5', label: 'GPT-5', provider: 'openai' }],
      }],
    }],
    available_companies: [{
      id: 'openai',
      label: 'OpenAI',
      access: [{
        id: 'codex',
        label: 'Codex',
        auth_type: 'oauth_external',
        description: '',
        supported: true,
        models: [{ id: 'gpt-5', label: 'GPT-5', provider: 'openai' }],
      }],
    }],
    agents: [{ id: 'team', label: 'Team', description: '', read_only: false, tool_policy: 'full' }],
    auth_status: {},
    active: false,
    owner: { configured: true },
  })
  api.listEmployees.mockResolvedValue({ employees: [employee], next_cursor: '' })
  api.getTeamTemplate.mockResolvedValue(team)
  api.dryRunEmployee.mockResolvedValue({
    employee_id: employee.id,
    revision: employee.revision,
    ready: true,
    checks: [{ name: 'provider', ready: true, detail: 'ready' }],
  })
  api.listApprovals.mockResolvedValue({ approvals: [] })
  api.getSession.mockResolvedValue({
    session: {
      id: 'session-1',
      next_event_sequence: 4,
      runs: [],
      tool_calls: [],
      test_results: [],
      approval_requests: [],
    },
    messages: [],
  })
  api.dryRunLoop.mockResolvedValue({
    loop_id: definition.id,
    definition_revision: 2,
    definition_valid: true,
    workspace_identity: definition.workspace_identity,
    workspace_matches: true,
    git_clean: true,
    task_prompt: definition.task_source.prompt,
    agent: definition.agent_selection,
    roles: [],
    write_scope: 'read-only',
    checks: [],
    budget: definition.budget,
    requires_approval: false,
    ready: true,
    reasons: [],
  })
})

describe('Loops Phase 4 pages', () => {
  it('restores a Definition from the URL and saves with its expected revision', async () => {
    const user = userEvent.setup()
    api.updateLoop.mockResolvedValue({ ...definition, revision: 3 })
    renderLoops('/loops/daily-review')

    const name = await screen.findByDisplayValue('Daily review')
    await user.clear(name)
    await user.type(name, 'Daily review updated')
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.updateLoop).toHaveBeenCalledWith(
      definition.id,
      expect.objectContaining({ revision: 2 }),
    ))
  })

  it('direct-loads an Invocation and binds the timeline to its Session only', async () => {
    renderLoops('/loops/daily-review/invocations/invocation-1')

    expect(await screen.findByTestId('loop-timeline')).toHaveTextContent('invocation-1')
    expect(api.getLoopInvocation).toHaveBeenCalledWith('invocation-1', expect.anything())
    expect(api.getSession).toHaveBeenCalledWith('session-1', expect.anything())
  })

  it('round-trips verification argv without parsing or joining arguments', async () => {
    const user = userEvent.setup()
    api.updateLoop.mockImplementation((_id: string, next: LoopDefinition) => Promise.resolve(next))
    renderLoops('/loops/daily-review')

    expect(await screen.findByRole('heading', { name: 'Verification checks' })).toBeVisible()
    expect(screen.getByLabelText('Command argument 4')).toHaveValue('Test Name')
    fireEvent.change(screen.getByLabelText('Description'), { target: { value: 'Verified daily review' } })
    fireEvent.change(screen.getByLabelText('Workspace identity'), { target: { value: '/workspace/release' } })
    fireEvent.change(screen.getByLabelText('Mission'), { target: { value: 'Review and verify.' } })
    fireEvent.change(screen.getByLabelText('Team template reference'), { target: { value: 'release-team' } })
    await user.click(screen.getByRole('button', { name: 'Add argument' }))
    await user.type(screen.getByLabelText('Command argument 6'), '--count=1')
    await user.click(screen.getByRole('checkbox', { name: 'Enabled' }))
    await user.click(screen.getByRole('checkbox', { name: 'Read-only workspace' }))
    await user.click(screen.getByRole('checkbox', { name: 'Require a clean Git workspace' }))
    await user.click(screen.getByRole('checkbox', { name: 'Require approval for mutation' }))
    await user.click(screen.getByRole('checkbox', { name: 'Include diff in output' }))
    fireEvent.change(screen.getByLabelText('Maximum model calls'), { target: { value: '18' } })
    fireEvent.change(screen.getByLabelText('Maximum tokens'), { target: { value: '180000' } })
    fireEvent.change(screen.getByLabelText('Timeout (seconds)'), { target: { value: '1800' } })
    await user.selectOptions(screen.getByLabelText('Plan mode'), 'auto')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(api.updateLoop).toHaveBeenCalled())
    const [savedId, savedDefinition] = api.updateLoop.mock.calls[0] as unknown as [
      string,
      LoopDefinition,
    ]
    expect(savedId).toBe(definition.id)
    expect(savedDefinition.revision).toBe(2)
    expect(savedDefinition.verification_recipe.checks[0]).toMatchObject({
      id: 'unit',
      command: ['go', 'test', '-run', 'Test Name', './...', '--count=1'],
      required: true,
      timeout_seconds: 90,
    })
    expect(savedDefinition).toMatchObject({
      enabled: false,
      description: 'Verified daily review',
      workspace_identity: '/workspace/release',
      task_source: { type: 'fixed_prompt', prompt: 'Review and verify.' },
      team_template_ref: 'release-team',
      plan_mode: 'auto',
      workspace_policy: { read_only: false, require_clean_git: true },
      approval_policy: { require_for_mutation: true },
      output_policy: { include_diff: true },
      budget: { max_model_calls: 18, max_tokens: 180000, timeout_seconds: 1800 },
    })
  })

  it('does not commit a delayed Loop mutation after routing to another Definition', async () => {
    const user = userEvent.setup()
    let resolveSave: ((value: LoopDefinition) => void) | undefined
    const delayedSave = new Promise<LoopDefinition>((resolve) => {
      resolveSave = resolve
    })
    api.getLoop.mockImplementation((id: string) => Promise.resolve({
      ...definition,
      id,
      name: id === 'second-loop' ? 'Second loop' : definition.name,
    }))
    api.updateLoop.mockReturnValue(delayedSave)
    renderLoops('/loops/daily-review')

    await screen.findByRole('heading', { name: definition.name })
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await user.click(screen.getByRole('button', { name: 'Go second loop' }))
    expect(await screen.findByRole('heading', { name: 'Second loop' })).toBeVisible()

    resolveSave?.({ ...definition, name: 'Stale saved loop' } as LoopDefinition)
    await waitFor(() => expect(api.updateLoop).toHaveBeenCalledOnce())
    expect(screen.getByRole('heading', { name: 'Second loop' })).toBeVisible()
  })

  it('configures Team roles with active Employees and an explicit model path', async () => {
    const user = userEvent.setup()
    renderLoops('/loops/daily-review')

    expect(await screen.findByRole('heading', { name: 'Team roles' })).toBeVisible()
    expect(screen.queryByRole('textbox', { name: 'Team' })).not.toBeInTheDocument()
    expect(screen.getByTestId('team-role-builder')).toHaveValue(employee.id)
    expect(screen.getByText(/Builder · r4 · Ready/)).toBeVisible()

    await user.click(screen.getByRole('checkbox', { name: 'Use Mission model override for builder' }))
    await user.click(screen.getByRole('button', { name: 'Save team' }))

    await waitFor(() => expect(api.importTeamTemplate).toHaveBeenCalled())
    const [savedTeam] = api.importTeamTemplate.mock.calls[0] as unknown as [TeamTemplate]
    expect(savedTeam.schema_version).toBe(2)
    expect(savedTeam.roles.builder).toMatchObject({
      employee_id: employee.id,
      company: 'openai',
      access: 'codex',
      model: 'gpt-5',
    })
  })

  it('shows Invocation evidence as structured projections instead of raw JSON', async () => {
    renderLoops('/loops/daily-review/invocations/invocation-1')

    expect(await screen.findByRole('heading', { name: 'Definition snapshot' })).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Plan' })).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Tools' })).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Verification' })).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Approvals' })).toBeVisible()
    expect(screen.queryByText(/"definition_snapshot"/)).not.toBeInTheDocument()
  })
})
