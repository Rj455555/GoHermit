import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { i18n } from '../../i18n/i18n'
import { UIProvider } from '../../state/UIContext'
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
  getSession: vi.fn(),
  listApprovals: vi.fn(),
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
  verification_recipe: { checks: [], independent_verifier: true, max_repair_attempts: 0 },
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

function renderLoops(path = '/loops') {
  return render(
    <I18nextProvider i18n={i18n}>
      <UIProvider>
        <MemoryRouter initialEntries={[path]}>
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

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.setItem('gohermit.ui.locale', 'en-US')
  void i18n.changeLanguage('en-US')
  api.listLoops.mockResolvedValue({ loops: [definition] })
  api.getLoop.mockResolvedValue(definition)
  api.listLoopInvocations.mockResolvedValue({ invocations: [invocation], limit: 50 })
  api.getLoopInvocation.mockResolvedValue(invocation)
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
})
