import { StrictMode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ConnectivityBanner, ConnectivityProvider, useConnectivity } from './ConnectivityProvider'
import { i18n } from '../i18n/i18n'

function Probe() {
  const connectivity = useConnectivity()
  return (
    <>
      <output aria-label="connectivity">{connectivity.status}</output>
      <output aria-label="generation">{connectivity.generation}</output>
      <ConnectivityBanner />
    </>
  )
}

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('ConnectivityProvider', () => {
  it('owns one effective heartbeat timer under StrictMode and cleans it on unmount', async () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => Promise.resolve(
      new Response('{"status":"ok","version":"0.3","active":false}', {
        headers: { 'Content-Type': 'application/json' },
      }),
    )))
    const view = render(
      <StrictMode>
        <I18nextProvider i18n={i18n}>
          <ConnectivityProvider>
            <Probe />
          </ConnectivityProvider>
        </I18nextProvider>
      </StrictMode>,
    )
    await act(async () => {
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(screen.getByLabelText('connectivity')).toHaveTextContent('online')
    expect(vi.getTimerCount()).toBe(1)
    view.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('keeps existing UI visible offline and reconnects only on an explicit action', async () => {
    vi.useRealTimers()
    const fetchMock = vi
      .fn()
      .mockRejectedValueOnce(new TypeError('private network detail'))
      .mockResolvedValueOnce(
        new Response('{"status":"ok","version":"0.3","active":false}', {
          headers: { 'Content-Type': 'application/json' },
        }),
      )
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()
    render(
      <I18nextProvider i18n={i18n}>
        <ConnectivityProvider>
          <Probe />
        </ConnectivityProvider>
      </I18nextProvider>,
    )
    await act(async () => {
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(screen.getByRole('alert')).toBeVisible()
    await user.click(screen.getByRole('button', { name: /重新连接|Reconnect/u }))
    await act(async () => {
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.getByLabelText('generation')).toHaveTextContent('1')
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
