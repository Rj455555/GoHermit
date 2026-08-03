import { useEffect, useMemo, useState } from 'react'
import { Alert, Badge, Button, Card, Empty, Skeleton, Space, Tag, Typography } from 'antd'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { getTaskBoard } from '../../api/endpoints'
import { ApiError } from '../../api/errors'
import type { TaskBoardCard, TaskBoardView } from '../../api/types'
import { useConnectivity } from '../../components/ConnectivityProvider'
import { translatedEnum } from '../../i18n/enumLabel'

function statusColor(status: string) {
  if (['completed', 'approved'].includes(status)) return 'success'
  if (['failed', 'denied'].includes(status)) return 'error'
  if (['cancelled', 'interrupted'].includes(status)) return 'warning'
  if (['running', 'verifying', 'prepared', 'waiting_owner'].includes(status)) return 'processing'
  return 'default'
}

function DashboardTaskBoardCard({ card }: { card: TaskBoardCard }) {
  const { t } = useTranslation()
  const title = card.task_id
    ? <Link to={`/tasks/${encodeURIComponent(card.task_id)}`}>{card.title}</Link>
    : <Typography.Text strong>{card.title}</Typography.Text>

  return (
    <div className={`task-board-card dashboard-task-board-card${card.blocked ? ' is-blocked' : ''}${card.pinned ? ' is-pinned' : ''}`}>
      <Card size="small" title={<Typography.Text ellipsis={{ tooltip: card.title }}>{title}</Typography.Text>} extra={card.kind === 'note' ? <Tag>{t('tasks.note')}</Tag> : <Tag color={statusColor(card.state ?? 'queued')}>{translatedEnum(t, 'taskStatus', card.state ?? 'queued')}</Tag>}>
        <Space direction="vertical" size={8} style={{ width: '100%' }}>
          <Space wrap size={[4, 4]}>
            {card.employee_name ? <Tag>{card.employee_name}</Tag> : null}
            {card.provider || card.model ? <Tag color="blue">{[card.provider, card.model].filter(Boolean).join(' / ')}</Tag> : null}
            {card.priority > 0 ? <Tag color="gold">P{card.priority}</Tag> : null}
            {card.blocked ? <Tag color="error">{t('tasks.blocked')}</Tag> : null}
            {card.approval_status !== 'none' ? <Tag color="warning">{t('tasks.approval')}: {card.approval_status}</Tag> : null}
          </Space>
          {card.kind === 'note' && card.body ? <Typography.Paragraph ellipsis={{ rows: 3 }} className="safe-wrap" style={{ marginBottom: 0 }}>{card.body}</Typography.Paragraph> : null}
          {card.labels.length > 0 ? <Space wrap size={[4, 4]}>{card.labels.map((label) => <Tag key={label}>{label}</Tag>)}</Space> : null}
          {card.loop_id ? <Link to={`/loops/${encodeURIComponent(card.loop_id)}`}>{t('tasks.loop')}: {card.loop_id}</Link> : null}
          <Space split="·" size={4} wrap>
            <Typography.Text type="secondary">{card.task_id ?? card.id}</Typography.Text>
            {card.session_count > 0 ? <Typography.Text type="secondary">{t('tasks.sessions')}: {card.session_count}</Typography.Text> : null}
            {card.session_event_sequence > 0 ? <Typography.Text type="secondary">{t('tasks.events')}: {card.session_event_sequence}</Typography.Text> : null}
            {card.stale ? <Tag color="warning">{t('tasks.stale')}</Tag> : null}
          </Space>
        </Space>
      </Card>
    </div>
  )
}

export function DashboardTaskBoard() {
  const { t } = useTranslation()
  const connectivity = useConnectivity()
  const [board, setBoard] = useState<TaskBoardView | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(false)
    void getTaskBoard({ signal: controller.signal })
      .then((next) => {
        if (!controller.signal.aborted) setBoard(next)
      })
      .catch((caught) => {
        if (!(caught instanceof ApiError && caught.code === 'aborted') && !controller.signal.aborted) setError(true)
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [connectivity.generation])

  const columns = useMemo(() => (board?.definition.columns ?? []).filter((column) => !column.hidden), [board])
  const cardsByColumn = useMemo(() => {
    const grouped = new Map<string, TaskBoardCard[]>()
    for (const card of board?.cards ?? []) {
      const cards = grouped.get(card.column_id) ?? []
      cards.push(card)
      grouped.set(card.column_id, cards)
    }
    for (const cards of grouped.values()) cards.sort((left, right) => left.rank - right.rank || Date.parse(left.authoritative_updated_at) - Date.parse(right.authoritative_updated_at))
    return grouped
  }, [board])

  return (
    <Card
      className="dashboard-task-board"
      title={<Space><span>{t('dashboard.taskBoard')}</span><Badge count={board?.cards.length ?? 0} showZero /></Space>}
      extra={<Button type="link" href="/tasks?view=board">{t('dashboard.openTaskBoard')}</Button>}
    >
      <Typography.Paragraph type="secondary" className="dashboard-task-board__description">{t('dashboard.taskBoardDescription')}</Typography.Paragraph>
      {loading ? <Skeleton active paragraph={{ rows: 4 }} /> : null}
      {!loading && error ? <Alert type="warning" showIcon message={t('dashboard.taskBoardUnavailable')} description={t('common.retryDescription')} /> : null}
      {!loading && !error && board ? <div className="task-board-scroll" data-testid="dashboard-task-board">
        {columns.map((column) => {
          const cards = cardsByColumn.get(column.id) ?? []
          return <section key={column.id} data-testid={`dashboard-task-board-column-${column.id}`} className="task-board-column">
            <header className="task-board-column__header"><Space><span className="task-board-column__swatch" style={{ background: column.color }} /><Typography.Text strong>{column.title}</Typography.Text><Badge count={cards.length} showZero /></Space>{column.wip_limit ? <Typography.Text type="secondary">/{column.wip_limit}</Typography.Text> : null}</header>
            <div className="task-board-column__cards">
              {cards.map((card) => <DashboardTaskBoardCard key={card.id} card={card} />)}
              {cards.length === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('tasks.emptyColumn')} /> : null}
            </div>
          </section>
        })}
      </div> : null}
    </Card>
  )
}
