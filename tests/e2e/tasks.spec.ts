import { expect, test, type Page, type Route } from '@playwright/test'

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

async function mockTasks(page: Page) {
  const now = new Date().toISOString()
  let starts = 0
  let resumes = 0
  let cancels = 0
  const approvalDecisions: string[] = []
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
  }, {
    id: 'task-interrupted', employee_id: employee.id, employee_revision: 3,
    prompt: 'Resume interrupted release.', state: 'interrupted', created_at: now, updated_at: now,
    snapshot_digest: 'd'.repeat(64), session_id: 'session-interrupted', run_id: 'run-interrupted',
    skills: [], knowledge: [], memory_facts: [], artifacts: [],
    project_binding: { id: 'project-main', label: 'GoHermit', workspace_fingerprint: 'f'.repeat(64), read_allowed: true, mutation_allowed: true, network_allowed: false, allowed_tool_capabilities: ['read'] },
    policy: { allowed_capabilities: ['read'], network_allowed: false, budget: { max_model_calls: 4, max_tokens: 4000, timeout_seconds: 600 } },
  }]
  const session = {
    id: 'session-existing', title: 'Review release notes.', status: 'closed',
    selection: { company: 'deepseek', access: 'deepseek', model: 'deepseek-chat', agent: 'coding' },
    tool_calls: [{ run_id: 'run-existing', call_id: 'call-1', name: 'read_file', args_digest: 'b'.repeat(64), status: 'completed', turn: 1, started_at: now, completed_at: now }],
    test_results: [{ command: 'go test ./...', passed: true, summary: 'All checks passed.' }],
    runs: [{
      id: 'run-existing', message: 'Review release notes.', status: 'completed', plan_mode: 'auto', created_at: now, updated_at: now,
      plan: { revision: 2, steps: [{ id: 'inspect', description: 'Inspect changes', status: 'completed' }, { id: 'verify', description: 'Run checks', status: 'completed' }] },
    }],
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
    if (path === '/api/employee-tasks/task-interrupted/resume' && method === 'POST') {
      resumes += 1
      const task = tasks.find(item => item.id === 'task-interrupted')
      Object.assign(task, { state: 'running', updated_at: now })
      return json(route, task)
    }
    const cancelMatch = path.match(/^\/api\/employee-tasks\/([^/]+)\/cancel$/)
    if (cancelMatch && method === 'POST') {
      cancels += 1
      const task = tasks.find(item => item.id === cancelMatch[1])
      Object.assign(task, { state: 'cancelled', updated_at: now })
      return json(route, task)
    }
    if (path === '/api/sessions/session-existing' && method === 'GET') return json(route, { session, messages: [] })
    if (path === '/api/sessions/session-new' && method === 'GET') return json(route, { session: { ...session, id: 'session-new', status: 'open', active_run_id: 'run-new', runs: [{ ...session.runs[0], id: 'run-new', status: 'running' }] }, messages: [] })
    if (path === '/api/sessions/session-interrupted' && method === 'GET') return json(route, { session: {
      ...session, id: 'session-interrupted', status: 'open', active_run_id: 'run-interrupted',
      tool_calls: [], test_results: [{command: 'go test ./...', passed: false, summary: 'Focused verification failed.'}],
      runs: [{ ...session.runs[0], id: 'run-interrupted', status: 'interrupted' }],
    }, messages: [] })
    if (path === '/api/sessions/session-existing/approvals') return json(route, { approvals: [
      { request_id: 'approval-1', session_id: 'session-existing', run_id: 'run-existing', tool: 'write_file', resource_paths: ['CHANGELOG.md'], args_summary: 'Update release notes', status: 'pending', created_at: now, expires_at: new Date(Date.now() + 60000).toISOString() },
      { request_id: 'approval-2', session_id: 'session-existing', run_id: 'run-existing', tool: 'shell', resource_paths: ['.'], args_summary: 'Run release command', status: 'pending', created_at: now, expires_at: new Date(Date.now() + 60000).toISOString() },
    ] })
    if (/^\/api\/sessions\/session-existing\/approvals\/approval-[12]\/decide$/.test(path) && method === 'POST') {
      approvalDecisions.push((request.postDataJSON() as any).decision)
      return json(route, { status: 'decided' })
    }
    if (path.endsWith('/approvals')) return json(route, { approvals: [] })
    if (path.endsWith('/events')) return route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' })
    return json(route, { error: `unmocked ${method} ${path}` }, 404)
  })

  return {
    starts: () => starts,
    resumes: () => resumes,
    cancels: () => cancels,
    approvalDecisions: () => approvalDecisions,
    failNextRefresh: () => { failRefresh = true },
  }
}

