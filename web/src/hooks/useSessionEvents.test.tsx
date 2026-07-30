import { act, renderHook } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { RuntimeEvent } from '../api/types'

interface SubscriberOptions {
  onEvent: (event: RuntimeEvent) => void
  onStatus: (status: 'connected' | 'reconnecting' | 'fatal') => void
  onReconnect: () => void
  onFatalError: () => void
}

const stream = vi.hoisted(() => ({
  reconnectSessionEvents: vi.fn(),
  subscribeSessionEvents: vi.fn<
    (sessionId: string, frontier: number, options: SubscriberOptions) => { unsubscribe: () => void }
  >(),
  unsubscribe: vi.fn(),
  options: undefined as SubscriberOptions | undefined,
}))
vi.mock('../api/sessionEvents', () => ({
  reconnectSessionEvents: stream.reconnectSessionEvents,
  subscribeSessionEvents: stream.subscribeSessionEvents,
}))

import { useSessionEvents } from './useSessionEvents'

const now = '2026-07-29T08:00:00Z'
function event(type: RuntimeEvent['type'], overrides: Partial<RuntimeEvent> = {}): RuntimeEvent {
  return {
    type,
    time: now,
    session_id: 'session-1',
    run_id: 'run-1',
    sequence: type === 'model_delta' ? 0 : 1,
    turn: 1,
    ...overrides,
  }
}

beforeEach(() => {
  stream.unsubscribe.mockReset()
  stream.reconnectSessionEvents.mockReset()
  stream.subscribeSessionEvents.mockReset().mockImplementation((_session, _frontier, options) => {
    stream.options = options
    return { unsubscribe: stream.unsubscribe }
  })
})

describe('useSessionEvents projection', () => {
  it('switches Session ownership cleanly and delegates explicit reconnect', () => {
    const refresh = vi.fn()
    const { result, rerender } = renderHook(
      ({ sessionId }) => useSessionEvents({ sessionId, frontier: 3, runId: 'run-1', onRefresh: refresh }),
      { initialProps: { sessionId: 'session-1' } },
    )
    expect(stream.subscribeSessionEvents).toHaveBeenCalledWith(
      'session-1',
      3,
      expect.objectContaining({ runId: 'run-1' }),
    )

    rerender({ sessionId: 'session-2' })
    expect(stream.unsubscribe).toHaveBeenCalledOnce()
    expect(stream.subscribeSessionEvents).toHaveBeenLastCalledWith(
      'session-2',
      3,
      expect.objectContaining({ runId: 'run-1' }),
    )

    act(() => result.current.reconnect())
    expect(stream.reconnectSessionEvents).toHaveBeenCalledWith('session-2')
  })

  it('keeps deltas ephemeral, refreshes authoritative checkpoints, and clears on reconnect', () => {
    const refresh = vi.fn()
    const { result } = renderHook(() =>
      useSessionEvents({ sessionId: 'session-1', frontier: 0, runId: 'run-1', onRefresh: refresh }),
    )

    act(() => {
      stream.options?.onEvent(event('model_started'))
      stream.options?.onEvent(event('model_delta', { message: '<b>literal</b>' }))
    })
    expect(result.current.streamingText).toBe('<b>literal</b>')

    act(() => stream.options?.onEvent(event('approval_requested', { sequence: 2 })))
    expect(refresh).toHaveBeenCalledOnce()
    expect(result.current.streamingText).toBe('<b>literal</b>')

    act(() => stream.options?.onReconnect())
    expect(result.current.streamingText).toBe('')
    expect(refresh).toHaveBeenCalledTimes(2)
  })

  it('keeps 100 maximum deltas out of Activity and clears local truncation from terminal truth', () => {
    const refresh = vi.fn()
    const { result } = renderHook(() =>
      useSessionEvents({ sessionId: 'session-1', frontier: 0, runId: 'run-1', onRefresh: refresh }),
    )
    const chunk = 'x'.repeat(32 << 10)
    act(() => {
      for (let index = 0; index < 100; index += 1) {
        stream.options?.onEvent(event('model_delta', { message: chunk }))
      }
      stream.options?.onEvent(event('tool_completed', {
        sequence: 1,
        tool: 'literal_tool',
        message: chunk,
        error: chunk,
      }))
    })

    expect(result.current.truncated).toBe(true)
    expect(result.current.fatal).toBe(false)
    expect(result.current.status).toBe('connected')
    expect(new TextEncoder().encode(result.current.streamingText).byteLength).toBe(256 << 10)
    expect(result.current.events).toHaveLength(1)
    expect(result.current.events[0]).toMatchObject({
      type: 'tool_completed',
      sequence: 1,
      tool: 'literal_tool',
    })
    expect(result.current.events.some((value) => value.type === 'model_delta')).toBe(false)
    expect(result.current.events.some((value) => value.message === chunk || value.error === chunk)).toBe(false)
    const truncatedText = result.current.streamingText

    act(() => {
      stream.options?.onEvent(event('model_delta', { message: 'ignored' }))
      stream.options?.onEvent(event('model_completed', { sequence: 2, message: chunk }))
    })
    expect(result.current.streamingText).toBe('')
    expect(result.current.streamingText).not.toBe(`${truncatedText}ignored`)
    expect(result.current.truncated).toBe(false)
    expect(result.current.events.some((value) => value.message === chunk)).toBe(false)
    expect(refresh).toHaveBeenCalledOnce()
    expect(stream.reconnectSessionEvents).not.toHaveBeenCalled()
  })

  it('derives fatal UI from status even when this subscriber did not witness the fatal event', () => {
    const { result } = renderHook(() =>
      useSessionEvents({ sessionId: 'session-1', frontier: 0, runId: 'run-1', onRefresh: vi.fn() }),
    )
    act(() => stream.options?.onStatus('fatal'))
    expect(result.current.fatal).toBe(true)

    act(() => result.current.reconnect())
    expect(result.current.fatal).toBe(false)
    expect(result.current.status).toBe('reconnecting')
    expect(stream.reconnectSessionEvents).toHaveBeenCalledOnce()
  })
})
