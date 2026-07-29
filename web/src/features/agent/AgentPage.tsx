import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
} from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useParams } from 'react-router-dom'

import {
  approvePlan,
  cancelRun,
  createSession,
  decideApproval,
  getSession,
  listApprovals,
  resumeRun,
  startRun,
} from '../../api/endpoints'
import { ApiError } from '../../api/errors'
import type {
  ApprovalRequest,
  PlanMode,
  Run,
  SessionDetailResponse,
} from '../../api/types'
import { useConnectivity } from '../../components/ConnectivityProvider'
import { ErrorState } from '../../components/ErrorState'
import { PageHeader } from '../../components/PageHeader'
import { StatusBadge } from '../../components/StatusBadge'
import { useSessionEvents } from '../../hooks/useSessionEvents'
import { useAgentData } from './AgentDataContext'

const MAX_MESSAGE_BYTES = 64 << 10

export function AgentLandingPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const connectivity = useConnectivity()
  const { info, loading, error, refresh } = useAgentData()
  const [selection, setSelection] = useState({
    company: '',
    access: '',
    model: '',
    agent: '',
    plan_mode: 'auto' as PlanMode,
    title: '',
  })
  const [submitting, setSubmitting] = useState(false)
  const submittingRef = useRef(false)

  const companies = info?.available_companies ?? []
  const selectedCompany = companies.find((company) => company.id === selection.company) ?? companies[0]
  const selectedAccess = selectedCompany?.access.find((access) => access.id === selection.access) ?? selectedCompany?.access[0]

  useEffect(() => {
    if (info === null) return
    setSelection((current) => ({
      ...current,
      company: selectedCompany?.id ?? '',
      access: selectedAccess?.id ?? '',
      model: selectedAccess?.models.find((model) => model.id === current.model)?.id ?? selectedAccess?.models[0]?.id ?? '',
      agent: info.agents.find((agent) => agent.id === current.agent)?.id ?? info.agents[0]?.id ?? '',
    }))
  }, [info, selectedAccess, selectedCompany?.id])

  async function submit(event: FormEvent) {
    event.preventDefault()
    if (
      submittingRef.current ||
      !connectivity.canMutate ||
      !selection.company ||
      !selection.access ||
      !selection.model ||
      !selection.agent
    ) return
    submittingRef.current = true
    setSubmitting(true)
    try {
      const created = await createSession({
        title: selection.title.trim(),
        company: selection.company,
        access: selection.access,
        model: selection.model,
        agent: selection.agent,
        plan_mode: selection.plan_mode,
      })
      await refresh()
      void navigate(`/agent/sessions/${encodeURIComponent(created.id)}`)
    } catch {
      setSubmitting(false)
      submittingRef.current = false
    }
  }

  if (loading && info === null) return <p role="status">{t('common.loading')}</p>
  if (error && info === null) return <ErrorState title={t('agent.loadError')} description={t('common.retryDescription')} />
  return (
    <article className="feature-page agent-page">
      <PageHeader title={t('agent.newSession')} description={t('agent.newSessionDescription')} />
      <form className="projection-card form-grid" onSubmit={(event) => void submit(event)}>
        <label>
          {t('agent.company')}
          <select
            value={selection.company}
            onChange={(event) => {
              const company = companies.find((item) => item.id === event.target.value)
              const access = company?.access[0]
              setSelection((current) => ({
                ...current,
                company: event.target.value,
                access: access?.id ?? '',
                model: access?.models[0]?.id ?? '',
              }))
            }}
          >
            {companies.map((company) => <option key={company.id} value={company.id}>{company.label}</option>)}
          </select>
        </label>
        <label>
          {t('agent.access')}
          <select
            value={selection.access}
            onChange={(event) => {
              const access = selectedCompany?.access.find((item) => item.id === event.target.value)
              setSelection((current) => ({ ...current, access: event.target.value, model: access?.models[0]?.id ?? '' }))
            }}
          >
            {selectedCompany?.access.map((access) => <option key={access.id} value={access.id}>{access.label}</option>)}
          </select>
        </label>
        <label>
          {t('agent.model')}
          <select value={selection.model} onChange={(event) => setSelection((current) => ({ ...current, model: event.target.value }))}>
            {selectedAccess?.models.map((model) => <option key={model.id} value={model.id}>{model.label}</option>)}
          </select>
        </label>
        <label>
          {t('agent.agent')}
          <select value={selection.agent} onChange={(event) => setSelection((current) => ({ ...current, agent: event.target.value }))}>
            {info?.agents.map((agent) => <option key={agent.id} value={agent.id}>{agent.label}</option>)}
          </select>
        </label>
        <label>
          {t('agent.planMode')}
          <select value={selection.plan_mode} onChange={(event) => setSelection((current) => ({ ...current, plan_mode: event.target.value as PlanMode }))}>
            <option value="auto">{t('agent.planAuto')}</option>
            <option value="review">{t('agent.planReview')}</option>
          </select>
        </label>
        <label>
          {t('agent.title')}
          <input maxLength={4096} value={selection.title} onChange={(event) => setSelection((current) => ({ ...current, title: event.target.value }))} />
        </label>
        <button type="submit" className="button button--primary" disabled={submitting || !connectivity.canMutate || companies.length === 0}>
          {t('agent.createSession')}
        </button>
      </form>
    </article>
  )
}

