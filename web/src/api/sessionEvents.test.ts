import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { RuntimeEvent } from './types'
import {
  getSessionEventDiagnostics,
  reconnectSessionEvents,
  resetSessionEventRegistryForTests,
  setEventSourceFactoryForTests,
  subscribeSessionEvents,
  validatedSessionHighWater,
} from './sessionEvents'

class FakeEventSource {
  readonly listeners = new Map<string, Set<(event: MessageEvent<string>) => void>>()
  closed = false
  onerror: (() => void) | null = null
  onopen: (() => void) | null = null

  constructor(readonly url: string) {}

  addEventListener(type: string, listener: EventListener) {
    const listeners = this.listeners.get(type) ?? new Set()
    listeners.add(listener)
    this.listeners.set(type, listeners)
  }

  close() {
    this.closed = true
  }

  emit(type: string, data: object) {
    for (const listener of this.listeners.get(type) ?? []) {
      listener(new MessageEvent(type, { data: JSON.stringify(data) }))
    }
  }
}

const now = '2026-07-29T08:00:00Z'
const sources: FakeEventSource[] = []

function source(index = 0): FakeEventSource {
  const result = sources[index]
  if (result === undefined) throw new Error('expected EventSource')
  return result
}

function event(overrides: Partial<RuntimeEvent> = {}): RuntimeEvent {
  return {
    type: 'session_updated',
    time: now,
    session_id: 'session-1',
    sequence: 1,
    turn: 0,
    ...overrides,
  }
}

beforeEach(() => {
  localStorage.clear()
  sources.length = 0
  setEventSourceFactoryForTests((url) => {
    const source = new FakeEventSource(url)
    sources.push(source)
    return source
  })
})

afterEach(() => {
  resetSessionEventRegistryForTests()
})

