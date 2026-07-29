import { createReadStream, mkdtempSync, rmSync, statSync } from 'node:fs'
import { createServer } from 'node:http'
import { extname, resolve } from 'node:path'
import { tmpdir } from 'node:os'
import { spawnSync } from 'node:child_process'

const host = '127.0.0.1'
const port = 4174
const root = resolve('internal/web/assets/dist')
const dialogPrefix = '/__test__/dialog'
const dialogRoot = mkdtempSync(resolve(tmpdir(), 'gohermit-dialog-e2e-'))
const types = {
  '.css': 'text/css; charset=utf-8',
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
}

const dialogBuild = spawnSync(
  'pnpm',
  [
    '--filter',
    '@gohermit/web',
    'exec',
    'vite',
    'build',
    '--config',
    resolve('tests/e2e-react/vite.dialog.config.mjs'),
  ],
  {
    cwd: resolve('.'),
    env: {
      ...process.env,
      NODE_ENV: 'production',
      GOHERMIT_DIALOG_HARNESS_OUT: dialogRoot,
    },
    stdio: 'inherit',
  },
)
if (dialogBuild.status !== 0) {
  rmSync(dialogRoot, { recursive: true, force: true })
  process.exit(dialogBuild.status ?? 1)
}

const now = '2026-07-29T08:00:00Z'
const selection = {
  company: 'openai',
  access: 'openai-codex',
  model: 'gpt-5.6',
  agent: 'coding',
}
const access = {
  id: 'openai-codex',
  label: 'Codex',
  auth_type: 'oauth_external',
  description: 'Browser login',
  supported: true,
  models: [{ id: 'gpt-5.6', label: 'GPT-5.6', provider: 'openai-codex' }],
}
const apiAccess = {
  id: 'openai-api',
  label: 'OpenAI API',
  auth_type: 'api_key',
  description: 'API key',
  api_key_env: 'OPENAI_API_KEY',
  supported: true,
  models: [{ id: 'gpt-5.6-api', label: 'GPT-5.6 API', provider: 'openai-api' }],
}
const company = { id: 'openai', label: 'OpenAI', access: [access, apiAccess] }
const info = {
  version: '0.3.0-phase3',
  workspace: '/test/workspace',
  model: {
    provider: 'openai-codex',
    protocol: 'responses',
    base_url: 'https://api.openai.com',
    model: 'gpt-5.6',
    api_key_env: '',
    api_key_configured: true,
  },
  selection,
  companies: [company],
  available_companies: [company],
  agents: [{
    id: 'coding',
    label: 'Coding',
    description: 'Coding agent',
    read_only: false,
    tool_policy: 'workspace',
  }],
  auth_status: {
    'openai-codex': { configured: true, source: 'test', detail: 'ready' },
    'openai-api': { configured: true, source: 'test', detail: 'ready' },
  },
  active: false,
  owner: { configured: true, display_name: 'Phase 3 Owner' },
}
let codexConfigured = true
let loginPolls = 0
let loginSession = null
let owner = {
  schema_version: 1,
  identity: { display_name: 'Phase 3 Owner', timezone: 'Asia/Shanghai', language: 'zh-CN' },
  preferences: { communication: '', coding: '', git: '', verification: '', risk: '' },
  environments: [],
  facts: [],
  updated_at: now,
}
let sessionCounter = 1
const sessions = new Map()
const eventJournals = new Map()

function makeSession(id, title = 'Phase 3 Session') {
  return {
    schema_version: 1,
    id,
    title,
    goal: '',
    status: 'open',
    selection,
    plan_mode: 'auto',
    created_at: now,
    updated_at: now,
    turns: 0,
    runs: [],
    next_event_sequence: 0,
    summary: '',
    tool_calls: [],
    modified_files: {},
    completed_steps: [],
    pending_steps: [],
    test_results: [],
    workspace: '/test/workspace',
    config_digest: 'fixture',
    workspace_changed: false,
    approval_requests: [{
      request_id: 'approval-1',
      session_id: id,
      run_id: 'run-approval',
      tool: 'write_file',
      resource_paths: ['safe/file.go'],
      args_summary: 'Write one workspace file',
      args_digest: 'fixture-digest',
      policy_fingerprint: 'fixture-policy',
      plan_revision: 1,
      created_at: now,
      expires_at: '2099-07-29T08:00:00Z',
      status: 'pending',
    }],
    messages: [],
  }
}
sessions.set('session-1', makeSession('session-1'))
eventJournals.set('session-1', [])

