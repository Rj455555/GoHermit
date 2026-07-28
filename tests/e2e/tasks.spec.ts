import { expect, test, type Page, type Route } from '@playwright/test'

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

async function mockTasks(page: Page) {
  const now = new Date().toISOString()
  let starts = 0
  let failRefresh = false
  const employee = {
    id: 'employee-ada', revision: 3, state: 'active', name: 'Ada',
    job_title: 'Release Engineer', agent_profile: 'coding', project_count: 1,
    created_at: now, updated_at: now,
  }
  const tasks: any[] = [{
    id: 'task-existing', employee_id: employee.id, employee_revision: 3,
    prompt: 'Review release notes.', state: 'completed', created_at: now, updated_at: now,
    snapshot_digest: 'a'.repeat(64), session_id: 'session-existing', run_id: 'run-existing',
    skills: [], knowledge: [], memory_facts: [], artifacts: [],
    project_binding: { id: 'project-main', label: 'GoHermit', workspace_fingerprint: 'f'.repeat(64), read_allowed: true, mutation_allowed: true, network_allowed: false, allowed_tool_capabilities: ['read'] },
    policy: { allowed_capabilities: ['read'], network_allowed: false, budget: { max_model_calls: 4, max_tokens: 4000, timeout_seconds: 600 } },
  }]
  const session = {
    id: 'session-existing', title: 'Review release notes.', status: 'closed',
    selection: { company: 'deepseek', access: 'deepseek', model: 'deepseek-chat', agent: 'coding' },
    plan: { revision: 2, steps: [{ id: 'inspect', description: 'Inspect changes', status: 'completed' }, { id: 'verify', description: 'Run checks', status: 'completed' }] },
    tools: [{ run_id: 'run-existing', call_id: 'call-1', name: 'read_file', args_digest: 'b'.repeat(64), status: 'completed', turn: 1, started_at: now, completed_at: now }],
    verification: { status: 'passed', summary: 'All checks passed.' },
    runs: [{ id: 'run-existing', message: 'Review release notes.', status: 'completed', plan_mode: 'auto', created_at: now, updated_at: now }],
    active_run_id: '',
  }

  await page.route('**/api/**', async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const method = request.method()
    if (path === '/api/health') return json(route, { status: 'ok', version: '0.7.0-dev', active: false })
    if (path === '/api/info') return json(route, {
      version: '0.7.0-dev', workspace: '/workspace/gohermit', active: false,
      selection: { company: 'deepseek', access: 'deepseek', model: 'deepseek-chat', agent: 'coding' },
      available_companies: [{ id: 'deepseek', label: 'DeepSeek', access: [{ id: 'deepseek', label: 'API Key', models: [{ id: 'deepseek-chat', label: 'DeepSeek Chat' }] }] }],
      companies: [], agents: [{ id: 'coding', label: 'Coding Agent' }], auth_status: {}, owner: { configured: true },
    })
    if (path === '/api/owner') return json(route, { schema_version: 1, identity: {}, preferences: {}, environments: [], facts: [] })
    if (path === '/api/sessions') return json(route, { sessions: [] })
    if (path === '/api/loops') return json(route, { loops: [] })
    if (path === '/api/employees') return json(route, { employees: [employee] })
    if (path === '/api/employees/employee-ada') return json(route, {
      employee: { ...employee, schema_version: 1, avatar: { kind: 'initials', value: 'A' }, charter: 'Ship verified releases.', responsibilities: [], behavior_boundaries: [], default_selection: { company: 'deepseek', access: 'deepseek', model: 'deepseek-chat' }, skill_bindings: [], project_binding_ids: ['project-main'], permission_policy: { allowed_capabilities: ['read'], network_allowed: false }, budget_policy: { max_model_calls: 8, max_tokens: 8000, timeout_seconds: 1200 }, concurrency_policy: { max_running_tasks: 1 }, memory_policy: { candidate_generation: true, promotion: 'owner_confirmation', max_context_facts: 8, max_context_bytes: 8192 } },
      project_bindings: [{ id: 'project-main', employee_id: employee.id, label: 'GoHermit', workspace_real_path: '/workspace/gohermit', workspace_fingerprint: 'f'.repeat(64), read_allowed: true, mutation_allowed: true, allowed_tool_capabilities: ['read'], network_allowed: false, created_at: now, updated_at: now }],
    })
    if (path === '/api/employees/employee-ada/skills') return json(route, { employee_id: employee.id, revision: 3, bindings: [] })
    if (path === '/api/employees/employee-ada/knowledge') return json(route, { sources: [], indexes: [], results: [] })
    if (path === '/api/employees/employee-ada/memory') return json(route, { facts: [] })
    if (path === '/api/employees/employee-ada/tasks' && method === 'GET') return json(route, { tasks })
    if (path === '/api/employees/employee-ada/tasks' && method === 'POST') {
      const input = request.postDataJSON() as any
      const task = {
        ...tasks[0], id: 'task-new', prompt: input.prompt, state: 'queued',
        session_id: '', run_id: '', created_at: now, updated_at: now,
      }
      tasks.unshift(task)
      return json(route, task, 201)
    }
    const taskMatch = path.match(/^\/api\/employee-tasks\/([^/]+)$/)
    if (taskMatch && method === 'GET') {
      const task = tasks.find(item => item.id === taskMatch[1])
      if (failRefresh && task?.id === 'task-existing') return json(route, { error: 'temporary disconnect' }, 503)
      return task ? json(route, task) : json(route, { error: 'not found' }, 404)
    }
    if (path === '/api/employee-tasks/task-new/start' && method === 'POST') {
      starts += 1
      const task = tasks.find(item => item.id === 'task-new')
      Object.assign(task, { state: 'running', session_id: 'session-new', run_id: 'run-new', updated_at: now })
      return json(route, task)
    }
    if (path === '/api/sessions/session-existing' && method === 'GET') return json(route, { session, messages: [] })
    if (path === '/api/sessions/session-new' && method === 'GET') return json(route, { session: { ...session, id: 'session-new', status: 'open', active_run_id: 'run-new', runs: [{ ...session.runs[0], id: 'run-new', status: 'running' }] }, messages: [] })
    if (path === '/api/sessions/session-existing/approvals') return json(route, { approvals: [{ request_id: 'approval-1', session_id: 'session-existing', run_id: 'run-existing', tool: 'write_file', resource_paths: ['CHANGELOG.md'], args_summary: 'Update release notes', status: 'pending', created_at: now, expires_at: new Date(Date.now() + 60000).toISOString() }] })
    if (path.endsWith('/events')) return route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' })
    return json(route, { error: `unmocked ${method} ${path}` }, 404)
  })

  return {
    starts: () => starts,
    failNextRefresh: () => { failRefresh = true },
  }
}

