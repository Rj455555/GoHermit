import { expect, test, type Page, type Route } from '@playwright/test'

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

type LoopDefinition = Record<string, any>
type Invocation = Record<string, any>

async function mockLoopWorkbench(page: Page, options: { providerReady?: boolean } = {}) {
  const now = new Date().toISOString()
  let providerReady = options.providerReady !== false
  let loops: LoopDefinition[] = []
  let invocations: Invocation[] = []
  const sessions = new Map<string, any>()
  let teamTemplate: Record<string, any> = {
    schema_version: 2,
    name: 'default',
    default: { company: 'deepseek', access: 'deepseek', model: 'deepseek-chat' },
    roles: { verifier: { company: 'deepseek', access: 'deepseek', model: 'deepseek-chat' } },
  }
  const employees = [
    { id: 'employee-a', name: 'Ada', job_title: 'Explorer', state: 'active', revision: 3 },
    { id: 'employee-b', name: 'Lin', job_title: 'Reviewer', state: 'active', revision: 7 },
  ]

  await page.route('**/api/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    const method = request.method()

    if (path === '/api/health') return json(route, { status: 'ok', version: '0.6.0-dev', active: false })
    if (path === '/api/info') return json(route, {
      version: '0.6.0-dev',
      workspace: '/workspace/gohermit',
      active: false,
      selection: { company: 'deepseek', access: 'deepseek', model: 'deepseek-chat', agent: 'team' },
      available_companies: [{
        id: 'deepseek', label: 'DeepSeek',
        access: [{ id: 'deepseek', label: 'API', models: [{ id: 'deepseek-chat', label: 'DeepSeek Chat' }] }],
      }],
      companies: [{
        id: 'deepseek', label: 'DeepSeek',
        access: [{ id: 'deepseek', label: 'API', auth_type: 'api_key', models: [{ id: 'deepseek-chat', label: 'DeepSeek Chat' }] }],
      }],
      agents: [{ id: 'coding', label: 'Coding Agent' }, { id: 'team', label: 'Personal Agent Team' }],
      auth_status: { deepseek: { configured: true, source: 'test', detail: 'Fake Provider' } },
      owner: { configured: false },
    })
    if (path === '/api/owner') return json(route, { schema_version: 1, identity: {}, preferences: {}, environments: [], facts: [] })
    if (path === '/api/team-template/export') return json(route, teamTemplate)
    if (path === '/api/team-template/import' && method === 'POST') {
      teamTemplate = JSON.parse(request.postData() || '{}')
      return json(route, { name: teamTemplate.name, roles: Object.keys(teamTemplate.roles || {}) })
    }
    if (path === '/api/employees' && method === 'GET') return json(route, { employees })
    if (path === '/api/sessions' && method === 'GET') {
      return json(route, {
        sessions: [...sessions.values()].map(({ session }) => ({
          id: session.id,
          title: session.title,
          status: session.status,
          updated_at: session.updated_at,
          active_run_id: session.active_run_id,
          last_run_status: session.runs.at(-1)?.status,
          selection: session.selection,
        })),
      })
    }
    const sessionMatch = path.match(/^\/api\/sessions\/([^/]+)$/)
    if (sessionMatch && method === 'GET') {
      const state = sessions.get(sessionMatch[1])
      return state ? json(route, state) : json(route, { error: 'not found' }, 404)
    }
    if (path.match(/^\/api\/sessions\/[^/]+\/approvals$/)) return json(route, { approvals: [] })
    if (path.endsWith('/events')) return route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' })

    if (path === '/api/loops' && method === 'GET') return json(route, { loops })
    if (path === '/api/loops' && method === 'POST') {
      const definition = request.postDataJSON() as LoopDefinition
      const saved = { ...definition, revision: 1, created_at: now, updated_at: now }
      loops = [...loops, saved]
      return json(route, saved, 201)
    }
    if (path === '/api/loops/import' && method === 'POST') {
      const definition = JSON.parse(request.postData() || '{}')
      const saved = { ...definition, revision: 1, created_at: now, updated_at: now }
      loops = [...loops, saved]
      return json(route, saved, 201)
    }
    const dryRunMatch = path.match(/^\/api\/loops\/([^/]+)\/dry-run$/)
    if (dryRunMatch && method === 'POST') {
      const definition = loops.find(item => item.id === dryRunMatch[1])
      if (!definition) return json(route, { error: 'not found' }, 404)
      return json(route, {
        loop_id: definition.id,
        definition_revision: definition.revision,
        definition_valid: true,
        workspace_identity: definition.workspace_identity,
        workspace_matches: true,
        git_clean: true,
        task_prompt: definition.task_source.prompt,
        agent: definition.agent_selection,
        team_template_ref: definition.team_template_ref,
        roles: [{
          role: 'verifier', ...definition.agent_selection,
          employee_id: teamTemplate.roles?.verifier?.employee_id,
          employee_revision: teamTemplate.roles?.verifier?.employee_id ? 7 : undefined,
          credential_configured: providerReady, detail: providerReady ? 'Fake Provider' : 'missing',
        }],
        write_scope: 'read-only: the loop may inspect the workspace but never modify it',
        checks: definition.verification_recipe.checks,
        budget: definition.budget,
        requires_approval: false,
        ready: providerReady,
        reasons: providerReady ? [] : ['credential missing for verifier (deepseek): missing'],
      })
    }
    const historyMatch = path.match(/^\/api\/loops\/([^/]+)\/invocations$/)
    if (historyMatch && method === 'GET') {
      return json(route, { invocations: invocations.filter(item => item.loop_id === historyMatch[1]), limit: Number(url.searchParams.get('limit') || 50) })
    }
    if (historyMatch && method === 'POST') {
      const definition = loops.find(item => item.id === historyMatch[1])
      if (!definition) return json(route, { error: 'not found' }, 404)
      if (!providerReady) return json(route, { error: 'provider unavailable' }, 400)
      const invocationID = `inv-${invocations.length + 1}`
      const sessionID = `session-loop-${invocations.length + 1}`
      const runID = `run-loop-${invocations.length + 1}`
      const invocation = {
        id: invocationID,
        loop_id: definition.id,
        definition_revision: definition.revision,
        definition_snapshot: structuredClone(definition),
        trigger: 'manual',
        task_snapshot: definition.task_source.prompt,
        session_id: sessionID,
        run_id: runID,
        status: 'attached',
        created_at: now,
        started_at: now,
      }
      invocations = [invocation, ...invocations]
      sessions.set(sessionID, {
        session: {
          id: sessionID,
          title: definition.name,
          status: 'open',
          selection: definition.agent_selection,
          active_run_id: runID,
          updated_at: now,
          runs: [{
            id: runID,
            message: definition.task_source.prompt,
            status: 'running',
            started_at: now,
            updated_at: now,
            plan_mode: 'review',
            plan_approved: true,
            verification_attempts: 1,
            plan: {
              status: 'active',
              steps: [
                { id: 'explore', title: 'Inspect canonical docs', status: 'completed', detail: 'Drift map ready' },
                { id: 'verify', title: 'Verify documentation', status: 'in_progress', detail: 'Running checks' },
              ],
            },
          }],
          tool_calls: [{ run_id: runID, name: 'read_file', summary: 'Read docs/ai/context.md', status: 'completed', time: now }],
          test_results: [{ run_id: runID, command: 'git diff --check', passed: true, summary: 'passed', time: now }],
          approval_requests: [{
            request_id: 'approval-loop-1',
            run_id: runID,
            tool: 'write_file',
            resource_paths: ['docs/ai/context.md'],
            status: 'pending',
            created_at: now,
          }],
          mission: {
            status: 'running',
            work_items: [
              { id: 'explore', role: 'explorer', title: 'Inspect docs', status: 'completed', started_at: now, completed_at: now },
              { id: 'verify', role: 'verifier', title: 'Verify docs', status: 'running', started_at: now },
            ],
          },
        },
        messages: [],
      })
      return json(route, invocation, 202)
    }
    const loopMatch = path.match(/^\/api\/loops\/([^/]+)$/)
    if (loopMatch && method === 'GET') {
      const definition = loops.find(item => item.id === loopMatch[1])
      return definition ? json(route, definition) : json(route, { error: 'not found' }, 404)
    }
    if (loopMatch && method === 'PUT') {
      const definition = request.postDataJSON() as LoopDefinition
      const previous = loops.find(item => item.id === loopMatch[1])
      if (!previous) return json(route, { error: 'not found' }, 404)
      const saved = { ...definition, revision: previous.revision + 1, created_at: previous.created_at, updated_at: now }
      loops = loops.map(item => item.id === saved.id ? saved : item)
      return json(route, saved)
    }
    const invocationMatch = path.match(/^\/api\/loop-invocations\/([^/]+)$/)
    if (invocationMatch && method === 'GET') {
      const invocation = invocations.find(item => item.id === invocationMatch[1])
      return invocation ? json(route, invocation) : json(route, { error: 'not found' }, 404)
    }
    const cancelMatch = path.match(/^\/api\/loop-invocations\/([^/]+)\/cancel$/)
    if (cancelMatch && method === 'POST') {
      const invocation = invocations.find(item => item.id === cancelMatch[1])
      if (!invocation) return json(route, { error: 'not found' }, 404)
      invocation.status = 'cancelled'
      invocation.finished_at = now
      invocation.failure_summary = 'run was cancelled'
      const state = sessions.get(invocation.session_id)
      if (state) {
        state.session.active_run_id = ''
        state.session.runs[0].status = 'cancelled'
        state.session.runs[0].completed_at = now
      }
      return json(route, invocation, 202)
    }
    return json(route, { error: `unmocked ${method} ${path}` }, 404)
  })

  return {
    setProviderReady(value: boolean) { providerReady = value },
    loops: () => loops,
    invocations: () => invocations,
    teamTemplate: () => teamTemplate,
  }
}

