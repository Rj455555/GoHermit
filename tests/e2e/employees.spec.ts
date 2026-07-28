import { expect, test, type Page, type Route } from '@playwright/test'

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

async function mockEmployees(page: Page) {
  const now = new Date().toISOString()
  const project = {
    id: 'service-workspace',
    label: 'GoHermit',
    workspace_real_path: '/workspace/gohermit',
    workspace_fingerprint: 'f'.repeat(64),
  }
  let employee: any = null
  let candidates: any[] = [{
    schema_version: 1,
    id: 'candidate-1',
    employee_id: 'employee-ada',
    category: 'workflow',
    value: 'Run focused tests before the full suite.',
    provenance: [{ source_type: 'run', source_id: 'run-1', source_task_id: 'task-1', source_session_id: 'session-1', source_run_id: 'run-1', verified_at: now }],
    created_at: now,
    digest: 'a'.repeat(64),
  }, {
    schema_version: 1,
    id: 'candidate-2',
    employee_id: 'employee-ada',
    category: 'style',
    value: 'Always publish automatically.',
    provenance: [{ source_type: 'run', source_id: 'run-2', source_task_id: 'task-2', source_session_id: 'session-2', source_run_id: 'run-2', verified_at: now }],
    created_at: now,
    digest: '9'.repeat(64),
  }]
  let facts: any[] = [{
    schema_version: 1,
    id: 'mem-1',
    candidate_id: 'candidate-old',
    employee_id: 'employee-ada',
    category: 'preference',
    value: 'Prefer concise progress reports.',
    provenance: [{ source_type: 'owner', source_id: 'owner-note', verified_at: now }],
    created_at: now,
    updated_at: now,
    digest: 'b'.repeat(64),
    owner_edited: false,
  }]
  const task = {
    id: 'task-1', employee_id: 'employee-ada', employee_revision: 1,
    prompt: 'Audit the dashboard.', state: 'queued', created_at: now, updated_at: now,
    snapshot_digest: 'c'.repeat(64), session_id: '', run_id: '', artifacts: [],
  }
  const activity: any[] = [{ id: 'evt-1', employee_id: 'employee-ada', type: 'employee_created', time: now, employee_revision: 1 }]

  await page.route('**/api/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    const method = request.method()
    if (path === '/api/health') return json(route, { status: 'ok', version: '0.7.0-dev', active: false })
    if (path === '/api/info') return json(route, {
      version: '0.7.0-dev', workspace: project.workspace_real_path, active: false,
      selection: { company: 'deepseek', access: 'deepseek', model: 'deepseek-chat', agent: 'coding' },
      available_companies: [{
        id: 'deepseek', label: 'DeepSeek',
        access: [{ id: 'deepseek', label: 'API Key', models: [{ id: 'deepseek-chat', label: 'DeepSeek Chat' }] }],
      }],
      companies: [
        { id: 'deepseek', label: 'DeepSeek', access: [{ id: 'deepseek', label: 'API Key', auth_type: 'api_key', models: [{ id: 'deepseek-chat', label: 'DeepSeek Chat' }] }] },
        { id: 'openai', label: 'OpenAI', access: [{ id: 'openai-codex', label: 'Codex', auth_type: 'oauth', models: [{ id: 'gpt-missing', label: 'Unavailable' }] }] },
      ],
      agents: [{ id: 'coding', label: 'Coding Agent' }],
      auth_status: { deepseek: { configured: true, detail: 'Fake Provider' }, 'openai-codex': { configured: false, detail: 'Login expired' } },
      owner: { configured: true },
    })
    if (path === '/api/owner') return json(route, { schema_version: 1, identity: {}, preferences: {}, environments: [], facts: [] })
    if (path === '/api/sessions') return json(route, { sessions: [] })
    if (path === '/api/loops') return json(route, { loops: [] })
    if (path === '/api/projects') return json(route, { projects: [project] })
    if (path === '/api/skills') return json(route, { skills: [
      { skill_id: 'go-tdd', version: '1.0.0', digest: 'd'.repeat(64), kind: 'native', title: 'Go TDD', description: 'Test-first Go workflow.', requested_capabilities: ['test.run'], configuration_schema: {} },
      { skill_id: 'legacy-review', version: 'sha256-e1', digest: 'e'.repeat(64), kind: 'skill_md_adapter', title: 'Legacy Review', description: 'Instruction-only compatibility Skill.', requested_capabilities: [], configuration_schema: {} },
    ] })
    if (path === '/api/employees' && method === 'GET') {
      return json(route, { employees: employee ? [{
        id: employee.employee.id, revision: employee.employee.revision, state: employee.employee.state,
        name: employee.employee.name, job_title: employee.employee.job_title,
        agent_profile: employee.employee.agent_profile, project_count: 1,
        created_at: now, updated_at: now,
      }] : [] })
    }
    if (path === '/api/employees' && method === 'POST') {
      const input = request.postDataJSON() as any
      employee = {
        employee: {
          ...input.employee, schema_version: 1, revision: 1, state: 'active',
          project_binding_ids: [input.project_bindings[0].id],
          created_at: now, updated_at: now,
        },
        project_bindings: input.project_bindings.map((binding: any) => ({
          ...binding, employee_id: input.employee.id, workspace_fingerprint: project.workspace_fingerprint,
          created_at: now, updated_at: now,
        })),
      }
      return json(route, employee, 201)
    }
    if (path === '/api/employees/employee-ada' && method === 'GET') return json(route, employee)
    const transition = path.match(/^\/api\/employees\/employee-ada\/(disable|enable|archive)$/)
    if (transition && method === 'POST') {
      const state = transition[1] === 'disable' ? 'disabled' : transition[1] === 'enable' ? 'active' : 'archived'
      employee.employee.state = state
      employee.employee.revision += 1
      employee.employee.updated_at = now
      activity.push({ id: `evt-${activity.length + 1}`, employee_id: 'employee-ada', type: `employee_${transition[1]}d`, time: now, employee_revision: employee.employee.revision })
      return json(route, employee)
    }
    if (path === '/api/employees/employee-ada/skills' && method === 'GET') {
      return json(route, { employee_id: 'employee-ada', revision: 1, bindings: [{
        binding: { skill_id: 'go-tdd', version: '1.0.0', digest: 'd'.repeat(64), enabled: true },
        status: 'current', kind: 'native',
      }] })
    }
    if (path === '/api/employees/employee-ada/knowledge' && method === 'GET') return json(route, {
      sources: [{ schema_version: 1, id: 'handbook', employee_id: 'employee-ada', kind: 'manual_text', title: 'Team handbook', digest: '1'.repeat(64), status: 'ready' }],
      results: [{ source_id: 'handbook', title: 'Team handbook', score: 3, citation: { id: 'cite-1', path: 'manual', heading: 'Testing', start_line: 1, end_line: 2, digest: '2'.repeat(64), snippet: 'Run focused tests first.' } }],
    })
    if (path === '/api/employees/employee-ada/memory' && method === 'GET') return json(route, { facts })
    if (path === '/api/employees/employee-ada/memory-candidates' && method === 'GET') return json(route, { candidates })
    if (path === '/api/employees/employee-ada/memory-candidates/candidate-1/accept' && method === 'POST') {
      const fact = { ...candidates[0], id: 'mem-accepted', candidate_id: 'candidate-1', updated_at: now, owner_edited: false }
      facts = [...facts, fact]
      candidates = []
      return json(route, fact)
    }
    const reject = path.match(/^\/api\/employees\/employee-ada\/memory-candidates\/([^/]+)$/)
    if (reject && method === 'DELETE') {
      candidates = candidates.filter(item => item.id !== reject[1])
      return route.fulfill({ status: 204, body: '' })
    }
    if (path === '/api/employees/employee-ada/memory/mem-1' && method === 'DELETE') {
      facts = facts.filter(item => item.id !== 'mem-1')
      return route.fulfill({ status: 204, body: '' })
    }
    if (path === '/api/employees/employee-ada/tasks' && method === 'GET') return json(route, { tasks: [task] })
    if (path === '/api/employees/employee-ada/activity') return json(route, { events: activity })
    return json(route, { error: `unmocked ${method} ${path}` }, 404)
  })
}

