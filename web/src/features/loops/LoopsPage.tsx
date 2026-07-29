import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useParams } from 'react-router-dom'

import {
  cancelLoopInvocation,
  createLoop,
  dryRunLoop,
  getLoop,
  getLoopInvocation,
  getSession,
  getTeamTemplate,
  importLoop,
  importTeamTemplate,
  listEmployees,
  listLoopInvocations,
  listLoops,
  startLoopInvocation,
  updateLoop,
} from '../../api/endpoints'
import { ApiError } from '../../api/errors'
import type {
  DryRunReport,
  EmployeeSummary,
  LoopDefinition,
  LoopInvocation,
  LoopSummary,
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

function newDefinition(): LoopDefinition {
  const now = new Date(0).toISOString()
  return {
    id: '',
    schema_version: 1,
    name: '',
    description: '',
    workspace_identity: '',
    enabled: true,
    task_source: { type: 'fixed_prompt', prompt: '' },
    agent_selection: { company: 'openai', access: 'codex', model: '', agent: 'team' },
    team_template_ref: '',
    plan_mode: 'review',
    verification_recipe: { checks: [], independent_verifier: true, max_repair_attempts: 0 },
    budget: { max_model_calls: 12, max_tokens: 120_000, timeout_seconds: 1_200 },
    approval_policy: { require_for_mutation: true },
    workspace_policy: { read_only: true, require_clean_git: false },
    output_policy: { include_diff: false, max_report_bytes: 65_536 },
    created_at: now,
    updated_at: now,
    revision: 0,
  }
}

export function LoopsPage() {
  const { t } = useTranslation()
  const { actions } = useUI()
  const navigate = useNavigate()
  const connectivity = useConnectivity()
  const [loops, setLoops] = useState<LoopSummary[]>([])
  const [creating, setCreating] = useState(false)
  const [draft, setDraft] = useState(newDefinition)
  const [importText, setImportText] = useState('')
  const [strictError, setStrictError] = useState('')

  const load = useCallback(() => {
    void listLoops().then((page) => setLoops(page.loops)).catch(() => {
      actions.showToast({ messageKey: 'mutation.failed', tone: 'error' })
    })
  }, [actions])
  useEffect(load, [connectivity.generation, load])

  async function saveNew() {
    setCreating(true)
    try {
      const saved = await createLoop(draft)
      void navigate(`/loops/${encodeURIComponent(saved.id)}`)
    } catch (error) {
      actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
    } finally {
      setCreating(false)
    }
  }

  async function importStrict() {
    setStrictError('')
    let value: unknown
    try {
      value = JSON.parse(importText) as unknown
      if (typeof value !== 'object' || value === null || Array.isArray(value)) {
        throw new Error('shape')
      }
    } catch {
      setStrictError(t('loops.invalidImport'))
      return
    }
    try {
      const saved = await importLoop(value)
      void navigate(`/loops/${encodeURIComponent(saved.id)}`)
    } catch (error) {
      setStrictError(error instanceof ApiError && error.status === 409
        ? t('mutation.conflict')
        : t('loops.rejectedImport'))
    }
  }

  return (
    <article className="feature-page">
      <PageHeader title={t('pages.loops.title')} description={t('loops.description')} />
      <ul className="entity-list">
        {loops.map((loop) => <li key={loop.id}><Link to={`/loops/${encodeURIComponent(loop.id)}`}>{loop.name}</Link><span>r{loop.revision}</span></li>)}
      </ul>
      <section className="projection-card">
        <h2>{t('loops.create')}</h2>
        <label>{t('loops.id')}<input value={draft.id} onChange={(event) => setDraft({ ...draft, id: event.target.value })} /></label>
        <label>{t('loops.name')}<input value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} /></label>
        <label>{t('loops.workspace')}<input value={draft.workspace_identity} onChange={(event) => setDraft({ ...draft, workspace_identity: event.target.value })} /></label>
        <label>{t('loops.mission')}<textarea value={draft.task_source.prompt} onChange={(event) => setDraft({ ...draft, task_source: { type: 'fixed_prompt', prompt: event.target.value } })} /></label>
        <button type="button" disabled={creating || !connectivity.canMutate} onClick={() => void saveNew()}>{t('loops.create')}</button>
      </section>
      <section className="projection-card">
        <h2>{t('loops.import')}</h2>
        <textarea aria-label={t('loops.import')} value={importText} onChange={(event) => setImportText(event.target.value)} />
        {strictError ? <p role="alert">{strictError}</p> : null}
        <button type="button" onClick={() => void importStrict()}>{t('loops.import')}</button>
      </section>
    </article>
  )
}

