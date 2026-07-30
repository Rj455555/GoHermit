import i18next from 'i18next'
import { initReactI18next } from 'react-i18next'

import { DEFAULT_LOCALE, translationResources } from './resources'

export const i18n = i18next.createInstance()

void i18n.use(initReactI18next).init({
  resources: translationResources,
  lng: DEFAULT_LOCALE,
  fallbackLng: DEFAULT_LOCALE,
  supportedLngs: ['zh-CN', 'en-US'],
  initAsync: false,
  interpolation: {
    escapeValue: false,
  },
  parseMissingKeyHandler: () => '—',
  returnEmptyString: false,
})
