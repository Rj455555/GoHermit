import { StrictMode } from 'react'
import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import { App } from '../App'
import { STORAGE_KEYS } from '../state/storage'
import { renderApp } from '../test/renderApp'

function installMobileQuery(initial: boolean) {
  let mobile = initial
  const listeners = new Set<(event: MediaQueryListEvent) => void>()
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: query === '(max-width: 900px)' ? mobile : false,
    media: query,
    onchange: null,
    addEventListener: (_: string, listener: (event: MediaQueryListEvent) => void) =>
      listeners.add(listener),
    removeEventListener: (_: string, listener: (event: MediaQueryListEvent) => void) =>
      listeners.delete(listener),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
  return {
    setMobile(value: boolean) {
      mobile = value
      for (const listener of listeners) {
        listener({ matches: value, media: '(max-width: 900px)' } as MediaQueryListEvent)
      }
    },
  }
}

describe('desktop shell preferences', () => {
  it('collapses and restores the navigation rail with canonical persistence', async () => {
    const user = userEvent.setup()
    renderApp('/dashboard')

    const rail = screen.getByRole('navigation', { name: '主导航' })
    expect(rail).toHaveAttribute('data-collapsed', 'false')
    await user.click(screen.getByRole('button', { name: '收起主导航' }))
    expect(rail).toHaveAttribute('data-collapsed', 'true')
    expect(localStorage.getItem(STORAGE_KEYS.navigationCollapsed)).toBe('true')
    expect(screen.getByRole('button', { name: '展开主导航' })).toHaveAttribute(
      'aria-expanded',
      'false',
    )
  })

  it('falls back to expanded when the stored navigation boolean is invalid', () => {
    localStorage.setItem(STORAGE_KEYS.navigationCollapsed, 'truthy')
    renderApp('/dashboard')

    expect(screen.getByRole('navigation', { name: '主导航' })).toHaveAttribute(
      'data-collapsed',
      'false',
    )
    expect(localStorage.getItem(STORAGE_KEYS.navigationCollapsed)).toBeNull()
  })

  it('shows the Session sidebar only on Agent routes and retains a focusable restore entry', async () => {
    const user = userEvent.setup()
    const app = renderApp('/agent/sessions/session-1')

    expect(screen.getByRole('complementary', { name: '会话' })).toHaveTextContent(
      '会话功能将在 Phase 3 接入',
    )
    expect(screen.queryByText('任务功能将在 Phase 3 接入')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '收起会话栏' }))
    const restore = await screen.findByRole('button', { name: '展开会话栏' })
    expect(restore).toHaveFocus()
    expect(localStorage.getItem(STORAGE_KEYS.sessionSidebarCollapsed)).toBe('true')

    await app.navigate('/settings')
    expect(screen.queryByRole('complementary', { name: '会话' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '展开会话栏' })).not.toBeInTheDocument()
  })

  it('persists a single preference side effect under StrictMode', async () => {
    const setItem = vi.spyOn(Storage.prototype, 'setItem')
    const user = userEvent.setup()
    render(
      <StrictMode>
        <MemoryRouter initialEntries={['/dashboard']}>
          <App />
        </MemoryRouter>
      </StrictMode>,
    )

    await user.click(screen.getByRole('button', { name: '收起主导航' }))
    const writes = setItem.mock.calls.filter(
      ([key]) => key === STORAGE_KEYS.navigationCollapsed,
    )
    expect(writes).toEqual([[STORAGE_KEYS.navigationCollapsed, 'true']])
  })
})

describe('mobile Session drawer', () => {
  it('supports Escape, focus trap, inert background, and focus return', async () => {
    installMobileQuery(true)
    const user = userEvent.setup()
    renderApp('/agent')

    const trigger = screen.getByRole('button', { name: '打开会话抽屉' })
    await user.click(trigger)
    const drawer = screen.getByRole('dialog', { name: '会话' })
    const close = screen.getByRole('button', { name: '关闭会话抽屉' })
    const done = screen.getByRole('button', { name: '完成' })
    expect(drawer).toBeInTheDocument()
    expect(close).toHaveFocus()
    expect(screen.getByTestId('shell-background')).toHaveAttribute('inert')
    expect(document.body.style.overflow).toBe('hidden')

    await user.tab({ shift: true })
    expect(done).toHaveFocus()
    await user.tab()
    expect(close).toHaveFocus()
    await user.keyboard('{Escape}')
    expect(screen.queryByRole('dialog', { name: '会话' })).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
    expect(document.body.style.overflow).toBe('')
  })

  it('closes from the overlay without persisting mobile state', async () => {
    installMobileQuery(true)
    const user = userEvent.setup()
    renderApp('/agent')

    await user.click(screen.getByRole('button', { name: '打开会话抽屉' }))
    await user.click(screen.getByTestId('session-drawer-overlay'))
    expect(screen.queryByRole('dialog', { name: '会话' })).not.toBeInTheDocument()
    expect(localStorage.getItem('gohermit.ui.mobileSessionDrawerOpen')).toBeNull()
  })

  it('restores the independent desktop preference after crossing the breakpoint', async () => {
    localStorage.setItem(STORAGE_KEYS.sessionSidebarCollapsed, 'true')
    const viewport = installMobileQuery(true)
    renderApp('/agent')

    expect(screen.getByRole('button', { name: '打开会话抽屉' })).toBeInTheDocument()
    act(() => viewport.setMobile(false))
    expect(await screen.findByRole('button', { name: '展开会话栏' })).toBeInTheDocument()
    expect(localStorage.getItem(STORAGE_KEYS.sessionSidebarCollapsed)).toBe('true')
  })
})
