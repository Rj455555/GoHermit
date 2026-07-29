import { useCallback, useEffect, useRef, useState } from 'react'

import {
  reconnectSessionEvents,
  subscribeSessionEvents,
} from '../api/sessionEvents'
import type { RuntimeEvent } from '../api/types'

const MAX_STREAM_BUFFER_BYTES = 256 << 10
const MAX_ACTIVITY_EVENTS = 100
const REFRESH_EVENT_TYPES = new Set([
  'model_completed',
  'checkpoint_saved',
  'task_completed',
  'task_failed',
  'task_cancelled',
  'run_interrupted',
  'plan_created',
  'plan_updated',
  'approval_requested',
  'approval_decided',
  'approval_expired',
  'approval_consumed',
])

function activityProjection(event: RuntimeEvent): RuntimeEvent {
  return {
    type: event.type,
    time: event.time,
    session_id: event.session_id,
    run_id: event.run_id,
    mission_id: event.mission_id,
    work_item_id: event.work_item_id,
    agent_id: event.agent_id,
    plan_step_id: event.plan_step_id,
    sequence: event.sequence,
    turn: event.turn,
    tool: event.tool,
  }
}

export function useSessionEvents({
  sessionId,
  frontier,
  runId,
  onRefresh,
}: {
  sessionId?: string | undefined
  frontier: number
  runId?: string | undefined
  onRefresh: () => void
}) {
  const refreshRef = useRef(onRefresh)
  const streamTruncatedRef = useRef(false)
  const [streamingText, setStreamingText] = useState('')
  const [events, setEvents] = useState<RuntimeEvent[]>([])
  const [status, setStatus] = useState<'connected' | 'reconnecting' | 'fatal'>('connected')
  const [truncated, setTruncated] = useState(false)

  useEffect(() => {
    refreshRef.current = onRefresh
  }, [onRefresh])

  useEffect(() => {
    setStreamingText('')
    setEvents([])
    setTruncated(false)
    streamTruncatedRef.current = false
    setStatus('connected')
    if (sessionId === undefined) return
    const subscription = subscribeSessionEvents(sessionId, frontier, {
      runId,
      onEvent(event) {
        if (event.type === 'model_delta') {
          if (streamTruncatedRef.current) return
          setStreamingText((current) => {
            const next = current + (event.message ?? '')
            if (new TextEncoder().encode(next).byteLength > MAX_STREAM_BUFFER_BYTES) {
              streamTruncatedRef.current = true
              setTruncated(true)
              return current
            }
            return next
          })
          return
        }
        setEvents((current) => [
          ...current.slice(-(MAX_ACTIVITY_EVENTS - 1)),
          activityProjection(event),
        ])
        if (event.type === 'model_started') {
          setStreamingText('')
          setTruncated(false)
          streamTruncatedRef.current = false
        }
        if (REFRESH_EVENT_TYPES.has(event.type)) {
          refreshRef.current()
          if (event.type !== 'approval_requested') {
            setStreamingText('')
            setTruncated(false)
            streamTruncatedRef.current = false
          }
        }
      },
      onStatus: setStatus,
      onReconnect() {
        setStreamingText('')
        setTruncated(false)
        streamTruncatedRef.current = false
        refreshRef.current()
      },
      onFatalError() {
        setStatus('fatal')
      },
    })
    return subscription.unsubscribe
  }, [frontier, runId, sessionId])

  const reconnect = useCallback(() => {
    if (sessionId !== undefined) {
      setStatus('reconnecting')
      reconnectSessionEvents(sessionId)
    }
  }, [sessionId])

  return {
    streamingText,
    events,
    status,
    fatal: status === 'fatal',
    truncated,
    reconnect,
  }
}
