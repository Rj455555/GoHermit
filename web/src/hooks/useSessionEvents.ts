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
  const [streamingText, setStreamingText] = useState('')
  const [events, setEvents] = useState<RuntimeEvent[]>([])
  const [status, setStatus] = useState<'connected' | 'reconnecting' | 'fatal'>('connected')
  const [fatal, setFatal] = useState(false)

  useEffect(() => {
    refreshRef.current = onRefresh
  }, [onRefresh])

  useEffect(() => {
    setStreamingText('')
    setEvents([])
    setFatal(false)
    setStatus('connected')
    if (sessionId === undefined) return
    const subscription = subscribeSessionEvents(sessionId, frontier, {
      runId,
      onEvent(event) {
        setEvents((current) => [...current.slice(-(MAX_ACTIVITY_EVENTS - 1)), event])
        if (event.type === 'model_started') {
          setStreamingText('')
        } else if (event.type === 'model_delta') {
          setStreamingText((current) => {
            const next = current + (event.message ?? '')
            if (new TextEncoder().encode(next).byteLength > MAX_STREAM_BUFFER_BYTES) {
              setFatal(true)
              setStatus('fatal')
              return current
            }
            return next
          })
        }
        if (REFRESH_EVENT_TYPES.has(event.type)) {
          refreshRef.current()
          if (event.type !== 'approval_requested') setStreamingText('')
        }
      },
      onStatus: setStatus,
      onReconnect() {
        setStreamingText('')
        refreshRef.current()
      },
      onFatalError() {
        setFatal(true)
        setStatus('fatal')
      },
    })
    return subscription.unsubscribe
  }, [frontier, runId, sessionId])

  const reconnect = useCallback(() => {
    if (sessionId !== undefined) reconnectSessionEvents(sessionId)
  }, [sessionId])

  return { streamingText, events, status, fatal, reconnect }
}
