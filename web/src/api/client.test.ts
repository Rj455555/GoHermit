import { afterEach, describe, expect, it, vi } from 'vitest'

import { apiRequest } from './client'
import { ApiError } from './errors'

const decodeValue = (value: unknown) => value

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
})
