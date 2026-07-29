import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { i18n } from '../../i18n/i18n'
import { UIProvider } from '../../state/UIContext'
import { TaskDetailPage, TasksPage } from './TasksPage'

const api = vi.hoisted(() => ({
  listEmployees: vi.fn(),
  listEmployeeTasks: vi.fn(),
  getEmployee: vi.fn(),
  getEmployeeTask: vi.fn(),
  getSession: vi.fn(),
  listApprovals: vi.fn(),
  decideApproval: vi.fn(),
  createEmployeeTask: vi.fn(),
  startEmployeeTask: vi.fn(),
  cancelEmployeeTask: vi.fn(),
  resumeEmployeeTask: vi.fn(),
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

function renderTasks(path = '/tasks') {
  return render(
    <I18nextProvider i18n={i18n}>
      <UIProvider>
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route path="/tasks" element={<TasksPage />} />
            <Route path="/tasks/:taskId" element={<TaskDetailPage />} />
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
  api.listEmployees.mockResolvedValue({ employees: [employee] })
  api.listEmployeeTasks.mockResolvedValue({ tasks: [queuedTask] })
  api.getEmployeeTask.mockResolvedValue(queuedTask)
  api.listApprovals.mockResolvedValue({ approvals: [] })
})

describe('Employee Tasks Phase 4 pages', () => {
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
})
