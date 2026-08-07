import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { i18n } from '../i18n/i18n'
import { SessionList } from './SessionList'

const agentData = vi.hoisted(() => ({
  sessions: [] as Array<Record<string, unknown>>,
  loading: false,
  error: false,
  info: null,
  refresh: vi.fn(),
}))

vi.mock('../features/agent/AgentDataContext', () => ({ useAgentData: () => agentData }))

beforeEach(() => {
  void i18n.changeLanguage('zh-CN')
  agentData.sessions = [{
    id: 'conversation-1', title: '产品讨论', kind: 'interactive', status: 'open',
    updated_at: '2026-08-07T08:00:00Z', last_run_status: 'completed',
    selection: { company: 'openai', access: 'codex', model: 'gpt-5.6', agent: 'coding' },
  }, {
    id: 'employee-1', title: '定时 Loop 执行', kind: 'employee_task', status: 'open',
    updated_at: '2026-08-07T09:00:00Z', last_run_status: 'queued',
    selection: { company: 'openai', access: 'codex', model: 'gpt-5.6', agent: 'coding' },
  }]
})

describe('SessionList', () => {
  it('renders searchable full-card interactive conversations without Employee execution Sessions', async () => {
    const user = userEvent.setup()
    render(
      <I18nextProvider i18n={i18n}>
        <MemoryRouter initialEntries={['/agent']}><SessionList /></MemoryRouter>
      </I18nextProvider>,
    )

    expect(screen.getByRole('link', { name: /产品讨论/u })).toHaveAttribute('href', '/agent/sessions/conversation-1')
    expect(screen.queryByText('定时 Loop 执行')).not.toBeInTheDocument()
    await user.type(screen.getByRole('textbox', { name: '搜索会话' }), '不存在')
    expect(screen.queryByText('产品讨论')).not.toBeInTheDocument()
    expect(screen.getByText('没有匹配的会话')).toBeInTheDocument()
  })
})