const loops = [{
  id: 'daily-review',
  name: 'Daily review',
  enabled: true,
  created_at: now,
  updated_at: now,
  revision: 1,
}]
const invocations = [{
  id: 'invocation-1',
  loop_id: 'daily-review',
  definition_revision: 1,
  trigger: 'manual',
  task_snapshot: 'Review',
  session_id: 'session-1',
  status: 'completed',
  created_at: now,
  started_at: now,
  finished_at: now,
}]
const streams = new Map()
const stats = {
  activeSSE: 0,
  maxSSE: 0,
  urls: [],
  runStarts: 0,
  approvalDecisions: 0,
  loginPolls: 0,
  lastEventIDs: [],
}

function sessionSummary(session) {
  const lastRun = session.runs.at(-1)
  return {
    id: session.id,
    title: session.title,
    status: session.status,
    updated_at: session.updated_at,
    active_run_id: session.active_run_id,
    last_run_status: lastRun?.status,
    selection: session.selection,
  }
}

function json(response, status, value) {
  const body = JSON.stringify(value)
  response.writeHead(status, {
    'Content-Type': 'application/json; charset=utf-8',
    'Content-Length': Buffer.byteLength(body),
  })
  response.end(body)
}

async function body(request) {
  const chunks = []
  let length = 0
  for await (const chunk of request) {
    length += chunk.length
    if (length > (1 << 20)) throw new Error('body too large')
    chunks.push(chunk)
  }
  return JSON.parse(Buffer.concat(chunks).toString('utf8') || '{}')
}

function encodeEvent(type, event) {
  const idLine = event.sequence > 0 ? `id: ${event.sequence}\n` : ''
  return `${idLine}event: ${type}\ndata: ${JSON.stringify(event)}\n\n`
}

function emit(sessionId, type, event, persist = true) {
  const value = { type, time: now, session_id: sessionId, ...event }
  if (persist) {
    const journal = eventJournals.get(sessionId) ?? []
    journal.push(value)
    eventJournals.set(sessionId, journal)
  }
  const payload = encodeEvent(type, value)
  for (const response of streams.get(sessionId) ?? []) response.write(payload)
}

function openEvents(request, response, sessionId, url) {
  const rawAfter = url.searchParams.get('after') ?? '0'
  const rawLastEventID = request.headers['last-event-id'] ?? '0'
  if (
    !/^(0|[1-9][0-9]*)$/u.test(rawAfter) ||
    typeof rawLastEventID !== 'string' ||
    !/^(0|[1-9][0-9]*)$/u.test(rawLastEventID)
  ) throw new Error('invalid event cursor')
  const after = Number(rawAfter)
  const lastEventID = Number(rawLastEventID)
  if (!Number.isSafeInteger(after) || !Number.isSafeInteger(lastEventID)) {
    throw new Error('invalid event cursor')
  }
  const cursor = Math.max(after, lastEventID)
  response.writeHead(200, {
    'Content-Type': 'text/event-stream',
    'Cache-Control': 'no-cache',
    Connection: 'keep-alive',
  })
  response.write(': connected\n\n')
  for (const event of eventJournals.get(sessionId) ?? []) {
    if (event.sequence > cursor) response.write(encodeEvent(event.type, event))
  }
  let group = streams.get(sessionId)
  if (!group) {
    group = new Set()
    streams.set(sessionId, group)
  }
  group.add(response)
  stats.activeSSE += 1
  stats.maxSSE = Math.max(stats.maxSSE, stats.activeSSE)
  stats.urls.push(url.pathname + url.search)
  stats.lastEventIDs.push(rawLastEventID)
  request.once('close', () => {
    if (!group.delete(response)) return
    stats.activeSSE -= 1
    if (group.size === 0) streams.delete(sessionId)
  })
}

