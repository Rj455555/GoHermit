import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Grid } from 'antd'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { i18n } from '../../i18n/i18n'
import { UIProvider, useUI } from '../../state/UIContext'
import { EmployeeDetailPage, EmployeesPage } from './EmployeesPage'

const api = vi.hoisted(() => ({
  listEmployees: vi.fn(),
  getEmployee: vi.fn(),
  getInfo: vi.fn(),
  createEmployee: vi.fn(),
  addEmployeeKnowledge: vi.fn(),
  listProjects: vi.fn(),
  listSkills: vi.fn(),
  dryRunEmployee: vi.fn(),
  listEmployeeTasks: vi.fn(),
  listLoops: vi.fn(),
  getEmployeeSkills: vi.fn(),
  getEmployeeKnowledge: vi.fn(),
  getEmployeeMemory: vi.fn(),
  getEmployeeMemoryCandidates: vi.fn(),
  getEmployeeActivity: vi.fn(),
  updateEmployee: vi.fn(),
  updateEmployeeSkills: vi.fn(),
  mutateEmployeeLifecycle: vi.fn(),
  refreshEmployeeKnowledge: vi.fn(),
  deleteEmployeeKnowledge: vi.fn(),
  acceptEmployeeMemoryCandidate: vi.fn(),
  rejectEmployeeMemoryCandidate: vi.fn(),
  editEmployeeMemory: vi.fn(),
  forgetEmployeeMemory: vi.fn(),
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
        <DialogProbe />
        <MemoryRouter initialEntries={[path]}>
          <NavigationProbe />
          <Routes>
            <Route path="/employees" element={<EmployeesPage />} />
            <Route path="/employees/:employeeId" element={<EmployeeDetailPage />} />
          </Routes>
        </MemoryRouter>
      </UIProvider>
    </I18nextProvider>,
  )
}

function NavigationProbe() {
  const navigate = useNavigate()
  return <button type="button" onClick={() => void navigate('/employees/employee-b?tab=activity')}>Go Employee B</button>
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

async function openEmployeeTab(user: ReturnType<typeof userEvent.setup>, name: string) {
  await user.click(await screen.findByRole('tab', { name }))
}

async function selectAntOption(
  user: ReturnType<typeof userEvent.setup>,
  label: string,
  option: string,
) {
  fireEvent.mouseDown(screen.getByRole('combobox', { name: label }))
  const optionLabel = (await screen.findAllByText(option, { exact: true })).find((node) =>
    node.classList.contains('ant-select-item-option-content'))
  expect(optionLabel).toBeDefined()
  fireEvent.click(optionLabel!)
}

async function advanceWizardToReview(
  user: ReturnType<typeof userEvent.setup>,
  { selectSkill = true }: { selectSkill?: boolean } = {},
) {
  await user.click(await screen.findByRole('button', { name: 'Create Employee' }))
  await user.click(screen.getByRole('button', { name: 'Advanced setup' }))
  await user.type(screen.getByLabelText('Employee ID'), 'employee.v2')
  await user.type(screen.getByLabelText('Name'), 'Ada')
  await user.type(screen.getByLabelText('Job title'), 'Engineer')
  await user.click(screen.getByRole('button', { name: 'Next' }))
  await user.click(screen.getByRole('button', { name: 'Next' }))
  await user.type(screen.getByLabelText('Charter'), 'Ship verified releases.')
  await user.click(screen.getByRole('button', { name: 'Next' }))
  if (selectSkill) {
    await user.click(screen.getByRole('checkbox', { name: /Release/u }))
  }
  await user.click(screen.getByRole('button', { name: 'Next' }))
  await user.click(screen.getByRole('button', { name: 'Next' }))
  await user.click(screen.getByRole('button', { name: 'Next' }))
  await user.click(screen.getByRole('button', { name: 'Next' }))
  await user.click(screen.getByRole('button', { name: 'Next' }))
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
  api.listLoops.mockResolvedValue({
    loops: [{
      id: 'release-archive',
      schema_version: 1,
      name: 'Release archive',
      description: '',
      employee_id: summary.id,
      contract: {
        goal: 'Archive verified release knowledge.',
        boundaries: ['Keep provenance.'],
        sop: ['Collect the release evidence.'],
        definition_of_done: ['Archive is reviewable.'],
        stop_conditions: ['Stop on conflicting evidence.'],
      },
      schedule: { kind: 'daily', local_time: '02:00', timezone: 'Asia/Shanghai' },
      workspace_identity: '/workspace/gohermit',
      enabled: true,
      task_source: { type: 'fixed_prompt', prompt: 'Archive verified release knowledge.' },
      agent_selection: { company: 'openai', access: 'codex', model: 'gpt', agent: 'coding' },
      team_template_ref: 'default',
      plan_mode: 'review',
      verification_recipe: { checks: [], independent_verifier: true, max_repair_attempts: 0 },
      budget: { max_model_calls: 8, max_tokens: 8000, timeout_seconds: 1200 },
      approval_policy: { require_for_mutation: true },
      workspace_policy: { read_only: true, require_clean_git: false },
      output_policy: { include_diff: false, max_report_bytes: 65536 },
      created_at: now,
      updated_at: now,
      revision: 1,
    }],
  })
  api.getEmployeeActivity.mockResolvedValue({ events: [] })
  api.getInfo.mockResolvedValue({
    workspace: '/workspace',
    available_companies: [{
      id: 'openai',
      label: 'OpenAI',
      access: [{
        id: 'codex',
        label: 'Codex',
        supported: true,
        models: [{ id: 'gpt', label: 'GPT', provider: 'openai' }],
      }],
    }],
    agents: [{ id: 'coding', label: 'Coding' }],
  })
  api.listProjects.mockResolvedValue({
    projects: [{
      id: 'service-workspace',
      label: 'GoHermit',
      workspace_real_path: '/workspace',
      workspace_fingerprint: 'f'.repeat(64),
    }],
  })
  api.listSkills.mockResolvedValue({
    skills: [{
      skill_id: 'release',
      version: '1.0.0',
      digest: 'd'.repeat(64),
      title: 'Release',
      description: 'Release safely',
      kind: 'native',
      status: 'ready',
      capabilities: ['read'],
      configuration_schema: {},
    }],
  })
  api.createEmployee.mockResolvedValue({
    ...employeeRecord,
    employee: { ...employeeRecord.employee, id: 'employee.v2', name: 'Ada', revision: 1 },
    project_bindings: [{
      id: 'project-employee.v2',
      employee_id: 'employee.v2',
      label: 'GoHermit',
      workspace_real_path: '/workspace',
      workspace_fingerprint: 'f'.repeat(64),
      read_allowed: true,
      mutation_allowed: true,
      allowed_tool_capabilities: ['read', 'write'],
      network_allowed: true,
      created_at: now,
      updated_at: now,
    }],
  })
  api.addEmployeeKnowledge.mockResolvedValue({})
  api.updateEmployeeSkills.mockResolvedValue({
    ...employeeRecord,
    employee: { ...employeeRecord.employee, id: 'employee.v2', name: 'Ada', revision: 2 },
  })
  api.dryRunEmployee.mockResolvedValue({
    employee_id: 'employee.v2',
    revision: 1,
    ready: true,
    checks: [{ name: 'provider', ready: true, detail: 'ready' }],
  })
})

describe('Employees Phase 4 pages', () => {
  it('renders the desktop Knowledge table without losing bounded metadata actions', async () => {
    const getComputedStyle = window.getComputedStyle.bind(window)
    vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element))
    vi.spyOn(Grid, 'useBreakpoint').mockReturnValue({ xs: true, sm: true, md: true, lg: true, xl: true, xxl: false })
    const user = userEvent.setup()
    const projection = {
      employee_id: summary.id,
      sources: [{
        schema_version: 1,
        id: 'desktop-guide',
        employee_id: summary.id,
        kind: 'manual_text',
        title: 'Desktop guide',
        digest: 'd'.repeat(64),
        status: 'ready',
      }],
      indexes: [{
        schema_version: 1,
        employee_id: summary.id,
        source_id: 'desktop-guide',
        source_digest: 'd'.repeat(64),
        documents: [],
      }],
      results: [],
    }
    api.getEmployeeKnowledge.mockResolvedValue(projection)
    api.refreshEmployeeKnowledge.mockResolvedValue(projection)

    renderEmployees('/employees/employee-ada?tab=knowledge')

    expect(await screen.findByRole('columnheader', { name: 'Title' })).toBeVisible()
    expect(screen.getByText('Manual text')).toBeVisible()
    expect(screen.getByText('Ready')).toBeVisible()
    expect(screen.getByText(/dddddddd/u)).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Actions' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Refresh' }))
    await waitFor(() => expect(api.refreshEmployeeKnowledge).toHaveBeenCalledOnce())
  })

  it('shows the durable work loops owned by an Employee', async () => {
    const user = userEvent.setup()
    renderEmployees(`/employees/${summary.id}`)

    await screen.findByRole('heading', { name: summary.name })
    await openEmployeeTab(user, 'Loops')

    expect(await screen.findByRole('link', { name: 'Release archive' })).toBeVisible()
    expect(screen.getByText('Archive verified release knowledge.')).toBeVisible()
    expect(api.listLoops).toHaveBeenCalled()
  })

  it('creates a conservative Employee immediately from only a display name', async () => {
    const user = userEvent.setup()
    renderEmployees()

    await user.click(await screen.findByRole('button', { name: 'Create Employee' }))
    expect(screen.getByRole('heading', { name: 'Create in seconds' })).toBeVisible()
    expect(screen.queryByTestId('employee-wizard-step')).not.toBeInTheDocument()

    await user.type(screen.getByLabelText('Name'), '档案管理员')
    await user.click(screen.getByRole('button', { name: 'Create now' }))

    await waitFor(() => expect(api.createEmployee).toHaveBeenCalledOnce())
    const payload = api.createEmployee.mock.calls[0]?.[0] as unknown as {
      employee: {
        id: string
        name: string
        job_title: string
        charter: string
        responsibilities: string[]
        behavior_boundaries: string[]
        skill_bindings: unknown[]
        permission_policy: { allowed_capabilities: string[]; network_allowed: boolean }
        memory_policy: {
          candidate_generation: boolean
          promotion: string
          max_context_facts: number
          max_context_bytes: number
        }
        project_binding_ids: string[]
      }
      project_bindings: Array<{
        id: string
        mutation_allowed: boolean
        network_allowed: boolean
        allowed_tool_capabilities: string[]
      }>
    }
    expect(payload.employee.id).toMatch(/^employee-[a-z0-9._-]+$/u)
    expect(payload.employee.name).toBe('档案管理员')
    expect(payload.employee.job_title).toBe('Role pending')
    expect(payload.employee.charter).toBe('Role details are pending. Configure this Employee before assigning work.')
    expect(payload.employee.responsibilities).toEqual([])
    expect(payload.employee.behavior_boundaries).toEqual([])
    expect(payload.employee.skill_bindings).toEqual([])
    expect(payload.employee.permission_policy).toEqual({
      allowed_capabilities: ['read'],
      network_allowed: false,
    })
    expect(payload.employee.memory_policy).toEqual({
      candidate_generation: false,
      promotion: 'disabled',
      max_context_facts: 0,
      max_context_bytes: 0,
    })
    expect(payload.employee.project_binding_ids).toHaveLength(1)
    expect(payload.project_bindings).toEqual([
      expect.objectContaining({
        id: payload.employee.project_binding_ids[0],
        mutation_allowed: false,
        network_allowed: false,
        allowed_tool_capabilities: ['read'],
      }),
    ])
    expect(api.updateEmployeeSkills).not.toHaveBeenCalled()
    expect(api.addEmployeeKnowledge).not.toHaveBeenCalled()
    expect(api.dryRunEmployee).not.toHaveBeenCalled()
  })

  it('persists a model selected for the Employee in quick create', async () => {
    const user = userEvent.setup()
    api.getInfo.mockResolvedValueOnce({
      workspace: '/workspace',
      available_companies: [
        {
          id: 'openai',
          label: 'OpenAI',
          access: [{ id: 'codex', label: 'Codex', supported: true, models: [{ id: 'gpt', label: 'GPT', provider: 'openai' }, { id: 'gpt-pro', label: 'GPT Pro', provider: 'openai' }] }],
        },
      ],
      agents: [{ id: 'coding', label: 'Coding' }],
    })
    renderEmployees()

    await user.click(await screen.findByRole('button', { name: 'Create Employee' }))
    await user.type(screen.getByLabelText('Name'), '新闻员工')
    await user.selectOptions(screen.getByLabelText('Model'), 'gpt-pro')
    await user.click(screen.getByRole('button', { name: 'Create now' }))

    await waitFor(() => expect(api.createEmployee).toHaveBeenCalledOnce())
    const payload = api.createEmployee.mock.calls[0]?.[0] as { employee: { default_selection: { model: string } } }
    expect(payload.employee.default_selection.model).toBe('gpt-pro')
  })

  it('validates required identity fields before advancing the creation wizard', async () => {
    const user = userEvent.setup()
    renderEmployees()

    await user.click(await screen.findByRole('button', { name: 'Create Employee' }))
    await user.click(screen.getByRole('button', { name: 'Advanced setup' }))
    await user.click(screen.getByRole('button', { name: 'Next' }))

    expect(screen.getByRole('alert')).toHaveTextContent(
      'Enter an Employee ID, name, and job title to continue.',
    )
    expect(screen.getByTestId('employee-wizard-step')).toHaveTextContent('Step 1 of 9')
    expect(api.createEmployee).not.toHaveBeenCalled()
  })

  it('repairs a Chinese Employee ID before leaving identity setup', async () => {
    const user = userEvent.setup()
    renderEmployees()

    await user.click(await screen.findByRole('button', { name: 'Create Employee' }))
    await user.click(screen.getByRole('button', { name: 'Advanced setup' }))
    await user.type(screen.getByLabelText('Employee ID'), '档案管理员')
    await user.type(screen.getByLabelText('Name'), '档案管理员')
    await user.type(screen.getByLabelText('Job title'), '档案管理员')

    expect(screen.getByText(/will generate a safe system ID/iu)).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Next' }))

    expect(screen.getByTestId('employee-wizard-step')).toHaveTextContent('Step 2 of 9')
    await user.click(screen.getByRole('button', { name: 'Previous' }))
    const repaired = screen.getByLabelText<HTMLInputElement>('Employee ID').value
    expect(repaired).toMatch(/^employee-[a-z0-9._-]+$/u)
    expect(repaired).not.toContain('档案管理员')
    expect(api.createEmployee).not.toHaveBeenCalled()
  })

  it('generates a recommended Employee draft and skips optional setup steps', async () => {
    const user = userEvent.setup()
    renderEmployees()

    await user.click(await screen.findByRole('button', { name: 'Create Employee' }))
    await user.click(screen.getByRole('button', { name: 'Advanced setup' }))
    await user.type(screen.getByLabelText('Employee name'), 'Mina')
    await user.type(
      screen.getByLabelText('What should this Employee take care of?'),
      'Maintain GoHermit and verify every change',
    )
    await user.click(screen.getByRole('button', { name: 'Generate recommended draft' }))

    expect(screen.getByLabelText<HTMLInputElement>('Employee ID').value).toMatch(/^developer-/u)
    expect(screen.getByLabelText('Name')).toHaveValue('Mina')
    expect(screen.getByLabelText('Job title')).toHaveValue('Software Engineer')
    expect(screen.getByText('Draft generated. You can edit every field before creating.')).toBeVisible()

    await user.click(screen.getByRole('button', { name: 'Use recommendation and review' }))
    expect(screen.getByTestId('employee-wizard-step')).toHaveTextContent('Step 8 of 9')
    await user.click(screen.getByRole('button', { name: 'Next' }))

    await waitFor(() => expect(api.createEmployee).toHaveBeenCalledOnce())
    const payload = api.createEmployee.mock.calls[0]?.[0] as unknown as {
      employee: { name: string; job_title: string; charter: string }
    }
    expect(payload.employee.name).toBe('Mina')
    expect(payload.employee.job_title).toBe('Software Engineer')
    expect(payload.employee.charter).toContain('Maintain GoHermit')
  })

  it('uses URL filters and bounded cursor pagination without hidden selection', async () => {
    const user = userEvent.setup()
    renderEmployees()

    expect(await screen.findByText('Ada')).toBeVisible()
    fireEvent.mouseDown(screen.getByRole('combobox', { name: 'State' }))
    const activeLabel = screen.getAllByText('Active', { exact: true }).find((node) =>
      node.classList.contains('ant-select-item-option-content'))
    expect(activeLabel).toBeDefined()
    fireEvent.mouseDown(activeLabel!)
    fireEvent.click(activeLabel!)
    await waitFor(() => expect(api.listEmployees.mock.calls.some(([query]) =>
      (query as { state?: string }).state === 'active')).toBe(true))
    await user.click(screen.getByRole('button', { name: 'Load more' }))
    await waitFor(() => expect(api.listEmployees).toHaveBeenLastCalledWith(
      expect.objectContaining({ cursor: 'next-page' }),
      expect.anything(),
    ))
  })

  it('uses the complete Employee card as the detail navigation link', async () => {
    const user = userEvent.setup()
    renderEmployees()

    const cardLink = await screen.findByRole('link', { name: /Ada/u })
    expect(cardLink).toHaveClass('employee-card-link')
    expect(cardLink).toHaveAttribute('href', '/employees/employee-ada')
    await user.click(cardLink)
    await openEmployeeTab(user, 'Settings')
    expect(await screen.findByLabelText('Name')).toHaveValue('Ada')
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

  it('renders the Employee shell without waiting for the auxiliary model catalog', async () => {
    api.getInfo.mockReturnValue(new Promise(() => undefined))
    renderEmployees('/employees/employee-ada')

    expect(await screen.findByRole('heading', { name: 'Ada' })).toBeVisible()
    expect(screen.getByTestId('employee-status')).toHaveTextContent('Active')
  })

  it('edits the authoritative overview and project policy with expected revision', async () => {
    const user = userEvent.setup()
    const activeRecord = {
      ...employeeRecord,
      project_bindings: [{
        id: 'project-main',
        employee_id: summary.id,
        label: 'GoHermit',
        workspace_real_path: '/workspace',
        workspace_fingerprint: 'f'.repeat(64),
        read_allowed: true,
        mutation_allowed: true,
        allowed_tool_capabilities: ['read', 'write'],
        network_allowed: false,
        created_at: now,
        updated_at: now,
      }],
    }
    api.getEmployee.mockResolvedValue(activeRecord)
    api.updateEmployee.mockResolvedValue(activeRecord)
    api.dryRunEmployee.mockResolvedValue({
      employee_id: summary.id,
      revision: summary.revision,
      ready: true,
      checks: [{ name: 'provider', ready: true, detail: 'ready' }],
    })
    renderEmployees('/employees/employee-ada')

    await user.click(await screen.findByRole('button', { name: 'Dry Run' }))
    await waitFor(() => expect(api.dryRunEmployee).toHaveBeenCalledOnce())
    expect(await screen.findByText('1/1')).toBeVisible()
    await openEmployeeTab(user, 'Settings')
    const name = await screen.findByLabelText('Name')
    await user.clear(name)
    await user.type(name, 'Ada Lovelace')
    await user.clear(screen.getByLabelText('Job title'))
    await user.type(screen.getByLabelText('Job title'), 'Principal Engineer')
    await user.clear(screen.getByLabelText('Charter'))
    await user.type(screen.getByLabelText('Charter'), 'Verify every release.')
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.updateEmployee).toHaveBeenCalled())
    const overviewPayload = api.updateEmployee.mock.calls.at(-1)?.[1] as unknown as {
      expected_revision: number
      employee: { name: string }
    }
    expect(overviewPayload).toMatchObject({
      expected_revision: summary.revision,
      employee: { name: 'Ada Lovelace' },
    })

    await openEmployeeTab(user, 'Projects')
    await user.click(screen.getByRole('button', { name: 'Edit' }))
    await user.click(await screen.findByRole('switch', { name: 'Read allowed' }))
    await user.click(screen.getByRole('switch', { name: 'Mutation allowed' }))
    await user.click(screen.getByRole('switch', { name: 'Network allowed' }))
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.updateEmployee).toHaveBeenLastCalledWith(summary.id, expect.objectContaining({
      project_bindings: [expect.objectContaining({
        read_allowed: false,
        mutation_allowed: false,
        network_allowed: true,
      })],
    }), expect.anything()))
  }, 15_000)

  it('edits every backend-supported Employee setting with the model catalog', async () => {
    const user = userEvent.setup()
    api.updateEmployee.mockImplementation((_id: string, input: typeof employeeRecord) => Promise.resolve({
      employee: input.employee,
      project_bindings: input.project_bindings,
    }))
    renderEmployees('/employees/employee-ada')

    await openEmployeeTab(user, 'Settings')
    await screen.findByLabelText('Name')
    await selectAntOption(user, 'Avatar', 'Emoji')
    await user.clear(screen.getByLabelText('Avatar value'))
    await user.type(screen.getByLabelText('Avatar value'), '🚀')
    fireEvent.change(screen.getByLabelText('Responsibilities'), { target: { value: 'Build\nVerify' } })
    fireEvent.change(screen.getByLabelText('Behavior boundaries'), { target: { value: 'Never publish secrets' } })
    expect(screen.getAllByLabelText('Company').length).toBeGreaterThan(0)
    expect(screen.getAllByLabelText('Access').length).toBeGreaterThan(0)
    expect(screen.getAllByLabelText('Model').length).toBeGreaterThan(0)
    expect(screen.getAllByLabelText('Agent').length).toBeGreaterThan(0)
    fireEvent.change(screen.getByLabelText('Capabilities'), { target: { value: 'read\nwrite' } })
    await user.click(screen.getByLabelText('Allow network'))
    await user.clear(screen.getByLabelText('Maximum model calls'))
    await user.type(screen.getByLabelText('Maximum model calls'), '6')
    await user.clear(screen.getByLabelText('Maximum tokens'))
    await user.type(screen.getByLabelText('Maximum tokens'), '6000')
    await user.clear(screen.getByLabelText('Timeout seconds'))
    await user.type(screen.getByLabelText('Timeout seconds'), '900')
    await user.clear(screen.getByLabelText('Maximum running tasks'))
    await user.type(screen.getByLabelText('Maximum running tasks'), '2')
    await user.click(screen.getByLabelText('Generate Memory candidates'))
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(api.updateEmployee).toHaveBeenCalledTimes(1), { timeout: 10_000 })
    const updateInput = api.updateEmployee.mock.calls.at(-1)?.[1] as {
      expected_revision: number
      employee: typeof employeeRecord.employee
    } | undefined
    expect(updateInput?.expected_revision).toBe(summary.revision)
    expect(updateInput?.employee).toMatchObject({
      avatar: { kind: 'emoji', value: '🚀' },
      responsibilities: ['Build', 'Verify'],
      behavior_boundaries: ['Never publish secrets'],
      default_selection: { company: 'openai', access: 'codex', model: 'gpt' },
      agent_profile: 'coding',
      permission_policy: {
        allowed_capabilities: ['read', 'write'],
        network_allowed: true,
      },
      concurrency_policy: { max_running_tasks: 2 },
    })
  }, 15_000)

  it('edits Native Skill configuration and enabled state without accepting invalid JSON', async () => {
    const user = userEvent.setup()
    const binding = {
      skill_id: 'release',
      version: '1.0.0',
      digest: 'd'.repeat(64),
      configuration: { mode: 'safe' },
      enabled: true,
    }
    const adapterBinding = {
      skill_id: 'adapter',
      version: '2.0.0',
      digest: 'a'.repeat(64),
      configuration: { ignored: true },
      enabled: true,
    }
    const activeRecord = {
      ...employeeRecord,
      employee: { ...employeeRecord.employee, skill_bindings: [binding, adapterBinding] },
    }
    api.getEmployee.mockResolvedValue(activeRecord)
    api.getEmployeeSkills.mockResolvedValue({
      employee_id: summary.id,
      revision: summary.revision,
      bindings: [
        { binding, status: 'current', kind: 'native' },
        { binding: adapterBinding, status: 'current', kind: 'skill_md_adapter' },
      ],
    })
    api.listSkills.mockResolvedValue({
      skills: [{
        skill_id: 'release',
        version: '1.0.0',
        digest: 'd'.repeat(64),
        title: 'Release',
        description: 'Release safely',
        kind: 'native',
        requested_capabilities: ['read'],
        configuration_schema: {},
      }, {
        skill_id: 'adapter',
        version: '2.0.0',
        digest: 'a'.repeat(64),
        title: 'Adapter',
        description: 'Metadata only',
        kind: 'skill_md_adapter',
        requested_capabilities: [],
        configuration_schema: {},
      }],
    })
    api.updateEmployeeSkills.mockResolvedValue(activeRecord)
    renderEmployees('/employees/employee-ada')

    await screen.findByRole('heading', { name: 'Ada' })
    await openEmployeeTab(user, 'Skills')
    const releaseCard = await screen.findByTestId('skill-card-release')
    await user.click(within(releaseCard).getByRole('button', { name: 'Inspect configuration' }))
    const configuration = await screen.findByLabelText('Configuration JSON Release')
    fireEvent.change(configuration, { target: { value: '{"mode":"strict"}' } })
    await user.click(screen.getByRole('switch', { name: 'Enabled Release' }))
    await user.click(screen.getByRole('button', { name: 'Save Skills' }))
    await waitFor(() => expect(api.updateEmployeeSkills).toHaveBeenCalledWith(
      summary.id,
      summary.revision,
      [
        {
        ...binding,
        enabled: false,
        configuration: { mode: 'strict' },
        },
        { ...adapterBinding, configuration: {} },
      ],
      expect.anything(),
    ))

    fireEvent.change(configuration, { target: { value: '{invalid' } })
    await user.click(screen.getByRole('button', { name: 'Save Skills' }))
    expect(api.updateEmployeeSkills).toHaveBeenCalledOnce()
  })

  it('binds a Skill by clicking the catalog card without a separate checkbox target', async () => {
    const user = userEvent.setup()
    const binding = {
      skill_id: 'adapter',
      version: '2.0.0',
      digest: 'a'.repeat(64),
      configuration: {},
      enabled: true,
    }
    const activeRecord = {
      ...employeeRecord,
      employee: { ...employeeRecord.employee, skill_bindings: [] },
    }
    api.getEmployee.mockResolvedValue(activeRecord)
    api.getEmployeeSkills.mockResolvedValue({
      employee_id: summary.id,
      revision: summary.revision,
      bindings: [],
    })
    api.listSkills.mockResolvedValue({
      skills: [{
        skill_id: 'adapter',
        version: '2.0.0',
        digest: binding.digest,
        title: 'Adapter',
        description: 'Metadata only',
        kind: 'skill_md_adapter',
        requested_capabilities: [],
        configuration_schema: {},
      }],
    })
    api.updateEmployeeSkills.mockResolvedValue({
      ...activeRecord,
      employee: { ...activeRecord.employee, skill_bindings: [binding] },
    })
    renderEmployees('/employees/employee-ada')

    await screen.findByRole('heading', { name: 'Ada' })
    await openEmployeeTab(user, 'Skills')
    const card = await screen.findByTestId('skill-card-adapter')
    await user.click(within(card).getByRole('button', { name: 'Bind Skill' }))

    expect(within(card).getByRole('button', { name: 'Remove Skill adapter@2.0.0' })).toBeEnabled()
    expect(screen.queryByLabelText('Configuration JSON Adapter')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Save Skills' }))
    await waitFor(() => expect(api.updateEmployeeSkills).toHaveBeenCalledWith(
      summary.id,
      summary.revision,
      [binding],
      expect.anything(),
    ))
  })

  it('unions Catalog and persisted bindings so missing Skills can be removed and drift upgraded', async () => {
    const user = userEvent.setup()
    const missing = {
      skill_id: 'missing', version: '1.0.0', digest: '1'.repeat(64),
      configuration: {}, enabled: true,
    }
    const stale = {
      skill_id: 'release', version: '1.0.0', digest: '2'.repeat(64),
      configuration: { mode: 'safe' }, enabled: true,
    }
    const activeRecord = {
      ...employeeRecord,
      employee: { ...employeeRecord.employee, skill_bindings: [missing, stale] },
    }
    api.getEmployee.mockResolvedValue(activeRecord)
    api.getEmployeeSkills.mockResolvedValue({
      employee_id: summary.id,
      revision: summary.revision,
      bindings: [
        { binding: missing, status: 'missing' },
        { binding: stale, status: 'digest_drift', kind: 'native' },
      ],
    })
    api.updateEmployeeSkills.mockResolvedValue({
      ...activeRecord,
      employee: { ...activeRecord.employee, revision: 5 },
    })
    renderEmployees('/employees/employee-ada?tab=skills')

    const missingCard = await screen.findByTestId('skill-card-missing')
    expect(within(missingCard).getAllByText('missing').length).toBeGreaterThan(0)
    expect(within(missingCard).getByText('1.0.0')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Remove Skill missing@1.0.0' }))
    await user.click(screen.getByRole('button', { name: 'Upgrade to current digest release@1.0.0' }))
    await user.click(screen.getByRole('button', { name: 'Save Skills' }))

    await waitFor(() => expect(api.updateEmployeeSkills).toHaveBeenCalledWith(
      summary.id,
      summary.revision,
      [expect.objectContaining({ skill_id: 'release', digest: 'd'.repeat(64) })],
      expect.anything(),
    ))
  })

  it('requires confirmation before Knowledge delete and Memory reject/forget mutations', async () => {
    const user = userEvent.setup()
    api.getEmployeeKnowledge.mockResolvedValue({
      employee_id: summary.id,
      sources: [{
        schema_version: 1,
        id: 'guide',
        employee_id: summary.id,
        kind: 'manual_text',
        title: 'Guide',
        digest: 'd'.repeat(64),
        status: 'ready',
      }],
      indexes: [],
      results: [],
    })
    api.getEmployeeMemoryCandidates.mockResolvedValue({
      candidates: [{
        schema_version: 1,
        id: 'candidate-1',
        employee_id: summary.id,
        category: 'release',
        value: 'candidate',
        provenance: [],
        created_at: now,
        digest: 'c'.repeat(64),
      }],
    })
    api.getEmployeeMemory.mockResolvedValue({
      facts: [{
        schema_version: 1,
        id: 'fact-1',
        candidate_id: 'candidate-1',
        employee_id: summary.id,
        category: 'release',
        value: 'fact',
        provenance: [],
        created_at: now,
        updated_at: now,
        owner_edited: false,
        digest: 'f'.repeat(64),
      }],
    })
    renderEmployees('/employees/employee-ada?tab=knowledge')

    const knowledgeCard = (await screen.findByText('Guide')).closest('.ant-card')
    expect(knowledgeCard).not.toBeNull()
    await user.click(within(knowledgeCard as HTMLElement).getByRole('button', { name: 'Actions' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    expect(api.deleteEmployeeKnowledge).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Cancel dialog' }))
    expect(api.deleteEmployeeKnowledge).not.toHaveBeenCalled()

    await openEmployeeTab(user, 'Memory')
    await user.click(await screen.findByRole('button', { name: 'Reject' }))
    expect(api.rejectEmployeeMemoryCandidate).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Confirm dialog' }))
    await waitFor(() => expect(api.rejectEmployeeMemoryCandidate).toHaveBeenCalledOnce())

    await user.click(screen.getByRole('button', { name: 'Forget' }))
    expect(api.forgetEmployeeMemory).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Confirm dialog' }))
    await waitFor(() => expect(api.forgetEmployeeMemory).toHaveBeenCalledOnce())
  })

  it('runs authoritative readiness and confirms every lifecycle mutation', async () => {
    const user = userEvent.setup()
    api.dryRunEmployee.mockResolvedValue({
      employee_id: summary.id,
      revision: summary.revision,
      ready: true,
      checks: [{ name: 'provider', ready: true, detail: 'ready' }],
    })
    api.mutateEmployeeLifecycle.mockResolvedValue(employeeRecord)
    renderEmployees('/employees/employee-ada')

    await user.click(await screen.findByRole('button', { name: 'Dry Run' }))
    await waitFor(() => expect(api.dryRunEmployee).toHaveBeenCalledWith(summary.id, expect.anything()))
    await openEmployeeTab(user, 'Settings')

    await user.click(screen.getByRole('button', { name: 'Disable' }))
    expect(api.mutateEmployeeLifecycle).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Confirm dialog' }))
    await waitFor(() => expect(api.mutateEmployeeLifecycle).toHaveBeenCalledWith(
      summary.id, 'disable', summary.revision, expect.anything(),
    ))

    await user.click(screen.getByRole('button', { name: 'Archive' }))
    await user.click(screen.getByRole('button', { name: 'Cancel dialog' }))
    expect(api.mutateEmployeeLifecycle).toHaveBeenCalledTimes(1)
  })

  it('creates, searches, refreshes, and inspects bounded Knowledge evidence', async () => {
    const user = userEvent.setup()
    const citation = {
      schema_version: 1,
      id: 'citation-1',
      employee_id: summary.id,
      source_id: 'guide',
      path: 'docs/guide.md',
      heading: 'Release',
      start_line: 2,
      end_line: 4,
      digest: 'c'.repeat(64),
      snippet: 'Verify before release.',
    }
    const projection = {
      employee_id: summary.id,
      sources: [{
        schema_version: 1,
        id: 'guide',
        employee_id: summary.id,
        kind: 'manual_text',
        title: 'Guide',
        digest: 'd'.repeat(64),
        status: 'ready',
      }],
      indexes: [{
        schema_version: 1,
        employee_id: summary.id,
        source_id: 'guide',
        source_digest: 'd'.repeat(64),
        documents: [{ path: 'docs/guide.md', digest: 'c'.repeat(64), terms: ['release'], citations: [citation] }],
      }],
      results: [],
    }
    api.getEmployeeKnowledge.mockResolvedValue(projection)
    api.refreshEmployeeKnowledge.mockResolvedValue(projection)
    api.addEmployeeKnowledge.mockResolvedValue(projection)
    renderEmployees('/employees/employee-ada?tab=knowledge')

    await screen.findByText('Guide')
    await user.type(screen.getByRole('searchbox', { name: 'Search' }), 'Guide')
    await user.click(screen.getByRole('button', { name: 'Citations (1)' }))
    expect(await screen.findByText('Verify before release.')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Close' }))

    await user.click(screen.getAllByRole('button', { name: 'Actions' }).at(-1)!)
    await user.click(await screen.findByRole('menuitem', { name: 'Refresh' }))
    await waitFor(() => expect(api.refreshEmployeeKnowledge).toHaveBeenCalledOnce())

    await user.click(screen.getByRole('button', { name: 'Add Knowledge source' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText('Source ID'), 'manual-release')
    await user.type(within(dialog).getByLabelText('Title'), 'Release notes')
    await user.type(within(dialog).getByLabelText('Manual text'), 'Ship only after verification.')
    await user.click(within(dialog).getByRole('button', { name: 'Add Knowledge' }))
    await waitFor(() => expect(api.addEmployeeKnowledge).toHaveBeenCalledWith(
      summary.id,
      expect.objectContaining({ id: 'manual-release', manual_text: 'Ship only after verification.' }),
      expect.anything(),
    ))
  }, 15_000)

  it('accepts and edits Memory and saves workspace-only Project policy', async () => {
    const user = userEvent.setup()
    const candidate = {
      schema_version: 1,
      id: 'candidate-1',
      employee_id: summary.id,
      category: 'release',
      value: 'candidate',
      provenance: [],
      created_at: now,
      digest: 'c'.repeat(64),
    }
    const fact = {
      ...candidate,
      id: 'fact-1',
      candidate_id: candidate.id,
      value: 'fact',
      updated_at: now,
      owner_edited: false,
      digest: 'f'.repeat(64),
    }
    const binding = {
      id: 'project-main',
      employee_id: summary.id,
      label: 'GoHermit',
      workspace_real_path: '/workspace',
      workspace_fingerprint: 'w'.repeat(64),
      read_allowed: true,
      mutation_allowed: false,
      allowed_tool_capabilities: ['read'],
      network_allowed: false,
      created_at: now,
      updated_at: now,
    }
    const record = { ...employeeRecord, project_bindings: [binding] }
    api.getEmployee.mockResolvedValue(record)
    api.getEmployeeMemoryCandidates.mockResolvedValue({ candidates: [candidate] })
    api.getEmployeeMemory.mockResolvedValue({ facts: [fact] })
    api.acceptEmployeeMemoryCandidate.mockResolvedValue({})
    api.editEmployeeMemory.mockResolvedValue({})
    api.updateEmployee.mockResolvedValue(record)
    renderEmployees('/employees/employee-ada?tab=memory')

    await user.click(await screen.findByRole('button', { name: 'Accept' }))
    await waitFor(() => expect(api.acceptEmployeeMemoryCandidate).toHaveBeenCalledOnce())
    const editor = screen.getByDisplayValue('fact')
    await user.clear(editor)
    await user.type(editor, 'reviewed fact')
    await user.click(screen.getByRole('button', { name: 'Edit' }))
    await waitFor(() => expect(api.editEmployeeMemory).toHaveBeenCalledWith(
      summary.id, fact.id, 'reviewed fact', expect.anything(),
    ))

    await openEmployeeTab(user, 'Projects')
    await user.click(await screen.findByRole('button', { name: 'Edit' }))
    await user.click(screen.getByRole('switch', { name: 'Mutation allowed' }))
    await user.click(screen.getByRole('switch', { name: 'Network allowed' }))
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.updateEmployee).toHaveBeenCalledWith(
      summary.id,
      expect.objectContaining({
        expected_revision: summary.revision,
        project_bindings: [expect.objectContaining({ mutation_allowed: true, network_allowed: true })],
      }),
      expect.anything(),
    ))
  }, 15_000)

  it('drops a delayed Employee A Activity page after routing to Employee B', async () => {
    const user = userEvent.setup()
    let resolveLate: ((value: { events: Array<Record<string, unknown>> }) => void) | undefined
    const late = new Promise<{ events: Array<Record<string, unknown>> }>((resolve) => {
      resolveLate = resolve
    })
    api.getEmployee.mockImplementation((id: string) => Promise.resolve({
      ...employeeRecord,
      employee: { ...employeeRecord.employee, id, name: id === 'employee-b' ? 'Employee B' : 'Employee A' },
    }))
    api.getEmployeeActivity.mockImplementation(async (id: string, query: { cursor?: string }) => {
      if (id === 'employee-ada' && query.cursor) return late
      return id === 'employee-b'
        ? { events: [{ schema_version: 1, id: 'b-event', employee_id: id, type: 'updated', time: now, subject_id: 'B-only' }] }
        : { events: [{ schema_version: 1, id: 'a-event', employee_id: id, type: 'updated', time: now, subject_id: 'A-first' }], next_cursor: 'next' }
    })
    renderEmployees('/employees/employee-ada?tab=activity')

    await user.click(await screen.findByRole('button', { name: 'Load more' }))
    await user.click(screen.getByRole('button', { name: 'Go Employee B' }))
    expect(await screen.findByText(/B-only/u)).toBeVisible()
    resolveLate?.({
      events: [{
        schema_version: 1,
        id: 'a-late',
        employee_id: 'employee-ada',
        type: 'updated',
        time: now,
        subject_id: 'A-late',
      }],
    })
    await waitFor(() => expect(screen.queryByText(/A-late/u)).not.toBeInTheDocument())
  })

  it('uses the approved nine-step order and ready server catalog', async () => {
    const user = userEvent.setup()
    renderEmployees()

    await user.click(await screen.findByRole('button', { name: 'Create Employee' }))
    await user.click(screen.getByRole('button', { name: 'Advanced setup' }))
    expect(screen.getByTestId('employee-wizard-step')).toHaveTextContent('Identity')
    expect(api.getInfo).toHaveBeenCalledOnce()
    expect(api.listProjects).toHaveBeenCalledOnce()

    await user.type(screen.getByLabelText('Employee ID'), 'employee.v2')
    await user.type(screen.getByLabelText('Name'), 'Ada')
    await user.type(screen.getByLabelText('Job title'), 'Engineer')
    await user.click(screen.getByRole('button', { name: 'Next' }))
    expect(screen.getByTestId('employee-wizard-step')).toHaveTextContent('Model / Agent')
  })

  it('executes every wizard step and persists the structured draft before opening it', async () => {
    const user = userEvent.setup()
    api.getEmployeeSkills.mockResolvedValue({
      employee_id: 'employee.v2',
      revision: 2,
      bindings: [{
        binding: {
          skill_id: 'release',
          version: '1.0.0',
          digest: 'd'.repeat(64),
          configuration: { mode: 'safe' },
          enabled: true,
        },
        status: 'current',
        kind: 'native',
      }],
    })
    api.getEmployeeKnowledge.mockResolvedValue({
      employee_id: 'employee.v2',
      sources: [{
        id: 'release-guide',
        employee_id: 'employee.v2',
        kind: 'manual_text',
        title: 'Release guide',
        status: 'ready',
        revision: 1,
        created_at: now,
        updated_at: now,
      }],
      indexes: [],
      results: [],
    })
    renderEmployees()

    await user.click(await screen.findByRole('button', { name: 'Create Employee' }))
    await user.click(screen.getByRole('button', { name: 'Advanced setup' }))
    await user.type(screen.getByLabelText('Employee ID'), 'employee.v2')
    await user.type(screen.getByLabelText('Name'), 'Ada')
    await user.type(screen.getByLabelText('Job title'), 'Engineer')
    await user.selectOptions(screen.getByLabelText('Avatar'), 'emoji')
    await user.type(screen.getByLabelText('Emoji'), 'A')
    await user.click(screen.getByRole('button', { name: 'Next' }))

    await user.selectOptions(screen.getByLabelText('Company'), 'openai')
    await user.selectOptions(screen.getByLabelText('Access'), 'codex')
    await user.selectOptions(screen.getByLabelText('Model'), 'gpt')
    await user.selectOptions(screen.getByLabelText('Agent'), 'coding')
    await user.click(screen.getByRole('button', { name: 'Next' }))

    await user.type(screen.getByLabelText('Charter'), 'Ship verified releases.')
    fireEvent.change(screen.getByLabelText('Responsibilities'), {
      target: { value: 'Build\nVerify' },
    })
    fireEvent.change(screen.getByLabelText('Behavior boundaries'), {
      target: { value: 'Never publish secrets' },
    })
    await user.click(screen.getByRole('button', { name: 'Next' }))

    await user.click(screen.getByRole('checkbox', { name: /Release/u }))
    fireEvent.change(screen.getByLabelText('Configuration JSON'), {
      target: { value: '{"mode":"safe"}' },
    })
    await user.click(screen.getByRole('button', { name: 'Next' }))

    await user.selectOptions(screen.getByLabelText('Knowledge kind'), 'manual_text')
    await user.type(screen.getByLabelText('Source ID'), 'release-guide')
    await user.type(screen.getByLabelText('Title'), 'Release guide')
    await user.type(screen.getByLabelText('Manual text'), 'Run verification first.')
    await user.click(screen.getByRole('button', { name: 'Next' }))

    await user.click(screen.getByRole('checkbox', { name: /generate memory candidates/iu }))
    await user.clear(screen.getByLabelText('Maximum context facts'))
    await user.type(screen.getByLabelText('Maximum context facts'), '12')
    await user.clear(screen.getByLabelText('Maximum context bytes'))
    await user.type(screen.getByLabelText('Maximum context bytes'), '16384')
    await user.click(screen.getByRole('button', { name: 'Next' }))

    await user.click(screen.getByRole('radio', { name: /GoHermit/u }))
    await user.click(screen.getByRole('button', { name: 'Next' }))

    await user.clear(screen.getByLabelText('Capabilities'))
    fireEvent.change(screen.getByLabelText('Capabilities'), {
      target: { value: 'read\nwrite' },
    })
    await user.click(screen.getByRole('checkbox', { name: 'Allow network' }))
    await user.clear(screen.getByLabelText('Maximum model calls'))
    await user.type(screen.getByLabelText('Maximum model calls'), '10')
    await user.clear(screen.getByLabelText('Maximum tokens'))
    await user.type(screen.getByLabelText('Maximum tokens'), '64000')
    await user.clear(screen.getByLabelText('Timeout seconds'))
    await user.type(screen.getByLabelText('Timeout seconds'), '900')
    await user.click(screen.getByRole('button', { name: 'Next' }))

    expect(await screen.findByTestId('employee-readiness')).toHaveTextContent('Ready')
    expect(api.createEmployee).toHaveBeenCalledOnce()
    const createPayload = api.createEmployee.mock.calls[0]?.[0] as unknown as {
      employee: {
        id: string
        responsibilities: string[]
        behavior_boundaries: string[]
        skill_bindings: Array<{
          skill_id: string
          version: string
          digest: string
          configuration: Record<string, unknown>
        }>
      }
    }
    expect(createPayload).toMatchObject({
      employee: {
        id: 'employee.v2',
        responsibilities: ['Build', 'Verify'],
        behavior_boundaries: ['Never publish secrets'],
        skill_bindings: [],
      },
    })
    expect(api.updateEmployeeSkills).toHaveBeenCalledWith(
      'employee.v2',
      1,
      [expect.objectContaining({
        skill_id: 'release',
        version: '1.0.0',
        digest: 'd'.repeat(64),
        configuration: { mode: 'safe' },
      })],
    )
    expect(api.addEmployeeKnowledge).toHaveBeenCalledWith('employee.v2', expect.objectContaining({
      id: 'release-guide',
      manual_text: 'Run verification first.',
    }))
    expect(api.dryRunEmployee).toHaveBeenCalledWith('employee.v2')
    await user.click(screen.getByRole('button', { name: 'Open Employee' }))
  })

  it('does not report Ready when Skill validation rejects and retries without recreating', async () => {
    const user = userEvent.setup()
    api.updateEmployeeSkills
      .mockRejectedValueOnce(new Error('invalid Native configuration'))
      .mockResolvedValueOnce({
        ...employeeRecord,
        employee: { ...employeeRecord.employee, id: 'employee.v2', revision: 2 },
      })
    api.getEmployeeSkills.mockResolvedValue({
      employee_id: 'employee.v2',
      revision: 2,
      bindings: [{
        binding: {
          skill_id: 'release',
          version: '1.0.0',
          digest: 'd'.repeat(64),
          configuration: {},
          enabled: true,
        },
        status: 'current',
        kind: 'native',
      }],
    })
    api.getEmployeeKnowledge.mockResolvedValue({
      employee_id: 'employee.v2', sources: [], indexes: [], results: [],
    })
    renderEmployees()

    await advanceWizardToReview(user)
    expect(await screen.findByTestId('employee-readiness')).toHaveTextContent('Blocked')
    expect(screen.getByText(/was persisted/i)).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Retry validation' }))

    await waitFor(() => expect(api.updateEmployeeSkills).toHaveBeenCalledTimes(2))
    expect(api.createEmployee).toHaveBeenCalledOnce()
  })

  it('does not report Ready after Catalog digest drift during creation', async () => {
    const user = userEvent.setup()
    api.getEmployeeSkills.mockResolvedValue({
      employee_id: 'employee.v2',
      revision: 2,
      bindings: [{
        binding: {
          skill_id: 'release',
          version: '1.0.0',
          digest: 'd'.repeat(64),
          configuration: {},
          enabled: true,
        },
        status: 'digest_drift',
        kind: 'native',
      }],
    })
    api.getEmployeeKnowledge.mockResolvedValue({
      employee_id: 'employee.v2', sources: [], indexes: [], results: [],
    })
    renderEmployees()

    await advanceWizardToReview(user)
    expect(await screen.findByTestId('employee-readiness')).toHaveTextContent('Blocked')
    expect(api.createEmployee).toHaveBeenCalledOnce()
  })
})
