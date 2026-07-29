import { useState } from 'react'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import { AppProviders } from '../state/AppProviders'
import { useUI } from '../state/UIContext'
import { AppShell } from '../layouts/AppShell'
import { RouteErrorBoundary } from './RouteErrorBoundary'
import { ConfirmDialog } from './ConfirmDialog'
import { EmptyState } from './EmptyState'
import { ErrorState } from './ErrorState'
import { PageHeader } from './PageHeader'
import { StatusBadge } from './StatusBadge'
import { ToastRegion } from './ToastRegion'

function Bomb({ active }: { active: boolean }) {
  if (active) throw new Error('private response body')
  return <p>recovered route</p>
}

function FeedbackHarness({ onConfirm }: { onConfirm: () => void }) {
  const { actions } = useUI()
  return (
    <>
      <button
        type="button"
        onClick={() => actions.showToast({ messageKey: 'toast.saved', tone: 'success' })}
      >
        toast
      </button>
      <button
        type="button"
        onClick={() =>
          actions.openDialog({
            titleKey: 'dialog.sampleTitle',
            descriptionKey: 'dialog.sampleDescription',
            confirmKey: 'actions.confirm',
            onConfirm,
          })
        }
      >
        dialog
      </button>
      <ToastRegion />
      <ConfirmDialog />
    </>
  )
}

describe('shared shell components', () => {
  it('keeps shell navigation usable when a route boundary fails and retries safely', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const user = userEvent.setup()
    function Harness() {
      const [active, setActive] = useState(true)
      return (
        <AppShell>
          <RouteErrorBoundary onRetry={() => setActive(false)}>
            <Bomb active={active} />
          </RouteErrorBoundary>
        </AppShell>
      )
    }
    render(
      <MemoryRouter initialEntries={['/dashboard']}>
        <AppProviders>
          <Harness />
        </AppProviders>
      </MemoryRouter>,
    )

    expect(screen.getByRole('navigation', { name: '主导航' })).toBeInTheDocument()
    expect(screen.getByRole('alert')).not.toHaveTextContent('private response body')
    await user.click(screen.getByRole('button', { name: '重试' }))
    expect(screen.getByText('recovered route')).toBeInTheDocument()
  })

  it('announces a toast through an accessible live region', async () => {
    const user = userEvent.setup()
    render(
      <AppProviders>
        <FeedbackHarness onConfirm={vi.fn()} />
      </AppProviders>,
    )

    await user.click(screen.getByRole('button', { name: 'toast' }))
    expect(screen.getByRole('status')).toHaveTextContent('已保存')
  })

  it('traps dialog focus, closes on Escape, and restores the trigger', async () => {
    const user = userEvent.setup()
    render(
      <AppProviders>
        <FeedbackHarness onConfirm={vi.fn()} />
      </AppProviders>,
    )

    const trigger = screen.getByRole('button', { name: 'dialog' })
    await user.click(trigger)
    const dialog = screen.getByRole('dialog', { name: '确认操作' })
    const cancel = screen.getByRole('button', { name: '取消' })
    const confirm = screen.getByRole('button', { name: '确认' })
    expect(cancel).toHaveFocus()
    await user.tab({ shift: true })
    expect(confirm).toHaveFocus()
    await user.keyboard('{Escape}')
    expect(dialog).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
  })

  it('confirms through the Context dialog without window.confirm', async () => {
    const onConfirm = vi.fn()
    const nativeConfirm = vi.spyOn(window, 'confirm')
    const user = userEvent.setup()
    render(
      <AppProviders>
        <FeedbackHarness onConfirm={onConfirm} />
      </AppProviders>,
    )

    await user.click(screen.getByRole('button', { name: 'dialog' }))
    await user.click(screen.getByRole('button', { name: '确认' }))
    expect(onConfirm).toHaveBeenCalledOnce()
    expect(nativeConfirm).not.toHaveBeenCalled()
  })

  it('renders the reusable state and header primitives', () => {
    render(
      <>
        <PageHeader title="Header" />
        <EmptyState title="Empty" description="No items" />
        <ErrorState title="Error" description="Try later" />
        <StatusBadge tone="success">Ready</StatusBadge>
      </>,
    )

    expect(screen.getByRole('heading', { name: 'Header' })).toBeInTheDocument()
    expect(screen.getByText('No items')).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('Try later')
    expect(screen.getByText('Ready')).toBeInTheDocument()
  })
})