async function handleApi(request, response, url) {
  const { pathname } = url
  if (request.method === 'GET' && pathname === '/api/health') {
    json(response, 200, { status: 'ok', version: info.version, active: false })
    return true
  }
  if (request.method === 'GET' && pathname === '/api/info') {
    const codexStatus = {
      configured: codexConfigured,
      source: codexConfigured ? 'test' : '',
      detail: codexConfigured ? 'ready' : 'login required',
    }
    json(response, 200, {
      ...info,
      available_companies: [{
        ...company,
        access: codexConfigured ? company.access : [apiAccess],
      }],
      auth_status: { ...info.auth_status, 'openai-codex': codexStatus },
    })
    return true
  }
  if (pathname === '/api/owner' && request.method === 'GET') {
    json(response, 200, owner)
    return true
  }
  if (pathname === '/api/owner' && request.method === 'PUT') {
    owner = { ...await body(request), updated_at: now }
    json(response, 200, owner)
    return true
  }
  const factMatch = pathname.match(/^\/api\/owner\/facts\/([^/]+)$/u)
  if (request.method === 'PUT' && factMatch) {
    const input = await body(request)
    if (
      typeof input.category !== 'string' ||
      typeof input.value !== 'string' ||
      typeof input.source !== 'string' ||
      typeof input.confirmed !== 'boolean' ||
      Object.keys(input).some((key) =>
        !['category', 'value', 'source', 'confirmed'].includes(key))
    ) throw new Error('invalid fact')
    const factId = decodeURIComponent(factMatch[1])
    const fact = {
      id: factId,
      category: input.category,
      value: input.value,
      source: input.source,
      confirmed: input.confirmed,
      created_at: now,
      updated_at: now,
    }
    owner = {
      ...owner,
      facts: [...owner.facts.filter((item) => item.id !== factId), fact],
      updated_at: now,
    }
    json(response, 200, owner)
    return true
  }
  if (request.method === 'DELETE' && factMatch) {
    const factId = decodeURIComponent(factMatch[1])
    owner = {
      ...owner,
      facts: owner.facts.filter((item) => item.id !== factId),
      updated_at: now,
    }
    json(response, 200, owner)
    return true
  }
  if (request.method === 'GET' && pathname === '/api/loops') {
    json(response, 200, { loops })
    return true
  }
  const apiKeyMatch = pathname.match(/^\/api\/settings\/providers\/([^/]+)\/api-key$/u)
  if (request.method === 'PUT' && apiKeyMatch) {
    const input = await body(request)
    if (typeof input.api_key !== 'string' || input.api_key.trim() === '') throw new Error('invalid key')
    json(response, 200, { configured: true, provider: decodeURIComponent(apiKeyMatch[1]) })
    return true
  }
  const credentialMatch = pathname.match(/^\/api\/settings\/providers\/([^/]+)\/credentials$/u)
  if (request.method === 'DELETE' && credentialMatch) {
    json(response, 200, { configured: false, provider: decodeURIComponent(credentialMatch[1]) })
    return true
  }
  if (request.method === 'POST' && pathname === '/api/settings/providers/openai-codex/login') {
    loginSession = {
      id: 'login-1',
      status: 'pending',
      user_code: 'ABCD-EFGH',
      verification_url: 'https://example.test/device',
      expires_at: '2099-07-29T08:00:00Z',
    }
    loginPolls = 0
    json(response, 201, loginSession)
    return true
  }
  if (request.method === 'GET' && pathname === '/api/settings/logins/login-1') {
    if (!loginSession) {
      json(response, 404, { code: 'not_found' })
      return true
    }
    loginPolls += 1
    stats.loginPolls = loginPolls
    codexConfigured = true
    loginSession = { ...loginSession, status: 'approved' }
    json(response, 200, loginSession)
    return true
  }
  if (request.method === 'GET' && pathname === '/api/loops/daily-review/invocations') {
    json(response, 200, { invocations, limit: 50 })
    return true
  }
  if (request.method === 'GET' && pathname === '/api/sessions') {
    json(response, 200, { sessions: [...sessions.values()].map(sessionSummary) })
    return true
  }
  if (request.method === 'POST' && pathname === '/api/sessions') {
    const input = await body(request)
    if (
      typeof input.title !== 'string' ||
      typeof input.company !== 'string' ||
      typeof input.access !== 'string' ||
      typeof input.model !== 'string' ||
      typeof input.agent !== 'string' ||
      !['auto', 'review'].includes(input.plan_mode) ||
      Object.keys(input).some((key) =>
        !['title', 'company', 'access', 'model', 'agent', 'plan_mode'].includes(key))
    ) throw new Error('invalid Session')
    sessionCounter += 1
    const session = makeSession(`session-${sessionCounter}`, input.title || `Session ${sessionCounter}`)
    session.selection = {
      company: input.company,
      access: input.access,
      model: input.model,
      agent: input.agent,
    }
    session.plan_mode = input.plan_mode
    sessions.set(session.id, session)
    eventJournals.set(session.id, [])
    json(response, 201, session)
    return true
  }
  const eventsMatch = pathname.match(/^\/api\/sessions\/([^/]+)\/events$/u)
  if (request.method === 'GET' && eventsMatch) {
    openEvents(request, response, decodeURIComponent(eventsMatch[1]), url)
    return true
  }
  const approvalMatch = pathname.match(/^\/api\/sessions\/([^/]+)\/approvals$/u)
  if (request.method === 'GET' && approvalMatch) {
    const session = sessions.get(decodeURIComponent(approvalMatch[1]))
    json(response, 200, {
      approvals: (session?.approval_requests ?? []).filter((approval) => approval.status === 'pending'),
    })
    return true
  }
  const decisionMatch = pathname.match(
    /^\/api\/sessions\/([^/]+)\/approvals\/([^/]+)\/decide$/u,
  )
  if (request.method === 'POST' && decisionMatch) {
    const session = sessions.get(decodeURIComponent(decisionMatch[1]))
    const approval = session?.approval_requests.find(
      (item) => item.request_id === decodeURIComponent(decisionMatch[2]),
    )
    const input = await body(request)
    if (!approval || approval.status !== 'pending') {
      json(response, 409, { code: 'conflict' })
      return true
    }
    if (!['approve', 'deny'].includes(input.decision) || Object.keys(input).length !== 1) {
      throw new Error('invalid decision')
    }
    approval.status = input.decision === 'approve' ? 'approved' : 'denied'
    stats.approvalDecisions += 1
    json(response, 200, {
      request: approval,
      event: { type: 'approval_decided', sequence: 1 },
    })
    return true
  }
  const runMatch = pathname.match(/^\/api\/sessions\/([^/]+)\/runs$/u)
  if (request.method === 'POST' && runMatch) {
    const session = sessions.get(decodeURIComponent(runMatch[1]))
    if (!session) {
      json(response, 404, { code: 'not_found' })
      return true
    }
    const input = await body(request)
    if (
      typeof input.message !== 'string' ||
      input.message.trim() === '' ||
      Object.keys(input).length !== 1
    ) throw new Error('invalid Run')
    const runId = `run-${session.runs.length + 1}`
    const run = {
      id: runId,
      message: input.message,
      status: 'running',
      started_at: now,
      updated_at: now,
      start_turn: 1,
      plan_mode: session.plan_mode,
      plan_approved: false,
    }
    session.runs.push(run)
    session.active_run_id = runId
    session.status = 'running'
    stats.runStarts += 1
    json(response, 201, { session_id: session.id, run_id: runId })
    setTimeout(() => {
      session.next_event_sequence = 1
      emit(session.id, 'model_started', { run_id: runId, turn: 1, sequence: 1 })
    }, 50)
    setTimeout(() => emit(session.id, 'model_delta', {
      run_id: runId,
      turn: 1,
      sequence: 0,
      message: 'streamed ',
    }, false), 150)
    setTimeout(() => emit(session.id, 'model_delta', {
      run_id: runId,
      turn: 1,
      sequence: 0,
      message: 'answer',
    }, false), 300)
    setTimeout(() => {
      session.next_event_sequence = 2
      run.status = 'completed'
      run.updated_at = now
      run.completed_at = now
      session.status = 'completed'
      session.messages.push({
        id: `message-${session.messages.length + 1}`,
        run_id: runId,
        role: 'user',
        content: input.message,
        created_at: now,
      }, {
        id: `message-${session.messages.length + 2}`,
        run_id: runId,
        role: 'assistant',
        content: 'streamed answer',
        created_at: now,
      })
      emit(session.id, 'model_completed', { run_id: runId, turn: 1, sequence: 2 })
    }, 900)
    return true
  }
  const runActionMatch = pathname.match(
    /^\/api\/sessions\/([^/]+)\/runs\/([^/]+)\/(cancel|resume|approve)$/u,
  )
  if (request.method === 'POST' && runActionMatch) {
    const session = sessions.get(decodeURIComponent(runActionMatch[1]))
    const run = session?.runs.find((item) => item.id === decodeURIComponent(runActionMatch[2]))
    const input = await body(request)
    if (Object.keys(input).length !== 0) throw new Error('invalid Run action')
    if (!session || !run) {
      json(response, 404, { code: 'not_found' })
      return true
    }
    const action = runActionMatch[3]
    if (action === 'cancel') {
      run.status = 'cancelled'
      run.updated_at = now
      run.completed_at = now
      session.status = 'cancelled'
      delete session.active_run_id
      session.next_event_sequence += 1
      emit(session.id, 'task_cancelled', {
        run_id: run.id,
        turn: run.start_turn,
        sequence: session.next_event_sequence,
      })
      json(response, 200, { cancelled: true, status: 'cancelled' })
      return true
    }
    if (action === 'resume') {
      run.status = 'running'
      run.updated_at = now
      session.status = 'running'
      session.active_run_id = run.id
      json(response, 200, { session_id: session.id, run_id: run.id })
      return true
    }
    run.plan_approved = true
    run.plan_approved_at = now
    run.updated_at = now
    json(response, 200, { session_id: session.id, run_id: run.id })
    return true
  }
  const sessionMatch = pathname.match(/^\/api\/sessions\/([^/]+)$/u)
  if (request.method === 'GET' && sessionMatch) {
    const session = sessions.get(decodeURIComponent(sessionMatch[1]))
    if (!session) {
      json(response, 404, { code: 'not_found' })
      return true
    }
    const { messages, ...projection } = session
    json(response, 200, { session: projection, messages })
    return true
  }
  json(response, 404, { code: 'not_found' })
  return true
}

