import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  getSessionEventDiagnostics,
  resetSessionEventRegistryForTests,
  setEventSourceFactoryForTests,
} from '../api/sessionEvents'
import type { RuntimeEvent } from '../api/types'
import { useSessionEvents } from './useSessionEvents'

class FakeEventSource {
  readonly listeners = new Map<string, Set<(event: MessageEvent<string>) => void>>()
  closed = false
  onerror: (() => void) | null = null
  onopen: (() => void) | null = null

  addEventListener(type: string, listener: EventListener) {
    const listeners = this.listeners.get(type) ?? new Set()
    listeners.add(listener)
    this.listeners.set(type, listeners)
  }

  close() {
    this.closed = true
  }

  emit(type: string, value: RuntimeEvent) {
    for (const listener of this.listeners.get(type) ?? []) {
      listener(new MessageEvent(type, { data: JSON.stringify(value) }))
    }
  }
}

const now = '2026-07-29T08:00:00Z'
const sources: FakeEventSource[] = []

beforeEach(() => {
  localStorage.clear()
  setEventSourceFactoryForTests(() => {
    const source = new FakeEventSource()
    sources.push(source)
    return source
  })
})

afterEach(() => {
  resetSessionEventRegistryForTests()
  sources.length = 0
})

describe('subscriber-local model stream truncation', () => {
  it('does not close the shared connection or block another Run-filtered subscriber', () => {
    const refreshOne = vi.fn()
    const refreshTwo = vi.fn()
    const { result } = renderHook(() => ({
      one: useSessionEvents({
        sessionId: 'session-1',
        frontier: 0,
        runId: 'run-1',
        onRefresh: refreshOne,
      }),
      two: useSessionEvents({
        sessionId: 'session-1',
        frontier: 0,
        runId: 'run-2',
        onRefresh: refreshTwo,
      }),
    }))
    const chunk = 'x'.repeat(32 << 10)
    const source = sources[0]
    if (source === undefined) throw new Error('expected shared EventSource')
    act(() => {
      for (let index = 0; index < 100; index += 1) {
        source.emit('model_delta', {
          type: 'model_delta',
          time: now,
          session_id: 'session-1',
          run_id: 'run-1',
          sequence: 0,
          turn: 1,
          message: chunk,
        })
      }
      source.emit('tool_completed', {
        type: 'tool_completed',
        time: now,
        session_id: 'session-1',
        run_id: 'run-2',
        sequence: 1,
        turn: 1,
      })
    })

    expect(result.current.one.truncated).toBe(true)
    expect(new TextEncoder().encode(result.current.one.streamingText).byteLength).toBe(256 << 10)
    expect(result.current.one.events).toEqual([])
    expect(result.current.two.events.map((event) => event.sequence)).toEqual([1])
    expect(sources).toHaveLength(1)
    expect(source.closed).toBe(false)
    expect(refreshOne).not.toHaveBeenCalled()
    expect(refreshTwo).not.toHaveBeenCalled()
  })

  it('restores fatal UI across StrictMode-style remount and one shared replacement source', async () => {
    const first = renderHook(() => useSessionEvents({
      sessionId: 'session-1',
      frontier: 10,
      runId: 'run-1',
      onRefresh: vi.fn(),
    }))
    const original = sources[0]
    if (original === undefined) throw new Error('expected original EventSource')
    act(() => {
      original.emit('tool_completed', {
        type: 'tool_completed',
        time: now,
        session_id: 'session-1',
        run_id: 'run-1',
        sequence: 3,
        turn: 1,
      })
      for (let count = 0; count < 5; count += 1) {
        original.emit('task_started', {
          type: 'task_started',
          time: now,
          session_id: 'session-1',
          sequence: 0,
          turn: 1,
        })
      }
    })
    expect(first.result.current.fatal).toBe(true)
    first.unmount()

    const remount = renderHook(() => useSessionEvents({
      sessionId: 'session-1',
      frontier: 10,
      runId: 'run-1',
      onRefresh: vi.fn(),
    }))
    const late = renderHook(() => useSessionEvents({
      sessionId: 'session-1',
      frontier: 10,
      runId: 'run-2',
      onRefresh: vi.fn(),
    }))
    expect(sources).toHaveLength(1)
    expect(remount.result.current.status).toBe('fatal')
    expect(remount.result.current.fatal).toBe(true)
    expect(late.result.current.status).toBe('fatal')
    expect(late.result.current.fatal).toBe(true)
    expect(getSessionEventDiagnostics('session-1')).toMatchObject({
      fatal: true,
      highWater: 3,
      subscribers: 2,
    })

    act(() => remount.result.current.reconnect())
    expect(sources).toHaveLength(2)
    expect(original.closed).toBe(true)
    expect(getSessionEventDiagnostics('session-1')).toMatchObject({
      fatal: false,
      highWater: 3,
      subscribers: 2,
    })
    const replacement = sources[1]
    if (replacement === undefined) throw new Error('expected replacement EventSource')
    act(() => {
      replacement.emit('tool_completed', {
        type: 'tool_completed',
        time: now,
        session_id: 'session-1',
        run_id: 'run-2',
        sequence: 4,
        turn: 1,
      })
    })
    expect(remount.result.current.events).toEqual([])
    expect(late.result.current.events.map((value) => value.sequence)).toEqual([4])
    await act(async () => Promise.resolve())
    expect(sources).toHaveLength(2)
  })
})
