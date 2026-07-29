import { RUNTIME_EVENT_TYPES, decodeRuntimeEvent } from './decoders'
import type { RuntimeEvent } from './types'

const STORAGE_PREFIX = 'gohermit.ui.sseSequence.'
const INVALID_EVENT_LIMIT = 5

type ConnectionStatus = 'connected' | 'reconnecting' | 'fatal'

interface EventSourceLike {
  onopen: ((event: Event) => void) | null
  onerror: ((event: Event) => void) | null
  addEventListener(type: string, listener: EventListener): void
  close(): void
}

type EventSourceFactory = (url: string) => EventSourceLike

interface Subscriber {
  id: number
  runId?: string | undefined
  onEvent: (event: RuntimeEvent) => void
  onStatus?: (status: ConnectionStatus) => void
  onReconnect?: () => void
  onFatalError?: () => void
}

interface Connection {
  sessionId: string
  highWater: number
  source: EventSourceLike
  subscribers: Map<number, Subscriber>
  status: ConnectionStatus
  hadError: boolean
  fatal: boolean
  invalidEvents: number
  disposalVersion: number
}

const registry = new Map<string, Connection>()
let nextSubscriberID = 0
let eventSourceFactory: EventSourceFactory = (url) => new EventSource(url)

function storageKey(sessionId: string): string {
  return `${STORAGE_PREFIX}${sessionId}`
}

function safeSessionID(sessionId: string): boolean {
  return /^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$/u.test(sessionId) && !sessionId.includes('..')
}

export function validatedSessionHighWater(sessionId: string, frontier: number): number {
  if (!safeSessionID(sessionId) || !Number.isSafeInteger(frontier) || frontier < 0) return 0
  const key = storageKey(sessionId)
  const raw = localStorage.getItem(key)
  if (raw === null) return 0
  const value = Number(raw)
  if (!/^(0|[1-9][0-9]*)$/u.test(raw) || !Number.isSafeInteger(value) || value < 0 || value > frontier) {
    localStorage.removeItem(key)
    return 0
  }
  return value
}

function eventUrl(sessionId: string, after: number): string {
  return `/api/sessions/${encodeURIComponent(sessionId)}/events?after=${after}`
}

function notifyStatus(connection: Connection, status: ConnectionStatus) {
  connection.status = status
  for (const subscriber of connection.subscribers.values()) subscriber.onStatus?.(status)
}

function fatalInvalidStream(connection: Connection) {
  connection.fatal = true
  connection.source.close()
  notifyStatus(connection, 'fatal')
  for (const subscriber of connection.subscribers.values()) subscriber.onFatalError?.()
}

function handlePayload(connection: Connection, payload: string) {
  if (connection.fatal) return
  let event: RuntimeEvent
  try {
    event = decodeRuntimeEvent(JSON.parse(payload) as unknown)
    if (event.session_id !== connection.sessionId) throw new Error('session mismatch')
  } catch {
    connection.invalidEvents += 1
    if (connection.invalidEvents >= INVALID_EVENT_LIMIT) fatalInvalidStream(connection)
    return
  }
  connection.invalidEvents = 0
  if (event.sequence > 0) {
    if (event.sequence <= connection.highWater) return
    connection.highWater = event.sequence
    localStorage.setItem(storageKey(connection.sessionId), String(event.sequence))
  }
  for (const subscriber of connection.subscribers.values()) {
    if (event.run_id !== undefined && subscriber.runId !== undefined && event.run_id !== subscriber.runId) {
      continue
    }
    subscriber.onEvent(event)
  }
}

function openSource(connection: Omit<Connection, 'source'>): EventSourceLike {
  const source = eventSourceFactory(eventUrl(connection.sessionId, connection.highWater))
  source.onopen = () => {
    const recovered = connection.hadError
    connection.hadError = false
    notifyStatus(connection as Connection, 'connected')
    if (recovered) {
      for (const subscriber of connection.subscribers.values()) subscriber.onReconnect?.()
    }
  }
  source.onerror = () => {
    if (connection.fatal) return
    connection.hadError = true
    notifyStatus(connection as Connection, 'reconnecting')
  }
  for (const type of RUNTIME_EVENT_TYPES) {
    source.addEventListener(type, ((message: MessageEvent<string>) => {
      handlePayload(connection as Connection, message.data)
    }) as EventListener)
  }
  return source
}

function createConnection(sessionId: string, frontier: number): Connection {
  const partial: Omit<Connection, 'source'> = {
    sessionId,
    highWater: validatedSessionHighWater(sessionId, frontier),
    subscribers: new Map(),
    status: 'connected',
    hadError: false,
    fatal: false,
    invalidEvents: 0,
    disposalVersion: 0,
  }
  const connection = partial as Connection
  connection.source = openSource(partial)
  registry.set(sessionId, connection)
  return connection
}

export function subscribeSessionEvents(
  sessionId: string,
  frontier: number,
  options: Omit<Subscriber, 'id'>,
): { unsubscribe: () => void } {
  if (!safeSessionID(sessionId) || !Number.isSafeInteger(frontier) || frontier < 0) {
    throw new Error('invalid Session event subscription')
  }
  const connection = registry.get(sessionId) ?? createConnection(sessionId, frontier)
  connection.disposalVersion += 1
  nextSubscriberID += 1
  const subscriber: Subscriber = { ...options, id: nextSubscriberID }
  connection.subscribers.set(subscriber.id, subscriber)
  if (connection.status !== 'connected') subscriber.onStatus?.(connection.status)
  let active = true
  return {
    unsubscribe() {
      if (!active) return
      active = false
      connection.subscribers.delete(subscriber.id)
      if (connection.subscribers.size !== 0) return
      connection.disposalVersion += 1
      const version = connection.disposalVersion
      queueMicrotask(() => {
        if (connection.subscribers.size !== 0 || connection.disposalVersion !== version) return
        connection.source.close()
        registry.delete(sessionId)
      })
    },
  }
}

export function reconnectSessionEvents(sessionId: string) {
  const connection = registry.get(sessionId)
  if (connection === undefined || connection.fatal) return
  connection.source.close()
  connection.hadError = true
  notifyStatus(connection, 'reconnecting')
  connection.source = openSource(connection)
}

export function getSessionEventDiagnostics(sessionId: string) {
  const connection = registry.get(sessionId)
  if (connection === undefined) return undefined
  return {
    highWater: connection.highWater,
    subscribers: connection.subscribers.size,
    status: connection.status,
    fatal: connection.fatal,
  }
}

export function setEventSourceFactoryForTests(factory: EventSourceFactory) {
  eventSourceFactory = factory
}

export function resetSessionEventRegistryForTests() {
  for (const connection of registry.values()) connection.source.close()
  registry.clear()
  eventSourceFactory = (url) => new EventSource(url)
  nextSubscriberID = 0
}
