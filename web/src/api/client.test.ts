import { afterEach, describe, expect, it, vi } from 'vitest'

import { apiRequest } from './client'
import { decodeSessionDetail } from './decoders'
import { ApiError } from './errors'

const decodeValue = (value: unknown) => value
const now = '2026-07-29T08:00:00Z'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('apiRequest', () => {
  it('accepts a same-origin relative API path and decodes bounded JSON', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response('{"status":"ok"}', {
        headers: { 'Content-Type': 'application/json; charset=utf-8' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await expect(apiRequest('/api/health', decodeValue)).resolves.toEqual({ status: 'ok' })
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/health',
      expect.objectContaining({ method: 'GET' }),
    )
  })

  it.each([
    'https://example.test/api/health',
    '//example.test/api/health',
    '/api/../owner',
    '/api/%2e%2e/owner',
    '/api\\owner',
    '/other',
  ])('rejects an unsafe path without fetching: %s', async (path) => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    await expect(apiRequest(path, decodeValue)).rejects.toMatchObject({
      code: 'invalid_path',
    })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('forwards AbortSignal and reports abort without response details', async () => {
    const controller = new AbortController()
    const fetchMock = vi.fn().mockImplementation((_path, init: RequestInit) => {
      expect(init.signal).toBe(controller.signal)
      return Promise.reject(new DOMException('cancelled private body', 'AbortError'))
    })
    vi.stubGlobal('fetch', fetchMock)

    await expect(
      apiRequest('/api/health', decodeValue, { signal: controller.signal }),
    ).rejects.toMatchObject({ code: 'aborted' })
  })

  it('rejects non-JSON, oversized, and malformed JSON responses', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response('plain secret response'))
      .mockResolvedValueOnce(
        new Response('{}', {
          headers: {
            'Content-Type': 'application/json',
            'Content-Length': '2000000',
          },
        }),
      )
      .mockResolvedValueOnce(
        new Response('{broken', { headers: { 'Content-Type': 'application/json' } }),
      )
    vi.stubGlobal('fetch', fetchMock)

    await expect(apiRequest('/api/health', decodeValue)).rejects.toMatchObject({
      code: 'invalid_content_type',
    })
    await expect(apiRequest('/api/health', decodeValue)).rejects.toMatchObject({
      code: 'response_too_large',
    })
    await expect(apiRequest('/api/health', decodeValue)).rejects.toMatchObject({
      code: 'invalid_json',
    })
  })

  it('sanitizes non-2xx failures and never retains request or response bodies', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response('{"error":"token=secret-response"}', {
        status: 500,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    const error = await apiRequest('/api/owner', decodeValue, {
      method: 'PUT',
      body: { api_key: 'secret-request' },
    }).catch((caught: unknown) => caught)

    expect(error).toBeInstanceOf(ApiError)
    expect(error).toMatchObject({ code: 'http_error', status: 500 })
    expect(JSON.stringify(error)).not.toContain('secret')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('does not retry a failed mutation', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new TypeError('offline private detail'))
    vi.stubGlobal('fetch', fetchMock)

    await expect(
      apiRequest('/api/sessions', decodeValue, {
        method: 'POST',
        body: { title: 'one' },
      }),
    ).rejects.toMatchObject({ code: 'network_error' })
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('allows a valid Session response above 1 MiB but below the 16 MiB endpoint limit', async () => {
    const value = {
      session: {
        schema_version: 6,
        id: 'session-1',
        title: 'Long Session',
        goal: '',
        status: 'open',
        selection: { company: 'openai', access: 'codex', model: 'gpt-5.6', agent: 'coding' },
        created_at: now,
        updated_at: now,
        turns: 0,
        runs: [],
        summary: '',
        tool_calls: [],
        modified_files: {},
        completed_steps: [],
        pending_steps: [],
        test_results: [],
        workspace: '/workspace',
        config_digest: 'digest',
      },
      messages: Array.from({ length: 18 }, (_, index) => ({
        id: `message-${index}`,
        run_id: 'run-1',
        role: 'assistant',
        content: '中'.repeat(21_845),
        created_at: now,
      })),
    }
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(value), {
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    const result = await apiRequest('/api/sessions/session-1', decodeSessionDetail)
    expect(result.messages).toHaveLength(18)
    expect(result.messages[0]?.content).toBe(value.messages[0]?.content)
  })

  it('cancels a streamed Session response after the 16 MiB hard limit', async () => {
    const cancel = vi.fn()
    const body = new ReadableStream<Uint8Array>({
      pull(controller) {
        controller.enqueue(new Uint8Array(1 << 20).fill(0x20))
      },
      cancel,
    })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response(body, { headers: { 'Content-Type': 'application/json' } }),
    ))

    await expect(apiRequest('/api/sessions/session-1', decodeValue)).rejects.toMatchObject({
      code: 'response_too_large',
    })
    expect(cancel).toHaveBeenCalledOnce()
  })

  it('rejects invalid UTF-8 as a sanitized invalid_response', async () => {
    const bytes = Uint8Array.from([
      ...new TextEncoder().encode('{"value":"'),
      0xc3,
      0x28,
      ...new TextEncoder().encode('"}'),
    ])
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response(bytes, { headers: { 'Content-Type': 'application/json' } }),
    ))

    await expect(apiRequest('/api/health', decodeValue)).rejects.toMatchObject({
      code: 'invalid_response',
    })
  })

  it('decodes legal Chinese split across multibyte stream chunks', async () => {
    const encoded = new TextEncoder().encode('{"value":"中文"}')
    const splitAt = encoded.indexOf(0xe4) + 1
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(encoded.slice(0, splitAt))
        controller.enqueue(encoded.slice(splitAt))
        controller.close()
      },
    })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response(body, { headers: { 'Content-Type': 'application/json' } }),
    ))

    await expect(apiRequest('/api/health', decodeValue)).resolves.toEqual({ value: '中文' })
  })
})
