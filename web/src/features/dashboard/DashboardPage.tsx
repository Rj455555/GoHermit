import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { listLoopInvocations, getInfo, listLoops, listSessions } from '../../api/endpoints'
import type { Info, InvocationSummary, LoopSummary, SessionSummary } from '../../api/types'
import { ErrorState } from '../../components/ErrorState'
import { PageHeader } from '../../components/PageHeader'
import { useConnectivity } from '../../components/ConnectivityProvider'

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
        const [info, loopResponse, sessionResponse] = await Promise.all([
          getInfo({ signal: controller.signal }),
          listLoops({ signal: controller.signal }),
          listSessions({ signal: controller.signal }),
        ])
        const invocations = await loadInvocations(loopResponse.loops, controller.signal)
        if (controller.signal.aborted || requestVersion.current !== version) return
        setProjection({
          info,
          loops: loopResponse.loops,
          sessions: sessionResponse.sessions,
          invocations,
        })
        setError(false)
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
      <PageHeader title={t('pages.dashboard.title')} description={t('dashboard.description')} />
      {error || connectivity.status === 'offline' ? (
        <p className="stale-notice">{t('connectivity.stale')}</p>
      ) : null}
      <section className="metric-grid" aria-label={t('dashboard.summary')}>
        <article><span>{t('dashboard.workspace')}</span><strong>{projection.info.workspace}</strong></article>
        <article><span>{t('dashboard.readyAccess')}</span><strong>{projection.info.available_companies.reduce((total, company) => total + company.access.length, 0)}</strong></article>
        <article><span>{t('dashboard.loops')}</span><strong>{projection.loops.length}</strong></article>
        <article><span>{t('dashboard.active')}</span><strong data-testid="invocation-active">{counts.active}</strong></article>
        <article><span>{t('dashboard.completed')}</span><strong data-testid="invocation-completed">{counts.completed}</strong></article>
        <article><span>{t('dashboard.failed')}</span><strong data-testid="invocation-failed">{counts.failed}</strong></article>
        <article><span>{t('dashboard.interrupted')}</span><strong data-testid="invocation-interrupted">{counts.interrupted}</strong></article>
      </section>
      <section className="projection-card">
        <h2>{t('dashboard.recent')}</h2>
        {recent ? <p>{projection.loops.find((loop) => loop.id === recent.loop_id)?.name ?? recent.loop_id} · {recent.status}</p> : <p>{t('common.empty')}</p>}
      </section>
      <section className="projection-card">
        <h2>{t('dashboard.activeSession')}</h2>
        <p>{activeSession?.title ?? t('common.empty')}</p>
      </section>
      <section className="projection-card">
        <h2>{t('dashboard.loopDefinitions')}</h2>
        {projection.loops.length === 0 ? <p>{t('common.empty')}</p> : (
          <ul>{projection.loops.map((loop) => <li key={loop.id}>{loop.name}</li>)}</ul>
        )}
      </section>
    </article>
  )
}
