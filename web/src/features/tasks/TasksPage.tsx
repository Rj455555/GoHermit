import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import {
  cancelEmployeeTask,
  createEmployeeTask,
  getEmployee,
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
  EmployeeSummary,
  EmployeeTask,
  SessionDetailResponse,
} from '../../api/types'
import { useConnectivity } from '../../components/ConnectivityProvider'
import { ErrorState } from '../../components/ErrorState'
import { PageHeader } from '../../components/PageHeader'
import { useSessionEvents } from '../../hooks/useSessionEvents'
import { translatedEnum } from '../../i18n/enumLabel'
import { useUI } from '../../state/UIContext'

function mutationKey(error: unknown) {
  if (error instanceof ApiError && error.code === 'network_error') return 'mutation.offline'
  if (error instanceof ApiError && error.status === 409) return 'mutation.conflict'
  return 'mutation.failed'
}

async function loadBoundedEmployeeTasks(employees: EmployeeSummary[]) {
  const tasks: EmployeeTask[] = []
  let index = 0
  const workers = Array.from({ length: Math.min(4, employees.length) }, async () => {
    while (index < employees.length) {
      const employee = employees[index]
      index += 1
      if (!employee) return
      const page = await listEmployeeTasks(employee.id, { limit: 100 }, {})
      tasks.push(...page.tasks)
    }
  })
  await Promise.all(workers)
  return tasks
}

export function TasksPage() {
  const { t } = useTranslation()
  const { actions } = useUI()
  const navigate = useNavigate()
  const connectivity = useConnectivity()
  const [params, setParams] = useSearchParams()
  const [employees, setEmployees] = useState<EmployeeSummary[]>([])
  const [tasks, setTasks] = useState<EmployeeTask[]>([])
  const [error, setError] = useState(false)
  const [creating, setCreating] = useState(false)
  const [employeeId, setEmployeeId] = useState('')
  const [prompt, setPrompt] = useState('')

  const load = useCallback(async () => {
    try {
      const employeePage = await listEmployees({}, {})
      const active = employeePage.employees
      const loadedTasks = await loadBoundedEmployeeTasks(active)
      setEmployees(active)
      setTasks(loadedTasks)
      setEmployeeId((current) => current || active[0]?.id || '')
      setError(false)
    } catch {
      setError(true)
    }
  }, [])
  useEffect(() => { void load() }, [load, connectivity.generation])

  const filtered = useMemo(() => tasks.filter((task) => {
    const employee = params.get('employee')
    const state = params.get('state')
    const project = params.get('project')
    return (!employee || task.employee_id === employee)
      && (!state || task.state === state)
      && (!project || task.project_binding.id === project)
  }), [params, tasks])

  async function create() {
    if (!employeeId || !prompt.trim()) return
    setCreating(true)
    try {
      const record = await getEmployee(employeeId)
      const project = record.project_bindings[0]
      if (!project) throw new Error('missing_project')
      const task = await createEmployeeTask(employeeId, {
        prompt,
        skills: record.employee.skill_bindings.filter((binding) => binding.enabled).map((binding) => ({
          skill_id: binding.skill_id,
          version: binding.version,
        })),
        knowledge: [],
        memory_fact_ids: [],
        project_binding_id: project.id,
        policy: {
          allowed_capabilities: record.employee.permission_policy.allowed_capabilities,
          network_allowed: record.employee.permission_policy.network_allowed,
          budget: record.employee.budget_policy,
        },
      })
      setPrompt('')
      void navigate(`/tasks/${encodeURIComponent(task.id)}`)
    } catch (error) {
      actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
    } finally {
      setCreating(false)
    }
  }

  if (error && tasks.length === 0) {
    return <ErrorState title={t('tasks.loadError')} description={t('common.retryDescription')} />
  }
  return (
    <article className="feature-page">
      <PageHeader title={t('pages.tasks.title')} description={t('tasks.description')} />
      <p className="stale-notice">{t('tasks.listBoundary')}</p>
      <section className="projection-card">
        <h2>{t('tasks.create')}</h2>
        <label>{t('tasks.employee')}
          <select value={employeeId} onChange={(event) => setEmployeeId(event.target.value)}>
            {employees.map((employee) => <option key={employee.id} value={employee.id}>{employee.name}</option>)}
          </select>
        </label>
        <label>{t('tasks.prompt')}<textarea value={prompt} onChange={(event) => setPrompt(event.target.value)} /></label>
        <button type="button" disabled={creating || !connectivity.canMutate || !prompt.trim()} onClick={() => void create()}>{t('tasks.createQueued')}</button>
      </section>
      <div className="filter-row">
        <label>{t('tasks.employee')}<select value={params.get('employee') ?? ''} onChange={(event) => {
          const next = new URLSearchParams(params)
          if (event.target.value) next.set('employee', event.target.value); else next.delete('employee')
          setParams(next)
        }}><option value="">{t('employees.all')}</option>{employees.map((employee) => <option key={employee.id} value={employee.id}>{employee.name}</option>)}</select></label>
        <label>{t('tasks.state')}<select value={params.get('state') ?? ''} onChange={(event) => {
          const next = new URLSearchParams(params)
          if (event.target.value) next.set('state', event.target.value); else next.delete('state')
          setParams(next)
        }}><option value="">{t('employees.all')}</option>{['queued', 'running', 'interrupted', 'completed', 'failed', 'cancelled'].map((state) => <option key={state} value={state}>{translatedEnum(t, 'taskStatus', state)}</option>)}</select></label>
      </div>
      <ul className="entity-list">
        {filtered.map((task) => (
          <li key={task.id}>
            <Link to={`/tasks/${encodeURIComponent(task.id)}`}>{task.prompt}</Link>
            <span>{translatedEnum(t, 'taskStatus', task.state)} · {task.project_binding.label}</span>
          </li>
        ))}
      </ul>
    </article>
  )
}

