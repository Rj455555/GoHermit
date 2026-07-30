import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { renderApp } from './test/renderApp'

const phase4Routes = [
  '/employees',
  '/employees/employee-1',
  '/tasks',
  '/tasks/task-1',
  '/loops',
  '/loops/loop-1',
  '/loops/loop-1/invocations/invocation-1',
] as const

describe('declared React routes', () => {
  it.each(phase4Routes)('mounts the Phase 4 feature route %s', (path) => {
    renderApp(path)

    expect(screen.getByTestId('react-bootstrap')).toBeInTheDocument()
    expect(screen.queryByTestId('placeholder-page')).not.toBeInTheDocument()
  })

  it.each(['/dashboard', '/agent', '/agent/sessions/session-1', '/settings'])(
    'mounts the Phase 3 projection route %s',
    (path) => {
      renderApp(path)

      expect(screen.getAllByRole('status').length).toBeGreaterThan(0)
      expect(screen.queryByTestId('placeholder-page')).not.toBeInTheDocument()
    },
  )

  it('redirects the root route to the dashboard', async () => {
    renderApp('/')

    expect((await screen.findAllByRole('status')).length).toBeGreaterThan(0)
    expect(screen.getByRole('link', { name: '仪表盘' })).toHaveAttribute('aria-current', 'page')
  })

  it('renders a localized Not Found page for an unknown React route', () => {
    renderApp('/unknown/react/path')

    expect(screen.getByRole('heading', { name: '页面未找到' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '返回仪表盘' })).toHaveAttribute('href', '/dashboard')
  })

  it('uses the current route as the only navigation selection source', async () => {
    const app = renderApp('/employees/employee-1')

    expect(screen.getByRole('link', { name: '电子员工' })).toHaveAttribute('aria-current', 'page')
    await app.navigate('/tasks/task-1')
    expect(screen.getByRole('link', { name: '任务' })).toHaveAttribute('aria-current', 'page')
  })
})
