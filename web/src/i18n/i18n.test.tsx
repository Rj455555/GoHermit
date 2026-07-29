import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { flattenLeafKeys, translationResources } from './resources'
import { RUNTIME_EVENT_TYPES } from '../api/decoders'
import { renderApp } from '../test/renderApp'

describe('i18n contract', () => {
  it('defaults to Simplified Chinese without browser-language detection', () => {
    renderApp('/dashboard')

    expect(screen.getByText('GOHERMIT · 工作流')).toBeInTheDocument()
    expect(document.documentElement.lang).toBe('zh-CN')
    expect(document.title).toBe('仪表盘 · GoHermit')
  })

  it('removes an invalid locale and safely falls back to Chinese', () => {
    localStorage.setItem('gohermit.ui.locale', 'fr-FR')
    renderApp('/settings')

    expect(screen.getByRole('link', { name: '设置' })).toHaveAttribute('aria-current', 'page')
    expect(localStorage.getItem('gohermit.ui.locale')).toBeNull()
  })

  it('switches all shell copy to English and back to Chinese immediately', async () => {
    const user = userEvent.setup()
    renderApp('/dashboard')

    await user.click(screen.getByRole('button', { name: '切换到 English' }))
    expect(screen.getByRole('link', { name: 'Dashboard' })).toBeInTheDocument()
    expect(screen.getByText('GOHERMIT · LOOP WORKBENCH')).toBeInTheDocument()
    expect(document.documentElement.lang).toBe('en-US')
    expect(document.title).toBe('Dashboard · GoHermit')
    expect(localStorage.getItem('gohermit.ui.locale')).toBe('en-US')

    await user.click(screen.getByRole('button', { name: 'Switch to 简体中文' }))
    expect(screen.getByRole('link', { name: '仪表盘' })).toBeInTheDocument()
    expect(document.documentElement.lang).toBe('zh-CN')
  })

  it('keeps both translation trees leaf-key equivalent', () => {
    expect(flattenLeafKeys(translationResources['en-US'].translation)).toEqual(
      flattenLeafKeys(translationResources['zh-CN'].translation),
    )
  })

  it('covers every backend metadata enum in both locales', () => {
    const expected = {
      invocationStatus: ['attached', 'blocked', 'cancelled', 'completed', 'dispatched', 'failed', 'prepared', 'skipped'],
      messageRole: ['assistant', 'system', 'tool', 'user'],
      runtimeEventType: [...RUNTIME_EVENT_TYPES],
      toolStatus: ['completed', 'started', 'uncertain'],
      workItemStatus: ['cancelled', 'completed', 'failed', 'interrupted', 'queued', 'running', 'skipped'],
    }
    for (const locale of ['zh-CN', 'en-US'] as const) {
      const resources = translationResources[locale].translation as unknown as Record<
        string,
        Record<string, string>
      >
      for (const [namespace, values] of Object.entries(expected)) {
        expect(Object.keys(resources[namespace] ?? {}).sort()).toEqual(
          expect.arrayContaining([...values].sort()),
        )
      }
    }
  })

  it('never exposes a raw missing translation key', () => {
    renderApp('/dashboard')

    expect(screen.queryByText(/navigation\.missing|undefined/)).not.toBeInTheDocument()
  })
})
