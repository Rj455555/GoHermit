import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { i18n } from '../../i18n/i18n'
import { UIProvider } from '../../state/UIContext'
import { EmployeeDetailPage, EmployeesPage } from './EmployeesPage'

const api = vi.hoisted(() => ({
  listEmployees: vi.fn(),
  getEmployee: vi.fn(),
  getInfo: vi.fn(),
  listProjects: vi.fn(),
  listSkills: vi.fn(),
  dryRunEmployee: vi.fn(),
  listEmployeeTasks: vi.fn(),
  getEmployeeSkills: vi.fn(),
  getEmployeeKnowledge: vi.fn(),
  getEmployeeMemory: vi.fn(),
  getEmployeeMemoryCandidates: vi.fn(),
  getEmployeeActivity: vi.fn(),
}))

vi.mock('../../api/endpoints', () => api)
vi.mock('../../components/ConnectivityProvider', () => ({
  useConnectivity: () => ({ status: 'online', generation: 0, canMutate: true }),
}))

const now = '2026-07-29T08:00:00Z'
const summary = {
  id: 'employee-ada',
  revision: 4,
  state: 'active',
  name: 'Ada',
  job_title: 'Release Engineer',
  agent_profile: 'coding',
  project_count: 1,
  created_at: now,
  updated_at: now,
}
const employeeRecord = {
  employee: {
    ...summary,
    schema_version: 1,
    avatar: { kind: 'initials', value: 'A' },
    charter: 'Ship verified releases.',
    responsibilities: [],
    behavior_boundaries: [],
    default_selection: { company: 'openai', access: 'codex', model: 'gpt' },
    skill_bindings: [],
    project_binding_ids: ['project-main'],
    permission_policy: { allowed_capabilities: ['read'], network_allowed: false },
    budget_policy: { max_model_calls: 8, max_tokens: 8_000, timeout_seconds: 1_200 },
    concurrency_policy: { max_running_tasks: 1 },
    memory_policy: {
      candidate_generation: true,
      promotion: 'owner_confirmation',
      max_context_facts: 8,
      max_context_bytes: 8_192,
    },
  },
  project_bindings: [],
}

function renderEmployees(path = '/employees') {
  return render(
    <I18nextProvider i18n={i18n}>
      <UIProvider>
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route path="/employees" element={<EmployeesPage />} />
            <Route path="/employees/:employeeId" element={<EmployeeDetailPage />} />
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
  api.listEmployees.mockResolvedValue({ employees: [summary], next_cursor: 'next-page' })
  api.getEmployee.mockResolvedValue(employeeRecord)
  api.getEmployeeSkills.mockResolvedValue({ employee_id: summary.id, revision: 4, bindings: [] })
  api.getEmployeeKnowledge.mockResolvedValue({ sources: [], indexes: [], results: [] })
  api.getEmployeeMemory.mockResolvedValue({ facts: [] })
  api.getEmployeeMemoryCandidates.mockResolvedValue({ candidates: [] })
  api.listEmployeeTasks.mockResolvedValue({ tasks: [] })
  api.getEmployeeActivity.mockResolvedValue({ events: [] })
})

describe('Employees Phase 4 pages', () => {
  it('uses URL filters and bounded cursor pagination without hidden selection', async () => {
    const user = userEvent.setup()
    renderEmployees()

    expect(await screen.findByText('Ada')).toBeVisible()
    await user.selectOptions(screen.getByLabelText('State'), 'active')
    await waitFor(() => expect(api.listEmployees).toHaveBeenLastCalledWith(
      expect.objectContaining({ state: 'active' }),
      expect.anything(),
    ))
    await user.click(screen.getByRole('button', { name: 'Load more' }))
    await waitFor(() => expect(api.listEmployees).toHaveBeenLastCalledWith(
      expect.objectContaining({ cursor: 'next-page' }),
      expect.anything(),
    ))
  })

  it('renders an archived Employee as fully read-only', async () => {
    api.getEmployee.mockResolvedValue({
      ...employeeRecord,
      employee: { ...employeeRecord.employee, state: 'archived' },
    })
    renderEmployees('/employees/employee-ada')

    expect(await screen.findByTestId('employee-status')).toHaveTextContent('Archived')
    expect(screen.queryByRole('button', { name: /save|disable|enable|archive/i })).not.toBeInTheDocument()
    expect(screen.getByText('Archived Employees are read-only.')).toBeVisible()
  })
})