test('tasks require explicit Start and render the existing Session execution truth', async ({ page }) => {
  const harness = await mockTasks(page)
  await page.goto('/')
  await page.getByTestId('nav-employee-tasks').click()

  await expect(page.getByTestId('task-row')).toHaveCount(1)
  await page.getByTestId('task-new').click()
  await page.getByTestId('task-prompt').fill('Prepare the release checklist.')
  await page.getByTestId('task-create').click()
  await expect(page.getByTestId('task-row')).toHaveCount(2)
  expect(harness.starts()).toBe(0)
  await expect(page.getByTestId('task-status')).toHaveText('queued')
  await page.getByTestId('task-start').click()
  expect(harness.starts()).toBe(1)
  await expect(page.getByTestId('task-status')).toHaveText('running')

  await page.getByTestId('task-row').filter({ hasText: 'Review release notes.' }).click()
  await expect(page.getByTestId('task-plan-step')).toHaveCount(2)
  await expect(page.getByTestId('task-plan-step').first().locator('input')).toBeChecked()
  await expect(page.getByTestId('task-tool')).toContainText('read_file')
  await expect(page.getByTestId('task-verification')).toContainText('All checks passed.')
  await expect(page.getByTestId('task-approval')).toContainText('write_file')

  harness.failNextRefresh()
  await page.getByTestId('task-refresh').click()
  await expect(page.getByTestId('task-error')).toContainText('temporary disconnect')
  await expect(page.getByTestId('task-plan-step')).toHaveCount(2)
  await expect(page.getByTestId('task-tool')).toContainText('read_file')
})
