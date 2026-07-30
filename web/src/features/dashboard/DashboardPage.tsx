import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Activity,
  Bot,
  CheckCircle2,
  FolderGit2,
  Plus,
  Workflow,
  XCircle,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { listLoopInvocations, getInfo, listLoops, listSessions } from '../../api/endpoints'
import type { Info, InvocationSummary, LoopSummary, SessionSummary } from '../../api/types'
import { ErrorState } from '../../components/ErrorState'
import { PageHeader } from '../../components/PageHeader'
import { useConnectivity } from '../../components/ConnectivityProvider'
import { translatedEnum } from '../../i18n/enumLabel'

interface DashboardProjection {
  info: Info
  loops: LoopSummary[]
  sessions: SessionSummary[]
  invocations: InvocationSummary[]
}

async function loadInvocations(
  loops: LoopSummary[],
  signal: AbortSignal,
): Promise<InvocationSummary[]> {
  const queue = loops.slice(0, 20)
  const results: InvocationSummary[] = []
  let index = 0
  const workers = Array.from({ length: Math.min(3, queue.length) }, async () => {
    while (index < queue.length) {
      const loop = queue[index]
      index += 1
      if (loop === undefined) return
      const response = await listLoopInvocations(loop.id, { signal })
      results.push(...response.invocations)
    }
  })
  await Promise.all(workers)
  return results
}

export function DashboardPage() {
  const { t } = useTranslation()
  const connectivity = useConnectivity()
  const [projection, setProjection] = useState<DashboardProjection | null>(null)
  const [error, setError] = useState(false)
  const requestVersion = useRef(0)

  useEffect(() => {
    const controller = new AbortController()
    requestVersion.current += 1
    const version = requestVersion.current
    async function load() {
      try {
        const [infoResult, loopResult, sessionResult] = await Promise.allSettled([
          getInfo({ signal: controller.signal }),
          listLoops({ signal: controller.signal }),
          listSessions({ signal: controller.signal }),
        ])
        if (infoResult.status === 'rejected') throw infoResult.reason
        const loops = loopResult.status === 'fulfilled' ? loopResult.value.loops : []
        const sessions = sessionResult.status === 'fulfilled' ? sessionResult.value.sessions : []
        let invocations: InvocationSummary[] = []
        let invocationFailed = false
        try {
          invocations = await loadInvocations(loops, controller.signal)
        } catch {
          invocationFailed = true
        }
        if (controller.signal.aborted || requestVersion.current !== version) return
        setProjection({
          info: infoResult.value,
          loops,
          sessions,
          invocations,
        })
        setError(
          loopResult.status === 'rejected'
          || sessionResult.status === 'rejected'
          || invocationFailed,
        )
      } catch {
        if (!controller.signal.aborted) setError(true)
      }
    }
    void load()
    return () => controller.abort()
  }, [connectivity.generation])

  const counts = useMemo(() => {
    const values = { active: 0, completed: 0, failed: 0, interrupted: 0 }
    for (const invocation of projection?.invocations ?? []) {
      if (['prepared', 'dispatched', 'attached'].includes(invocation.status)) values.active += 1
      else if (invocation.status === 'completed') values.completed += 1
      else if (invocation.status === 'failed' || invocation.status === 'blocked') values.failed += 1
      else if (invocation.status === 'cancelled' || invocation.status === 'skipped') values.interrupted += 1
    }
    return values
  }, [projection])

  if (projection === null && error) {
    return <ErrorState title={t('dashboard.errorTitle')} description={t('dashboard.errorDescription')} />
  }
  if (projection === null) {
    return <p role="status">{t('common.loading')}</p>
  }
  const activeSession = projection.sessions.find((session) => session.active_run_id !== undefined)
  const recent = [...projection.invocations].sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at))[0]
  return (
    <article className="feature-page dashboard-page">
      <PageHeader
        title={t('pages.dashboard.title')}
        description={t('dashboard.description')}
        actions={(
          <div className="button-row">
            <Link className="button button--secondary" to="/employees">
              <Bot size={16} aria-hidden="true" />
              {t('pages.employees.title')}
            </Link>
            <Link className="button button--primary" to="/agent">
              <Plus size={16} aria-hidden="true" />
              {t('agent.newSession')}
            </Link>
          </div>
        )}
      />
      {error || connectivity.status === 'offline' ? (
        <p className="stale-notice">{t('connectivity.stale')}</p>
      ) : null}
      <section className="dashboard-hero" aria-label={t('dashboard.summary')}>
        <div className="dashboard-hero__workspace">
          <span className="section-kicker">{t('dashboard.workspace')}</span>
          <strong>{projection.info.workspace}</strong>
          <p>
            <span className={`live-dot${projection.info.active ? ' live-dot--busy' : ''}`} />
            {projection.info.active ? t('dashboard.running') : t('dashboard.idle')}
          </p>
        </div>
        <div className="dashboard-hero__facts">
          <div><Bot size={18} aria-hidden="true" /><span>{t('dashboard.readyAccess')}</span><strong>{projection.info.available_companies.reduce((total, company) => total + company.access.length, 0)}</strong></div>
          <div><Workflow size={18} aria-hidden="true" /><span>{t('dashboard.loops')}</span><strong>{projection.loops.length}</strong></div>
          <div><Activity size={18} aria-hidden="true" /><span>{t('dashboard.active')}</span><strong data-testid="invocation-active">{counts.active}</strong></div>
        </div>
      </section>

      <section className="dashboard-status-strip" aria-label={t('dashboard.summary')}>
        <div><CheckCircle2 size={17} aria-hidden="true" /><span>{t('dashboard.completed')}</span><strong data-testid="invocation-completed">{counts.completed}</strong></div>
        <div><XCircle size={17} aria-hidden="true" /><span>{t('dashboard.failed')}</span><strong data-testid="invocation-failed">{counts.failed}</strong></div>
        <div><Activity size={17} aria-hidden="true" /><span>{t('dashboard.interrupted')}</span><strong data-testid="invocation-interrupted">{counts.interrupted}</strong></div>
      </section>

      <section className="dashboard-columns">
        <section className="projection-card dashboard-primary-panel">
          <div className="panel-heading">
            <div>
              <span className="section-kicker">{t('dashboard.recent')}</span>
              <h2>{recent ? projection.loops.find((loop) => loop.id === recent.loop_id)?.name ?? recent.loop_id : t('common.empty')}</h2>
            </div>
            {recent ? <span className="status-badge">{translatedEnum(t, 'invocationStatus', recent.status)}</span> : null}
          </div>
          {recent ? <p>{translatedEnum(t, 'invocationStatus', recent.status)}</p> : <p>{t('dashboard.noRecent')}</p>}
        </section>
        <div className="dashboard-stack">
          <section className="projection-card">
            <div className="panel-heading">
              <div>
                <span className="section-kicker">{t('dashboard.activeSession')}</span>
                <h2>{activeSession?.title ?? t('common.empty')}</h2>
              </div>
              <Bot size={19} aria-hidden="true" />
            </div>
          </section>
          <section className="projection-card">
            <div className="panel-heading">
              <div>
                <span className="section-kicker">{t('dashboard.loopDefinitions')}</span>
                <h2>{projection.loops.length}</h2>
              </div>
              <FolderGit2 size={19} aria-hidden="true" />
            </div>
            {projection.loops.length === 0 ? <p>{t('common.empty')}</p> : (
              <ul className="compact-list">{projection.loops.map((loop) => <li key={loop.id}>{loop.name}</li>)}</ul>
            )}
          </section>
        </div>
      </section>
    </article>
  )
}