function existingFile(fileRoot, pathname) {
  const candidate = resolve(fileRoot, `.${pathname}`)
  if (!candidate.startsWith(`${fileRoot}/`)) return null
  try {
    return statSync(candidate).isFile() ? candidate : null
  } catch {
    return null
  }
}

const server = createServer(async (request, response) => {
  try {
    const url = new URL(request.url || '/', `http://${host}`)
    const { pathname } = url
    if (pathname === '/__test__/state') {
      json(response, 200, stats)
      return
    }
    if (pathname === '/__test__/codex-unconfigured' && request.method === 'POST') {
      codexConfigured = false
      loginSession = null
      json(response, 200, { configured: false })
      return
    }
    const disconnectMatch = pathname.match(/^\/__test__\/disconnect\/([^/]+)$/u)
    if (request.method === 'POST' && disconnectMatch) {
      const sessionId = decodeURIComponent(disconnectMatch[1])
      for (const stream of [...(streams.get(sessionId) ?? [])]) stream.end()
      json(response, 200, { disconnected: sessionId })
      return
    }
    if (pathname === '/__test__/reset' && request.method === 'POST') {
      sessions.clear()
      sessions.set('session-1', makeSession('session-1'))
      eventJournals.clear()
      eventJournals.set('session-1', [])
      sessionCounter = 1
      codexConfigured = true
      loginSession = null
      loginPolls = 0
      stats.maxSSE = stats.activeSSE
      stats.urls = []
      stats.runStarts = 0
      stats.approvalDecisions = 0
      stats.loginPolls = 0
      stats.lastEventIDs = []
      json(response, 200, stats)
      return
    }
    if (pathname === '/api' || pathname.startsWith('/api/')) {
      await handleApi(request, response, url)
      return
    }
    if (pathname === dialogPrefix || pathname === `${dialogPrefix}/`) {
      response.setHeader('Content-Type', types['.html'])
      createReadStream(resolve(dialogRoot, 'dialog-harness.html')).pipe(response)
      return
    }
    if (pathname.startsWith(`${dialogPrefix}/`)) {
      const dialogAsset = existingFile(dialogRoot, pathname.slice(dialogPrefix.length))
      if (dialogAsset) {
        response.setHeader('Content-Type', types[extname(dialogAsset)] || 'application/octet-stream')
        createReadStream(dialogAsset).pipe(response)
        return
      }
      response.writeHead(404).end('not found')
      return
    }
    const asset = existingFile(root, pathname)
    if (asset) {
      response.setHeader('Content-Type', types[extname(asset)] || 'application/octet-stream')
      createReadStream(asset).pipe(response)
      return
    }
    if (extname(pathname)) {
      response.writeHead(404).end('not found')
      return
    }
    response.setHeader('Content-Type', types['.html'])
    createReadStream(resolve(root, 'index.html')).pipe(response)
  } catch {
    json(response, 400, { code: 'bad_request' })
  }
})

function close() {
  for (const group of streams.values()) {
    for (const response of group) response.end()
  }
  server.close(() => {
    rmSync(dialogRoot, { recursive: true, force: true })
    process.exit(0)
  })
}

process.once('SIGINT', close)
process.once('SIGTERM', close)
process.once('exit', () => rmSync(dialogRoot, { recursive: true, force: true }))
server.listen(port, host)
