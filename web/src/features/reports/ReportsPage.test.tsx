import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { i18n } from '../../i18n/i18n'
import { UIProvider } from '../../state/UIContext'
import { ReportsPage } from './ReportsPage'

const api = vi.hoisted(() => ({
  listReports: vi.fn(),
  retryReport: vi.fn(),
  getWeixinAccounts: vi.fn(),
  getWeixinConversation: vi.fn(),
}))

vi.mock('../../api/endpoints', () => api)
vi.mock('../../components/ConnectivityProvider', () => ({
  useConnectivity: () => ({ status: 'online', generation: 0, canMutate: true, reconnect: vi.fn() }),
}))

function renderPage() {
  return render(
    <I18nextProvider i18n={i18n}>
      <UIProvider>
        <MemoryRouter>
          <ReportsPage />
        </MemoryRouter>
      </UIProvider>
    </I18nextProvider>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  void i18n.changeLanguage('zh-CN')
  api.listReports.mockResolvedValue({ reports: [], limit: 100 })
  api.getWeixinAccounts.mockResolvedValue({ accounts: [{
    id: 'account-1', label: 'Owner 微信', state: 'connected',
    created_at: '2026-08-07T08:00:00Z', updated_at: '2026-08-07T08:00:00Z',
  }] })
  api.getWeixinConversation.mockResolvedValue({ items: [{
    id: 'inbox-1', account_id: 'account-1', peer_id: 'peer-secret', message_id: 'message-1',
    direction: 'inbound', kind: 'message', text: '帮我整理今天的重点新闻', state: 'queued',
    task_id: 'task-1', time: '2026-08-07T08:01:00Z',
  }, {
    id: 'out-1', account_id: 'account-1', peer_id: 'peer-secret', message_id: 'message-1',
    direction: 'outbound', kind: 'ack', text: '消息已收到，等待 Owner 显式开始。', state: 'sent',
    task_id: 'task-1', attempts: 1, time: '2026-08-07T08:01:01Z',
  }], limit: 100 })
})

describe('ReportsPage Weixin conversation', () => {
  it('shows the authoritative inbound and outbound OpenClaw Weixin timeline', async () => {
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('tab', { name: /微信对话/u }))
    expect(await screen.findByText('帮我整理今天的重点新闻')).toBeInTheDocument()
    expect(screen.getByText('消息已收到，等待 Owner 显式开始。')).toBeInTheDocument()
    expect(api.getWeixinConversation).toHaveBeenCalledWith('account-1', expect.any(Object))
    expect(screen.queryByText('peer-secret')).not.toBeInTheDocument()
    const taskLinks = screen.getAllByRole('link', { name: /task-1/u })
    expect(taskLinks).toHaveLength(2)
    for (const taskLink of taskLinks) {
      expect(taskLink).toHaveAttribute('href', '/tasks/task-1')
    }
  })
})