describe('Session EventSource registry', () => {
  it('shares one EventSource across two Run filters and projects by Run', () => {
    const runOne: RuntimeEvent[] = []
    const runTwo: RuntimeEvent[] = []
    const one = subscribeSessionEvents('session-1', 10, { runId: 'run-1', onEvent: (value) => runOne.push(value) })
    const two = subscribeSessionEvents('session-1', 10, { runId: 'run-2', onEvent: (value) => runTwo.push(value) })

    expect(sources).toHaveLength(1)
    source().emit('session_updated', event({ sequence: 1 }))
    source().emit('tool_completed', event({ type: 'tool_completed', sequence: 2, run_id: 'run-1' }))

    expect(runOne.map((value) => value.sequence)).toEqual([1, 2])
    expect(runTwo.map((value) => value.sequence)).toEqual([1])
    one.unsubscribe()
    expect(source().closed).toBe(false)
    two.unsubscribe()
  })

  it('advances the shared high-water before filtering and drops duplicate or descending events', () => {
    const runTwo: RuntimeEvent[] = []
    const subscription = subscribeSessionEvents('session-1', 10, {
      runId: 'run-2',
      onEvent: (value) => runTwo.push(value),
    })

    source().emit('tool_completed', event({ type: 'tool_completed', sequence: 4, run_id: 'run-1' }))
    source().emit('session_updated', event({ sequence: 4 }))
    source().emit('session_updated', event({ sequence: 3 }))

    expect(runTwo).toEqual([])
    expect(localStorage.getItem('gohermit.ui.sseSequence.session-1')).toBe('4')
    expect(getSessionEventDiagnostics('session-1')?.highWater).toBe(4)
    subscription.unsubscribe()
  })

  it('defers final disposal so a StrictMode remount reuses the connection', async () => {
    const first = subscribeSessionEvents('session-1', 0, { onEvent: vi.fn() })
    first.unsubscribe()
    const remount = subscribeSessionEvents('session-1', 0, { onEvent: vi.fn() })
    await Promise.resolve()

    expect(sources).toHaveLength(1)
    expect(source().closed).toBe(false)
    expect(getSessionEventDiagnostics('session-1')?.subscribers).toBe(1)
    remount.unsubscribe()
    await Promise.resolve()
    expect(source().closed).toBe(true)
  })

  it('uses a valid Session high-water and safely resets corrupt or frontier-ahead values', () => {
    localStorage.setItem('gohermit.ui.sseSequence.session-1', '7')
    expect(validatedSessionHighWater('session-1', 9)).toBe(7)

    for (const value of ['-1', '1.5', 'not-a-number', '9007199254740992', '10']) {
      localStorage.setItem('gohermit.ui.sseSequence.session-1', value)
      expect(validatedSessionHighWater('session-1', 9)).toBe(0)
      expect(localStorage.getItem('gohermit.ui.sseSequence.session-1')).toBeNull()
    }
  })

  it('uses after=<savedSequence> and manual reconnect resumes from current high-water', () => {
    localStorage.setItem('gohermit.ui.sseSequence.session-1', '7')
    const subscription = subscribeSessionEvents('session-1', 9, { onEvent: vi.fn() })
    expect(source().url).toBe('/api/sessions/session-1/events?after=7')
    source().emit('session_updated', event({ sequence: 8 }))

    reconnectSessionEvents('session-1')
    expect(source().closed).toBe(true)
    expect(source(1).url).toBe('/api/sessions/session-1/events?after=8')
    subscription.unsubscribe()
  })

  it('delivers sequence-zero model_delta without advancing or persisting high-water', () => {
    const values: RuntimeEvent[] = []
    const subscription = subscribeSessionEvents('session-1', 0, {
      runId: 'run-1',
      onEvent: (value) => values.push(value),
    })
    source().emit('model_delta', event({
      type: 'model_delta',
      sequence: 0,
      run_id: 'run-1',
      turn: 1,
      message: '<em>literal</em>',
    }))
    source().emit('model_delta', event({
      type: 'model_delta',
      sequence: 0,
      run_id: 'run-2',
      turn: 1,
      message: 'other',
    }))

    expect(values).toHaveLength(1)
    expect(values[0]?.message).toBe('<em>literal</em>')
    expect(localStorage.getItem('gohermit.ui.sseSequence.session-1')).toBeNull()
    subscription.unsubscribe()
  })

  it('recovers a fatal connection once without losing high-water, subscribers, or Run filters', async () => {
    const runOne: RuntimeEvent[] = []
    const runTwo: RuntimeEvent[] = []
    const onFatalError = vi.fn()
    const one = subscribeSessionEvents('session-1', 10, {
      runId: 'run-1',
      onEvent: (value) => runOne.push(value),
      onFatalError,
    })
    const two = subscribeSessionEvents('session-1', 10, {
      runId: 'run-2',
      onEvent: (value) => runTwo.push(value),
    })
    source().emit('tool_completed', event({
      type: 'tool_completed',
      sequence: 3,
      run_id: 'run-1',
    }))
    for (let count = 0; count < 5; count += 1) {
      source().emit('task_started', event({ type: 'task_started', sequence: 0 }))
    }

    expect(onFatalError).toHaveBeenCalledOnce()
    expect(source().closed).toBe(true)
    expect(getSessionEventDiagnostics('session-1')).toMatchObject({
      fatal: true,
      highWater: 3,
      subscribers: 2,
    })

    reconnectSessionEvents('session-1')
    expect(sources).toHaveLength(2)
    expect(source(1).url).toBe('/api/sessions/session-1/events?after=3')
    expect(getSessionEventDiagnostics('session-1')).toMatchObject({
      fatal: false,
      highWater: 3,
      subscribers: 2,
    })
    source(1).emit('tool_completed', event({
      type: 'tool_completed',
      sequence: 4,
      run_id: 'run-2',
    }))
    source(1).emit('tool_completed', event({
      type: 'tool_completed',
      sequence: 5,
      run_id: 'run-1',
    }))
    expect(runOne.map((value) => value.sequence)).toEqual([3, 5])
    expect(runTwo.map((value) => value.sequence)).toEqual([4])

    one.unsubscribe()
    two.unsubscribe()
    await Promise.resolve()
  })

  it('reuses fatal connection during remount and reports fatal to every late subscriber', async () => {
    const firstStatuses: string[] = []
    const first = subscribeSessionEvents('session-1', 10, {
      runId: 'run-1',
      onEvent: vi.fn(),
      onStatus: (status) => firstStatuses.push(status),
    })
    source().emit('tool_completed', event({
      type: 'tool_completed',
      sequence: 3,
      run_id: 'run-1',
    }))
    for (let count = 0; count < 5; count += 1) {
      source().emit('task_started', event({ type: 'task_started', sequence: 0 }))
    }
    expect(firstStatuses).toEqual(['fatal'])
    first.unsubscribe()

    const remountStatuses: string[] = []
    const lateStatuses: string[] = []
    const remountEvents: RuntimeEvent[] = []
    const lateEvents: RuntimeEvent[] = []
    const remount = subscribeSessionEvents('session-1', 10, {
      runId: 'run-1',
      onEvent: (value) => remountEvents.push(value),
      onStatus: (status) => remountStatuses.push(status),
    })
    const late = subscribeSessionEvents('session-1', 10, {
      runId: 'run-2',
      onEvent: (value) => lateEvents.push(value),
      onStatus: (status) => lateStatuses.push(status),
    })
    await Promise.resolve()

    expect(sources).toHaveLength(1)
    expect(remountStatuses).toEqual(['fatal'])
    expect(lateStatuses).toEqual(['fatal'])
    expect(getSessionEventDiagnostics('session-1')).toMatchObject({
      fatal: true,
      highWater: 3,
      subscribers: 2,
    })

    reconnectSessionEvents('session-1')
    expect(sources).toHaveLength(2)
    expect(source(1).url).toBe('/api/sessions/session-1/events?after=3')
    expect(getSessionEventDiagnostics('session-1')).toMatchObject({
      fatal: false,
      highWater: 3,
      subscribers: 2,
    })
    source(1).emit('tool_completed', event({
      type: 'tool_completed',
      sequence: 4,
      run_id: 'run-2',
    }))
    expect(remountEvents).toEqual([])
    expect(lateEvents.map((value) => value.sequence)).toEqual([4])

    remount.unsubscribe()
    late.unsubscribe()
    await Promise.resolve()
  })
})