export function LoopDetailPage() {
  const { loopId } = useParams()
  const { t } = useTranslation()
  const { actions } = useUI()
  const connectivity = useConnectivity()
  const [definition, setDefinition] = useState<LoopDefinition | null>(null)
  const [history, setHistory] = useState<LoopInvocation[]>([])
  const [employees, setEmployees] = useState<EmployeeSummary[]>([])
  const [report, setReport] = useState<DryRunReport | null>(null)
  const [teamJSON, setTeamJSON] = useState('{}')
  const [busy, setBusy] = useState(false)
  const [notFound, setNotFound] = useState(false)

  const refresh = useCallback(async () => {
    if (!loopId) return
    try {
      const [next, invocations, employeePage, team] = await Promise.all([
        getLoop(loopId),
        listLoopInvocations(loopId),
        listEmployees({ state: 'active' }),
        getTeamTemplate(),
      ])
      setDefinition(next)
      setHistory(invocations.invocations as LoopInvocation[])
      setEmployees(employeePage.employees)
      setTeamJSON(JSON.stringify(team, null, 2))
      setNotFound(false)
    } catch (error) {
      setNotFound(error instanceof ApiError && error.status === 404)
    }
  }, [loopId])
  useEffect(() => { void refresh() }, [refresh, connectivity.generation])

  function edit(next: LoopDefinition) {
    setDefinition(next)
    setReport(null)
  }

  async function mutate(action: 'save' | 'start' | 'dry-run') {
    if (!loopId || !definition) return
    setBusy(true)
    try {
      if (action === 'save') {
        setDefinition(await updateLoop(loopId, definition))
        setReport(null)
      }
      if (action === 'start') {
        await startLoopInvocation(loopId)
        await refresh()
      }
      if (action === 'dry-run') setReport(await dryRunLoop(loopId))
    } catch (error) {
      actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
      if (error instanceof ApiError && error.status === 409) await refresh()
    } finally {
      setBusy(false)
    }
  }

  async function saveTeam() {
    let parsed: unknown
    try {
      parsed = JSON.parse(teamJSON) as unknown
      if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) throw new Error()
    } catch {
      actions.showToast({ messageKey: 'loops.invalidTeam', tone: 'error' })
      return
    }
    try {
      await importTeamTemplate(parsed as Record<string, unknown>)
      actions.showToast({ messageKey: 'toast.saved', tone: 'success' })
    } catch (error) {
      actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
    }
  }

  if (notFound) return <ErrorState title={t('loops.notFound')} description={t('loops.notFoundDescription')} />
  if (!definition) return <p role="status">{t('common.loading')}</p>
  return (
    <article className="feature-page">
      <PageHeader title={definition.name} description={`${definition.id} · r${definition.revision}`} />
      <section className="projection-card">
        <label>{t('loops.name')}<input value={definition.name} onChange={(event) => edit({ ...definition, name: event.target.value })} /></label>
        <label>{t('loops.descriptionLabel')}<textarea value={definition.description} onChange={(event) => edit({ ...definition, description: event.target.value })} /></label>
        <label>{t('loops.mission')}<textarea value={definition.task_source.prompt} onChange={(event) => edit({ ...definition, task_source: { ...definition.task_source, prompt: event.target.value } })} /></label>
        <label>{t('loops.defaultModel')}<input value={definition.agent_selection.model} onChange={(event) => edit({ ...definition, agent_selection: { ...definition.agent_selection, model: event.target.value } })} /></label>
        <label><input type="checkbox" checked={definition.workspace_policy.read_only} onChange={(event) => edit({ ...definition, workspace_policy: { ...definition.workspace_policy, read_only: event.target.checked } })} /> {t('loops.readOnly')}</label>
        <div className="button-row">
          <button type="button" disabled={busy} onClick={() => void mutate('save')}>{t('loops.save')}</button>
          <button type="button" disabled={busy} onClick={() => void mutate('dry-run')}>{t('loops.dryRun')}</button>
          <button type="button" disabled={busy || !report?.ready} onClick={() => void mutate('start')}>{t('loops.start')}</button>
        </div>
        {report ? <pre data-testid="loop-dry-run">{JSON.stringify(report, null, 2)}</pre> : <p>{t('loops.runDryFirst')}</p>}
      </section>
      <section className="projection-card">
        <h2>{t('loops.team')}</h2>
        <p>{t('loops.employeeRevisionReadiness')}</p>
        <ul>{employees.map((employee) => <li key={employee.id}>{employee.name} · r{employee.revision} · {translatedEnum(t, 'employeeStatus', employee.state)}</li>)}</ul>
        <p>{t('loops.modelOverrideRule')}</p>
        <textarea aria-label={t('loops.team')} value={teamJSON} onChange={(event) => setTeamJSON(event.target.value)} />
        <button type="button" onClick={() => void saveTeam()}>{t('loops.saveTeam')}</button>
      </section>
      <section className="projection-card">
        <h2>{t('loops.history')}</h2>
        <ul>{history.map((invocation) => <li key={invocation.id}><Link to={`/loops/${encodeURIComponent(definition.id)}/invocations/${encodeURIComponent(invocation.id)}`}>{invocation.id}</Link> · {translatedEnum(t, 'invocationStatus', invocation.status)}</li>)}</ul>
      </section>
    </article>
  )
}