test('maps a Team Role to an exact active Employee and preserves the model-precedence choice', async ({ page }) => {
  const harness = await mockLoopWorkbench(page)
  await page.goto('/')
  await page.getByTestId('nav-loops').click()
  await page.getByTestId('loop-new').click()
  const explorer = page.locator('#loop-team-roles .role-preview').first()
  await explorer.getByTestId('team-role-employee').selectOption('employee-b')
  await expect.poll(() => harness.teamTemplate().roles.explorer?.employee_id).toBe('employee-b')
  expect(harness.teamTemplate().roles.explorer.model).toBe('deepseek-chat')
  await explorer.getByTestId('team-role-default-model').check()
  await expect.poll(() => harness.teamTemplate().roles.explorer?.model).toBe('')
  expect(harness.teamTemplate().roles.explorer).toMatchObject({
    employee_id: 'employee-b', company: '', access: '', model: '',
  })
  await expect(explorer).toContainText('Lin')
  await expect(explorer).toContainText('r7')
})

test('creates, persists and revises a Loop Definition with Dry Run readiness', async ({ page }) => {
  const harness = await mockLoopWorkbench(page)
  await page.goto('/')
  await page.getByTestId('nav-loops').click()
  await expect(page.getByTestId('loops-workbench')).toBeVisible()
  await page.getByTestId('loop-new').click()
  await expect(page.locator('#loop-name')).toHaveValue('文档维护 Loop')
  await page.getByTestId('loop-save').click()
  await expect(page.locator('#loop-editor-meta')).toContainText('revision 1')
  expect(harness.loops()).toHaveLength(1)

  await page.reload()
  await expect(page.getByTestId('loops-workbench')).toBeVisible()
  await expect(page.locator('#loop-editor-meta')).toContainText('revision 1')
  await page.locator('#loop-name').fill('文档维护 Loop v2')
  await page.getByTestId('loop-save').click()
  await expect(page.locator('#loop-editor-meta')).toContainText('revision 2')

  await page.locator('#loop-dry-run').click()
  await expect(page.locator('#loop-dry-run-result')).toContainText('Ready')
  await expect(page.getByTestId('loop-start')).toBeEnabled()
})

