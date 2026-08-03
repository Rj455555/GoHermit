import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Grid } from 'antd'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { i18n } from '../../i18n/i18n'
import { UIProvider, useUI } from '../../state/UIContext'
import { TaskDetailPage, TasksPage } from './TasksPage'

const api = vi.hoisted(() => ({
  listEmployees: vi.fn(),
  listEmployeeTasks: vi.fn(),
  getEmployee: vi.fn(),
  getEmployeeSkills: vi.fn(),
  getEmployeeKnowledge: vi.fn(),
  getEmployeeMemory: vi.fn(),
  getEmployeeTask: vi.fn(),
  getSession: vi.fn(),
  listApprovals: vi.fn(),
  decideApproval: vi.fn(),
  createEmployeeTask: vi.fn(),
  startEmployeeTask: vi.fn(),
  cancelEmployeeTask: vi.fn(),
  resumeEmployeeTask: vi.fn(),
  getTaskBoard: vi.fn(),
  updateTaskBoardCard: vi.fn(),
  createTaskBoardNote: vi.fn(),
}))
const eventState = vi.hoisted(() => ({
  events: [] as Array<Record<string, unknown>>,
  streamingText: '',
  status: 'connected',
  fatal: false,
  truncated: false,
  reconnect: vi.fn(),
}))

vi.mock('../../api/endpoints', () => api)
vi.mock('../../hooks/useSessionEvents', () => ({ useSessionEvents: () => eventState }))
vi.mock('../../components/ConnectivityProvider', () => ({
  useConnectivity: () => ({ status: 'online', generation: 0, canMutate: true }),
}))

const now = '2026-07-29T08:00:00Z'