const TERMINAL = new Set(['completed', 'failed', 'cancelled'])

export function TaskDetailPage() {
  const { taskId } = useParams()
  const { t } = useTranslation()
  const { actions } = useUI()
  const connectivity = useConnectivity()
  const [task, setTask] = useState<EmployeeTask | null>(null)
  const [session, setSession] = useState<SessionDetailResponse | null>(null)
  const [approvals, setApprovals] = useState<ApprovalRequest[]>([])
  const [prepared, setPrepared] = useState(false)
  const [busy, setBusy] = useState(false)
  const [notFound, setNotFound] = useState(false)

  const refresh = useCallback(async () => {
    if (!taskId) return
    try {
      const next = await getEmployeeTask(taskId)
      setTask(next)
      setNotFound(false)
      if (next.session_id) {
        const [sessionProjection, approvalProjection] = await Promise.all([
          getSession(next.session_id),
          listApprovals(next.session_id),
        ])
        setSession(sessionProjection)
        setApprovals(approvalProjection.approvals)
      } else {
        setSession(null)
        setApprovals([])
      }
    } catch (error) {
      setNotFound(error instanceof ApiError && error.status === 404)
    }
  }, [taskId])
  useEffect(() => { void refresh() }, [refresh, connectivity.generation])

  const events = useSessionEvents({
    sessionId: task?.session_id,
    frontier: session?.session.next_event_sequence ?? 0,
    runId: task?.run_id,
    onRefresh: () => { void refresh() },
  })

  async function mutate(action: 'start' | 'cancel' | 'resume') {
    if (!task) return
    setBusy(true)
    try {
      const next = action === 'start'
        ? await startEmployeeTask(task.id)
        : action === 'cancel'
          ? await cancelEmployeeTask(task.id)
          : await resumeEmployeeTask(task.id)
      setTask(next)
      setPrepared(false)
      await refresh()
    } catch (error) {
      actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
      if (error instanceof ApiError && error.status === 409) await refresh()
    } finally {
      setBusy(false)
    }
  }

  if (notFound) return <ErrorState title={t('tasks.notFound')} description={t('tasks.notFoundDescription')} />
  if (!task) return <p role="status">{t('common.loading')}</p>
  return (
    <article className="feature-page">
      <PageHeader title={task.prompt} description={`${task.id} · ${task.employee_id}`} />
      <p data-testid="task-status">{translatedEnum(t, 'taskStatus', task.state)}</p>
      <div className="button-row">
        {task.state === 'queued' && !task.run_id && !prepared ? (
          <button type="button" disabled={busy} onClick={() => setPrepared(true)}>{t('tasks.prepare')}</button>
        ) : null}
        {task.state === 'queued' && !task.run_id && prepared ? (
          <button type="button" disabled={busy || !connectivity.canMutate} onClick={() => void mutate('start')}>{t('tasks.start')}</button>
        ) : null}
        {task.state === 'interrupted' ? <button type="button" disabled={busy} onClick={() => void mutate('resume')}>{t('tasks.resume')}</button> : null}
        {!TERMINAL.has(task.state) && task.state !== 'interrupted' ? <button type="button" disabled={busy} onClick={() => void mutate('cancel')}>{t('tasks.cancel')}</button> : null}
      </div>
      {prepared ? <p>{t('tasks.preparedAuthority')}</p> : null}
      <section className="projection-card">
        <h2>{t('tasks.context')}</h2>
        <dl>
          <dt>{t('tasks.employeeRevision')}</dt><dd>{task.employee_revision}</dd>
          <dt>{t('tasks.project')}</dt><dd>{task.project_binding.label}</dd>
          <dt>{t('tasks.session')}</dt><dd>{task.session_id ?? t('common.empty')}</dd>
          <dt>{t('tasks.run')}</dt><dd>{task.run_id ?? t('common.empty')}</dd>
        </dl>
      </section>
      <section className="projection-card">
        <h2>{t('tasks.execution')}</h2>
        <pre>{JSON.stringify({
          plan: session?.session.runs.find((run) => run.id === task.run_id)?.plan,
          tools: session?.session.tool_calls,
          verification: session?.session.test_results,
          approvals,
          artifacts: task.artifacts,
        }, null, 2)}</pre>
      </section>
      <section className="projection-card" data-testid="task-timeline">
        <h2>{t('tasks.activity')}</h2>
        {events.truncated ? <p>{t('session.streamingTruncated')}</p> : null}
        <ul>{events.events.map((event) => <li key={`${event.sequence}:${event.type}`}>{translatedEnum(t, 'runtimeEventType', event.type)}</li>)}</ul>
      </section>
    </article>
  )
}
