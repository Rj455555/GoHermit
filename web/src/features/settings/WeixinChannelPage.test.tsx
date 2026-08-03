import { I18nextProvider } from 'react-i18next'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { i18n } from '../../i18n/i18n'
import { UIProvider } from '../../state/UIContext'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { WeixinChannelPage } from './WeixinChannelPage'

const api = vi.hoisted(() => ({
  getWeixinAccounts: vi.fn(),
  getWeixinBindings: vi.fn(),
  listEmployees: vi.fn(),
  startWeixinLogin: vi.fn(),
  getWeixinLoginStatus: vi.fn(),
  getWeixinInbox: vi.fn(),
  cancelWeixinLogin: vi.fn(),
  logoutWeixinAccount: vi.fn(),
  saveWeixinBinding: vi.fn(),
  deleteWeixinBinding: vi.fn(),
}))

vi.mock('../../api/endpoints', () => api)
vi.mock('../../components/ConnectivityProvider', () => ({
  useConnectivity: () => ({ status: 'online', generation: 0, canMutate: true, reconnect: vi.fn() }),
}))

function renderPage() {
  return render(
    <I18nextProvider i18n={i18n}>
      <UIProvider>
        <WeixinChannelPage />
        <ConfirmDialog />
      </UIProvider>
    </I18nextProvider>,
  )
}

beforeEach(() => {
  api.getWeixinAccounts.mockResolvedValue({ accounts: [{
    id: 'account-one',
    label: 'Owner Weixin',
    state: 'connected',
    weixin_user_id: 'wx****01',
    created_at: '2026-08-04T00:00:00Z',
    updated_at: '2026-08-04T00:00:00Z',
  }] })
  api.getWeixinBindings.mockResolvedValue({ bindings: [] })
  api.listEmployees.mockResolvedValue({ employees: [] })
  api.getWeixinInbox.mockResolvedValue({ items: [] })
  api.startWeixinLogin.mockResolvedValue({
    id: 'attempt-one',
    account_id: 'account-one',
    state: 'connected',
    expires_at: '2099-08-04T00:00:00Z',
    created_at: '2026-08-04T00:00:00Z',
    updated_at: '2026-08-04T00:00:00Z',
    qr_available: true,
  })
  api.getWeixinLoginStatus.mockResolvedValue({
    id: 'saved-attempt',
    account_id: 'account-one',
    state: 'connected',
    expires_at: '2099-08-04T00:00:00Z',
    created_at: '2026-08-04T00:00:00Z',
    updated_at: '2026-08-04T00:00:00Z',
    qr_available: false,
  })
})

afterEach(() => {
  window.localStorage.clear()
})

describe('WeixinChannelPage', () => {
  it('shows server-owned account state and the explicit Start boundary without secrets', async () => {
    renderPage()

    expect(await screen.findByText('Owner Weixin')).toBeInTheDocument()
    expect(screen.getByText(/Owner.*Start|Owner.*Start/u)).toBeInTheDocument()
    expect(screen.getByText(/wx\*\*\*\*01/u)).toBeInTheDocument()
    expect(screen.queryByText('bearer-secret')).not.toBeInTheDocument()
  })

  it('opens the QR flow only after explicit Add account action', async () => {
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: /添加微信账号|Add WeChat account/u }))
    expect(api.startWeixinLogin).toHaveBeenCalledOnce()
    expect(await screen.findByRole('img', { name: /微信登录二维码|WeChat login QR code/u })).toHaveAttribute(
      'src',
      '/api/channels/weixin/login/attempt-one/qr',
    )
  })

  it('supports QR refresh/cancel, account-scoped Inbox loading, and confirmed logout', async () => {
    const user = userEvent.setup()
    api.startWeixinLogin.mockResolvedValue({
      id: 'attempt-pending',
      account_id: 'account-one',
      state: 'qr_pending',
      expires_at: '2099-08-04T00:00:00Z',
      created_at: '2026-08-04T00:00:00Z',
      updated_at: '2026-08-04T00:00:00Z',
      qr_available: true,
    })
    api.cancelWeixinLogin.mockResolvedValue(undefined)
    api.logoutWeixinAccount.mockResolvedValue(undefined)
    api.getWeixinInbox.mockResolvedValue({ items: [{
      id: 'inbox-1',
      account_id: 'account-one',
      peer_id: 'peer-secret',
      message_id: 'message-1',
      sequence: 1,
      text: 'queued message',
      state: 'received',
      received_at: '2026-08-04T00:00:00Z',
    }] })
    renderPage()

    await user.click(await screen.findByRole('button', { name: /添加微信账号|Add WeChat account/u }))
    await screen.findByRole('img', { name: /微信登录二维码|WeChat login QR code/u })
    await user.click(screen.getByRole('button', { name: /重\s*试|Retry/u }))
    expect(api.startWeixinLogin).toHaveBeenCalledTimes(2)
    await user.click(screen.getByRole('button', { name: /取\s*消|Cancel/u }))
    expect(api.cancelWeixinLogin).toHaveBeenCalledWith('attempt-pending')

    const selects = screen.getAllByRole('combobox')
    await user.click(selects[selects.length - 1]!)
    const accountLabels = await screen.findAllByText('Owner Weixin')
    await user.click(accountLabels[accountLabels.length - 1]!)
    await user.click(screen.getByRole('button', { name: /加载 Inbox|Load Inbox/u }))
    expect(await screen.findByText(/queued message/u)).toBeInTheDocument()
    expect(screen.queryByText('peer-secret')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /退出登录|Log out/u }))
    await user.click(screen.getByRole('button', { name: i18n.t('actions.confirm') }))
    expect(api.logoutWeixinAccount).toHaveBeenCalledWith('account-one')
  })

  it('restores a saved login attempt without creating another one and saves an Employee binding', async () => {
    const user = userEvent.setup()
    window.localStorage.setItem('gohermit.weixin.loginAttempt', 'saved-attempt')
    api.listEmployees.mockResolvedValue({ employees: [{ id: 'employee-1', name: 'Worker' }] })
    api.saveWeixinBinding.mockResolvedValue({ bindings: [] })
    renderPage()

    await screen.findByRole('combobox', { name: 'Employee' })
    expect(api.startWeixinLogin).not.toHaveBeenCalled()
    expect(api.getWeixinLoginStatus).toHaveBeenCalledWith('saved-attempt', expect.anything())

    const selects = screen.getAllByRole('combobox')
    await user.click(selects[0]!)
    const accountOptions = await screen.findAllByText('Owner Weixin')
    await user.click(accountOptions[accountOptions.length - 1]!)
    await user.click(selects[1]!)
    await user.click(await screen.findByText('Worker (employee-1)'))
    await user.click(screen.getByRole('button', { name: /保\s*存|Save/u }))
    expect(api.saveWeixinBinding).toHaveBeenCalledOnce()
  })
})