test('employee wizard uses only ready models and opens isolated resource detail', async ({ page }) => {
  await mockEmployees(page)
  await page.goto('/')

  await expect(page.getByTestId('nav-dashboard')).toBeVisible()
  await expect(page.getByTestId('nav-employees')).toBeVisible()
  await expect(page.getByTestId('nav-employee-tasks')).toBeVisible()
  await expect(page.getByTestId('nav-agent')).toBeVisible()
  await expect(page.getByTestId('nav-loops')).toBeVisible()
  await expect(page.getByTestId('nav-settings')).toBeVisible()

  await page.getByTestId('nav-employees').click()
  await page.getByTestId('employee-new').click()
  await page.getByTestId('employee-id').fill('employee-ada')
  await page.getByTestId('employee-name').fill('Ada')
  await page.getByTestId('employee-job-title').fill('Release Engineer')
  await page.getByTestId('wizard-next').click()
  await expect(page.getByTestId('employee-company')).toHaveValue('deepseek')
  await expect(page.getByTestId('employee-company').locator('option')).toHaveCount(1)
  await expect(page.getByTestId('employee-model')).toHaveValue('deepseek-chat')

  for (let step = 0; step < 7; step += 1) await page.getByTestId('wizard-next').click()
  await page.getByTestId('employee-create').click()
  await expect(page.getByTestId('employee-card')).toContainText('Ada')
  await page.getByTestId('employee-card').click()

  await expect(page.getByTestId('employee-status')).toHaveText('active')
  await page.getByRole('tab', { name: 'Skills' }).click()
  const goTDD = page.getByTestId('employee-skill').filter({ hasText: 'Go TDD' })
  await expect(goTDD).toContainText('Go TDD')
  await expect(goTDD).toContainText('current')
  await expect(page.getByTestId('employee-skill').filter({ hasText: 'Legacy Review' })).toContainText('skill_md_adapter')

  await page.getByRole('tab', { name: 'Knowledge' }).click()
  await expect(page.getByTestId('knowledge-source')).toContainText('Team handbook')
  await expect(page.getByTestId('knowledge-citation')).toContainText('Run focused tests first.')

  await page.getByRole('tab', { name: 'Memory' }).click()
  await expect(page.getByTestId('memory-candidate')).toHaveCount(2)
  await page.getByTestId('memory-candidate').filter({ hasText: 'Always publish' }).getByRole('button', { name: 'Reject' }).click()
  await expect(page.getByTestId('memory-candidate')).toHaveCount(1)
  await page.getByTestId('candidate-accept').click()
  await expect(page.getByTestId('memory-candidate')).toHaveCount(0)
  await page.getByTestId('memory-forget').first().click()
  await expect(page.getByText('Prefer concise progress reports.')).toHaveCount(0)

  await page.getByRole('tab', { name: 'Projects' }).click()
  await expect(page.getByTestId('employee-project')).toContainText('/workspace/gohermit')
  await page.getByRole('tab', { name: 'Tasks' }).click()
  await expect(page.getByTestId('employee-task-row')).toContainText('Audit the dashboard.')

  await page.getByRole('button', { name: 'Disable' }).click()
  await expect(page.getByTestId('employee-status')).toHaveText('disabled')
  await page.getByRole('button', { name: 'Enable' }).click()
  await expect(page.getByTestId('employee-status')).toHaveText('active')
  await page.getByRole('button', { name: 'Archive' }).click()
  await expect(page.getByTestId('employee-status')).toHaveText('archived')
  await page.getByRole('tab', { name: 'Activity' }).click()
  await expect(page.getByText('employee_disabled')).toBeVisible()
  await expect(page.getByText('employee_enabled')).toBeVisible()
  await expect(page.getByText('employee_archived')).toBeVisible()
})
