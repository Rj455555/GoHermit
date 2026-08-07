import { useCallback, useEffect, useMemo, useState } from 'react'
import { BellRing, CheckCircle2, MessageCircle, RefreshCw, Send, TriangleAlert } from 'lucide-react'
import { Alert, Button, Card, Empty, Select, Space, Spin, Tabs, Tag, Typography } from 'antd'
import { Link, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'

import { getWeixinAccounts, getWeixinConversation, listReports, retryReport } from '../../api/endpoints'
import type { ReportRecord, WeixinAccount, WeixinConversationItem } from '../../api/types'
import { PageHeader } from '../../components/PageHeader'
import { useConnectivity } from '../../components/ConnectivityProvider'
import { useUI } from '../../state/UIContext'

function deliveryColor(status: ReportRecord['delivery_status']): 'success' | 'warning' | 'default' {
  if (status === 'sent') return 'success'
  if (status === 'failed') return 'warning'
  return 'default'
}

function conversationStateColor(state: string): 'success' | 'processing' | 'warning' | 'default' {
  if (state === 'sent' || state === 'queued') return 'success'
  if (state === 'pending' || state === 'received') return 'processing'
  if (state === 'unknown') return 'warning'
  return 'default'
}

function maskConversationID(value: string): string {
  if (value.length <= 4) return '••••'
  return `${value.slice(0, 2)}••••${value.slice(-2)}`
}

export function ReportsPage() {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const connectivity = useConnectivity()
  const { actions } = useUI()
  const [reports, setReports] = useState<ReportRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [retrying, setRetrying] = useState<string | null>(null)
  const [accounts, setAccounts] = useState<WeixinAccount[]>([])
  const [accountsLoading, setAccountsLoading] = useState(true)
  const [accountsError, setAccountsError] = useState(false)
  const [accountID, setAccountID] = useState('')
  const [conversation, setConversation] = useState<WeixinConversationItem[]>([])
  const [conversationLoading, setConversationLoading] = useState(false)
  const [conversationError, setConversationError] = useState(false)

  const loadReports = useCallback(async (signal?: AbortSignal) => {
    setLoading(true)
    try {
      const result = await listReports(signal ? { signal } : {})
      if (!signal?.aborted) { setReports(result.reports); setError(false) }
    } catch {
      if (!signal?.aborted) setError(true)
    } finally {
      if (!signal?.aborted) setLoading(false)
    }
  }, [])

  const loadAccounts = useCallback(async (signal?: AbortSignal) => {
    setAccountsLoading(true)
    try {
      const result = await getWeixinAccounts(signal ? { signal } : {})
      if (signal?.aborted) return
      setAccounts(result.accounts)
      setAccountsError(false)
      setAccountID((current) => result.accounts.some((account) => account.id === current) ? current : (result.accounts[0]?.id ?? ''))
    } catch {
      if (!signal?.aborted) setAccountsError(true)
    } finally {
      if (!signal?.aborted) setAccountsLoading(false)
    }
  }, [])

  const loadConversation = useCallback(async (selectedAccountID: string, signal?: AbortSignal) => {
    if (!selectedAccountID) {
      setConversation([])
      return
    }
    setConversationLoading(true)
    try {
      const result = await getWeixinConversation(selectedAccountID, signal ? { signal } : {})
      if (!signal?.aborted) { setConversation(result.items); setConversationError(false) }
    } catch {
      if (!signal?.aborted) setConversationError(true)
    } finally {
      if (!signal?.aborted) setConversationLoading(false)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void loadReports(controller.signal)
    void loadAccounts(controller.signal)
    return () => controller.abort()
  }, [connectivity.generation, loadAccounts, loadReports])

  useEffect(() => {
    const controller = new AbortController()
    void loadConversation(accountID, controller.signal)
    return () => controller.abort()
  }, [accountID, connectivity.generation, loadConversation])

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

  const reportHistory = (
    <div className="report-center-panel">
      {error ? <Alert type="error" showIcon message={t('reports.loadFailed')} action={<Button size="small" onClick={() => void loadReports()}>{t('common.retry')}</Button>} /> : null}
      {loading ? <Card className="report-center-loading"><Spin /></Card> : null}
      {!loading && !error && reports.length === 0 ? (
        <Card className="report-center-empty"><Empty image={<Send size={28} aria-hidden="true" />} description={<><Typography.Title level={4}>{t('reports.emptyTitle')}</Typography.Title><Typography.Text type="secondary">{t('reports.emptyDescription')}</Typography.Text></>} /></Card>
      ) : null}
      {!loading && !error && reports.length > 0 ? (
        <Card className="report-center-list" title={t('reports.history')} extra={<Typography.Text type="secondary">{reports.length}</Typography.Text>} aria-label={t('reports.history')}>
          <div className="report-center-items">
            {reports.map((report) => (
              <article className="report-center-item" key={report.id}>
                <div className="report-center-item__status" aria-hidden="true">
                  {report.delivery_status === 'sent' ? <CheckCircle2 size={18} /> : report.delivery_status === 'failed' ? <TriangleAlert size={18} /> : <RefreshCw size={18} />}
                </div>
                <div className="report-center-item__body">
                  <div className="report-center-item__heading"><strong>{report.title}</strong><Tag color={deliveryColor(report.delivery_status)}>{t(`reports.delivery.${report.delivery_status}`)}</Tag></div>
                  <p>{report.summary || t('reports.noSummary')}</p>
                  <small>{report.source_type === 'employee_task' ? t('reports.employeeTask') : t('reports.loop')} · {report.status} · {new Date(report.updated_at).toLocaleString(i18n.language)}</small>
                  {report.last_error ? <div className="report-center-item__error">{report.last_error}</div> : null}
                </div>
                {report.delivery_status !== 'sent' ? <Button type="default" size="small" loading={retrying === report.id} disabled={!connectivity.canMutate} onClick={() => void retry(report)}>{t('reports.retry')}</Button> : null}
              </article>
            ))}
          </div>
        </Card>
      ) : null}
    </div>
  )

  const accountOptions = useMemo(
    () => accounts.map((account) => ({ value: account.id, label: account.label || account.id })),
    [accounts],
  )
  const conversationTimeline = (
    <Card className="weixin-conversation-card" title={t('reports.conversationTitle')}>
      <div className="weixin-conversation-toolbar">
        <Select
          aria-label={t('reports.weixinAccount')}
          loading={accountsLoading}
          options={accountOptions}
          placeholder={t('reports.chooseAccount')}
          value={accountID || undefined}
          onChange={(value) => setAccountID(value ?? '')}
        />
        <Button icon={<RefreshCw size={16} aria-hidden="true" />} disabled={!accountID || conversationLoading} loading={conversationLoading} onClick={() => void loadConversation(accountID)}>
          {t('common.refresh')}
        </Button>
      </div>
      <Typography.Paragraph type="secondary" className="weixin-conversation-description">
        {t('reports.conversationDescription')}
      </Typography.Paragraph>
      {accountsError || conversationError ? <Alert type="error" showIcon message={t('reports.conversationLoadFailed')} /> : null}
      {accountsLoading || conversationLoading ? <div className="weixin-conversation-loading"><Spin /></div> : null}
      {!accountsLoading && !accountsError && accounts.length === 0 ? (
        <Empty description={t('reports.noWeixinAccount')}>
          <Button type="primary" onClick={() => void navigate('/settings')}>{t('reports.configureWeixin')}</Button>
        </Empty>
      ) : null}
      {!conversationLoading && accountID && !conversationError && conversation.length === 0 ? <Empty description={t('reports.noConversation')} /> : null}
      {!conversationLoading && !conversationError && conversation.length > 0 ? (
        <div className="weixin-conversation-timeline" aria-live="polite">
          {conversation.map((item) => (
            <article className={`weixin-message weixin-message--${item.direction}`} key={item.id}>
              <header>
                <Space size={8} wrap>
                  <Tag color={item.direction === 'outbound' ? 'blue' : 'default'}>{t(`reports.direction.${item.direction}`)}</Tag>
                  <Typography.Text type="secondary">{maskConversationID(item.group_id || item.peer_id)}</Typography.Text>
                </Space>
                <time dateTime={item.time}>{new Intl.DateTimeFormat(i18n.language, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(item.time))}</time>
              </header>
              <p>{item.text}</p>
              <footer>
                <Tag color={conversationStateColor(item.state)}>{t(`reports.messageState.${item.state}`, item.state)}</Tag>
                {item.task_id ? <Link to={`/tasks/${encodeURIComponent(item.task_id)}`}>Task · {item.task_id}</Link> : null}
              </footer>
            </article>
          ))}
        </div>
      ) : null}
    </Card>
  )

  return (
    <article className="feature-page report-center-page">
      <PageHeader title={t('reports.title')} description={t('reports.description')} />
      <Card className="report-center-overview">
        <div className="report-center-overview__icon" aria-hidden="true"><BellRing size={22} /></div>
        <div><Typography.Text strong>{t('reports.unifiedTitle')}</Typography.Text><Typography.Paragraph type="secondary">{t('reports.unifiedDescription')}</Typography.Paragraph></div>
        <Tag color="blue">{t('reports.wechatChannel')}</Tag>
      </Card>
      <Tabs
        className="report-center-tabs"
        items={[
          { key: 'reports', label: <Space size={7}><BellRing size={16} aria-hidden="true" />{t('reports.reportTab')}</Space>, children: reportHistory },
          { key: 'conversation', label: <Space size={7}><MessageCircle size={16} aria-hidden="true" />{t('reports.conversationTab')}</Space>, children: conversationTimeline },
        ]}
      />
    </article>
  )
}