export function AgentSessionPage() {
  const { t } = useTranslation()
  const { sessionId } = useParams()
  const connectivity = useConnectivity()
  const { refresh: refreshSessions } = useAgentData()
  const [projection, setProjection] = useState<SessionDetailResponse | null>(null)
  const [approvals, setApprovals] = useState<ApprovalRequest[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [composer, setComposer] = useState('')
  const [mutating, setMutating] = useState(false)
  const [now, setNow] = useState(() => Date.now())
  const requestVersion = useRef(0)
  const requestController = useRef<AbortController | null>(null)
  const mutationInFlight = useRef(false)
  const decisionInFlight = useRef(new Set<string>())

  const refresh = useCallback(async () => {
    if (sessionId === undefined) return
    requestController.current?.abort()
    requestVersion.current += 1
    const version = requestVersion.current
    const controller = new AbortController()
    requestController.current = controller
    try {
      const [detail, approvalResponse] = await Promise.all([
        getSession(sessionId, { signal: controller.signal }),
        listApprovals(sessionId, { signal: controller.signal }),
      ])
      if (requestVersion.current !== version) return
      setProjection(detail)
      setApprovals(approvalResponse.approvals)
      setError(false)
      await refreshSessions()
    } catch {
      if (!controller.signal.aborted && requestVersion.current === version) setError(true)
    } finally {
      if (requestVersion.current === version) setLoading(false)
    }
  }, [refreshSessions, sessionId])

  useEffect(() => {
    void refresh()
    return () => {
      requestVersion.current += 1
      requestController.current?.abort()
    }
  }, [connectivity.generation, refresh])

  useEffect(() => {
    if (!approvals.some((approval) => approval.status === 'pending')) return
    const timer = window.setInterval(() => setNow(Date.now()), 1_000)
    return () => window.clearInterval(timer)
  }, [approvals])

  const activeRun = useMemo<Run | undefined>(
    () => projection?.session.runs.find((run) => run.id === projection.session.active_run_id)
      ?? projection?.session.runs.at(-1),
    [projection],
  )
  const eventState = useSessionEvents({
    sessionId: projection?.session.id,
    frontier: projection?.session.next_event_sequence ?? 0,
    runId: activeRun?.id,
    onRefresh: () => void refresh(),
  })

  async function submitRun() {
    const message = composer.trim()
    if (
      sessionId === undefined ||
      mutationInFlight.current ||
      !connectivity.canMutate ||
      message === '' ||
      new TextEncoder().encode(message).byteLength > MAX_MESSAGE_BYTES
    ) return
    mutationInFlight.current = true
    setMutating(true)
    try {
      await startRun(sessionId, message)
      setComposer('')
      await refresh()
    } catch {
      // Keep the user's text for correction/retry; errors stay generic in UI.
    } finally {
      mutationInFlight.current = false
      setMutating(false)
    }
  }

  function composerKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') {
      event.preventDefault()
      void submitRun()
    }
  }

  async function runAction(action: 'cancel' | 'resume' | 'approve') {
    if (
      sessionId === undefined ||
      activeRun === undefined ||
      mutationInFlight.current ||
      !connectivity.canMutate
    ) return
    mutationInFlight.current = true
    setMutating(true)
    try {
      if (action === 'cancel') await cancelRun(sessionId, activeRun.id)
      else if (action === 'resume') await resumeRun(sessionId, activeRun.id)
      else await approvePlan(sessionId, activeRun.id)
      await refresh()
    } finally {
      mutationInFlight.current = false
      setMutating(false)
    }
  }

  async function decide(request: ApprovalRequest, decision: 'approve' | 'deny') {
    if (
      sessionId === undefined ||
      decisionInFlight.current.has(request.request_id) ||
      Date.parse(request.expires_at) <= Date.now() ||
      !connectivity.canMutate
    ) return
    decisionInFlight.current.add(request.request_id)
    try {
      await decideApproval(sessionId, request.request_id, decision)
    } catch (caught) {
      if (!(caught instanceof ApiError && caught.status === 409)) return
    } finally {
      decisionInFlight.current.delete(request.request_id)
      await refresh()
    }
  }

  if (loading && projection === null) return <p role="status">{t('common.loading')}</p>
  if (projection === null) return <ErrorState title={t('agent.sessionNotFound')} description={t('agent.sessionNotFoundDescription')} />
  const session = projection.session
  const composerDisabled = mutating || !connectivity.canMutate || activeRun?.status === 'running' || activeRun?.status === 'verifying'

  return (
    <article className="feature-page session-page">
      <PageHeader title={session.title} description={`${session.selection.company} / ${session.selection.access} / ${session.selection.model} / ${session.selection.agent}`} />
      {error || connectivity.status === 'offline' ? <p className="stale-notice">{t('connectivity.stale')}</p> : null}
      {eventState.status === 'reconnecting' ? <p role="status">{t('session.reconnecting')}</p> : null}
      {eventState.fatal ? <button type="button" className="button button--secondary" onClick={eventState.reconnect}>{t('session.reconnectEvents')}</button> : null}

      <section className="projection-card run-card">
        <h2>{t('session.run')}</h2>
        <p>{activeRun ? <StatusBadge tone={activeRun.status === 'failed' ? 'error' : 'info'}>{t(`runStatus.${activeRun.status}`)}</StatusBadge> : t('session.noActiveRun')}</p>
        <div className="action-row">
          {activeRun?.status === 'running' || activeRun?.status === 'verifying' ? <button type="button" className="button button--danger" disabled={mutating || !connectivity.canMutate} onClick={() => void runAction('cancel')}>{t('session.cancelRun')}</button> : null}
          {activeRun?.status === 'interrupted' ? <button type="button" className="button button--primary" disabled={mutating || !connectivity.canMutate} onClick={() => void runAction('resume')}>{t('session.resumeRun')}</button> : null}
          {activeRun?.plan_mode === 'review' && !activeRun.plan_approved ? <button type="button" className="button button--primary" disabled={mutating || !connectivity.canMutate} onClick={() => void runAction('approve')}>{t('session.approvePlan')}</button> : null}
        </div>
      </section>

      <section className="projection-card message-timeline">
        <h2>{t('session.messages')}</h2>
        {projection.messages.map((message) => <article className={`message message--${message.role}`} key={message.id}><strong>{message.role}</strong><p>{message.content}</p></article>)}
        {eventState.streamingText ? <article className="message message--assistant" data-testid="streaming-bubble"><strong>assistant</strong><p>{eventState.streamingText}</p></article> : null}
      </section>

      {activeRun?.plan ? (
        <section className="projection-card">
          <h2>{t('session.plan')} · r{activeRun.plan.revision}</h2>
          <ol>{activeRun.plan.steps.map((step) => <li key={step.id}><StatusBadge tone="info">{t(`planStatus.${step.status}`)}</StatusBadge> {step.title}{step.detail ? <small>{step.detail}</small> : null}</li>)}</ol>
        </section>
      ) : null}

      {session.mission ? (
        <section className="projection-card">
          <h2>{t('session.mission')}</h2>
          <p>{session.mission.goal}</p>
          <ul>{session.mission.work_items.map((item) => <li key={item.id}>{item.title} · {item.status}</li>)}</ul>
        </section>
      ) : null}

      <section className="projection-card">
        <h2>{t('session.tools')}</h2>
        {session.tool_calls.length === 0 ? <p>{t('common.empty')}</p> : <ul>{session.tool_calls.map((tool) => <li key={`${tool.call_id}-${tool.time}`}><strong>{tool.name}</strong> · {tool.status || 'completed'} · {tool.summary}</li>)}</ul>}
      </section>

      <section className="projection-card">
        <h2>{t('session.verification')}</h2>
        {session.test_results.length === 0 ? <p>{t('session.noVerification')}</p> : <ul>{session.test_results.map((result) => <li key={`${result.command}-${result.time}`}><StatusBadge tone={result.passed ? 'success' : 'error'}>{result.passed ? t('verification.passed') : t('verification.failed')}</StatusBadge> {result.command} · {result.summary}</li>)}</ul>}
      </section>

      <section className="projection-card">
        <h2>{t('session.approvals')}</h2>
        {approvals.length === 0 ? <p>{t('common.empty')}</p> : approvals.map((approval) => {
          const remaining = Math.max(0, Math.ceil((Date.parse(approval.expires_at) - now) / 1000))
          const expired = remaining === 0
          return (
            <article className="approval-card" key={approval.request_id}>
              <h3>{approval.tool}</h3>
              <p>{approval.resource_paths.join(', ')}</p>
              <p>{approval.args_summary}</p>
              <p>{t('approval.countdown', { count: remaining })}</p>
              <button type="button" className="button button--primary" disabled={expired || !connectivity.canMutate} onClick={() => void decide(approval, 'approve')}>{t('approval.approve')}</button>
              <button type="button" className="button button--danger" disabled={expired || !connectivity.canMutate} onClick={() => void decide(approval, 'deny')}>{t('approval.deny')}</button>
            </article>
          )
        })}
      </section>

      <section className="projection-card">
        <h2>{t('session.activity')}</h2>
        <ul>{eventState.events.map((event, index) => <li key={`${event.sequence}-${event.type}-${index}`}>{event.type}{event.tool ? ` · ${event.tool}` : ''}</li>)}</ul>
      </section>

      <section className="projection-card composer">
        <h2>{t('session.composer')}</h2>
        <label>
          {t('session.message')}
          <textarea value={composer} onChange={(event) => setComposer(event.target.value)} onKeyDown={composerKeyDown} disabled={composerDisabled} />
        </label>
        <small>{new TextEncoder().encode(composer).byteLength} / {MAX_MESSAGE_BYTES}</small>
        <button type="button" className="button button--primary" disabled={composerDisabled || composer.trim() === '' || new TextEncoder().encode(composer).byteLength > MAX_MESSAGE_BYTES} onClick={() => void submitRun()}>{t('session.send')}</button>
      </section>
    </article>
  )
}
