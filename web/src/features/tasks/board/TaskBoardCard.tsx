import type { PointerEvent } from 'react'
import { Button, Card, Space, Tag, Tooltip, Typography } from 'antd'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import type { TaskBoardCard } from '../../../api/types'
import { translatedEnum } from '../../../i18n/enumLabel'
import { statusColor } from './useTaskBoard'

const MAX_VISIBLE_LABELS = 2

export interface TaskBoardCardViewProps {
  card: TaskBoardCard
  isDragging: boolean
  suppressClick: () => boolean
  onPressStart: (card: TaskBoardCard, event: PointerEvent<HTMLDivElement>) => void
  onActivate: (card: TaskBoardCard) => void
  onUseNoteAsTask?: ((card: TaskBoardCard) => void) | undefined
}

export function TaskBoardCardView({ card, isDragging, suppressClick, onPressStart, onActivate, onUseNoteAsTask }: TaskBoardCardViewProps) {
  const { t } = useTranslation()
  const destinationKey = card.kind === 'note'
    ? 'tasks.openNote'
    : card.session_id ? 'tasks.openSessionForTask' : 'tasks.openTaskDetail'

  function activate() {
    if (suppressClick()) return
    onActivate(card)
  }

  const visibleLabels = card.labels.slice(0, MAX_VISIBLE_LABELS)
  const hiddenLabelCount = card.labels.length - visibleLabels.length

  return <div
    className={`task-board-card${card.blocked ? ' is-blocked' : ''}${card.pinned ? ' is-pinned' : ''}${isDragging ? ' is-dragging' : ''}`}
    onPointerDown={(event) => onPressStart(card, event)}
    onClick={activate}
    onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); activate() } }}
    role="link"
    tabIndex={0}
    aria-label={t(destinationKey, { title: card.title })}
  >
    <Card size="small" title={<span className="task-board-card__title">{card.title}</span>} extra={card.kind === 'note' ? <Tag color="default">{t('tasks.note')}</Tag> : <Tag color={statusColor(card.state ?? 'queued')}>{translatedEnum(t, 'taskStatus', card.state ?? 'queued')}</Tag>}>
      <Space direction="vertical" size={8} style={{ width: '100%' }}>
        <Space wrap size={[4, 4]}>
          {card.employee_name ? <Tag>{card.employee_name}</Tag> : null}
          {card.provider || card.model ? <Tag color="blue">{[card.provider, card.model].filter(Boolean).join(' / ')}</Tag> : null}
          {card.priority > 0 ? <Tag color="gold">P{card.priority}</Tag> : null}
          {card.blocked ? <Tag color="error">{t('tasks.blocked')}</Tag> : null}
          {card.approval_status !== 'none' ? <Tag color="warning">{t('tasks.approval')}: {card.approval_status}</Tag> : null}
          {card.source_url?.startsWith('task-board://notes/') ? <Tag color="purple">{t('tasks.sourceNote')}</Tag> : null}
        </Space>
        {card.kind === 'note' && card.body ? <Typography.Paragraph ellipsis={{ rows: 3 }} className="safe-wrap" style={{ marginBottom: 0 }}>{card.body}</Typography.Paragraph> : null}
        {card.kind === 'note' && onUseNoteAsTask ? <Button type="link" size="small" onClick={(event) => { event.stopPropagation(); onUseNoteAsTask(card) }}>{t('tasks.useAsTask')}</Button> : null}
        {card.labels.length > 0 ? <Space wrap size={[4, 4]}>{visibleLabels.map((label) => <Tag key={label}>{label}</Tag>)}{hiddenLabelCount > 0 ? <Tag>{`+${hiddenLabelCount}`}</Tag> : null}</Space> : null}
        {card.loop_id ? <Link to={`/loops/${encodeURIComponent(card.loop_id)}`} onClick={(event) => event.stopPropagation()}>{t('tasks.loop')}: {card.loop_id}</Link> : null}
        <Space className="task-board-card__meta" split="·" size={4} wrap>
          <Typography.Text type="secondary">{card.kind === 'task' ? card.id : t('tasks.note')}</Typography.Text>
          {card.session_count > 0 ? <Typography.Text type="secondary">{t('tasks.sessions')}: {card.session_count}</Typography.Text> : null}
          {card.session_event_sequence > 0 ? <Typography.Text type="secondary">{t('tasks.events')}: {card.session_event_sequence}</Typography.Text> : null}
          {card.stale ? <span onClick={(event) => event.stopPropagation()}><Tooltip title={t('tasks.staleProjection')}><Tag color="warning">{t('tasks.stale')}</Tag></Tooltip></span> : null}
        </Space>
      </Space>
    </Card>
  </div>
}