function selectAntOption(label: string, option: string) {
  fireEvent.mouseDown(screen.getByRole('combobox', { name: label }))
  const optionLabel = screen.getAllByText(option, { exact: true }).find((node) =>
    node.classList.contains('ant-select-item-option-content'))
  expect(optionLabel).toBeDefined()
  fireEvent.click(optionLabel!)
}
const employee = {
  id: 'employee-ada',
  revision: 3,
  state: 'active',
  name: 'Ada',
  job_title: 'Release Engineer',
  agent_profile: 'coding',
  project_count: 1,
  created_at: now,
  updated_at: now,
}
const queuedTask = {
  schema_version: 1,
  id: 'task-queued',
  employee_id: employee.id,
  employee_revision: 3,
  prompt: 'Prepare release.',
  state: 'queued',
  created_at: now,
  updated_at: now,
  employee_snapshot: {
    schema_version: 1,
    employee_id: employee.id,
    revision: 3,
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

const taskBoard = {
  schema_version: 1,
  definition: {
    id: 'default',
    name: 'Task workspace',
    columns: [
      { id: 'backlog', title: 'Backlog', color: '#64748b', hidden: false },
      { id: 'todo', title: 'Todo', color: '#2563eb', hidden: false },
      { id: 'in_progress', title: 'In progress', color: '#0891b2', hidden: false },
      { id: 'review', title: 'Review', color: '#d97706', hidden: false },
      { id: 'done', title: 'Done', color: '#16a34a', hidden: false },
    ],
  },
  cards: [{
    id: queuedTask.id,
    task_id: queuedTask.id,
    kind: 'task',
    title: queuedTask.prompt,
    column_id: 'todo',
    rank: 0,
    labels: [],
    priority: 0,
    pinned: false,
    blocked: false,
    depends_on: [],
    employee_id: employee.id,
    employee_name: employee.name,
    provider: 'openai',
    model: 'gpt',
    state: 'queued',
    state_source: 'employee_task',
    projection_reason: 'queued_task',
    authoritative_updated_at: now,
    session_event_sequence: 0,
    session_count: 0,
    approval_status: 'none',
    verification_status: 'none',
    stale: false,
  }],
  view: { view: 'board', wip_enabled: false },
  filters: { states: [], labels: [] },
  updated_at: now,
  projection_generated_at: now,
}

function renderTasks(path = '/tasks') {
  return render(
    <I18nextProvider i18n={i18n}>
      <UIProvider>
        <DialogProbe />
        <MemoryRouter initialEntries={[path]}>
          <TaskNavigationProbe />
          <Routes>
            <Route path="/tasks" element={<TasksPage />} />
            <Route path="/tasks/:taskId" element={<TaskDetailPage />} />
            <Route path="/elsewhere" element={<p>Elsewhere</p>} />
          </Routes>
        </MemoryRouter>
      </UIProvider>
    </I18nextProvider>,
  )
}

function DialogProbe() {
  const { state, actions } = useUI()
  if (!state.dialog) return null
  return (
    <div role="dialog">
      <button type="button" onClick={() => actions.closeDialog()}>Cancel dialog</button>
      <button type="button" onClick={() => {
        const confirm = state.dialog?.onConfirm
        actions.closeDialog()
        confirm?.()
      }}>Confirm dialog</button>
    </div>
  )
}

function TaskNavigationProbe() {
  const location = useLocation()
  const navigate = useNavigate()
  return (
    <>
      <output data-testid="task-owner-location">{location.pathname}</output>
      <button type="button" onClick={() => void navigate('/elsewhere')}>Leave task</button>
    </>
  )
}

function LocationProbe() {
  const location = useLocation()
  return <output data-testid="location">{location.search}</output>
}

function renderTasksWithLocation(path = '/tasks') {
  return render(
    <I18nextProvider i18n={i18n}>
      <UIProvider>
        <MemoryRouter initialEntries={[path]}>
          <LocationProbe />
          <Routes>
            <Route path="/tasks" element={<TasksPage />} />
          </Routes>
        </MemoryRouter>
      </UIProvider>
    </I18nextProvider>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  eventState.events = []
  eventState.streamingText = ''
  eventState.status = 'connected'
  eventState.fatal = false
  eventState.truncated = false
  localStorage.setItem('gohermit.ui.locale', 'en-US')
  void i18n.changeLanguage('en-US')
  api.listEmployees.mockResolvedValue({ employees: [employee] })
  api.listEmployeeTasks.mockResolvedValue({ tasks: [queuedTask] })
  api.getEmployeeTask.mockResolvedValue(queuedTask)
  api.listApprovals.mockResolvedValue({ approvals: [] })
  api.getEmployee.mockResolvedValue({
    employee: {
      ...employee,
      schema_version: 1,
      avatar: { kind: 'initials', value: 'A' },
      charter: 'Ship releases',
      responsibilities: [],
      behavior_boundaries: [],
      default_selection: { company: 'openai', access: 'codex', model: 'gpt' },
      skill_bindings: [],
      project_binding_ids: ['project-main'],
      permission_policy: { allowed_capabilities: ['read'], network_allowed: false },
      budget_policy: { max_model_calls: 4, max_tokens: 4000, timeout_seconds: 600 },
      concurrency_policy: { max_running_tasks: 1 },
      memory_policy: {
        candidate_generation: true,
        promotion: 'owner_confirmation',
        max_context_facts: 8,
        max_context_bytes: 8192,
      },
    },
    project_bindings: [{
      id: 'project-main',
      employee_id: employee.id,
      label: 'GoHermit',
      workspace_real_path: '/workspace',
      workspace_fingerprint: 'f'.repeat(64),
      read_allowed: true,
      mutation_allowed: true,
      allowed_tool_capabilities: ['read'],
      network_allowed: false,
      created_at: now,
      updated_at: now,
    }],
  })
  api.getEmployeeSkills.mockResolvedValue({ employee_id: employee.id, revision: 3, bindings: [] })
  api.getEmployeeKnowledge.mockResolvedValue({ employee_id: employee.id, sources: [], indexes: [], results: [] })
  api.getEmployeeMemory.mockResolvedValue({ employee_id: employee.id, facts: [] })
  api.getTaskBoard.mockResolvedValue(taskBoard)
  api.updateTaskBoardCard.mockResolvedValue({ ...taskBoard, cards: [{ ...taskBoard.cards[0], column_id: 'in_progress', state: 'running', projection_reason: 'run_running', session_id: 'session-1', run_id: 'run-1', session_count: 1 }] })
  api.createTaskBoardNote.mockResolvedValue(taskBoard)
})

describe('Employee Tasks Phase 4 pages', () => {
  it('preserves terminal Task evidence through reconnecting, truncated, and fatal SSE states', async () => {
    const user = userEvent.setup()
    const completedTask = {
      ...queuedTask,
      state: 'completed',
      session_id: 'session-1',
      run_id: 'run-1',
    }
    api.getEmployeeTask.mockResolvedValue(completedTask)
    api.getSession.mockResolvedValue({
      session: { id: 'session-1', next_event_sequence: 4, runs: [], tool_calls: [], test_results: [] },
      messages: [],
    })
    eventState.status = 'reconnecting'
    eventState.truncated = true
    eventState.events = [{ type: 'model_completed', time: now, sequence: 4 }]

    const reconnecting = renderTasks('/tasks/task-queued')
    expect(await screen.findByTestId('task-status')).toHaveTextContent('Completed')
    expect(screen.getByText('Reconnecting live events…')).toBeVisible()
    expect(screen.getByText(/Streaming content was truncated/u)).toBeVisible()
    expect(screen.getByTestId('task-timeline')).toHaveTextContent('#4')
    expect(screen.queryByRole('button', { name: 'Cancel' })).not.toBeInTheDocument()
    reconnecting.unmount()

    eventState.status = 'fatal'
    eventState.truncated = false
    renderTasks('/tasks/task-queued')
    await user.click(await screen.findByRole('button', { name: 'Reconnect event stream' }))
    expect(eventState.reconnect).toHaveBeenCalledOnce()
  })

  it('renders the desktop Task table with authoritative links and bounded summaries', async () => {
    const getComputedStyle = window.getComputedStyle.bind(window)
    vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element))
    vi.spyOn(Grid, 'useBreakpoint').mockReturnValue({ xs: true, sm: true, md: true, lg: true, xl: true, xxl: false })
    api.listEmployeeTasks.mockResolvedValue({ tasks: [queuedTask] })

    renderTasks('/tasks')

    expect(await screen.findByRole('columnheader', { name: 'Task prompt' })).toBeVisible()
    expect(screen.getByRole('link', { name: 'Prepare release.' })).toHaveAttribute('href', '/tasks/task-queued')
    expect(screen.getByText('Queued')).toBeVisible()
    expect(screen.getByText('Ada')).toBeVisible()
  })

  it('renders Board cards and requires explicit Start confirmation before an In progress drop', async () => {
    const user = userEvent.setup()
    api.startEmployeeTask.mockResolvedValue({ ...queuedTask, state: 'running', session_id: 'session-1', run_id: 'run-1' })
    renderTasks('/tasks?view=board')

    const card = await screen.findByRole('link', { name: /Prepare release/u })
    fireEvent.dragStart(card)
    fireEvent.drop(screen.getByTestId('task-board-column-in_progress'))
    await waitFor(() => {
      const startButtons = screen.getAllByRole('button', { name: 'Start' })
      expect(startButtons[startButtons.length - 1]).toBeVisible()
    })
    expect(api.startEmployeeTask).not.toHaveBeenCalled()
    const startButtons = screen.getAllByRole('button', { name: 'Start' })
    const startButton = startButtons[startButtons.length - 1]!
    expect(startButton.closest('.ant-modal')).toHaveTextContent('Employee')
    await user.click(startButton)
    await waitFor(() => expect(api.startEmployeeTask).toHaveBeenCalledWith(queuedTask.id))
    expect(api.updateTaskBoardCard).toHaveBeenCalledWith(queuedTask.id, expect.objectContaining({ column_id: 'in_progress' }))
  })

  it('loads the last 100 Tasks per Employee and exposes the boundary', async () => {
    renderTasks()

    expect(await screen.findByText('Prepare release.')).toBeVisible()
    expect(screen.getByText('Showing the latest 100 Tasks per Employee.')).toBeVisible()
    expect(api.listEmployeeTasks).toHaveBeenCalledWith(
      employee.id,
      expect.objectContaining({ limit: 100 }),
      expect.anything(),
    )
  })

  it('keeps creation queued and requires explicit Prepare then Start', async () => {
    const user = userEvent.setup()
    api.startEmployeeTask.mockResolvedValue({ ...queuedTask, state: 'running' })
    renderTasks('/tasks/task-queued')

    expect(await screen.findByTestId('task-status')).toHaveTextContent('Queued')
    expect(screen.queryByRole('button', { name: 'Start' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Prepare' }))
    expect(api.startEmployeeTask).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Start' }))
    await waitFor(() => expect(api.startEmployeeTask).toHaveBeenCalledOnce())
  })

  it('confirms cancellation and Approval, then resumes only an interrupted Task', async () => {
    const user = userEvent.setup()
    const runningTask = { ...queuedTask, state: 'running', session_id: 'session-1', run_id: 'run-1' }
    const approval = {
      request_id: 'approval-1',
      session_id: 'session-1',
      run_id: 'run-1',
      tool_call_id: 'tool-1',
      tool: 'write_file',
      args_summary: 'Update release notes',
      resource_paths: ['docs/release.md'],
      status: 'pending',
      requested_at: now,
    }
    api.getEmployeeTask.mockResolvedValue(runningTask)
    api.getSession.mockResolvedValue({
      session: { id: 'session-1', next_event_sequence: 4, runs: [], tool_calls: [], test_results: [] },
      messages: [],
    })
    api.listApprovals.mockResolvedValue({ approvals: [approval] })
    api.cancelEmployeeTask.mockResolvedValue(runningTask)
    api.decideApproval.mockResolvedValue({ ...approval, status: 'approved' })
    const running = renderTasks('/tasks/task-queued')

    await user.click(await screen.findByRole('button', { name: 'Cancel' }))
    expect(api.cancelEmployeeTask).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Confirm dialog' }))
    await waitFor(() => expect(api.cancelEmployeeTask).toHaveBeenCalledWith(queuedTask.id))
    await user.click(screen.getByRole('button', { name: 'Approve' }))
    await user.click(screen.getByRole('button', { name: 'Confirm dialog' }))
    await waitFor(() => expect(api.decideApproval).toHaveBeenCalledWith('session-1', approval.request_id, 'approve'))
    running.unmount()

    const interruptedTask = { ...runningTask, state: 'interrupted' }
    api.getEmployeeTask.mockResolvedValue(interruptedTask)
    api.resumeEmployeeTask.mockResolvedValue(interruptedTask)
    renderTasks('/tasks/task-queued')
    await user.click(await screen.findByRole('button', { name: 'Resume' }))
    await waitFor(() => expect(api.resumeEmployeeTask).toHaveBeenCalledWith(queuedTask.id))
    expect(screen.queryByRole('button', { name: 'Cancel' })).not.toBeInTheDocument()
  })

  it('requires an explicit project selection and shows UTF-8 prompt bytes', async () => {
    const user = userEvent.setup()
    renderTasks()

    await screen.findByText('Prepare release.')
    await user.type(screen.getByLabelText('Task prompt'), '中😀')
    expect(screen.getByTestId('task-prompt-bytes')).toHaveTextContent('7 / 16384')
    expect(screen.getByRole('button', { name: 'Create as queued' })).toBeDisabled()
    selectAntOption('Project', 'GoHermit')
    expect(screen.getByRole('button', { name: 'Create as queued' })).toBeEnabled()
  })

  it('creates a bounded queued Task from the authoritative Employee context', async () => {
    const user = userEvent.setup()
    api.createEmployeeTask.mockResolvedValue(queuedTask)
    renderTasks()

    await screen.findByText('Prepare release.')
    selectAntOption('Project', 'GoHermit')
    await user.type(screen.getByLabelText('Task prompt'), 'Review release evidence.')
    await user.click(screen.getByRole('button', { name: 'Create as queued' }))

    await waitFor(() => expect(api.createEmployeeTask).toHaveBeenCalledWith(
      employee.id,
      expect.objectContaining({
        prompt: 'Review release evidence.',
        project_binding_id: 'project-main',
        skills: [],
        knowledge: [],
        memory_fact_ids: [],
      }),
    ))
    await waitFor(() => {
      expect(screen.getByTestId('task-owner-location')).toHaveTextContent('/tasks/task-queued')
    }, { timeout: 10_000 })
  })

  it('renders execution evidence as structured sections, not raw JSON', async () => {
    renderTasks('/tasks/task-queued')

    await screen.findByTestId('task-status')
    expect(screen.queryByText(/\{"plan"/u)).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Plan' })).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Tools' })).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Verification' })).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Approvals' })).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Artifacts' })).toBeVisible()
  })

  it('keeps complete Employee, Project, State and Time filters in the URL', async () => {
    renderTasksWithLocation('/tasks')
    await screen.findByText('Prepare release.')

    selectAntOption('Employee filter', 'Ada')
    selectAntOption('Project filter', 'GoHermit')
    selectAntOption('State filter', 'Waiting for owner')
    selectAntOption('Time filter', 'Last 7 days')

    expect(screen.getByTestId('location')).toHaveTextContent(
      'employee=employee-ada&project=project-main&state=waiting_owner&time=7d',
    )
    for (const [state, label] of [
      ['queued', 'Queued'],
      ['prepared', 'Prepared'],
      ['waiting_owner', 'Waiting for owner'],
      ['running', 'Running'],
      ['verifying', 'Verifying'],
      ['interrupted', 'Interrupted'],
      ['completed', 'Completed'],
      ['failed', 'Failed'],
      ['cancelled', 'Cancelled'],
    ] as const) {
      expect(state).toBeTruthy()
      selectAntOption('State filter', label)
    }
  })

  it('only offers authoritative current Skill bindings for Task creation', async () => {
    api.getEmployee.mockResolvedValue({
      employee: {
        ...employee,
        skill_bindings: [{
          skill_id: 'stale',
          version: '1.0.0',
          digest: 'a'.repeat(64),
          configuration: {},
          enabled: true,
        }, {
          skill_id: 'current',
          version: '1.0.0',
          digest: 'b'.repeat(64),
          configuration: {},
          enabled: true,
        }],
      },
      project_bindings: [],
    })
    api.getEmployeeSkills.mockResolvedValue({
      employee_id: employee.id,
      revision: 3,
      bindings: [{
        binding: {
          skill_id: 'stale', version: '1.0.0', digest: 'a'.repeat(64),
          configuration: {}, enabled: true,
        },
        status: 'digest_drift',
      }, {
        binding: {
          skill_id: 'current', version: '1.0.0', digest: 'b'.repeat(64),
          configuration: {}, enabled: true,
        },
        status: 'current',
      }],
    })
    renderTasks()

    fireEvent.click(await screen.findByText('Skills (exact version)'))
    expect(await screen.findByRole('checkbox', { name: /current@1.0.0/u })).toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: /stale@1.0.0/u })).not.toBeInTheDocument()
  })

  it('does not commit a delayed Task mutation after routing away', async () => {
    const user = userEvent.setup()
    let resolveStart: ((value: typeof queuedTask) => void) | undefined
    const delayedStart = new Promise<typeof queuedTask>((resolve) => {
      resolveStart = resolve
    })
    renderTasks('/tasks/task-queued')

    await user.click(await screen.findByRole('button', { name: 'Prepare' }))
    api.startEmployeeTask.mockReturnValue(delayedStart)
    await user.click(await screen.findByRole('button', { name: 'Start' }))
    await user.click(screen.getByRole('button', { name: 'Leave task' }))
    expect(screen.getByTestId('task-owner-location')).toHaveTextContent('/elsewhere')

    resolveStart?.({ ...queuedTask, state: 'running' })
    await waitFor(() => expect(api.startEmployeeTask).toHaveBeenCalledOnce())
    expect(api.getEmployeeTask).toHaveBeenCalledTimes(2)
  })
})