test('tasks require explicit Start and render the existing Session execution truth', async ({ page }) => {
  const harness = await mockTasks(page)
  await page.goto('/')
  await page.getByTestId('nav-employee-tasks').click()

  await expect(page.getByTestId('task-row')).toHaveCount(2)
  await expect(page.locator('#task-project-filter')).toContainText('GoHermit')
  await page.locator('#task-state-filter').selectOption('completed')
  await expect(page.getByTestId('task-row')).toHaveCount(1)
  await page.locator('#task-state-filter').selectOption('')
  await page.getByTestId('task-new').click()
  await page.getByTestId('task-prompt').fill('Prepare the release checklist.')
  await page.getByTestId('task-create').click()
  await expect(page.getByTestId('task-row')).toHaveCount(3)
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
  await expect(page.getByTestId('task-approval')).toHaveCount(2)
  await page.getByTestId('task-approval').filter({ hasText: 'write_file' }).getByRole('button', { name: 'Approve' }).click()
  await page.getByTestId('task-approval').filter({ hasText: 'shell' }).getByRole('button', { name: 'Deny' }).click()
  expect(harness.approvalDecisions()).toEqual(['approve', 'deny'])

  harness.failNextRefresh()
  await page.getByTestId('task-refresh').click()
  await expect(page.getByTestId('task-error')).toContainText(/temporary disconnect|Live updates disconnected/)
  await expect(page.getByTestId('task-plan-step')).toHaveCount(2)
  await expect(page.getByTestId('task-tool')).toContainText('read_file')

  await page.getByTestId('task-row').filter({ hasText: 'Resume interrupted release.' }).click()
  await expect(page.getByTestId('task-verification')).toContainText('Focused verification failed.')
  await page.getByTestId('task-resume').click()
  expect(harness.resumes()).toBe(1)
  await page.getByTestId('task-cancel').click()
  expect(harness.cancels()).toBe(1)
})

test('task Session SSE resumes by sequence, suppresses duplicates, isolates Tasks, and closes on navigation', async ({ page }) => {
  await page.addInitScript(() => {
    const instances: any[] = []
    class ControlledEventSource {
      static CONNECTING = 0
      static OPEN = 1
      static CLOSED = 2
      url: string
      readyState = ControlledEventSource.OPEN
      closed = false
      listeners = new Map<string, Array<(event: any) => void>>()
      onerror: null | (() => void) = null
      constructor(url: string) {
        this.url = String(url)
        instances.push(this)
      }
      addEventListener(type: string, listener: (event: any) => void) {
        this.listeners.set(type, [...(this.listeners.get(type) || []), listener])
      }
      emit(type: string, sequence: number, data: Record<string, unknown>) {
        for (const listener of this.listeners.get(type) || []) {
          listener({ data: JSON.stringify({...data, sequence}), lastEventId: String(sequence) })
        }
      }
      fail() { this.readyState = ControlledEventSource.CLOSED; this.onerror?.() }
      close() { this.closed = true; this.readyState = ControlledEventSource.CLOSED }
    }
    ;(window as any).EventSource = ControlledEventSource
    ;(window as any).__taskSSE = { instances }
  })
  await mockTasks(page)
  await page.goto('/')
  await page.getByTestId('nav-employee-tasks').click()
  await page.getByTestId('task-row').filter({ hasText: 'Review release notes.' }).click()

  await expect.poll(() => page.evaluate(() => (window as any).__taskSSE.instances.length)).toBe(1)
  const firstURL = await page.evaluate(() => (window as any).__taskSSE.instances[0].url)
  expect(firstURL).toContain('/api/sessions/session-existing/events?after=0')
  await page.evaluate(() => {
    const source = (window as any).__taskSSE.instances[0]
    source.emit('plan_updated', 4, {run_id: 'run-existing', message: 'plan four'})
    source.emit('plan_updated', 4, {run_id: 'run-existing', message: 'duplicate'})
    source.emit('tool_completed', 5, {run_id: 'other-run', message: 'wrong run'})
    source.emit('tool_completed', 6, {run_id: 'run-existing', message: 'tool six'})
  })
  await expect(page.locator('#task-events .timeline-event')).toHaveCount(2)
  await expect(page.locator('#task-events')).not.toContainText('duplicate')
  await expect(page.locator('#task-events')).not.toContainText('wrong run')
  await expect.poll(() => page.evaluate(() => (window as any).__taskSSE.instances.length)).toBe(1)

  await page.getByTestId('task-row').filter({ hasText: 'Resume interrupted release.' }).click()
  await expect.poll(() => page.evaluate(() => (window as any).__taskSSE.instances.length)).toBe(2)
  await expect(page.locator('#task-events .timeline-event')).toHaveCount(0)
  await page.getByTestId('task-row').filter({ hasText: 'Review release notes.' }).click()
  await expect.poll(() => page.evaluate(() => (window as any).__taskSSE.instances.at(-1).url)).toContain('after=6')
  await expect(page.locator('#task-events .timeline-event')).toHaveCount(2)

  await page.evaluate(() => (window as any).__taskSSE.instances.at(-1).fail())
  await page.getByTestId('task-refresh').click()
  await expect.poll(() => page.evaluate(() => (window as any).__taskSSE.instances.at(-1).url)).toContain('after=6')

  await page.getByTestId('nav-employees').click()
  await expect.poll(() => page.evaluate(() => (window as any).__taskSSE.instances.at(-1).closed)).toBe(true)
})

test('native EventSource receives non-empty Session history after the saved sequence', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('gohermit.task-sse.task-existing.session-existing', '9'))
  await mockTasks(page)
  let requestedAfter = ''
  await page.route('**/api/sessions/session-existing/events?after=*', async route => {
    requestedAfter = new URL(route.request().url()).searchParams.get('after') || ''
    return route.fulfill({
      status: 200,
      contentType: 'text/event-stream',
      headers: {'Cache-Control': 'no-cache'},
      body: 'id: 10\nevent: tool_completed\ndata: {"sequence":10,"run_id":"run-existing","message":"history ten"}\n\n',
    })
  })
  await page.goto('/')
  await page.getByTestId('nav-employee-tasks').click()
  await page.getByTestId('task-row').filter({ hasText: 'Review release notes.' }).click()
  await expect(page.locator('#task-events')).toContainText('history ten')
  expect(requestedAfter).toBe('9')
  await page.getByTestId('nav-employees').click()
})
