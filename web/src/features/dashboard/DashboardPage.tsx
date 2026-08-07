import { useEffect, useMemo, useRef, useState } from 'react'
import { Alert, Button, Card, Col, Empty, List, Row, Skeleton, Space, Statistic, Tag, Typography } from 'antd'
import {
  Bot,
  Plus,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'

import { listLoopInvocations, getInfo, listLoops, listSessions } from '../../api/endpoints'
import type { Info, InvocationSummary, LoopSummary, SessionSummary } from '../../api/types'
import { ErrorState } from '../../components/ErrorState'
import { PageHeader } from '../../components/PageHeader'
import { useConnectivity } from '../../components/ConnectivityProvider'
import { translatedEnum } from '../../i18n/enumLabel'
import { DashboardTaskBoard } from './DashboardTaskBoard'

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
  const navigate = useNavigate()
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
    return <Card className="feature-page dashboard-page" loading={false}>
      <span role="status" className="sr-only">{t('common.loading')}</span>
      <Skeleton active paragraph={{ rows: 5 }} />
    </Card>
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
            <Button type="primary" onClick={() => { void navigate('/employees') }} icon={<Plus size={16} aria-hidden="true" />}>{t('pages.employees.title')}</Button>
            <Button href="/agent" icon={<Bot size={16} aria-hidden="true" />}>{t('agent.newSession')}</Button>
          </div>
        )}
      />
      <Space direction="vertical" size={16} className="dashboard-content-stack" aria-label={t('dashboard.summary')}>
        {error || connectivity.status === 'offline' ? <Alert className="stale-notice" type="warning" showIcon message={t('connectivity.stale')} /> : null}
        <Row gutter={[16, 16]} className="dashboard-summary">
          <Col xs={24} sm={12} xl={6}><Card><Statistic title={t('dashboard.active')} value={projection.info.active ? t('dashboard.running') : t('dashboard.idle')} /></Card></Col>
          <Col xs={24} sm={12} xl={6}><Card><Statistic title={t('dashboard.readyAccess')} value={availableAccess} /></Card></Col>
          <Col xs={24} sm={12} xl={6}><Card><Statistic title={t('dashboard.workspace')} value={projection.info.workspace} valueStyle={{ fontSize: 14 }} /></Card></Col>
          <Col xs={24} sm={12} xl={6}><Card><Statistic title="Owner" value={ownerName} /></Card></Col>
        </Row>
        <DashboardTaskBoard />
        <Card className="dashboard-hero-card" aria-label={t('dashboard.summary')}>
          <Typography.Text className="dashboard-hero-eyebrow">NEXT BEST ACTION</Typography.Text>
          <Typography.Title level={2}>{recent ? (recentLoop?.name ?? recent.loop_id) : t('dashboard.idle')}</Typography.Title>
          <Typography.Paragraph>{recent ? translatedEnum(t, 'invocationStatus', recent.status) : t('dashboard.noRecent')}</Typography.Paragraph>
          <Space className="dashboard-hero-actions" size={8} wrap>
            {recent ? <Button type="primary" href={`/loops/${encodeURIComponent(recent.loop_id)}`}>{t('employees.openEmployee')}</Button> : null}
            <Button href="/agent">{t('agent.newSession')}</Button>
          </Space>
        </Card>
        <Row gutter={[16, 16]} className="dashboard-metrics">
          <Col xs={24} sm={12} xl={6}><Card><Statistic title={t('dashboard.loopDefinitions')} value={projection.loops.length} /></Card></Col>
          <Col xs={24} sm={12} xl={6}><Card><Statistic title={t('dashboard.active')} value={counts.active} formatter={(value) => <span data-testid="invocation-active">{value}</span>} /></Card></Col>
          <Col xs={24} sm={12} xl={6}><Card><Statistic title={t('dashboard.completed')} value={counts.completed} formatter={(value) => <span data-testid="invocation-completed">{value}</span>} /></Card></Col>
          <Col xs={24} sm={12} xl={6}><Card><Statistic title={t('dashboard.failed')} value={counts.failed + counts.interrupted} formatter={(value) => <span data-testid="invocation-failed">{value}</span>} /></Card></Col>
        </Row>
        <Row gutter={[16, 16]} className="dashboard-recent-row">
          <Col xs={24} xl={16}>
            <Card title={t('dashboard.recent')} extra={<Tag color="blue">{projection.loops.length}</Tag>}>
            <List
              dataSource={projection.loops.slice(0, 2)}
              locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('common.empty')} /> }}
              renderItem={(loop: LoopSummary) => {
                const invocation = projection.invocations.find((item) => item.loop_id === loop.id)
                return (
                  <List.Item
                    actions={[<Button key="open" href={`/loops/${encodeURIComponent(loop.id)}`}>{t('employees.openEmployee')}</Button>]}
                  >
                    <div className="row-title-group">
                      <Typography.Text strong><Link className="row-title" to={`/loops/${encodeURIComponent(loop.id)}`}>{loop.name}</Link></Typography.Text>
                      <Typography.Text type="secondary" ellipsis>{loop.id}</Typography.Text>
                    </div>
                    <Tag color={invocation?.status === 'failed' ? 'error' : 'processing'}>{invocation ? translatedEnum(t, 'invocationStatus', invocation.status) : t('common.empty')}</Tag>
                  </List.Item>
                )
              }}
            />
            </Card>
          </Col>
          <Col xs={24} xl={8}>
            <Card title={t('dashboard.readyAccess')}>
            <List
              dataSource={[
                { name: 'GoHermit API', detail: `active · v${projection.info.version}`, ok: true },
                { name: projection.info.model?.provider ?? 'Provider', detail: projection.info.model?.api_key_configured ? t('common.yes') : t('common.no'), ok: Boolean(projection.info.model?.api_key_configured) },
                { name: 'Owner', detail: ownerName, ok: true },
                { name: t('dashboard.activeSession'), detail: activeSession?.title ?? t('common.empty'), ok: true },
              ]}
              renderItem={(item) => <List.Item><List.Item.Meta avatar={<Tag color={item.ok ? 'success' : 'error'}>{item.ok ? 'OK' : '!'}</Tag>} title={item.name} description={item.detail} /></List.Item>}
            />
            </Card>
          </Col>
        </Row>
      </Space>
    </article>
  )
}
