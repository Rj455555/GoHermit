import { expect, test, type Page, type Route } from '@playwright/test'
import { AgentPage } from './pages/agent-page'

const now = new Date().toISOString()
const expiresAt = new Date(Date.now() + 10 * 60 * 1000).toISOString()

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

const pendingApproval = {
  request_id: 'apr-session-1-0',
  session_id: 'session-1',
  run_id: 'run-1',
  tool: 'run_command',
  resource_paths: ['internal/web/assets/app.js', 'package.json'],
  args_summary: 'pnpm test（运行前端测试）',
  plan_revision: 1,
  created_at: now,
  expires_at: expiresAt,
  status: 'pending',
}

const session = {
  id: 'session-1',
  title: '审批面板测试',
  status: 'open',
  selection: { company: 'deepseek', access: 'deepseek', model: 'deepseek-chat', agent: 'team' },
  runs: [{ id: 'run-1', message: '审批面板测试', status: 'running', plan_mode: 'auto' }],
  active_run_id: 'run-1',
}

async function mockHarness(page: Page, approvals: unknown[]) {
  let capturedDecision: any = null

  await page.route('**/api/**', async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (path === '/api/health') return json(route, { status: 'ok', version: '0.5.0-dev', active: false })
    if (path === '/api/info') return json(route, {
      version: '0.5.0-dev', workspace: '/workspace', active: true,
      selection: { company: 'deepseek', access: 'deepseek', model: 'deepseek-chat', agent: 'team' },
      available_companies: [{ id: 'deepseek', label: 'DeepSeek', access: [{ id: 'deepseek', label: 'API', models: [{ id: 'deepseek-chat', label: 'DeepSeek Chat' }] }] }],
      companies: [], agents: [{ id: 'team', label: 'Personal Agent Team' }], auth_status: {}, owner: { configured: false },
    })
    if (path === '/api/sessions' && request.method() === 'GET') {
      return json(route, { sessions: [{ id: session.id, title: session.title, updated_at: now, active_run_id: session.active_run_id, last_run_status: 'running', selection: session.selection }] })
    }
    if (path === '/api/sessions/session-1' && request.method() === 'GET') return json(route, { session, messages: [] })
    if (path === '/api/sessions/session-1/approvals' && request.method() === 'GET') return json(route, { approvals })
    if (path === '/api/sessions/session-1/approvals/apr-session-1-0/decide' && request.method() === 'POST') {
      capturedDecision = request.postDataJSON()
      return json(route, { request: { ...pendingApproval, status: capturedDecision.decision === 'approve' ? 'approved' : 'denied' } })
    }
    if (path.endsWith('/events')) return route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' })
    return json(route, { error: `unmocked ${request.method()} ${path}` }, 404)
  })

  await page.addInitScript(id => localStorage.setItem('gohermit.session', id), session.id)
  return { getCapturedDecision: () => capturedDecision }
}

test('pending approval renders the panel with tool, paths, summary and countdown', async ({ page }) => {
  await mockHarness(page, [pendingApproval])
  const agent = new AgentPage(page)
  await agent.goto()

  await expect(agent.approvalPanel).toBeVisible()
  await expect(agent.approvalCards).toHaveCount(1)
  const card = agent.approvalCards.first()
  await expect(card).toContainText('run_command')
  await expect(card).toContainText('internal/web/assets/app.js')
  await expect(card).toContainText('package.json')
  await expect(card).toContainText('pnpm test（运行前端测试）')
  await expect(card.locator('.approval-countdown')).toContainText('剩余')
  await expect(card.locator('.approval-approve')).toBeEnabled()
  await expect(card.locator('.approval-deny')).toBeEnabled()
})

test('approve click calls decide with the correct body and clears the panel', async ({ page }) => {
  const harness = await mockHarness(page, [pendingApproval])
  const agent = new AgentPage(page)
  await agent.goto()

  await expect(agent.approvalCards).toHaveCount(1)
  await agent.approvalCards.first().locator('.approval-approve').click()

  await expect(agent.approvalPanel).toBeHidden()
  await expect(agent.approvalCards).toHaveCount(0)
  expect(harness.getCapturedDecision()).toEqual({ decision: 'approve' })
})

test('no pending approvals renders no panel', async ({ page }) => {
  await mockHarness(page, [])
  const agent = new AgentPage(page)
  await agent.goto()

  await expect(agent.approvalPanel).toBeHidden()
  await expect(agent.approvalCards).toHaveCount(0)
})
