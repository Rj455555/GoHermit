import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
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
let source: FakeEventSource

beforeEach(() => {
  localStorage.clear()
  setEventSourceFactoryForTests(() => {
    source = new FakeEventSource()
    return source
  })
})

afterEach(() => resetSessionEventRegistryForTests())

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
    act(() => {
      for (let index = 0; index < 9; index += 1) {
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
    expect(result.current.two.events.map((event) => event.sequence)).toEqual([1])
    expect(source.closed).toBe(false)
    expect(refreshOne).not.toHaveBeenCalled()
    expect(refreshTwo).not.toHaveBeenCalled()
  })
})