test('blocks start when provider is unavailable, then restores timeline and cancels a Fake Provider invocation', async ({ page }) => {
  const harness = await mockLoopWorkbench(page, { providerReady: false })
  await page.goto('/')
  await page.getByTestId('nav-loops').click()
  await page.getByTestId('loop-new').click()
  await page.getByTestId('loop-save').click()

  await page.locator('#loop-dry-run').click()
  await expect(page.locator('#loop-dry-run-result')).toContainText('Not ready')
  await expect(page.locator('#loop-dry-run-result')).toContainText('credential missing')
  await expect(page.getByTestId('loop-start')).toBeDisabled()

  harness.setProviderReady(true)
  await page.locator('#loop-dry-run').click()
  await expect(page.getByTestId('loop-start')).toBeEnabled()
  await page.getByTestId('loop-start').click()
  await expect(page.locator('#loop-timeline-panel')).toBeVisible()
  await expect(page.locator('#timeline-events')).toContainText('Plan · Inspect canonical docs')
  await expect(page.locator('#timeline-events')).toContainText('Verification · git diff --check')
  await expect(page.locator('#timeline-events')).toContainText('Approval · write_file')
  await expect(page.locator('#timeline-events')).toContainText('Tool · read_file')

  await page.reload()
  await expect(page.getByTestId('loops-workbench')).toBeVisible()
  await expect(page.locator('#loop-timeline-panel')).toBeVisible()
  await expect(page.locator('#timeline-events')).toContainText('Verification · git diff --check')

  await page.getByTestId('loop-cancel').click()
  await expect(page.locator('#timeline-title')).toContainText('Cancelled')
  expect(harness.invocations()[0].status).toBe('cancelled')

  await page.locator('#loop-definition-tab').click()
  await page.locator('#loop-history .history-item').first().click()
  await expect(page.locator('#timeline-title')).toContainText('Cancelled')
})

test('Dashboard, Agent and Settings navigation remain usable', async ({ page }) => {
  await mockLoopWorkbench(page)
  await page.goto('/')
  await page.getByTestId('nav-dashboard').click()
  await expect(page.getByTestId('dashboard-view')).toBeVisible()
  await expect(page.locator('#dashboard-loop-count')).toHaveText('0')

  await page.getByTestId('nav-agent').click()
  await expect(page.locator('#agent-view')).toBeVisible()
  await expect(page.locator('#task')).toBeVisible()

  await page.getByTestId('nav-settings').click()
  await expect(page.locator('#settings-drawer')).toBeVisible()
  await expect(page.locator('#settings-list')).toContainText('DeepSeek')
})
