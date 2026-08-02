import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Button, Card } from 'antd'
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
const utf8Bytes = (value: string) => new TextEncoder().encode(value).byteLength

function mutationKey(error: unknown) {
  if (error instanceof ApiError && error.code === 'network_error') return 'mutation.offline'
  if (error instanceof ApiError && error.status === 409) return 'mutation.conflict'
  return 'mutation.failed'
}

async function loadAllEmployees(signal: AbortSignal) {
  const employees: EmployeeSummary[] = []
  let cursor: string | undefined
  do {
    const page = await listEmployees({
      limit: 100,
      ...(cursor ? { cursor } : {}),
    }, { signal })
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

export function TasksWorkbenchPage() {
  const { t } = useTranslation()
  const { actions } = useUI()
  const navigate = useNavigate()
  const connectivity = useConnectivity()
  const [params, setParams] = useSearchParams()
  const contextEpoch = useRef(0)
  const [employees, setEmployees] = useState<EmployeeSummary[]>([])
  const [tasks, setTasks] = useState<EmployeeTask[]>([])
  const [error, setError] = useState(false)
  const [creating, setCreating] = useState(false)
  const [employeeId, setEmployeeId] = useState('')
  const [context, setContext] = useState<{
    record: EmployeeRecord
    knowledge: EmployeeKnowledge
    memory: MemoryFact[]
    skills: Awaited<ReturnType<typeof getEmployeeSkills>>
  } | null>(null)
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
      setEmployeeId((current) => {
        const active = all.filter((item) => item.state === 'active')
        return active.some((item) => item.id === current) ? current : active[0]?.id ?? ''
      })
      setError(false)
    } catch (caught) {
      if (!(caught instanceof ApiError && caught.code === 'aborted')) setError(true)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load, connectivity.generation])

  useEffect(() => {
    if (!employeeId) {
      setContext(null)
      return
    }
    const controller = new AbortController()
    const epoch = ++contextEpoch.current
    setContext(null)
    setProjectId('')
    setSkillKeys([])
    setCitationIds([])
    setMemoryIds([])
    Promise.all([
      getEmployee(employeeId, { signal: controller.signal }),
      getEmployeeSkills(employeeId, { signal: controller.signal }),
      getEmployeeKnowledge(employeeId, { signal: controller.signal }),
      getEmployeeMemory(employeeId, { signal: controller.signal }),
    ]).then(([record, skills, knowledge, memory]) => {
      if (epoch !== contextEpoch.current) return
      setContext({ record, skills, knowledge, memory: memory.facts })
      setCapabilities(record.employee.permission_policy.allowed_capabilities.join('\n'))
      setNetwork(record.employee.permission_policy.network_allowed)
      setBudget(record.employee.budget_policy)
    }).catch(() => undefined)
    return () => {
      contextEpoch.current += 1
      controller.abort()
    }
  }, [employeeId])

  const filtered = useMemo(() => tasks.filter((task) => {
    const employee = params.get('employee')
    const state = params.get('state')
    const project = params.get('project')
    const time = params.get('time')
    const timeWindow = time === '24h' ? 24 * 60 * 60 * 1000
      : time === '7d' ? 7 * 24 * 60 * 60 * 1000
        : time === '30d' ? 30 * 24 * 60 * 60 * 1000
          : 0
    return (!employee || task.employee_id === employee)
      && (!state || task.state === state)
      && (!project || task.project_binding.id === project)
      && (!timeWindow || Date.parse(task.updated_at) >= Date.now() - timeWindow)
  }), [params, tasks])

  const promptBytes = utf8Bytes(prompt)
  const availableSkills = context?.skills.bindings.filter((item) =>
    item.status === 'current' && item.binding.enabled) ?? []
  const selectedSkills = availableSkills.map((item) => item.binding).filter((binding) =>
    skillKeys.includes(`${binding.skill_id}\0${binding.version}`))
  const knowledgeInput = context?.knowledge.sources.flatMap((source) => {
    const citations = context.knowledge.indexes
      .filter((index) => index.source_id === source.id)
      .flatMap((index) => index.documents)
      .flatMap((document) => document.citations)
      .filter((citation) => citationIds.includes(citation.id))
      .map((citation) => citation.id)
    return citations.length ? [{ source_id: source.id, citation_ids: citations }] : []
  }) ?? []

  async function create() {
    if (!context || !projectId || !prompt.trim() || promptBytes > MAX_PROMPT_BYTES) return
    const epoch = contextEpoch.current
    const owner = employeeId
    setCreating(true)
    try {
      const task = await createEmployeeTask(employeeId, {
        prompt,
        skills: selectedSkills.map((binding) => ({
          skill_id: binding.skill_id,
          version: binding.version,
        })),
        knowledge: knowledgeInput,
        memory_fact_ids: memoryIds,
        project_binding_id: projectId,
        policy: {
          allowed_capabilities: capabilities.split(/\r?\n|,/u).map((item) => item.trim()).filter(Boolean),
          network_allowed: network,
          budget,
        },
      })
      if (epoch !== contextEpoch.current || owner !== employeeId) return
      setPrompt('')
      await navigate(`/tasks/${encodeURIComponent(task.id)}`)
    } catch (caught) {
      if (epoch !== contextEpoch.current || owner !== employeeId) return
      actions.showToast({ messageKey: mutationKey(caught), tone: 'error' })
    } finally {
      setCreating(false)
    }
  }

  if (error && tasks.length === 0) {
    return <ErrorState title={t('tasks.loadError')} description={t('common.retryDescription')} />
  }
  const activeEmployees = employees.filter((item) => item.state === 'active')
  const projectOptions = Array.from(new Map(tasks.map((task) => [
    task.project_binding.id,
    { id: task.project_binding.id, label: task.project_binding.label },
  ])).values())
  const taskStates = [
    'queued', 'prepared', 'waiting_owner', 'running', 'verifying',
    'interrupted', 'completed', 'failed', 'cancelled',
  ] as const
  const setFilter = (name: 'employee' | 'project' | 'state' | 'time', value: string) => {
    const next = new URLSearchParams(params)
    if (value) next.set(name, value); else next.delete(name)
    setParams(next)
  }
  return (
    <article className="feature-page">
      <PageHeader title={t('pages.tasks.title')} description={t('tasks.description')} />
      <p className="stale-notice">{t('tasks.listBoundary')}</p>
      <Card className="projection-card" variant="borderless">
        <h2>{t('tasks.create')}</h2>
        <div className="form-grid">
          <label>{t('tasks.employee')}<select value={employeeId} onChange={(event) => setEmployeeId(event.target.value)}>{activeEmployees.map((employee) => <option key={employee.id} value={employee.id}>{employee.name}</option>)}</select></label>
          <label>{t('tasks.prompt')}<textarea value={prompt} onChange={(event) => setPrompt(event.target.value)} /></label>
          <p data-testid="task-prompt-bytes">{promptBytes} / {MAX_PROMPT_BYTES}</p>
          <label>{t('tasks.project')}<select value={projectId} onChange={(event) => setProjectId(event.target.value)}><option value="">{t('common.select')}</option>{context?.record.project_bindings.map((project) => <option key={project.id} value={project.id}>{project.label}</option>)}</select></label>
        </div>
        <fieldset><legend>{t('tasks.exactSkills')}</legend>{availableSkills.map(({ binding }) => {
          const key = `${binding.skill_id}\0${binding.version}`
          return <label key={key}><input type="checkbox" checked={skillKeys.includes(key)} onChange={(event) => setSkillKeys((current) => event.target.checked ? [...current, key] : current.filter((item) => item !== key))} />{binding.skill_id}@{binding.version} · {binding.digest}</label>
        })}</fieldset>
        <fieldset><legend>{t('tasks.knowledgeCitations')}</legend>{context?.knowledge.indexes.flatMap((index) => index.documents).flatMap((document) => document.citations).map((citation) => <label key={citation.id}><input type="checkbox" checked={citationIds.includes(citation.id)} onChange={(event) => setCitationIds((current) => event.target.checked ? [...current, citation.id] : current.filter((item) => item !== citation.id))} />{citation.path}:{citation.start_line}-{citation.end_line} · {citation.digest}</label>)}</fieldset>
        <fieldset><legend>{t('tasks.acceptedMemory')}</legend>{context?.memory.map((fact) => <label key={fact.id}><input type="checkbox" checked={memoryIds.includes(fact.id)} onChange={(event) => setMemoryIds((current) => event.target.checked ? [...current, fact.id] : current.filter((item) => item !== fact.id))} />{fact.category}: {fact.value}</label>)}</fieldset>
        <div className="form-grid">
          <label>{t('tasks.capabilities')}<textarea value={capabilities} onChange={(event) => setCapabilities(event.target.value)} /></label>
          <label><input type="checkbox" checked={network} onChange={(event) => setNetwork(event.target.checked)} />{t('tasks.networkAllowed')}</label>
          <label>{t('tasks.maxCalls')}<input type="number" min="1" value={budget.max_model_calls} onChange={(event) => setBudget({ ...budget, max_model_calls: Number(event.target.value) })} /></label>
          <label>{t('tasks.maxTokens')}<input type="number" min="1" value={budget.max_tokens} onChange={(event) => setBudget({ ...budget, max_tokens: Number(event.target.value) })} /></label>
          <label>{t('tasks.timeoutSeconds')}<input type="number" min="1" value={budget.timeout_seconds} onChange={(event) => setBudget({ ...budget, timeout_seconds: Number(event.target.value) })} /></label>
        </div>
        <Button type="primary" loading={creating} disabled={!connectivity.canMutate || !prompt.trim() || promptBytes > MAX_PROMPT_BYTES || !projectId} onClick={() => void create()}>{t('tasks.createQueued')}</Button>
      </Card>
      <div className="filter-row">
        <label>{t('tasks.employeeFilter')}<select aria-label={t('tasks.employeeFilter')} value={params.get('employee') ?? ''} onChange={(event) => setFilter('employee', event.target.value)}><option value="">{t('employees.all')}</option>{employees.map((employee) => <option key={employee.id} value={employee.id}>{employee.name}</option>)}</select></label>
        <label>{t('tasks.projectFilter')}<select aria-label={t('tasks.projectFilter')} value={params.get('project') ?? ''} onChange={(event) => setFilter('project', event.target.value)}><option value="">{t('employees.all')}</option>{projectOptions.map((project) => <option key={project.id} value={project.id}>{project.label}</option>)}</select></label>
        <label>{t('tasks.stateFilter')}<select aria-label={t('tasks.stateFilter')} value={params.get('state') ?? ''} onChange={(event) => setFilter('state', event.target.value)}><option value="">{t('employees.all')}</option>{taskStates.map((state) => <option key={state} value={state}>{translatedEnum(t, 'taskStatus', state)}</option>)}</select></label>
        <label>{t('tasks.timeFilter')}<select aria-label={t('tasks.timeFilter')} value={params.get('time') ?? ''} onChange={(event) => setFilter('time', event.target.value)}><option value="">{t('tasks.timeAll')}</option><option value="24h">{t('tasks.time24h')}</option><option value="7d">{t('tasks.time7d')}</option><option value="30d">{t('tasks.time30d')}</option></select></label>
      </div>
      <ul className="entity-list">{filtered.map((task) => <li key={task.id}><Link to={`/tasks/${encodeURIComponent(task.id)}`}>{task.prompt}</Link><span>{translatedEnum(t, 'taskStatus', task.state)} · {task.project_binding.label}</span></li>)}</ul>
    </article>
  )
}

