import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'

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

const DRAG_THRESHOLD_PX = 6
const BOARD_DRAGGING_CLASS = 'is-board-dragging'
const BOARD_COLUMN_SELECTOR = '[data-board-column]'

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
  loading: boolean
}

export interface UseTaskBoardOptions {
  board: TaskBoardView | null
  onBoardChange: (next: TaskBoardView) => void
  onRefresh?: ((() => Promise<void>) | (() => void)) | undefined
  resolveTask?: ((taskId: string) => Promise<EmployeeTask | undefined> | EmployeeTask | undefined) | undefined
  onTaskUpdated?: ((task: EmployeeTask) => void) | undefined
}

interface PressState {
  card: TaskBoardCard
  startX: number
  startY: number
  dragging: boolean
  overColumnID: string | null
}

function hitColumnID(clientX: number, clientY: number) {
  const hit = document.elementFromPoint(clientX, clientY)
  const column = hit instanceof Element ? hit.closest(BOARD_COLUMN_SELECTOR) : null
  return column?.getAttribute('data-board-column') ?? null
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
  const pressRef = useRef<PressState | null>(null)
  const detachPressListenersRef = useRef<(() => void) | null>(null)

  useEffect(() => () => {
    detachPressListenersRef.current?.()
    document.body.classList.remove(BOARD_DRAGGING_CLASS)
  }, [])

  function clearDragState() {
    setDraggingID(null)
    setDragOverColumnID(null)
  }

  function onPressStart(card: TaskBoardCard, event: ReactPointerEvent<HTMLDivElement>) {
    if (event.button !== 0 || !event.isPrimary) return
    const target = event.target
    if (target instanceof Element && target.closest('a,button,input,select,textarea,[data-no-drag]')) return
    detachPressListenersRef.current?.()
    const press: PressState = {
      card,
      startX: event.clientX,
      startY: event.clientY,
      dragging: false,
      overColumnID: null,
    }
    pressRef.current = press

    function handleMove(moveEvent: PointerEvent) {
      const active = pressRef.current
      if (active !== press) return
      if (!active.dragging) {
        const distance = Math.hypot(moveEvent.clientX - active.startX, moveEvent.clientY - active.startY)
        if (distance < DRAG_THRESHOLD_PX) return
        active.dragging = true
        setDraggingID(active.card.id)
        document.body.classList.add(BOARD_DRAGGING_CLASS)
      }
      moveEvent.preventDefault()
      active.overColumnID = hitColumnID(moveEvent.clientX, moveEvent.clientY)
      setDragOverColumnID(active.overColumnID)
    }

    function finishPress(drop: boolean) {
      window.removeEventListener('pointermove', handleMove)
      window.removeEventListener('pointerup', handleUp)
      window.removeEventListener('pointercancel', handleCancel)
      detachPressListenersRef.current = null
      const active = pressRef.current
      pressRef.current = null
      document.body.classList.remove(BOARD_DRAGGING_CLASS)
      if (!active?.dragging) return
      if (drop) {
        lastDragEndAtRef.current = Date.now()
        if (active.overColumnID) {
          dropCard(active.card, active.overColumnID)
          return
        }
      }
      clearDragState()
    }

    function handleUp() {
      finishPress(true)
    }

    function handleCancel() {
      finishPress(false)
    }

    window.addEventListener('pointermove', handleMove)
    window.addEventListener('pointerup', handleUp)
    window.addEventListener('pointercancel', handleCancel)
    detachPressListenersRef.current = () => {
      window.removeEventListener('pointermove', handleMove)
      window.removeEventListener('pointerup', handleUp)
      window.removeEventListener('pointercancel', handleCancel)
    }
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

  function loadTask(taskId: string): Promise<EmployeeTask> | EmployeeTask {
    if (resolveTask) {
      const resolved = resolveTask(taskId)
      if (resolved instanceof Promise) {
        return resolved.then((task) => task ?? getEmployeeTask(taskId))
      }
      if (resolved) return resolved
    }
    return getEmployeeTask(taskId)
  }

  function dropCard(card: TaskBoardCard, columnID: string) {
    clearDragState()
    if (columnID === card.column_id) return
    if (card.kind === 'task' && columnID === 'in_progress') {
      const startable = card.state === 'queued' || card.state === 'prepared' || card.state === 'interrupted'
      if (!startable || !card.task_id) {
        if (!startable) actions.showToast({ messageKey: 'tasks.startNotAllowed', tone: 'info' })
        return
      }
      let resolved: Promise<EmployeeTask> | EmployeeTask
      try {
        resolved = loadTask(card.task_id)
      } catch (caught) {
        void reportMutationError(caught)
        return
      }
      if (resolved instanceof Promise) {
        setStartCandidate({ card, task: null, targetColumn: columnID, loading: true })
        void resolved.then(
          (task) => setStartCandidate({ card, task, targetColumn: columnID, loading: false }),
          (caught) => {
            setStartCandidate(null)
            void reportMutationError(caught)
          },
        )
      } else {
        setStartCandidate({ card, task: resolved, targetColumn: columnID, loading: false })
      }
      return
    }
    void saveBoardCard(card, columnID)
  }

  async function confirmStartCandidate() {
    if (!startCandidate?.card.task_id || startCandidate.loading || !startCandidate.task || !connectivity.canMutate || startBusy) return
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
    setStartCandidate,
    setNoteDetail,
    clearDragState,
    onPressStart,
    dropCard,
    confirmStartCandidate,
    suppressClick,
  }
}
