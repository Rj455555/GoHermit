import type { ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'

import { i18n } from '../i18n/i18n'
import { UIProvider } from './UIContext'

export function AppProviders({ children }: { children: ReactNode }) {
  return (
    <I18nextProvider i18n={i18n}>
      <UIProvider>{children}</UIProvider>
    </I18nextProvider>
  )
}