export function LoopInvocationPage() {
  const { invocationId } = useParams()
  const { t } = useTranslation()
  const { actions } = useUI()
  const [invocation, setInvocation] = useState<LoopInvocation | null>(null)
  const [session, setSession] = useState<SessionDetailResponse | null>(null)
  const [notFound, setNotFound] = useState(false)

  const refresh = useCallback(async () => {
    if (!invocationId) return
    try {
      const next = await getLoopInvocation(invocationId, {})
      setInvocation(next)
      setSession(next.session_id ? await getSession(next.session_id, {}) : null)
      setNotFound(false)
    } catch (error) {
      setNotFound(error instanceof ApiError && error.status === 404)
    }
  }, [invocationId])
  useEffect(() => { void refresh() }, [refresh])
  const events = useSessionEvents({
    sessionId: invocation?.session_id,
    frontier: session?.session.next_event_sequence ?? 0,
    runId: invocation?.run_id,
    onRefresh: () => { void refresh() },
  })

  function cancel() {
    if (!invocation) return
    actions.openDialog({
      titleKey: 'loops.cancelTitle',
      descriptionKey: 'loops.cancelDescription',
      confirmKey: 'loops.cancel',
      tone: 'warning',
      onConfirm: () => {
        void cancelLoopInvocation(invocation.id).then(setInvocation).catch((error: unknown) => {
          actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
        })
      },
    })
  }

  if (notFound) return <ErrorState title={t('loops.invocationNotFound')} description={t('loops.notFoundDescription')} />
  if (!invocation) return <p role="status">{t('common.loading')}</p>
  const active = ['prepared', 'dispatched', 'attached'].includes(invocation.status)
  return (
    <article className="feature-page">
      <PageHeader title={invocation.id} description={`${invocation.loop_id} · ${translatedEnum(t, 'invocationStatus', invocation.status)}`} />
      {active ? <button type="button" onClick={cancel}>{t('loops.cancel')}</button> : null}
      <section className="projection-card" data-testid="loop-timeline">
        <h2>{t('loops.timeline')}</h2>
        <p>{invocation.id}</p>
        <pre>{JSON.stringify({
          session: session?.session,
          events: events.events,
          definition: invocation.definition_snapshot,
        }, null, 2)}</pre>
      </section>
    </article>
  )
}
