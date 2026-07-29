import { I18nextProvider } from 'react-i18next'
import { StrictMode } from 'react'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { i18n } from '../../i18n/i18n'
import { UIProvider } from '../../state/UIContext'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import type { OwnerProfile } from '../../api/types'
import { SettingsPage } from './SettingsPage'

const api = vi.hoisted(() => ({
  getInfo: vi.fn(),
  getOwner: vi.fn(),
  saveOwner: vi.fn<(profile: OwnerProfile) => Promise<OwnerProfile>>(),
  saveOwnerFact: vi.fn(),
  forgetOwnerFact: vi.fn(),
  saveProviderAPIKey: vi.fn(),
  deleteProviderCredentials: vi.fn(),
  startCodexLogin: vi.fn(),
  getCodexLogin: vi.fn(),
}))

vi.mock('../../api/endpoints', () => api)
vi.mock('../../components/ConnectivityProvider', () => ({
  useConnectivity: () => ({ status: 'online', generation: 0, canMutate: true, reconnect: vi.fn() }),
}))

function renderPage(strict = false) {
  const page = (
    <I18nextProvider i18n={i18n}>
      <UIProvider>
        <SettingsPage />
        <ConfirmDialog />
      </UIProvider>
    </I18nextProvider>
  )
  return render(
    strict ? <StrictMode>{page}</StrictMode> : page,
  )
}

beforeEach(() => {
  api.getInfo.mockResolvedValue({
    companies: [{
      id: 'openai',
      label: 'OpenAI',
      access: [{
        id: 'openai-api',
        label: 'OpenAI API',
        auth_type: 'api_key',
        supported: true,
        models: [],
      }],
    }],
    auth_status: { 'openai-api': { configured: true, source: 'GoHermit', detail: 'ready' } },
  })
  api.getOwner.mockResolvedValue({
    schema_version: 1,
    identity: { display_name: 'Owner', timezone: 'Asia/Shanghai', language: 'zh-CN' },
    preferences: { communication: '', coding: '', git: '', verification: '', risk: '' },
    environments: [],
    facts: [],
  })
})

describe('SettingsPage', () => {
  it('never persists or re-displays API keys and clears the key after failure', async () => {
    api.saveProviderAPIKey.mockRejectedValue(new Error('failure'))
    const storageSpy = vi.spyOn(Storage.prototype, 'setItem')
    const user = userEvent.setup()
    renderPage()
    const key = await screen.findByLabelText('OpenAI API API Key')

    await user.type(key, 'sk-private-value')
    await user.click(screen.getByRole('button', { name: /保存 API Key|Save API Key/u }))

    await waitFor(() => expect(key).toHaveValue(''))
    expect(storageSpy).not.toHaveBeenCalledWith(expect.anything(), 'sk-private-value')
    expect(screen.queryByDisplayValue('sk-private-value')).not.toBeInTheDocument()
  })

  it('requires ConfirmDialog before deleting provider credentials', async () => {
    api.deleteProviderCredentials.mockResolvedValue({ configured: false, provider: 'openai-api' })
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: /删除凭据|Delete credentials/u }))
    expect(api.deleteProviderCredentials).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: /确认|Confirm/u }))

    await waitFor(() => expect(api.deleteProviderCredentials).toHaveBeenCalledOnce())
  })

  it('saves the Owner profile only from an explicit submit', async () => {
    api.saveOwner.mockImplementation((profile) => Promise.resolve(profile))
    const user = userEvent.setup()
    renderPage()
    const name = await screen.findByLabelText(/显示名称|Display name/u)

    await user.clear(name)
    await user.type(name, 'Updated Owner')
    expect(api.saveOwner).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: /保存个人配置|Save profile/u }))

    await waitFor(() => expect(api.saveOwner).toHaveBeenCalledOnce())
    expect(api.saveOwner.mock.calls[0]?.[0].identity.display_name).toBe('Updated Owner')
  })

  it('adds an Owner fact and forgets an existing fact only after confirmation', async () => {
    const fact = {
      id: 'fact-existing',
      category: 'editor',
      value: 'vim',
      source: 'owner-settings',
      confirmed: true,
      created_at: '2026-07-29T08:00:00Z',
      updated_at: '2026-07-29T08:00:00Z',
    }
    const profile = {
      schema_version: 1,
      identity: { display_name: 'Owner', timezone: 'Asia/Shanghai', language: 'zh-CN' },
      preferences: { communication: '', coding: '', git: '', verification: '', risk: '' },
      environments: [],
      facts: [fact],
    }
    api.getOwner.mockResolvedValue(profile)
    api.saveOwnerFact.mockResolvedValue(profile)
    api.forgetOwnerFact.mockResolvedValue({ ...profile, facts: [] })
    const user = userEvent.setup()
    renderPage()

    await user.type(await screen.findByLabelText(i18n.t('settings.factCategory')), 'shell')
    await user.type(screen.getByLabelText(i18n.t('settings.factValue')), 'zsh')
    await user.click(screen.getByRole('button', { name: i18n.t('settings.addFact') }))
    await waitFor(() => expect(api.saveOwnerFact).toHaveBeenCalledOnce())
    expect(api.saveOwnerFact.mock.calls[0]?.[1]).toEqual({
      category: 'shell',
      value: 'zsh',
      source: 'owner-settings',
      confirmed: true,
    })

    await user.click(screen.getByRole('button', { name: i18n.t('settings.forgetFact') }))
    expect(api.forgetOwnerFact).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: i18n.t('actions.confirm') }))
    await waitFor(() => expect(api.forgetOwnerFact).toHaveBeenCalledWith('fact-existing'))
  })

  it('keeps one Codex poll in StrictMode and stops after a terminal response', async () => {
    vi.useFakeTimers()
    try {
      api.getInfo.mockResolvedValue({
        companies: [{
          id: 'openai',
          label: 'OpenAI',
          access: [{
            id: 'openai-codex',
            label: 'Codex',
            auth_type: 'oauth_external',
            supported: true,
            models: [],
          }],
        }],
        auth_status: {
          'openai-codex': { configured: false, source: '', detail: 'login required' },
        },
      })
      api.startCodexLogin.mockResolvedValue({
        id: 'login-1',
        status: 'pending',
        expires_at: '2099-07-29T08:00:00Z',
      })
      api.getCodexLogin.mockResolvedValue({
        id: 'login-1',
        status: 'approved',
        expires_at: '2099-07-29T08:00:00Z',
      })
      renderPage(true)

      await act(async () => {
        await vi.advanceTimersByTimeAsync(0)
      })
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: i18n.t('settings.loginCodex') }))
        await Promise.resolve()
      })
      await act(async () => {
        await vi.advanceTimersByTimeAsync(2_000)
      })
      expect(api.getCodexLogin).toHaveBeenCalledOnce()
      expect(api.getInfo).toHaveBeenCalledTimes(3)

      await act(async () => {
        await vi.advanceTimersByTimeAsync(10_000)
      })
      expect(api.getCodexLogin).toHaveBeenCalledOnce()
    } finally {
      vi.useRealTimers()
    }
  })
})
