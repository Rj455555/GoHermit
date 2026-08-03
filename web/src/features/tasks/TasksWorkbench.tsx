import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Alert,
  Badge,
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
  Modal,
  Row,
  Select,
  Segmented,
  Skeleton,
  Space,
  Switch,
  Table,
  Tag,
  Timeline,
  Tooltip,
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
  getTaskBoard,
  createTaskBoardNote,
  listApprovals,
  listEmployeeTasks,
  listEmployees,
  resumeEmployeeTask,
  startEmployeeTask,
  updateTaskBoardCard,
  updateTaskBoardSettings,
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
  TaskBoardCard,
  TaskBoardDefinition,
  TaskBoardView,
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

function boardCardInput(card: TaskBoardCard, columnId = card.column_id, rank = card.rank) {
  return {
    column_id: columnId,
    rank,
    labels: card.labels,
    priority: card.priority,
    due_at: card.due_at ?? null,
    pinned: card.pinned,
    blocked: card.blocked,
    blocker_reason: card.blocker_reason ?? '',
    depends_on: card.depends_on,
    source_url: card.source_url ?? '',
    loop_id: card.loop_id ?? '',
  }
}

function BoardCardView({ card, onOpen, onDragStart, onConvert }: { card: TaskBoardCard; onOpen: (card: TaskBoardCard) => void; onDragStart: (card: TaskBoardCard) => void; onConvert: (card: TaskBoardCard) => void }) {
  const { t } = useTranslation()
  const isTask = card.kind === 'task' && Boolean(card.task_id)
  return <div
    className={`task-board-card${card.blocked ? ' is-blocked' : ''}${card.pinned ? ' is-pinned' : ''}`}
    draggable={isTask}
    onDragStart={() => onDragStart(card)}
    onClick={() => onOpen(card)}
    onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); onOpen(card) } }}
    role={isTask ? 'link' : 'article'}
    tabIndex={0}
  >
    <Card size="small" title={<Typography.Text ellipsis={{ tooltip: card.title }}>{card.title}</Typography.Text>} extra={card.kind === 'note' ? <Tag color="default">{t('tasks.note')}</Tag> : <Tag color={statusColor(card.state ?? 'queued')}>{translatedEnum(t, 'taskStatus', card.state ?? 'queued')}</Tag>}>
      <Space direction="vertical" size={8} style={{ width: '100%' }}>
        <Space wrap size={[4, 4]}>
          {card.employee_name ? <Tag>{card.employee_name}</Tag> : null}
          {card.provider || card.model ? <Tag color="blue">{[card.provider, card.model].filter(Boolean).join(' / ')}</Tag> : null}
          {card.priority > 0 ? <Tag color="gold">P{card.priority}</Tag> : null}
          {card.blocked ? <Tag color="error">{t('tasks.blocked')}</Tag> : null}
          {card.approval_status !== 'none' ? <Tag color="warning">{t('tasks.approval')}: {card.approval_status}</Tag> : null}
        </Space>
        {card.kind === 'note' && card.body ? <Typography.Paragraph ellipsis={{ rows: 3 }} className="safe-wrap" style={{ marginBottom: 0 }}>{card.body}</Typography.Paragraph> : null}
        {card.kind === 'note' ? <Button type="link" size="small" onClick={(event) => { event.stopPropagation(); onConvert(card) }}>{t('tasks.useAsTask')}</Button> : null}
        {card.labels.length > 0 ? <Space wrap size={[4, 4]}>{card.labels.map((label) => <Tag key={label}>{label}</Tag>)}</Space> : null}
        {card.loop_id ? <Link to={`/loops/${encodeURIComponent(card.loop_id)}`} onClick={(event) => event.stopPropagation()}>{t('tasks.loop')}: {card.loop_id}</Link> : null}
        <Space split="·" size={4} wrap>
          <Typography.Text type="secondary">{card.kind === 'task' ? card.id : t('tasks.note')}</Typography.Text>
          {card.session_count > 0 ? <Typography.Text type="secondary">{t('tasks.sessions')}: {card.session_count}</Typography.Text> : null}
          {card.session_event_sequence > 0 ? <Typography.Text type="secondary">{t('tasks.events')}: {card.session_event_sequence}</Typography.Text> : null}
          {card.stale ? <Tooltip title={t('tasks.staleProjection')}><Tag color="warning">{t('tasks.stale')}</Tag></Tooltip> : null}
        </Space>
      </Space>
    </Card>
  </div>
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
    { title: t('tasks.prompt'), key: 'prompt', render: (_, task) => <Link className="task-prompt-link" to={`/tasks/${encodeURIComponent(task.id)}`}><Paragraph className="task-prompt-text" ellipsis={{ rows: 3, expandable: true }}>{task.prompt}</Paragraph></Link> },
    { title: t('tasks.employee'), dataIndex: 'employee_id', key: 'employee' },
    { title: t('tasks.state'), dataIndex: 'state', key: 'state', render: (state: string) => <Tag color={statusColor(state)}>{translatedEnum(t, 'taskStatus', state)}</Tag> },
    { title: t('tasks.project'), key: 'project', render: (_, task) => task.project_binding.label },
    { title: t('tasks.updated'), dataIndex: 'updated_at', key: 'updated' },
    { title: t('actions.actions'), key: 'action', render: (_, task) => <Button><Link to={`/tasks/${encodeURIComponent(task.id)}`}>{t('actions.open')}</Link></Button> },
  ]
  if (screens.md) return <Table className="task-list-table" rowKey="id" columns={columns} dataSource={tasks} pagination={{ pageSize: 20, showSizeChanger: false }} />
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
  const [board, setBoard] = useState<TaskBoardView | null>(null)
  const [error, setError] = useState(false)
  const [boardError, setBoardError] = useState(false)
  const [creating, setCreating] = useState(false)
  const [noteOpen, setNoteOpen] = useState(false)
  const [noteTitle, setNoteTitle] = useState('')
  const [noteBody, setNoteBody] = useState('')
  const [noteCreating, setNoteCreating] = useState(false)
  const [definitionOpen, setDefinitionOpen] = useState(false)
  const [definitionText, setDefinitionText] = useState('')
  const [definitionSaving, setDefinitionSaving] = useState(false)
  const [draggingID, setDraggingID] = useState<string | null>(null)
  const [startCandidate, setStartCandidate] = useState<{ card: TaskBoardCard; targetColumn: string } | null>(null)
  const [startBusy, setStartBusy] = useState(false)
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
      let projectedBoard: TaskBoardView | null = null
      try {
        projectedBoard = await getTaskBoard({ signal })
      } catch (caught) {
        if (caught instanceof ApiError && caught.code === 'aborted') throw caught
      }
      setEmployees(all)
      setTasks(loaded)
      setBoard(projectedBoard)
      setBoardError(projectedBoard === null)
      setEmployeeId((current) => { const active = all.filter((item) => item.state === 'active'); return active.some((item) => item.id === current) ? current : active[0]?.id ?? '' })
      setError(false)
    } catch (caught) {
      if (!(caught instanceof ApiError && caught.code === 'aborted')) {
        setError(true)
        setBoardError(true)
      }
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
    const query = params.get('q')?.trim().toLocaleLowerCase() ?? ''
    const employee = params.get('employee')
    const state = params.get('state')
    const project = params.get('project')
    const time = params.get('time')
    const windowMs = time === '24h' ? 86_400_000 : time === '7d' ? 604_800_000 : time === '30d' ? 2_592_000_000 : 0
    return (!query || task.prompt.toLocaleLowerCase().includes(query) || task.id.toLocaleLowerCase().includes(query)) && (!employee || task.employee_id === employee) && (!state || task.state === state) && (!project || task.project_binding.id === project) && (!windowMs || Date.parse(task.updated_at) >= Date.now() - windowMs)
  }), [params, tasks])

  const taskByID = useMemo(() => new Map(tasks.map((task) => [task.id, task])), [tasks])
  const viewMode = params.get('view') === 'board' ? 'board' : 'list'
  const filteredBoardCards = useMemo(() => {
    if (!board) return []
    const query = params.get('q')?.trim().toLocaleLowerCase() ?? ''
    const employee = params.get('employee')
    const state = params.get('state')
    const label = params.get('label')
    const provider = params.get('provider')
    const model = params.get('model')
    const loop = params.get('loop')
    const priority = Number(params.get('priority') ?? 0)
    const blocked = params.get('blocked')
    const owner = params.get('owner')
    const time = params.get('time')
    const windowMs = time === '24h' ? 86_400_000 : time === '7d' ? 604_800_000 : time === '30d' ? 2_592_000_000 : 0
    return board.cards.filter((card) => {
      const task = card.task_id ? taskByID.get(card.task_id) : undefined
      const matchesQuery = !query || card.title.toLocaleLowerCase().includes(query) || card.id.toLocaleLowerCase().includes(query)
      const matchesState = !state || card.state === state
      const matchesProject = !params.get('project') || task?.project_binding.id === params.get('project')
      return matchesQuery && (!employee || card.employee_id === employee) && matchesState && matchesProject && (!label || card.labels.includes(label)) && (!provider || card.provider === provider) && (!model || card.model === model) && (!loop || card.loop_id === loop) && (!priority || card.priority === priority) && (blocked === null || blocked === '' || card.blocked === (blocked === 'true')) && (owner === null || owner === '' || (owner === 'true' ? card.approval_status === 'pending' : card.approval_status !== 'pending')) && (!windowMs || Date.parse(card.authoritative_updated_at) >= Date.now() - windowMs)
    })
  }, [board, params, taskByID])

  const setFilter = (name: string, value: string) => {
    const next = new URLSearchParams(params)
    if (value) next.set(name, value)
    else next.delete(name)
    setParams(next)
  }

  async function saveBoardCard(card: TaskBoardCard, columnID: string) {
    if (!connectivity.canMutate || !board) return
    try {
      const next = await updateTaskBoardCard(card.task_id ?? card.id, boardCardInput(card, columnID, Date.now()))
      setBoard(next)
    } catch (caught) {
      actions.showToast({ messageKey: mutationKey(caught), tone: 'error' })
    }
  }

  function dropCard(card: TaskBoardCard, columnID: string) {
    setDraggingID(null)
    if (card.kind === 'task' && columnID === 'in_progress' && card.state !== 'running' && card.state !== 'verifying') {
      setStartCandidate({ card, targetColumn: columnID })
      return
    }
    void saveBoardCard(card, columnID)
  }

  async function confirmStartCandidate() {
    if (!startCandidate?.card.task_id || !connectivity.canMutate || startBusy) return
    const task = taskByID.get(startCandidate.card.task_id)
    if (!task) return
    setStartBusy(true)
    try {
      const nextTask = task.state === 'interrupted'
        ? await resumeEmployeeTask(task.id)
        : await startEmployeeTask(task.id)
      setTasks((current) => current.map((item) => item.id === nextTask.id ? nextTask : item))
      const nextBoard = await updateTaskBoardCard(task.id, boardCardInput(startCandidate.card, startCandidate.targetColumn, Date.now()))
      setBoard(nextBoard)
      setStartCandidate(null)
    } catch (caught) {
      actions.showToast({ messageKey: mutationKey(caught), tone: 'error' })
      if (caught instanceof ApiError && caught.status === 409) {
        const refreshed = await getTaskBoard().catch(() => undefined)
        if (refreshed) setBoard(refreshed)
      }
    } finally {
      setStartBusy(false)
    }
  }

  async function createNote() {
    if (!noteTitle.trim() || noteCreating || !connectivity.canMutate) return
    setNoteCreating(true)
    try {
      const next = await createTaskBoardNote({
        title: noteTitle.trim(), body: noteBody, column_id: 'backlog', rank: Date.now(), labels: [], priority: 0,
        due_at: null, pinned: false, source_url: '', blocker_reason: '',
      })
      setBoard(next)
      setNoteTitle('')
      setNoteBody('')
      setNoteOpen(false)
    } catch (caught) {
      actions.showToast({ messageKey: mutationKey(caught), tone: 'error' })
    } finally {
      setNoteCreating(false)
    }
  }

  function openDefinitionEditor() {
    if (!board) return
    setDefinitionText(JSON.stringify(board.definition, null, 2))
    setDefinitionOpen(true)
  }

  async function saveDefinition() {
    if (!board || definitionSaving || !connectivity.canMutate) return
    let definition: TaskBoardDefinition
    try {
      definition = JSON.parse(definitionText) as TaskBoardDefinition
    } catch {
      actions.showToast({ messageKey: 'tasks.invalidDefinition', tone: 'error' })
      return
    }
    setDefinitionSaving(true)
    try {
      const next = await updateTaskBoardSettings({ definition, view: board.view, filters: board.filters })
      setBoard(next)
      setDefinitionOpen(false)
    } catch (caught) {
      actions.showToast({ messageKey: mutationKey(caught), tone: 'error' })
    } finally {
      setDefinitionSaving(false)
    }
  }

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

  function useNoteAsTask(card: TaskBoardCard) {
    if (card.kind !== 'note') return
    setPrompt([card.title, card.body].filter(Boolean).join('\n\n'))
    const targetEmployee = card.employee_id && activeEmployees.some((employee) => employee.id === card.employee_id)
      ? card.employee_id
      : activeEmployees[0]?.id ?? ''
    setEmployeeId(targetEmployee)
    setProjectId('')
    setFilter('view', 'list')
    actions.showToast({ messageKey: 'tasks.notePrefilled', tone: 'info' })
  }

  if (error && tasks.length === 0) return <ErrorState title={t('tasks.loadError')} description={t('common.retryDescription')} />
  const activeEmployees = employees.filter((item) => item.state === 'active')
  const projectOptions = Array.from(new Map(tasks.map((task) => [task.project_binding.id, { id: task.project_binding.id, label: task.project_binding.label }])).values())
  const providerOptions = Array.from(new Set((board?.cards ?? []).map((card) => card.provider).filter((value): value is string => Boolean(value))))
  const modelOptions = Array.from(new Set((board?.cards ?? []).map((card) => card.model).filter((value): value is string => Boolean(value))))
  const labelOptions = Array.from(new Set((board?.cards ?? []).flatMap((card) => card.labels)))
  const boardColumns = (board?.definition.columns ?? []).filter((column) => !column.hidden || params.get('archived') === '1')
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
    <Card title={<Space wrap><span>{t('tasks.filters')}</span><Segmented aria-label={t('tasks.view')} value={viewMode} options={[{ label: t('tasks.board'), value: 'board' }, { label: t('tasks.list'), value: 'list' }]} onChange={(value) => setFilter('view', String(value))} /></Space>} extra={<Space wrap><Button disabled={!board} onClick={openDefinitionEditor}>{t('tasks.boardSettings')}</Button><Button onClick={() => setNoteOpen(true)}>{t('tasks.newNote')}</Button><Button onClick={() => setFilter('archived', params.get('archived') === '1' ? '' : '1')}>{params.get('archived') === '1' ? t('tasks.hideArchived') : t('tasks.showArchived')}</Button></Space>}>
      <Row gutter={[16, 0]}>
        <Col xs={24} sm={12} lg={8}><Form.Item label={t('tasks.search')}><Input.Search aria-label={t('tasks.search')} allowClear defaultValue={params.get('q') ?? ''} onSearch={(value) => setFilter('q', value)} /></Form.Item></Col>
        <Col xs={24} sm={12} lg={6}><Form.Item label={t('tasks.employeeFilter')}><Select aria-label={t('tasks.employeeFilter')} allowClear value={params.get('employee') || undefined} options={employees.map((employee) => ({ value: employee.id, label: employee.name }))} onChange={(value) => setFilter('employee', value ?? '')} /></Form.Item></Col>
        <Col xs={24} sm={12} lg={6}><Form.Item label={t('tasks.projectFilter')}><Select aria-label={t('tasks.projectFilter')} allowClear value={params.get('project') || undefined} options={projectOptions.map((project) => ({ value: project.id, label: project.label }))} onChange={(value) => setFilter('project', value ?? '')} /></Form.Item></Col>
        <Col xs={24} sm={12} lg={6}><Form.Item label={t('tasks.stateFilter')}><Select aria-label={t('tasks.stateFilter')} allowClear value={params.get('state') || undefined} options={TASK_STATES.map((state) => ({ value: state, label: translatedEnum(t, 'taskStatus', state) }))} onChange={(value) => setFilter('state', value ?? '')} /></Form.Item></Col>
        <Col xs={24} sm={12} lg={6}><Form.Item label={t('tasks.timeFilter')}><Select aria-label={t('tasks.timeFilter')} allowClear value={params.get('time') || undefined} options={[{ value: '24h', label: t('tasks.time24h') }, { value: '7d', label: t('tasks.time7d') }, { value: '30d', label: t('tasks.time30d') }]} onChange={(value) => setFilter('time', value ?? '')} /></Form.Item></Col>
        <Col xs={24} sm={12} lg={6}><Form.Item label={t('tasks.labelFilter')}><Select aria-label={t('tasks.labelFilter')} allowClear value={params.get('label') || undefined} options={labelOptions.map((label) => ({ value: label, label }))} onChange={(value) => setFilter('label', value ?? '')} /></Form.Item></Col>
        <Col xs={24} sm={12} lg={6}><Form.Item label={t('tasks.providerFilter')}><Select aria-label={t('tasks.providerFilter')} allowClear value={params.get('provider') || undefined} options={providerOptions.map((value) => ({ value, label: value }))} onChange={(value) => setFilter('provider', value ?? '')} /></Form.Item></Col>
        <Col xs={24} sm={12} lg={6}><Form.Item label={t('tasks.modelFilter')}><Select aria-label={t('tasks.modelFilter')} allowClear value={params.get('model') || undefined} options={modelOptions.map((value) => ({ value, label: value }))} onChange={(value) => setFilter('model', value ?? '')} /></Form.Item></Col>
        <Col xs={24} sm={12} lg={6}><Form.Item label={t('tasks.priorityFilter')}><Select aria-label={t('tasks.priorityFilter')} allowClear value={params.get('priority') || undefined} options={[1, 2, 3, 4].map((value) => ({ value: String(value), label: `P${value}` }))} onChange={(value) => setFilter('priority', value ?? '')} /></Form.Item></Col>
        <Col xs={24} sm={12} lg={6}><Form.Item label={t('tasks.blockedFilter')}><Select aria-label={t('tasks.blockedFilter')} allowClear value={params.get('blocked') || undefined} options={[{ value: 'true', label: t('common.yes') }, { value: 'false', label: t('common.no') }]} onChange={(value) => setFilter('blocked', value ?? '')} /></Form.Item></Col>
        <Col xs={24} sm={12} lg={6}><Form.Item label={t('tasks.ownerFilter')}><Select aria-label={t('tasks.ownerFilter')} allowClear value={params.get('owner') || undefined} options={[{ value: 'true', label: t('tasks.needsOwner') }, { value: 'false', label: t('tasks.noOwner') }]} onChange={(value) => setFilter('owner', value ?? '')} /></Form.Item></Col>
      </Row>
    </Card>
    {boardError ? <Alert type="warning" showIcon message={t('tasks.boardUnavailable')} description={t('common.retryDescription')} /> : null}
    {viewMode === 'list' ? <Card><TaskList tasks={filtered} /></Card> : <Card title={<Space><span>{board?.definition.name ?? t('tasks.board')}</span><Badge count={filteredBoardCards.length} showZero /></Space>}>
      <div className="task-board-scroll" data-testid="task-board">
        {boardColumns.map((column) => {
          const cards = filteredBoardCards.filter((card) => card.column_id === column.id).sort((left, right) => left.rank - right.rank || Date.parse(left.authoritative_updated_at) - Date.parse(right.authoritative_updated_at))
          return <section key={column.id} data-testid={`task-board-column-${column.id}`} className="task-board-column" onDragOver={(event) => event.preventDefault()} onDrop={(event) => { event.preventDefault(); const card = filteredBoardCards.find((item) => item.id === draggingID); if (card) dropCard(card, column.id) }}>
            <header className="task-board-column__header"><Space><span className="task-board-column__swatch" style={{ background: column.color }} /><Typography.Text strong>{column.title}</Typography.Text><Badge count={cards.length} showZero /></Space>{column.wip_limit ? <Typography.Text type="secondary">/{column.wip_limit}</Typography.Text> : null}</header>
          <div className="task-board-column__cards">{cards.map((card) => <BoardCardView key={card.id} card={card} onOpen={(item) => { if (item.task_id) void navigate(`/tasks/${encodeURIComponent(item.task_id)}`) }} onDragStart={(item) => { setDraggingID(item.id) }} onConvert={useNoteAsTask} />)}{cards.length === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('tasks.emptyColumn')} /> : null}</div>
          </section>
        })}
      </div>
    </Card>}
    <Modal open={Boolean(startCandidate)} title={t('tasks.startConfirmationTitle')} okText={t('tasks.start')} cancelText={t('actions.cancel')} confirmLoading={startBusy} onCancel={() => setStartCandidate(null)} onOk={() => void confirmStartCandidate()}>
      {startCandidate ? (() => { const task = startCandidate.card.task_id ? taskByID.get(startCandidate.card.task_id) : undefined; return task ? <Space direction="vertical" size={16} style={{ width: '100%' }}><Alert type="warning" showIcon message={t('tasks.startConfirmation')} /><Descriptions bordered column={1} size="small"><Descriptions.Item label={t('tasks.employee')}>{startCandidate.card.employee_name || task.employee_id}</Descriptions.Item><Descriptions.Item label={t('tasks.model')}>{[startCandidate.card.provider, startCandidate.card.model].filter(Boolean).join(' / ') || '—'}</Descriptions.Item><Descriptions.Item label={t('tasks.workspace')}>{task.project_binding.workspace_fingerprint}</Descriptions.Item><Descriptions.Item label={t('tasks.skills')}>{task.skills.map((skill) => `${skill.skill_id}@${skill.version}`).join(', ') || '—'}</Descriptions.Item><Descriptions.Item label={t('tasks.permissions')}>{task.policy.allowed_capabilities.join(', ') || '—'} · {task.policy.network_allowed ? t('tasks.networkAllowed') : t('tasks.networkDisabled')}</Descriptions.Item><Descriptions.Item label={t('tasks.writerLease')}>{task.project_binding.mutation_allowed ? t('tasks.writerLeaseRequired') : t('tasks.readOnlyWorkspace')}</Descriptions.Item></Descriptions></Space> : null })() : null}
    </Modal>
    <Modal open={noteOpen} title={t('tasks.newNote')} okText={t('actions.save')} cancelText={t('actions.cancel')} confirmLoading={noteCreating} onCancel={() => setNoteOpen(false)} onOk={() => void createNote()}>
      <Form layout="vertical"><Form.Item label={t('tasks.noteTitle')} required><Input value={noteTitle} onChange={(event) => setNoteTitle(event.target.value)} /></Form.Item><Form.Item label={t('tasks.noteBody')}><Input.TextArea autoSize={{ minRows: 4, maxRows: 10 }} value={noteBody} onChange={(event) => setNoteBody(event.target.value)} /></Form.Item></Form>
    </Modal>
    <Modal open={definitionOpen} title={t('tasks.boardSettings')} okText={t('actions.save')} cancelText={t('actions.cancel')} confirmLoading={definitionSaving} onCancel={() => setDefinitionOpen(false)} onOk={() => void saveDefinition()}>
      <Form layout="vertical"><Form.Item label={t('tasks.definitionJSON')} help={t('tasks.definitionHelp')}><Input.TextArea aria-label={t('tasks.definitionJSON')} autoSize={{ minRows: 12, maxRows: 28 }} value={definitionText} onChange={(event) => setDefinitionText(event.target.value)} /></Form.Item></Form>
    </Modal>
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
