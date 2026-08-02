import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Bot,
  Plus,
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
  const availableAccess = projection.info.available_companies.reduce((total, company) => total + company.access.length, 0)
  const recentLoop = recent ? projection.loops.find((loop) => loop.id === recent.loop_id) : undefined
  const ownerName = projection.info.owner?.display_name ?? 'Owner'
  return (
    <article className="feature-page dashboard-page prototype-page">
      <PageHeader
        title={t('pages.dashboard.title')}
        description={t('dashboard.description')}
        actions={(
          <div className="button-row">
            <Link className="button button--primary" to="/employees"><Plus size={16} aria-hidden="true" />{t('pages.employees.title')}</Link>
            <Link className="button button--secondary" to="/agent"><Bot size={16} aria-hidden="true" />{t('agent.newSession')}</Link>
          </div>
        )}
      />
      {error || connectivity.status === 'offline' ? <p className="stale-notice">{t('connectivity.stale')}</p> : null}
      <div className="status-strip" aria-label={t('dashboard.summary')}>
        <div className="status-item"><span>{t('dashboard.active')}</span><strong>{projection.info.active ? t('dashboard.running') : t('dashboard.idle')}</strong></div>
        <div className="status-item"><span>{t('dashboard.readyAccess')}</span><strong>{availableAccess}</strong></div>
        <div className="status-item"><span>{t('dashboard.workspace')}</span><strong>{projection.info.workspace}</strong></div>
        <div className="status-item"><span>Owner</span><strong>{ownerName}</strong></div>
      </div>
      <section className="hero" aria-label={t('dashboard.summary')}>
        <p className="eyebrow">NEXT BEST ACTION</p>
        <h2>{recent ? (recentLoop?.name ?? recent.loop_id) : t('dashboard.idle')}</h2>
        <p>{recent ? translatedEnum(t, 'invocationStatus', recent.status) : t('dashboard.noRecent')}</p>
        <div className="actions">
          {recent ? <Link className="button button--primary" to={`/loops/${encodeURIComponent(recent.loop_id)}`}>{t('employees.openEmployee')}</Link> : null}
          <Link className="button button--secondary" to="/agent">{t('agent.newSession')}</Link>
        </div>
      </section>
      <div className="grid-4 prototype-metric-grid">
        <section className="panel"><div className="metric"><strong>{projection.loops.length}</strong><span>{t('dashboard.loopDefinitions')}</span></div><p className="muted tiny">{t('dashboard.loops')}</p></section>
        <section className="panel"><div className="metric"><strong data-testid="invocation-active">{counts.active}</strong><span>{t('dashboard.active')}</span></div><p className="muted tiny">{t('dashboard.activeSession')}</p></section>
        <section className="panel"><div className="metric"><strong data-testid="invocation-completed">{counts.completed}</strong><span>{t('dashboard.completed')}</span></div><p className="muted tiny">{t('dashboard.recent')}</p></section>
        <section className="panel"><div className="metric"><strong data-testid="invocation-failed">{counts.failed + counts.interrupted}</strong><span>{t('dashboard.failed')}</span></div><p className="muted tiny">{t('dashboard.interrupted')}</p></section>
      </div>
      <div className="split-8-4">
        <section className="panel">
          <div className="panel-head"><div><h2>{t('dashboard.recent')}</h2><p>{t('dashboard.description')}</p></div></div>
          <div className="table-wrap"><table><thead><tr><th>{t('dashboard.loopDefinitions')}</th><th>{t('dashboard.active')}</th><th>{t('dashboard.completed')}</th><th>{t('employees.openEmployee')}</th></tr></thead><tbody>
            {projection.loops.slice(0, 2).map((loop) => {
              const invocation = projection.invocations.find((item) => item.loop_id === loop.id)
              return <tr key={loop.id}><td><Link className="row-title" to={`/loops/${encodeURIComponent(loop.id)}`}>{loop.name}</Link><span className="row-sub">{loop.id}</span></td><td>{invocation ? translatedEnum(t, 'invocationStatus', invocation.status) : t('common.empty')}</td><td>{invocation?.created_at ?? '—'}</td><td><Link className="button button--secondary" to={`/loops/${encodeURIComponent(loop.id)}`}>{t('employees.openEmployee')}</Link></td></tr>
            })}
            {!projection.loops.length ? <tr><td colSpan={4}>{t('common.empty')}</td></tr> : null}
          </tbody></table></div>
        </section>
        <aside className="panel">
          <div className="panel-head"><div><h2>{t('dashboard.readyAccess')}</h2><p>{t('dashboard.description')}</p></div></div>
          <ul className="readiness-list">
            <li className="readiness-item"><span className="readiness-icon">✓</span><div><strong>GoHermit API</strong><div className="muted tiny">active · v{projection.info.version}</div></div></li>
            <li className="readiness-item"><span className={`readiness-icon${projection.info.model?.api_key_configured ? '' : ' fail'}`}>{projection.info.model?.api_key_configured ? '✓' : '!'}</span><div><strong>{projection.info.model?.provider ?? 'Provider'}</strong><div className="muted tiny">{projection.info.model?.api_key_configured ? t('common.yes') : t('common.no')}</div></div></li>
            <li className="readiness-item"><span className="readiness-icon">✓</span><div><strong>Owner</strong><div className="muted tiny">{ownerName}</div></div></li>
            <li className="readiness-item"><span className="readiness-icon">✓</span><div><strong>{t('dashboard.activeSession')}</strong><div className="muted tiny">{activeSession?.title ?? t('common.empty')}</div></div></li>
          </ul>
        </aside>
      </div>
    </article>
  )
}
