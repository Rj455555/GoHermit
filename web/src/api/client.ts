import { ApiError } from './errors'

const DEFAULT_MAX_RESPONSE_BYTES = 1 << 20
const SESSION_MAX_RESPONSE_BYTES = 16 << 20

export type Decoder<T> = (value: unknown) => T

export interface ApiRequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  body?: unknown
  signal?: AbortSignal
}

function validateApiPath(path: string): string {
  if (
    !path.startsWith('/api/') ||
    path.startsWith('//') ||
    path.includes('\\') ||
    path.includes('#') ||
    [...path].some((character) => {
      const codePoint = character.codePointAt(0) ?? 0
      return codePoint < 32 || codePoint === 127
    })
  ) {
    throw new ApiError('invalid_path')
  }
  const rawPath = path.split('?', 1)[0] ?? ''
  let decoded: string
  try {
    decoded = decodeURIComponent(rawPath)
  } catch {
    throw new ApiError('invalid_path')
  }
  if (
    decoded.includes('\\') ||
    decoded.split('/').some((segment) => segment === '.' || segment === '..')
  ) {
    throw new ApiError('invalid_path')
  }
  const origin = window.location.origin
  const parsed = new URL(path, origin)
  if (parsed.origin !== origin || !parsed.pathname.startsWith('/api/')) {
    throw new ApiError('invalid_path')
  }
  return `${parsed.pathname}${parsed.search}`
}

function responseByteLimit(path: string, method: string): number {
  const pathname = path.split('?', 1)[0] ?? ''
  if (method === 'GET' && /^\/api\/sessions\/[^/]+$/u.test(pathname)) {
    return SESSION_MAX_RESPONSE_BYTES
  }
  return DEFAULT_MAX_RESPONSE_BYTES
}

async function safelyCancel(
  stream: { cancel(reason?: unknown): Promise<void> } | null | undefined,
): Promise<void> {
  try {
    await stream?.cancel()
  } catch {
    // Cancellation is best-effort; the sanitized boundary error remains authoritative.
  }
}

async function readBoundedText(response: Response, maxBytes: number): Promise<string> {
  const declaredLength = response.headers.get('Content-Length')
  if (declaredLength !== null) {
    const length = Number(declaredLength)
    if (!Number.isSafeInteger(length) || length < 0 || length > maxBytes) {
      await safelyCancel(response.body)
      throw new ApiError('response_too_large', response.status)
    }
  }
  if (response.body === null) return ''
  const reader = response.body.getReader()
  const decoder = new TextDecoder('utf-8', { fatal: true })
  let size = 0
  let text = ''
  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      size += value.byteLength
      if (size > maxBytes) {
        await safelyCancel(reader)
        throw new ApiError('response_too_large', response.status)
      }
      try {
        text += decoder.decode(value, { stream: true })
      } catch {
        await safelyCancel(reader)
        throw new ApiError('invalid_response', response.status)
      }
    }
    try {
      return text + decoder.decode()
    } catch {
      await safelyCancel(reader)
      throw new ApiError('invalid_response', response.status)
    }
  } finally {
    reader.releaseLock()
  }
}

export async function apiRequest<T>(
  path: string,
  decode: Decoder<T>,
  options: ApiRequestOptions = {},
): Promise<T> {
  const safePath = validateApiPath(path)
  const method = options.method ?? 'GET'
  try {
    const init: RequestInit = {
      method,
      credentials: 'same-origin',
    }
    if (options.signal !== undefined) init.signal = options.signal
    if (options.body !== undefined) {
      init.headers = { 'Content-Type': 'application/json' }
      init.body = JSON.stringify(options.body)
    }
    const response = await fetch(safePath, init)
    if (!response.ok) {
      await safelyCancel(response.body)
      throw new ApiError('http_error', response.status)
    }
    const contentType = response.headers.get('Content-Type')?.toLowerCase() ?? ''
    if (!contentType.startsWith('application/json')) {
      await safelyCancel(response.body)
      throw new ApiError('invalid_content_type', response.status)
    }
    const text = await readBoundedText(response, responseByteLimit(safePath, method))
    let value: unknown
    try {
      value = JSON.parse(text) as unknown
    } catch {
      throw new ApiError('invalid_json', response.status)
    }
    try {
      return decode(value)
    } catch (error) {
      if (error instanceof ApiError) throw error
      throw new ApiError('invalid_response', response.status)
    }
  } catch (error) {
    if (error instanceof ApiError) throw error
    if (
      error instanceof DOMException &&
      (error.name === 'AbortError' || options.signal?.aborted === true)
    ) {
      throw new ApiError('aborted')
    }
    throw new ApiError('network_error')
  }
}
