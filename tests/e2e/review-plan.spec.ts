import { expect, test, type Page, type Route } from '@playwright/test'
import { AgentPage } from './pages/agent-page'

const now = new Date().toISOString()

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

async function mockHarness(page: Page) {
  let session: any = null
  let capturedCreate: any = null

  await page.route('**/api/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    if (path === '/api/health') return json(route, { status: 'ok', version: '0.5.0-dev', active: false })
    if (path === '/api/info') return json(route, {
      version: '0.5.0-dev', workspace: '/workspace', active: false,
      selection: { company: 'deepseek', access: 'deepseek', model: 'deepseek-chat', agent: 'team' },
      available_companies: [{ id: 'deepseek', label: 'DeepSeek', access: [{ id: 'deepseek', label: 'API', models: [{ id: 'deepseek-chat', label: 'DeepSeek Chat' }] }] }],
      companies: [], agents: [{ id: 'team', label: 'Personal Agent Team' }], auth_status: {}, owner: { configured: false },
    })
    if (path === '/api/sessions' && request.method() === 'GET') {
      return json(route, { sessions: session ? [{ id: session.id, title: session.title, updated_at: now, active_run_id: session.active_run_id, last_run_status: session.runs.at(-1)?.status, selection: session.selection }] : [] })
    }
    if (path === '/api/sessions' && request.method() === 'POST') {
      capturedCreate = request.postDataJSON()
      session = { id: 'session-1', title: 'New conversation', status: 'open', plan_mode: capturedCreate.plan_mode, selection: capturedCreate, runs: [], active_run_id: '' }
      return json(route, session, 201)
    }
    if (path === '/api/sessions/session-1' && request.method() === 'GET') return json(route, { session, messages: [] })
    if (path === '/api/sessions/session-1/runs' && request.method() === 'POST') {
      const message = request.postDataJSON().message
      session.title = message
      session.active_run_id = 'run-1'
      session.runs = [{
        id: 'run-1', message, status: 'queued', plan_mode: 'review', plan_approved: false,
        plan: { schema_version: 1, id: 'plan-run-1', status: 'active', revision: 1, allow_parallel: true, steps: [
          { id: 'explore', title: `分析「${message}」的目标与约束`, status: 'pending', updated_at: now },
          { id: 'verify', title: `独立验证「${message}」`, status: 'pending', updated_at: now },
        ] },
      }]
      return json(route, { session_id: session.id, run_id: 'run-1' }, 202)
    }
    if (path.endsWith('/events')) return route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' })
    if (path.endsWith('/approve') && request.method() === 'POST') {
      session.runs[0].plan_approved = true
      session.runs[0].plan_approved_at = now
      session.runs[0].status = 'running'
      return json(route, { session_id: session.id, run_id: 'run-1' }, 202)
    }
    return json(route, { error: `unmocked ${request.method()} ${path}` }, 404)
  })

  return { getCapturedCreate: () => capturedCreate }
}

test('review-first plan survives refresh and starts only after approval', async ({ page }) => {
  const harness = await mockHarness(page)
  const agent = new AgentPage(page)
  await agent.goto()
  await agent.startReviewTask('修复 Codex 登录流式输出')

  await expect(agent.approvePlan).toBeVisible()
  await expect(agent.planSteps).toHaveCount(2)
  await expect(agent.planSteps.first()).toContainText('Codex 登录')
  expect(harness.getCapturedCreate()).toMatchObject({ company: 'deepseek', access: 'deepseek', model: 'deepseek-chat', agent: 'team', plan_mode: 'review' })

  await page.reload()
  await expect(agent.approvePlan).toBeVisible()
  await expect(agent.planSteps.first()).toContainText('Codex 登录')

  await agent.approvePlan.click()
  await expect(agent.approvePlan).toBeHidden()
  await expect(page.locator('#cancel-run')).toBeVisible()
})
