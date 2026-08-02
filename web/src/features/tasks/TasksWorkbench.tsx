import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Col,
  Collapse,
  Descriptions,
  Empty,
  Flex,
  Form,
  Grid,
  Input,
  InputNumber,
  List,
  Row,
  Select,
  Skeleton,
  Space,
  Switch,
  Table,
  Tag,
  Timeline,
  Typography,
  type TableColumnsType,
} from 'antd'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import {
  cancelEmployeeTask,
  createEmployeeTask,
  decideApproval,
  getEmployee,
  getEmployeeKnowledge,
  getEmployeeMemory,
  getEmployeeSkills,
  getEmployeeTask,
  getSession,
  listApprovals,
  listEmployeeTasks,
  listEmployees,
  resumeEmployeeTask,
  startEmployeeTask,
} from '../../api/endpoints'
import { ApiError } from '../../api/errors'
import type {
  ApprovalRequest,
  EmployeeKnowledge,
  EmployeeRecord,
  EmployeeSummary,
  EmployeeTask,
  MemoryFact,
  SessionDetailResponse,
} from '../../api/types'
import { useConnectivity } from '../../components/ConnectivityProvider'
import { ErrorState } from '../../components/ErrorState'
import { PageHeader } from '../../components/PageHeader'
import { useSessionEvents } from '../../hooks/useSessionEvents'
import { translatedEnum } from '../../i18n/enumLabel'
import { useUI } from '../../state/UIContext'

const MAX_PROMPT_BYTES = 16 << 10
const TERMINAL = new Set(['completed', 'failed', 'cancelled'])
const TASK_STATES = ['queued', 'prepared', 'waiting_owner', 'running', 'verifying', 'interrupted', 'completed', 'failed', 'cancelled'] as const
const utf8Bytes = (value: string) => new TextEncoder().encode(value).byteLength
const { Paragraph, Text, Title } = Typography

function mutationKey(error: unknown) {
  if (error instanceof ApiError && error.code === 'network_error') return 'mutation.offline'
  if (error instanceof ApiError && error.status === 409) return 'mutation.conflict'
  return 'mutation.failed'
}

function statusColor(status: string) {
  if (['completed', 'approved'].includes(status)) return 'success'
  if (['failed', 'denied'].includes(status)) return 'error'
  if (['cancelled', 'interrupted'].includes(status)) return 'warning'
  if (['running', 'verifying', 'prepared', 'waiting_owner'].includes(status)) return 'processing'
  return 'default'
}

async function loadAllEmployees(signal: AbortSignal) {
  const employees: EmployeeSummary[] = []
  let cursor: string | undefined
  do {
    const page = await listEmployees({ limit: 100, ...(cursor ? { cursor } : {}) }, { signal })
    employees.push(...page.employees)
    cursor = page.next_cursor
  } while (cursor)
  return employees
}

async function loadLatestTasks(employees: EmployeeSummary[], signal: AbortSignal) {
  const tasks: EmployeeTask[] = []
  let index = 0
  const workers = Array.from({ length: Math.min(4, employees.length) }, async () => {
    while (index < employees.length) {
      const employee = employees[index++]
      if (!employee) return
      const page = await listEmployeeTasks(employee.id, { limit: 100 }, { signal })
      tasks.push(...page.tasks)
    }
  })
  await Promise.all(workers)
  return tasks
}

