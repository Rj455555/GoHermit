import { useRef, useState } from 'react'

import {
  getEmployeeTask,
  getTaskBoard,
  resumeEmployeeTask,
  startEmployeeTask,
  updateTaskBoardCard,
} from '../../../api/endpoints'
import { ApiError } from '../../../api/errors'
import type {
  EmployeeTask,
  TaskBoardCard,
  TaskBoardCardInput,
  TaskBoardView,
} from '../../../api/types'
import { useConnectivity } from '../../../components/ConnectivityProvider'
import { useUI } from '../../../state/UIContext'

export function mutationKey(error: unknown) {
  if (error instanceof ApiError && error.code === 'network_error') return 'mutation.offline'
  if (error instanceof ApiError && error.status === 409) return 'mutation.conflict'
  return 'mutation.failed'
}

export function statusColor(status: string) {
  if (['completed', 'approved'].includes(status)) return 'success'
  if (['failed', 'denied'].includes(status)) return 'error'
  if (['cancelled', 'interrupted'].includes(status)) return 'warning'
  if (['running', 'verifying', 'prepared', 'waiting_owner'].includes(status)) return 'processing'
  return 'default'
}

export function boardCardInput(card: TaskBoardCard, columnId = card.column_id, rank = card.rank): TaskBoardCardInput {
  return {
    column_id: columnId,
    rank,
    labels: card.labels,
    priority: card.priority,
    due_at: card.due_at ?? null,
    pinned: card.pinned,
    blocked: card.blocked,
    blocker_reason: card.blocker_reason ?? '',
    depends_on: card.depends_on,
    source_url: card.source_url ?? '',
    loop_id: card.loop_id ?? '',
  }
}

export interface StartCandidate {
  card: TaskBoardCard
  task: EmployeeTask | null
  targetColumn: string
}

export interface UseTaskBoardOptions {
  board: TaskBoardView | null
  onBoardChange: (next: TaskBoardView) => void
  onRefresh?: ((() => Promise<void>) | (() => void)) | undefined
  resolveTask?: ((taskId: string) => Promise<EmployeeTask | undefined> | EmployeeTask | undefined) | undefined
  onTaskUpdated?: ((task: EmployeeTask) => void) | undefined
}

export function useTaskBoard({ board, onBoardChange, onRefresh, resolveTask, onTaskUpdated }: UseTaskBoardOptions) {
  const { actions } = useUI()
  const connectivity = useConnectivity()
  const [draggingID, setDraggingID] = useState<string | null>(null)
  const [dragOverColumnID, setDragOverColumnID] = useState<string | null>(null)
  const [startCandidate, setStartCandidate] = useState<StartCandidate | null>(null)
  const [startBusy, setStartBusy] = useState(false)
  const [noteDetail, setNoteDetail] = useState<TaskBoardCard | null>(null)
  const lastDragEndAtRef = useRef(0)

  function clearDragState() {
    setDraggingID(null)
    setDragOverColumnID(null)
  }

  function endDrag() {
    lastDragEndAtRef.current = Date.now()
    clearDragState()
  }

  async function refreshBoard() {
    if (onRefresh) {
      await onRefresh()
      return
    }
    onBoardChange(await getTaskBoard())
  }

  async function reportMutationError(caught: unknown) {
    actions.showToast({ messageKey: mutationKey(caught), tone: 'error' })
    await refreshBoard().catch(() => undefined)
  }

  async function saveBoardCard(card: TaskBoardCard, columnID: string) {
    if (!connectivity.canMutate || !board) return
    try {
      onBoardChange(await updateTaskBoardCard(card.task_id ?? card.id, boardCardInput(card, columnID, Date.now())))
    } catch (caught) {
      await reportMutationError(caught)
    }
  }

  function loadTask(taskId: string) {
    if (resolveTask) return resolveTask(taskId)
    return getEmployeeTask(taskId).catch(() => undefined)
  }

  function dropCard(card: TaskBoardCard, columnID: string) {
    clearDragState()
    if (columnID === card.column_id) return
    if (card.kind === 'task' && columnID === 'in_progress' && card.state !== 'running' && card.state !== 'verifying') {
      const resolved = card.task_id ? loadTask(card.task_id) : undefined
      if (resolved instanceof Promise) {
        void resolved.then((task) => setStartCandidate({ card, task: task ?? null, targetColumn: columnID }))
      } else {
        setStartCandidate({ card, task: resolved ?? null, targetColumn: columnID })
      }
      return
    }
    void saveBoardCard(card, columnID)
  }

  async function confirmStartCandidate() {
    if (!startCandidate?.card.task_id || !startCandidate.task || !connectivity.canMutate || startBusy) return
    setStartBusy(true)
    try {
      const task = startCandidate.task
      const nextTask = task.state === 'interrupted'
        ? await resumeEmployeeTask(task.id)
        : await startEmployeeTask(task.id)
      onTaskUpdated?.(nextTask)
      onBoardChange(await updateTaskBoardCard(task.id, boardCardInput(startCandidate.card, startCandidate.targetColumn, Date.now())))
      setStartCandidate(null)
    } catch (caught) {
      await reportMutationError(caught)
    } finally {
      setStartBusy(false)
    }
  }

  function suppressClick() {
    return draggingID !== null || Date.now() - lastDragEndAtRef.current < 300
  }

  return {
    draggingID,
    dragOverColumnID,
    startCandidate,
    startBusy,
    noteDetail,
    setDraggingID,
    setDragOverColumnID,
    setStartCandidate,
    setNoteDetail,
    clearDragState,
    endDrag,
    dropCard,
    confirmStartCandidate,
    suppressClick,
  }
}
