import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { renderApp } from './test/renderApp'

const declaredRoutes = [
  ['/dashboard', '仪表盘'],
  ['/employees', '电子员工'],
  ['/employees/employee-1', '电子员工'],
  ['/tasks', '任务'],
  ['/tasks/task-1', '任务'],
  ['/agent', '智能体'],
  ['/agent/sessions/session-1', '智能体'],
  ['/loops', '工作流'],
  ['/loops/loop-1', '工作流'],
  ['/loops/loop-1/invocations/invocation-1', '工作流'],
  ['/settings', '设置'],
] as const

describe('Phase 2 routes', () => {
  it.each(declaredRoutes)('renders the localized placeholder for %s', (path, title) => {
    renderApp(path)

    expect(screen.getByRole('heading', { level: 1, name: title })).toBeInTheDocument()
    expect(screen.getByTestId('placeholder-page')).toBeInTheDocument()
  })

  it('redirects the root route to the dashboard', async () => {
    renderApp('/')

    expect(await screen.findByRole('heading', { level: 1, name: '仪表盘' })).toBeInTheDocument()
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