function TaskList({ tasks }: { tasks: EmployeeTask[] }) {
  const { t } = useTranslation()
  const screens = Grid.useBreakpoint()
  const columns: TableColumnsType<EmployeeTask> = [
    { title: t('tasks.prompt'), key: 'prompt', render: (_, task) => <Link to={`/tasks/${encodeURIComponent(task.id)}`}><Text ellipsis={{ tooltip: task.prompt }}>{task.prompt}</Text></Link> },
    { title: t('tasks.employee'), dataIndex: 'employee_id', key: 'employee' },
    { title: t('tasks.state'), dataIndex: 'state', key: 'state', render: (state: string) => <Tag color={statusColor(state)}>{translatedEnum(t, 'taskStatus', state)}</Tag> },
    { title: t('tasks.project'), key: 'project', render: (_, task) => task.project_binding.label },
    { title: t('tasks.updated'), dataIndex: 'updated_at', key: 'updated' },
    { title: t('actions.actions'), key: 'action', render: (_, task) => <Button><Link to={`/tasks/${encodeURIComponent(task.id)}`}>{t('actions.open')}</Link></Button> },
  ]
  if (screens.md) return <Table rowKey="id" columns={columns} dataSource={tasks} pagination={{ pageSize: 20, showSizeChanger: false }} scroll={{ x: 940 }} />
  return <List dataSource={tasks} locale={{ emptyText: <Empty /> }} renderItem={(task) => <List.Item><Card className="mobile-resource-card" title={<Link to={`/tasks/${encodeURIComponent(task.id)}`}>{task.prompt}</Link>} extra={<Tag color={statusColor(task.state)}>{translatedEnum(t, 'taskStatus', task.state)}</Tag>}><Descriptions column={1} size="small"><Descriptions.Item label={t('tasks.employee')}>{task.employee_id}</Descriptions.Item><Descriptions.Item label={t('tasks.project')}>{task.project_binding.label}</Descriptions.Item><Descriptions.Item label={t('tasks.updated')}>{task.updated_at}</Descriptions.Item><Descriptions.Item label="Session / Run"><Space direction="vertical" size={0}><Text copyable ellipsis={{ tooltip: task.session_id }}>{task.session_id ?? '—'}</Text><Text copyable ellipsis={{ tooltip: task.run_id }}>{task.run_id ?? '—'}</Text></Space></Descriptions.Item></Descriptions><Button block type="primary"><Link to={`/tasks/${encodeURIComponent(task.id)}`}>{t('actions.open')}</Link></Button></Card></List.Item>} />
}

