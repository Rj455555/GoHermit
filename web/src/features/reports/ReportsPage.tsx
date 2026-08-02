import { useCallback, useEffect, useState } from 'react'
import { BellRing, CheckCircle2, RefreshCw, Send, TriangleAlert } from 'lucide-react'
import { Alert, Button, Card, Empty, Spin, Tag, Typography } from 'antd'
import { useTranslation } from 'react-i18next'

import { listReports, retryReport } from '../../api/endpoints'
import type { ReportRecord } from '../../api/types'
import { PageHeader } from '../../components/PageHeader'
import { useConnectivity } from '../../components/ConnectivityProvider'
import { useUI } from '../../state/UIContext'

function deliveryColor(status: ReportRecord['delivery_status']): 'success' | 'warning' | 'default' {
  if (status === 'sent') return 'success'
  if (status === 'failed') return 'warning'
  return 'default'
}

export function ReportsPage() {
  const { t } = useTranslation()
  const connectivity = useConnectivity()
  const { actions } = useUI()
  const [reports, setReports] = useState<ReportRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [retrying, setRetrying] = useState<string | null>(null)

  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      const result = await listReports(signal ? { signal } : {})
      if (!signal?.aborted) { setReports(result.reports); setError(false) }
    } catch {
      if (!signal?.aborted) setError(true)
    } finally {
      if (!signal?.aborted) setLoading(false)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load, connectivity.generation])

  async function retry(report: ReportRecord) {
    if (!connectivity.canMutate || retrying) return
    setRetrying(report.id)
    try {
      const updated = await retryReport(report.id)
      setReports((current) => current.map((item) => item.id === updated.id ? updated : item))
      actions.showToast({ messageKey: 'reports.retrySuccess', tone: 'success' })
    } catch {
      actions.showToast({ messageKey: 'reports.retryFailed', tone: 'error' })
    } finally {
      setRetrying(null)
    }
  }

  return (
    <article className="feature-page report-center-page">
      <PageHeader
        title={t('reports.title')}
        description={t('reports.description')}
      />
      <Card className="report-center-overview">
        <div className="report-center-overview__icon" aria-hidden="true"><BellRing size={22} /></div>
        <div><Typography.Text strong>{t('reports.unifiedTitle')}</Typography.Text><Typography.Paragraph type="secondary">{t('reports.unifiedDescription')}</Typography.Paragraph></div>
        <Tag color="blue">{t('reports.wechatChannel')}</Tag>
      </Card>
      {error ? <Alert type="error" showIcon message={t('reports.loadFailed')} action={<Button size="small" onClick={() => { setLoading(true); void load() }}>{t('common.retry')}</Button>} /> : null}
      {loading ? <Card><Spin tip={t('common.loading')} /></Card> : null}
      {!loading && !error && reports.length === 0 ? (
        <Card className="report-center-empty"><Empty image={<Send size={28} aria-hidden="true" />} description={<><Typography.Title level={4}>{t('reports.emptyTitle')}</Typography.Title><Typography.Text type="secondary">{t('reports.emptyDescription')}</Typography.Text></>} /></Card>
      ) : null}
      {!loading && !error && reports.length > 0 ? (
        <Card className="report-center-list" title={t('reports.history')} extra={<Typography.Text type="secondary">{reports.length}</Typography.Text>} aria-label={t('reports.history')}>
          <header className="section-heading-row"><div><span className="loop-kicker">DELIVERY LOG</span><h2>{t('reports.history')}</h2></div><span className="muted">{reports.length}</span></header>
          <div className="report-center-items">
            {reports.map((report) => (
              <article className="report-center-item" key={report.id}>
                <div className="report-center-item__status" aria-hidden="true">
                  {report.delivery_status === 'sent' ? <CheckCircle2 size={18} /> : report.delivery_status === 'failed' ? <TriangleAlert size={18} /> : <RefreshCw size={18} />}
                </div>
                <div className="report-center-item__body">
                  <div className="report-center-item__heading"><strong>{report.title}</strong><Tag color={deliveryColor(report.delivery_status)}>{t(`reports.delivery.${report.delivery_status}`)}</Tag></div>
                  <p>{report.summary || t('reports.noSummary')}</p>
                  <small>{report.source_type === 'employee_task' ? t('reports.employeeTask') : t('reports.loop')} · {report.status} · {new Date(report.updated_at).toLocaleString()}</small>
                  {report.last_error ? <div className="report-center-item__error">{report.last_error}</div> : null}
                </div>
                {report.delivery_status !== 'sent' ? <Button type="default" size="small" loading={retrying === report.id} disabled={!connectivity.canMutate} onClick={() => void retry(report)}>{t('reports.retry')}</Button> : null}
              </article>
            ))}
          </div>
        </Card>
      ) : null}
    </article>
  )
}
