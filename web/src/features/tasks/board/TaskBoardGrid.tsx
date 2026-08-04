import { useMemo } from 'react'
import {
  Alert,
  Badge,
  Button,
  Descriptions,
  Empty,
  Modal,
  Space,
  Typography,
} from 'antd'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'

import type { EmployeeTask, TaskBoardCard, TaskBoardView } from '../../../api/types'
import { TaskBoardCardView } from './TaskBoardCard'
import { useTaskBoard } from './useTaskBoard'

export interface TaskBoardGridProps {
  board: TaskBoardView
  onBoardChange: (next: TaskBoardView) => void
  onRefresh?: ((() => Promise<void>) | (() => void)) | undefined
  resolveTask?: ((taskId: string) => Promise<EmployeeTask | undefined> | EmployeeTask | undefined) | undefined
  onTaskUpdated?: ((task: EmployeeTask) => void) | undefined
  cards?: TaskBoardCard[] | undefined
  showHiddenColumns?: boolean | undefined
  testIdPrefix?: string | undefined
  onUseNoteAsTask?: ((note: TaskBoardCard) => void) | undefined
}

export function TaskBoardGrid({ board, onBoardChange, onRefresh, resolveTask, onTaskUpdated, cards, showHiddenColumns = false, testIdPrefix = 'task-board', onUseNoteAsTask }: TaskBoardGridProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const boardState = useTaskBoard({ board, onBoardChange, onRefresh, resolveTask, onTaskUpdated })
  const columns = useMemo(() => board.definition.columns.filter((column) => showHiddenColumns || !column.hidden), [board, showHiddenColumns])
  const visibleCards = cards ?? board.cards
  const cardsByColumn = useMemo(() => {
    const grouped = new Map<string, TaskBoardCard[]>()
    for (const card of visibleCards) {
      const columnCards = grouped.get(card.column_id) ?? []
      columnCards.push(card)
      grouped.set(card.column_id, columnCards)
    }
    for (const columnCards of grouped.values()) columnCards.sort((left, right) => left.rank - right.rank || Date.parse(left.authoritative_updated_at) - Date.parse(right.authoritative_updated_at))
    return grouped
  }, [visibleCards])

  function activateCard(card: TaskBoardCard) {
    if (card.kind === 'note') {
      boardState.setNoteDetail(card)
      return
    }
    if (card.session_id) {
      void navigate(`/agent/sessions/${encodeURIComponent(card.session_id)}`)
      return
    }
    if (card.task_id) void navigate(`/tasks/${encodeURIComponent(card.task_id)}`)
  }

  const startCandidate = boardState.startCandidate
  const startTask = startCandidate?.task ?? null
  const noteDetail = boardState.noteDetail
  return <>
    <div className="task-board-scroll" data-testid={testIdPrefix}>
      {columns.map((column) => {
        const columnCards = cardsByColumn.get(column.id) ?? []
        return <section
          key={column.id}
          data-testid={`${testIdPrefix}-column-${column.id}`}
          className={`task-board-column${boardState.dragOverColumnID === column.id ? ' is-drop-target' : ''}`}
          onDragOver={(event) => {
            if (!boardState.draggingID) return
            event.preventDefault()
            const transfer = event.dataTransfer as DataTransfer | undefined
            if (transfer) transfer.dropEffect = 'move'
            if (boardState.dragOverColumnID !== column.id) boardState.setDragOverColumnID(column.id)
          }}
          onDragLeave={(event) => {
            const related = event.relatedTarget
            if (related instanceof Node && event.currentTarget.contains(related)) return
            if (boardState.dragOverColumnID === column.id) boardState.setDragOverColumnID(null)
          }}
          onDrop={(event) => {
            event.preventDefault()
            const card = visibleCards.find((item) => item.id === boardState.draggingID)
            if (card) boardState.dropCard(card, column.id)
            else boardState.clearDragState()
          }}
        >
          <header className="task-board-column__header"><Space><span className="task-board-column__swatch" style={{ background: column.color }} /><Typography.Text strong>{column.title}</Typography.Text><Badge count={columnCards.length} showZero /></Space>{column.wip_limit ? <Typography.Text type="secondary">/{column.wip_limit}</Typography.Text> : null}</header>
          <div className="task-board-column__cards">
            {columnCards.map((card) => <TaskBoardCardView
              key={card.id}
              card={card}
              isDragging={boardState.draggingID === card.id}
              suppressClick={boardState.suppressClick}
              onDragStart={(event, dragged) => {
                const transfer = event.dataTransfer as DataTransfer | undefined
                transfer?.setData('text/plain', dragged.id)
                if (transfer) transfer.effectAllowed = 'move'
                boardState.setDraggingID(dragged.id)
              }}
              onDragEnd={boardState.endDrag}
              onActivate={activateCard}
              {...(onUseNoteAsTask ? { onUseNoteAsTask } : {})}
            />)}
            {columnCards.length === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('tasks.emptyColumn')} /> : null}
          </div>
        </section>
      })}
    </div>
    <Modal open={Boolean(startCandidate)} title={t('tasks.startConfirmationTitle')} okText={t('tasks.start')} cancelText={t('actions.cancel')} confirmLoading={boardState.startBusy} onCancel={() => boardState.setStartCandidate(null)} onOk={() => void boardState.confirmStartCandidate()}>
      {startCandidate && startTask ? <Space direction="vertical" size={16} style={{ width: '100%' }}><Alert type="warning" showIcon message={t('tasks.startConfirmation')} /><Descriptions bordered column={1} size="small"><Descriptions.Item label={t('tasks.employee')}>{startCandidate.card.employee_name || startTask.employee_id}</Descriptions.Item><Descriptions.Item label={t('tasks.model')}>{[startCandidate.card.provider, startCandidate.card.model].filter(Boolean).join(' / ') || '—'}</Descriptions.Item><Descriptions.Item label={t('tasks.workspace')}>{startTask.project_binding.workspace_fingerprint}</Descriptions.Item><Descriptions.Item label={t('tasks.skills')}>{startTask.skills.map((skill) => `${skill.skill_id}@${skill.version}`).join(', ') || '—'}</Descriptions.Item><Descriptions.Item label={t('tasks.permissions')}>{startTask.policy.allowed_capabilities.join(', ') || '—'} · {startTask.policy.network_allowed ? t('tasks.networkAllowed') : t('tasks.networkDisabled')}</Descriptions.Item><Descriptions.Item label={t('tasks.writerLease')}>{startTask.project_binding.mutation_allowed ? t('tasks.writerLeaseRequired') : t('tasks.readOnlyWorkspace')}</Descriptions.Item></Descriptions></Space> : null}
    </Modal>
    <Modal
      open={Boolean(noteDetail)}
      title={t('tasks.noteDetails')}
      onCancel={() => boardState.setNoteDetail(null)}
      footer={<Space>
        {noteDetail && onUseNoteAsTask ? <Button type="primary" onClick={() => { boardState.setNoteDetail(null); onUseNoteAsTask(noteDetail) }}>{t('tasks.useAsTask')}</Button> : null}
        <Button onClick={() => boardState.setNoteDetail(null)}>{t('actions.dismiss')}</Button>
      </Space>}
    >
      {noteDetail ? <Space direction="vertical" size={8} style={{ width: '100%' }}><Typography.Title level={4} style={{ marginTop: 0 }}>{noteDetail.title}</Typography.Title>{noteDetail.body ? <Typography.Paragraph className="safe-wrap" style={{ marginBottom: 0 }}>{noteDetail.body}</Typography.Paragraph> : null}</Space> : null}
    </Modal>
  </>
}