export function TaskWorkbenchDetailPage() {
  const { taskId } = useParams()
  const { t } = useTranslation()
  const { actions } = useUI()
  const connectivity = useConnectivity()
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
        const [projection, approvalProjection] = await Promise.all([
          getSession(next.session_id, signal ? { signal } : {}),
          listApprovals(next.session_id, signal ? { signal } : {}),
        ])
        if (epoch !== epochRef.current) return
        setSession(projection)
        setApprovals(approvalProjection.approvals)
      } else {
        setSession(null)
        setApprovals([])
      }
    } catch (caught) {
      if (epoch !== epochRef.current) return
      setNotFound(caught instanceof ApiError && caught.status === 404)
    }
  }, [taskId])

  useEffect(() => {
    const controller = new AbortController()
    setTask(null)
    setSession(null)
    setApprovals([])
    setPrepared(false)
    void refresh(controller.signal)
    return () => {
      epochRef.current += 1
      controller.abort()
    }
  }, [refresh, connectivity.generation])

  const events = useSessionEvents({
    sessionId: task?.session_id,
    frontier: session?.session.next_event_sequence ?? 0,
    runId: task?.run_id,
    onRefresh: () => { void refresh() },
  })

  async function mutate(action: 'start' | 'cancel' | 'resume') {
    if (!task || !connectivity.canMutate) return
    const epoch = epochRef.current
    const owner = task.id
    setBusy(true)
    try {
      const next = action === 'start' ? await startEmployeeTask(task.id)
        : action === 'cancel' ? await cancelEmployeeTask(task.id)
          : await resumeEmployeeTask(task.id)
      if (epoch !== epochRef.current) return
      setTask(next)
      setPrepared(false)
      await refresh()
    } catch (caught) {
      if (epoch !== epochRef.current) return
      actions.showToast({ messageKey: mutationKey(caught), tone: 'error' })
      if (caught instanceof ApiError && caught.status === 409) await refresh()
    } finally {
      if (owner === taskId) setBusy(false)
    }
  }

  async function prepare() {
    if (!task || !connectivity.canMutate) return
    const epoch = epochRef.current
    setBusy(true)
    try {
      const next = await getEmployeeTask(task.id)
      if (epoch !== epochRef.current) return
      setTask(next)
      setPrepared(true)
    } catch (caught) {
      if (epoch === epochRef.current) {
        actions.showToast({ messageKey: mutationKey(caught), tone: 'error' })
      }
    } finally {
      if (epoch === epochRef.current) setBusy(false)
    }
  }

  function confirmCancel() {
    actions.openDialog({
      titleKey: 'tasks.cancelTitle',
      descriptionKey: 'tasks.cancelDescription',
      confirmKey: 'tasks.cancel',
      tone: 'warning',
      onConfirm: () => void mutate('cancel'),
    })
  }

  async function approval(requestId: string, decision: 'approve' | 'deny') {
    if (!task?.session_id || !connectivity.canMutate) return
    const epoch = epochRef.current
    const owner = task.id
    setBusy(true)
    try {
      await decideApproval(task.session_id, requestId, decision)
      if (epoch !== epochRef.current) return
      await refresh()
    } catch (caught) {
      if (epoch === epochRef.current) {
        actions.showToast({ messageKey: mutationKey(caught), tone: 'error' })
      }
    } finally {
      if (owner === taskId) setBusy(false)
    }
  }

  if (notFound) return <ErrorState title={t('tasks.notFound')} description={t('tasks.notFoundDescription')} />
  if (!task) return <p role="status">{t('common.loading')}</p>
  const activeRun = session?.session.runs.find((run) => run.id === task.run_id)
  const canMutate = connectivity.canMutate && !busy
  return (
    <article className="feature-page">
      <PageHeader title={task.prompt} description={`${task.id} · ${task.employee_id}`} />
      <p data-testid="task-status">{translatedEnum(t, 'taskStatus', task.state)}</p>
      {!connectivity.canMutate ? <p className="stale-notice">{t('mutation.offline')}</p> : null}
      <div className="button-row">
        {task.state === 'queued' && !task.run_id && !prepared ? <button type="button" disabled={!canMutate} onClick={() => void prepare()}>{t('tasks.prepare')}</button> : null}
        {task.state === 'queued' && !task.run_id && prepared ? <button type="button" disabled={!canMutate} onClick={() => void mutate('start')}>{t('tasks.start')}</button> : null}
        {task.state === 'interrupted' ? <button type="button" disabled={!canMutate} onClick={() => void mutate('resume')}>{t('tasks.resume')}</button> : null}
        {!TERMINAL.has(task.state) && task.state !== 'interrupted' ? <button type="button" disabled={!canMutate} onClick={confirmCancel}>{t('tasks.cancel')}</button> : null}
      </div>
      {prepared ? <p>{t('tasks.preparedAuthority')}</p> : null}
      <section className="projection-card"><h2>{t('tasks.context')}</h2><dl>
        <dt>{t('tasks.employeeRevision')}</dt><dd>{task.employee_revision}</dd>
        <dt>{t('tasks.employeeSnapshot')}</dt><dd>r{task.employee_snapshot.revision} · {task.employee_snapshot.digest}</dd>
        <dt>{t('tasks.project')}</dt><dd>{task.project_binding.label} · {task.project_binding.workspace_fingerprint}</dd>
        <dt>{t('tasks.skills')}</dt><dd>{task.skills.map((item) => `${item.skill_id}@${item.version} · ${item.digest}`).join('; ') || '—'}</dd>
        <dt>{t('tasks.knowledge')}</dt><dd>{task.knowledge.flatMap((item) => item.citations.map((citation) => `${citation.path}:${citation.start_line}-${citation.end_line}`)).join('; ') || '—'}</dd>
        <dt>{t('tasks.memory')}</dt><dd>{task.memory_facts.map((item) => `${item.fact_id} · ${item.digest}`).join('; ') || '—'}</dd>
        <dt>{t('tasks.session')}</dt><dd>{task.session_id ?? t('common.empty')}</dd>
        <dt>{t('tasks.run')}</dt><dd>{task.run_id ?? t('common.empty')}</dd>
      </dl></section>
      <section className="projection-card"><h2>{t('session.plan')}</h2>{activeRun?.plan ? <ol>{activeRun.plan.steps.map((step) => <li key={step.id}>{step.title} · {translatedEnum(t, 'planStatus', step.status)}</li>)}</ol> : <p>{t('common.empty')}</p>}</section>
      <section className="projection-card"><h2>{t('tasks.tools')}</h2><ul>{session?.session.tool_calls.filter((tool) => !task.run_id || tool.run_id === task.run_id).map((tool) => <li key={tool.call_id}>{tool.name} · {tool.summary} · {translatedEnum(t, 'toolStatus', tool.status || 'unknown')}</li>)}</ul></section>
      <section className="projection-card"><h2>{t('session.verification')}</h2><ul>{session?.session.test_results.filter((result) => !task.run_id || result.run_id === task.run_id).map((result, index) => <li key={`${result.command}:${index}`}>{result.command} · {result.passed ? t('verification.passed') : t('verification.failed')} · {result.summary}</li>)}</ul></section>
      <section className="projection-card"><h2>{t('session.approvals')}</h2>{approvals.map((request) => <article key={request.request_id}><strong>{request.tool}</strong><p>{request.args_summary} · {request.resource_paths.join(', ')}</p>{request.status === 'pending' ? <div className="button-row"><button type="button" disabled={!canMutate} onClick={() => void approval(request.request_id, 'approve')}>{t('approval.approve')}</button><button type="button" disabled={!canMutate} onClick={() => void approval(request.request_id, 'deny')}>{t('approval.deny')}</button></div> : <p>{translatedEnum(t, 'approvalStatus', request.status)}</p>}</article>)}</section>
      <section className="projection-card"><h2>{t('tasks.artifacts')}</h2><ul>{task.artifacts.map((artifact) => <li key={artifact.id}>{artifact.path} · {artifact.digest} · {artifact.verified_at}</li>)}</ul></section>
      <section className="projection-card" data-testid="task-timeline"><h2>{t('tasks.activity')}</h2>
        {events.status === 'fatal' ? <p><button type="button" onClick={events.reconnect}>{t('session.reconnectEvents')}</button></p> : null}
        {events.status === 'reconnecting' ? <p>{t('session.reconnecting')}</p> : null}
        {events.truncated ? <p>{t('session.streamingTruncated')}</p> : null}
        <ul>{events.events.map((event) => <li key={`${event.sequence}:${event.type}:${event.time}`}><time>{event.time}</time> · {translatedEnum(t, 'runtimeEventType', event.type)}</li>)}</ul>
      </section>
    </article>
  )
}