export function TasksWorkbenchPage() {
  const { t } = useTranslation()
  const { actions } = useUI()
  const navigate = useNavigate()
  const connectivity = useConnectivity()
  const screens = Grid.useBreakpoint()
  const [params, setParams] = useSearchParams()
  const contextEpoch = useRef(0)
  const [employees, setEmployees] = useState<EmployeeSummary[]>([])
  const [tasks, setTasks] = useState<EmployeeTask[]>([])
  const [error, setError] = useState(false)
  const [creating, setCreating] = useState(false)
  const [employeeId, setEmployeeId] = useState('')
  const [context, setContext] = useState<{ record: EmployeeRecord; knowledge: EmployeeKnowledge; memory: MemoryFact[]; skills: Awaited<ReturnType<typeof getEmployeeSkills>> } | null>(null)
  const [prompt, setPrompt] = useState('')
  const [projectId, setProjectId] = useState('')
  const [skillKeys, setSkillKeys] = useState<string[]>([])
  const [citationIds, setCitationIds] = useState<string[]>([])
  const [memoryIds, setMemoryIds] = useState<string[]>([])
  const [capabilities, setCapabilities] = useState('read')
  const [network, setNetwork] = useState(false)
  const [budget, setBudget] = useState({ max_model_calls: 4, max_tokens: 4000, timeout_seconds: 600 })

  const load = useCallback(async (signal: AbortSignal) => {
    try {
      const all = await loadAllEmployees(signal)
      const loaded = await loadLatestTasks(all, signal)
      setEmployees(all)
      setTasks(loaded)
      setEmployeeId((current) => { const active = all.filter((item) => item.state === 'active'); return active.some((item) => item.id === current) ? current : active[0]?.id ?? '' })
      setError(false)
    } catch (caught) {
      if (!(caught instanceof ApiError && caught.code === 'aborted')) setError(true)
    }
  }, [])

  useEffect(() => { const controller = new AbortController(); void load(controller.signal); return () => controller.abort() }, [load, connectivity.generation])
  useEffect(() => {
    if (!employeeId) { setContext(null); return }
    const controller = new AbortController()
    const epoch = ++contextEpoch.current
    setContext(null)
    setProjectId('')
    setSkillKeys([])
    setCitationIds([])
    setMemoryIds([])
    void Promise.all([getEmployee(employeeId, { signal: controller.signal }), getEmployeeSkills(employeeId, { signal: controller.signal }), getEmployeeKnowledge(employeeId, { signal: controller.signal }), getEmployeeMemory(employeeId, { signal: controller.signal })])
      .then(([record, skills, knowledge, memory]) => {
        if (epoch !== contextEpoch.current) return
        setContext({ record, skills, knowledge, memory: memory.facts })
        setCapabilities(record.employee.permission_policy.allowed_capabilities.join('\n'))
        setNetwork(record.employee.permission_policy.network_allowed)
        setBudget(record.employee.budget_policy)
      }).catch(() => undefined)
    return () => { contextEpoch.current += 1; controller.abort() }
  }, [employeeId])

  const filtered = useMemo(() => tasks.filter((task) => {
    const employee = params.get('employee')
    const state = params.get('state')
    const project = params.get('project')
    const time = params.get('time')
    const windowMs = time === '24h' ? 86_400_000 : time === '7d' ? 604_800_000 : time === '30d' ? 2_592_000_000 : 0
    return (!employee || task.employee_id === employee) && (!state || task.state === state) && (!project || task.project_binding.id === project) && (!windowMs || Date.parse(task.updated_at) >= Date.now() - windowMs)
  }), [params, tasks])

  const promptBytes = utf8Bytes(prompt)
  const availableSkills = context?.skills.bindings.filter((item) => item.status === 'current' && item.binding.enabled) ?? []
  const selectedSkills = availableSkills.map((item) => item.binding).filter((binding) => skillKeys.includes(`${binding.skill_id}\0${binding.version}\0${binding.digest}`))
  const knowledgeInput = context?.knowledge.sources.flatMap((source) => {
    const citations = context.knowledge.indexes.filter((index) => index.source_id === source.id).flatMap((index) => index.documents).flatMap((document) => document.citations).filter((citation) => citationIds.includes(citation.id)).map((citation) => citation.id)
    return citations.length ? [{ source_id: source.id, citation_ids: citations }] : []
  }) ?? []

  async function create() {
    if (!context || !projectId || !prompt.trim() || promptBytes > MAX_PROMPT_BYTES || creating) return
    const epoch = contextEpoch.current
    const owner = employeeId
    setCreating(true)
    try {
      const task = await createEmployeeTask(employeeId, {
        prompt,
        skills: selectedSkills.map((binding) => ({ skill_id: binding.skill_id, version: binding.version })),
        knowledge: knowledgeInput,
        memory_fact_ids: memoryIds,
        project_binding_id: projectId,
        policy: { allowed_capabilities: capabilities.split(/\r?\n|,/u).map((item) => item.trim()).filter(Boolean), network_allowed: network, budget },
      })
      if (epoch !== contextEpoch.current || owner !== employeeId) return
      setPrompt('')
      await navigate(`/tasks/${encodeURIComponent(task.id)}`)
    } catch (caught) {
      if (epoch === contextEpoch.current && owner === employeeId) actions.showToast({ messageKey: mutationKey(caught), tone: 'error' })
    } finally {
      if (epoch === contextEpoch.current && owner === employeeId) setCreating(false)
    }
  }

  if (error && tasks.length === 0) return <ErrorState title={t('tasks.loadError')} description={t('common.retryDescription')} />
  const activeEmployees = employees.filter((item) => item.state === 'active')
  const projectOptions = Array.from(new Map(tasks.map((task) => [task.project_binding.id, { id: task.project_binding.id, label: task.project_binding.label }])).values())
  const setFilter = (name: 'employee' | 'project' | 'state' | 'time', value: string) => { const next = new URLSearchParams(params); if (value) next.set(name, value); else next.delete(name); setParams(next) }
  const citations = context?.knowledge.indexes.flatMap((index) => index.documents).flatMap((document) => document.citations) ?? []
  return <article className="feature-page antd-deep-page tasks-workbench-page">
    <PageHeader title={t('pages.tasks.title')} description={t('tasks.description')} />
    <Alert type="info" showIcon message={t('tasks.listBoundary')} />
    <Card title={<Title level={2}>{t('tasks.create')}</Title>}>
      <Form layout="vertical">
        <Row gutter={[16, 0]}>
          <Col xs={24} md={8}><Form.Item label={t('tasks.employee')} required><Select<string> aria-label={t('tasks.employee')} {...(employeeId ? { value: employeeId } : {})} options={activeEmployees.map((employee) => ({ value: employee.id, label: employee.name }))} onChange={setEmployeeId} /></Form.Item></Col>
          <Col xs={24} md={8}><Form.Item label={t('tasks.project')} required><Select<string> aria-label={t('tasks.project')} {...(projectId ? { value: projectId } : {})} placeholder={t('common.select')} options={context?.record.project_bindings.map((project) => ({ value: project.id, label: project.label })) ?? []} onChange={setProjectId} /></Form.Item></Col>
          <Col xs={24}><Form.Item label={t('tasks.prompt')} required {...(promptBytes > MAX_PROMPT_BYTES ? { validateStatus: 'error' as const } : {})} help={<Text data-testid="task-prompt-bytes" type={promptBytes > MAX_PROMPT_BYTES ? 'danger' : 'secondary'}>{promptBytes} / {MAX_PROMPT_BYTES}</Text>}><Input.TextArea aria-label={t('tasks.prompt')} autoSize={{ minRows: 4, maxRows: 12 }} value={prompt} onChange={(event) => setPrompt(event.target.value)} /></Form.Item></Col>
        </Row>
        <Collapse items={[
          { key: 'skills', label: t('tasks.exactSkills'), children: <Checkbox.Group value={skillKeys} onChange={setSkillKeys}><Space direction="vertical">{availableSkills.map(({ binding }) => { const key = `${binding.skill_id}\0${binding.version}\0${binding.digest}`; return <Checkbox key={key} value={key}>{binding.skill_id}@{binding.version} · <Text copyable ellipsis={{ tooltip: binding.digest }}>{binding.digest}</Text></Checkbox> })}</Space></Checkbox.Group> },
          { key: 'knowledge', label: t('tasks.knowledgeCitations'), children: <Checkbox.Group value={citationIds} onChange={setCitationIds}><Space direction="vertical">{citations.map((citation) => <Checkbox key={citation.id} value={citation.id}>{citation.path}:{citation.start_line}-{citation.end_line} · <Text copyable>{citation.digest}</Text></Checkbox>)}</Space></Checkbox.Group> },
          { key: 'memory', label: t('tasks.acceptedMemory'), children: <Checkbox.Group value={memoryIds} onChange={setMemoryIds}><Space direction="vertical">{context?.memory.map((fact) => <Checkbox key={fact.id} value={fact.id}>{fact.category}: {fact.value}</Checkbox>)}</Space></Checkbox.Group> },
          { key: 'policy', label: t('tasks.capabilities'), children: <Row gutter={[16, 0]}><Col xs={24}><Form.Item label={t('tasks.capabilities')}><Input.TextArea aria-label={t('tasks.capabilities')} value={capabilities} onChange={(event) => setCapabilities(event.target.value)} /></Form.Item></Col><Col xs={24}><Form.Item label={t('tasks.networkAllowed')}><Switch aria-label={t('tasks.networkAllowed')} checked={network} onChange={setNetwork} /></Form.Item></Col><Col xs={24} md={8}><Form.Item label={t('tasks.maxCalls')}><InputNumber aria-label={t('tasks.maxCalls')} min={1} inputMode="numeric" value={budget.max_model_calls} onChange={(value) => setBudget({ ...budget, max_model_calls: value ?? 1 })} /></Form.Item></Col><Col xs={24} md={8}><Form.Item label={t('tasks.maxTokens')}><InputNumber aria-label={t('tasks.maxTokens')} min={1} inputMode="numeric" value={budget.max_tokens} onChange={(value) => setBudget({ ...budget, max_tokens: value ?? 1 })} /></Form.Item></Col><Col xs={24} md={8}><Form.Item label={t('tasks.timeoutSeconds')}><InputNumber aria-label={t('tasks.timeoutSeconds')} min={1} inputMode="numeric" value={budget.timeout_seconds} onChange={(value) => setBudget({ ...budget, timeout_seconds: value ?? 1 })} /></Form.Item></Col></Row> },
        ]} />
        <Button className="task-create-action" block={!screens.md} type="primary" loading={creating} disabled={!connectivity.canMutate || !prompt.trim() || promptBytes > MAX_PROMPT_BYTES || !projectId} onClick={() => void create()}>{t('tasks.createQueued')}</Button>
      </Form>
    </Card>
    <Card title={t('tasks.filters')}>
      <Row gutter={[16, 0]}>
        <Col xs={24} sm={12} lg={6}><Form.Item label={t('tasks.employeeFilter')}><Select aria-label={t('tasks.employeeFilter')} allowClear value={params.get('employee') || undefined} options={employees.map((employee) => ({ value: employee.id, label: employee.name }))} onChange={(value) => setFilter('employee', value ?? '')} /></Form.Item></Col>
        <Col xs={24} sm={12} lg={6}><Form.Item label={t('tasks.projectFilter')}><Select aria-label={t('tasks.projectFilter')} allowClear value={params.get('project') || undefined} options={projectOptions.map((project) => ({ value: project.id, label: project.label }))} onChange={(value) => setFilter('project', value ?? '')} /></Form.Item></Col>
        <Col xs={24} sm={12} lg={6}><Form.Item label={t('tasks.stateFilter')}><Select aria-label={t('tasks.stateFilter')} allowClear value={params.get('state') || undefined} options={TASK_STATES.map((state) => ({ value: state, label: translatedEnum(t, 'taskStatus', state) }))} onChange={(value) => setFilter('state', value ?? '')} /></Form.Item></Col>
        <Col xs={24} sm={12} lg={6}><Form.Item label={t('tasks.timeFilter')}><Select aria-label={t('tasks.timeFilter')} allowClear value={params.get('time') || undefined} options={[{ value: '24h', label: t('tasks.time24h') }, { value: '7d', label: t('tasks.time7d') }, { value: '30d', label: t('tasks.time30d') }]} onChange={(value) => setFilter('time', value ?? '')} /></Form.Item></Col>
      </Row>
    </Card>
    <Card><TaskList tasks={filtered} /></Card>
  </article>
}

export function TaskWorkbenchDetailPage() {
  const { taskId } = useParams()
  const { t } = useTranslation()
  const { actions } = useUI()
  const connectivity = useConnectivity()
  const screens = Grid.useBreakpoint()
  const epochRef = useRef(0)
  const [task, setTask] = useState<EmployeeTask | null>(null)
  const [session, setSession] = useState<SessionDetailResponse | null>(null)
  const [approvals, setApprovals] = useState<ApprovalRequest[]>([])
  const [prepared, setPrepared] = useState(false)
  const [busy, setBusy] = useState(false)
  const [notFound, setNotFound] = useState(false)

  const refresh = useCallback(async (signal?: AbortSignal) => {
    if (!taskId) return
    const epoch = ++epochRef.current
    try {
      const next = await getEmployeeTask(taskId, signal ? { signal } : {})
      if (epoch !== epochRef.current) return
      setTask(next)
      setNotFound(false)
      if (next.session_id) {
        const [projection, approvalProjection] = await Promise.all([getSession(next.session_id, signal ? { signal } : {}), listApprovals(next.session_id, signal ? { signal } : {})])
        if (epoch !== epochRef.current) return
        setSession(projection)
        setApprovals(approvalProjection.approvals)
      } else { setSession(null); setApprovals([]) }
    } catch (caught) {
      if (epoch === epochRef.current) setNotFound(caught instanceof ApiError && caught.status === 404)
    }
  }, [taskId])

  useEffect(() => { const controller = new AbortController(); setTask(null); setSession(null); setApprovals([]); setPrepared(false); void refresh(controller.signal); return () => { epochRef.current += 1; controller.abort() } }, [refresh, connectivity.generation])
  const events = useSessionEvents({ sessionId: task?.session_id, frontier: session?.session.next_event_sequence ?? 0, runId: task?.run_id, onRefresh: () => { void refresh() } })

  async function mutate(action: 'start' | 'cancel' | 'resume') {
    if (!task || !connectivity.canMutate || busy) return
    const epoch = epochRef.current
    const owner = task.id
    setBusy(true)
    try {
      const next = action === 'start' ? await startEmployeeTask(task.id) : action === 'cancel' ? await cancelEmployeeTask(task.id) : await resumeEmployeeTask(task.id)
      if (epoch !== epochRef.current) return
      setTask(next)
      setPrepared(false)
      await refresh()
    } catch (caught) {
      if (epoch === epochRef.current) { actions.showToast({ messageKey: mutationKey(caught), tone: 'error' }); if (caught instanceof ApiError && caught.status === 409) await refresh() }
    } finally { if (owner === taskId) setBusy(false) }
  }

  async function prepare() {
    if (!task || !connectivity.canMutate || busy) return
    const epoch = epochRef.current
    setBusy(true)
    try { const next = await getEmployeeTask(task.id); if (epoch === epochRef.current) { setTask(next); setPrepared(true) } }
    catch (caught) { if (epoch === epochRef.current) actions.showToast({ messageKey: mutationKey(caught), tone: 'error' }) }
    finally { if (epoch === epochRef.current) setBusy(false) }
  }

  function confirmCancel() { actions.openDialog({ titleKey: 'tasks.cancelTitle', descriptionKey: 'tasks.cancelDescription', confirmKey: 'tasks.cancel', tone: 'warning', onConfirm: () => void mutate('cancel') }) }
  async function approval(requestId: string, decision: 'approve' | 'deny') {
    if (!task?.session_id || !connectivity.canMutate || busy) return
    const epoch = epochRef.current
    const owner = task.id
    setBusy(true)
    try { await decideApproval(task.session_id, requestId, decision); if (epoch === epochRef.current) await refresh() }
    catch (caught) { if (epoch === epochRef.current) actions.showToast({ messageKey: mutationKey(caught), tone: 'error' }) }
    finally { if (owner === taskId) setBusy(false) }
  }
  function confirmApproval(request: ApprovalRequest, decision: 'approve' | 'deny') { actions.openDialog({ titleKey: decision === 'approve' ? 'approval.approve' : 'approval.deny', descriptionKey: 'loops.requireApproval', confirmKey: decision === 'approve' ? 'approval.approve' : 'approval.deny', tone: decision === 'approve' ? 'info' : 'error', onConfirm: () => { void approval(request.request_id, decision) } }) }

  if (notFound) return <ErrorState title={t('tasks.notFound')} description={t('tasks.notFoundDescription')} />
  if (!task) return <Skeleton active paragraph={{ rows: 10 }} />
  const activeRun = session?.session.runs.find((run) => run.id === task.run_id)
  const tools = session?.session.tool_calls.filter((tool) => !task.run_id || tool.run_id === task.run_id) ?? []
  const verification = session?.session.test_results.filter((result) => !task.run_id || result.run_id === task.run_id) ?? []
  const canMutate = connectivity.canMutate && !busy
  const actionsBar = <Space direction={screens.md ? 'horizontal' : 'vertical'} className="task-detail-actions">
    {task.state === 'queued' && !task.run_id && !prepared ? <Button block={!screens.md} loading={busy} disabled={!canMutate} onClick={() => void prepare()}>{t('tasks.prepare')}</Button> : null}
    {task.state === 'queued' && !task.run_id && prepared ? <Button block={!screens.md} type="primary" loading={busy} disabled={!canMutate} onClick={() => void mutate('start')}>{t('tasks.start')}</Button> : null}
    {task.state === 'interrupted' ? <Button block={!screens.md} type="primary" loading={busy} disabled={!canMutate} onClick={() => void mutate('resume')}>{t('tasks.resume')}</Button> : null}
    {!TERMINAL.has(task.state) && task.state !== 'interrupted' ? <Button block={!screens.md} danger loading={busy} disabled={!canMutate} onClick={confirmCancel}>{t('tasks.cancel')}</Button> : null}
  </Space>
  return <article className="feature-page antd-deep-page task-detail-page">
    <PageHeader title={task.prompt} description={`${task.id} · ${task.employee_id}`} />
    <Card className="task-summary-card" title={<Space wrap><Title level={2}>{t('tasks.context')}</Title><Tag data-testid="task-status" color={statusColor(task.state)}>{translatedEnum(t, 'taskStatus', task.state)}</Tag></Space>}>
      {!connectivity.canMutate ? <Alert type="warning" showIcon message={t('mutation.offline')} /> : null}
      {prepared ? <Alert type="info" showIcon message={t('tasks.preparedAuthority')} /> : null}
      <Descriptions column={{ xs: 1, sm: 2, lg: 3 }} bordered size="small"><Descriptions.Item label={t('tasks.employeeRevision')}>{task.employee_revision}</Descriptions.Item><Descriptions.Item label={t('tasks.employeeSnapshot')}><Text copyable ellipsis={{ tooltip: task.employee_snapshot.digest }}>r{task.employee_snapshot.revision} · {task.employee_snapshot.digest}</Text></Descriptions.Item><Descriptions.Item label={t('tasks.project')}>{task.project_binding.label} · <Text copyable>{task.project_binding.workspace_fingerprint}</Text></Descriptions.Item><Descriptions.Item label={t('tasks.skills')}>{task.skills.map((item) => `${item.skill_id}@${item.version}`).join('; ') || '—'}</Descriptions.Item><Descriptions.Item label={t('tasks.session')}><Text copyable>{task.session_id ?? '—'}</Text></Descriptions.Item><Descriptions.Item label={t('tasks.run')}><Text copyable>{task.run_id ?? '—'}</Text></Descriptions.Item></Descriptions>
      {screens.md ? actionsBar : null}
    </Card>
    <Card data-testid="task-timeline" title={<Title level={2}>{t('tasks.activity')}</Title>}>
      {events.status === 'fatal' ? <Alert type="error" showIcon message={t('session.reconnectEvents')} action={<Button onClick={events.reconnect}>{t('session.reconnectEvents')}</Button>} /> : null}
      {events.status === 'reconnecting' ? <Alert type="warning" showIcon message={t('session.reconnecting')} /> : null}
      {events.truncated ? <Alert type="warning" showIcon message={t('session.streamingTruncated')} /> : null}
      <Timeline items={events.events.map((event) => ({ children: <Space direction="vertical" size={0}><Text>{translatedEnum(t, 'runtimeEventType', event.type)}</Text><Text type="secondary">{event.time} · #{event.sequence}</Text></Space> }))} />
    </Card>
    <Card title={<Title level={2}>{t('session.plan')}</Title>}>{activeRun?.plan ? <List dataSource={activeRun.plan.steps} renderItem={(step) => <List.Item><Flex gap={12} align="center"><Checkbox checked={step.status === 'completed'} disabled /><Text>{step.title}</Text><Tag color={statusColor(step.status)}>{translatedEnum(t, 'planStatus', step.status)}</Tag></Flex></List.Item>} /> : <Empty />}</Card>
    <Card title={<Title level={2}>{t('tasks.tools')}</Title>}><List dataSource={tools} locale={{ emptyText: <Empty /> }} renderItem={(tool) => <List.Item><Card className="mobile-resource-card" title={tool.name} extra={<Tag color={statusColor(tool.status || 'unknown')}>{translatedEnum(t, 'toolStatus', tool.status || 'unknown')}</Tag>}><Paragraph className="safe-wrap">{tool.summary}</Paragraph></Card></List.Item>} /></Card>
    <Card title={<Title level={2}>{t('session.approvals')}</Title>}><List dataSource={approvals} locale={{ emptyText: <Empty /> }} renderItem={(request) => <List.Item><Card className="mobile-resource-card" title={request.tool} extra={<Tag color={statusColor(request.status)}>{translatedEnum(t, 'approvalStatus', request.status)}</Tag>}><Paragraph>{request.args_summary}</Paragraph><Paragraph type="secondary" className="safe-wrap">{request.resource_paths.join(', ')}</Paragraph>{request.status === 'pending' ? <Space direction={screens.md ? 'horizontal' : 'vertical'}><Button block={!screens.md} type="primary" disabled={!canMutate} onClick={() => confirmApproval(request, 'approve')}>{t('approval.approve')}</Button><Button block={!screens.md} danger disabled={!canMutate} onClick={() => confirmApproval(request, 'deny')}>{t('approval.deny')}</Button></Space> : null}</Card></List.Item>} /></Card>
    <Card title={<Title level={2}>{t('session.verification')}</Title>}><List dataSource={verification} locale={{ emptyText: <Empty /> }} renderItem={(result) => <List.Item><Alert type={result.passed ? 'success' : 'error'} showIcon message={result.command} description={result.summary} /></List.Item>} /></Card>
    <Card title={<Title level={2}>{t('tasks.artifacts')}</Title>}><List dataSource={task.artifacts} locale={{ emptyText: <Empty /> }} renderItem={(artifact) => <List.Item><Descriptions column={1} size="small"><Descriptions.Item label={t('tasks.project')}><Text copyable className="safe-wrap">{artifact.path}</Text></Descriptions.Item><Descriptions.Item label="Digest"><Text copyable ellipsis={{ tooltip: artifact.digest }}>{artifact.digest}</Text></Descriptions.Item><Descriptions.Item label={t('session.verification')}>{artifact.verified_at}</Descriptions.Item></Descriptions></List.Item>} /></Card>
    {!screens.md ? <div className="antd-mobile-action-bar task-sticky-action-bar">{actionsBar}</div> : null}
  </article>
}
