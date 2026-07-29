import {
  DEFAULT_LOCALE,
  SUPPORTED_LOCALES,
  type Locale,
} from '../i18n/resources'

export const STORAGE_KEYS = {
  locale: 'gohermit.ui.locale',
  navigationCollapsed: 'gohermit.ui.navigationCollapsed',
  sessionSidebarCollapsed: 'gohermit.ui.sessionSidebarCollapsed',
} as const

type BooleanStorageKey =
  | typeof STORAGE_KEYS.navigationCollapsed
  | typeof STORAGE_KEYS.sessionSidebarCollapsed

export function readStoredLocale(): Locale {
  const value = localStorage.getItem(STORAGE_KEYS.locale)
  if (value === null) return DEFAULT_LOCALE
  if ((SUPPORTED_LOCALES as readonly string[]).includes(value)) return value as Locale
  localStorage.removeItem(STORAGE_KEYS.locale)
  return DEFAULT_LOCALE
}

export function writeStoredLocale(locale: Locale): void {
  localStorage.setItem(STORAGE_KEYS.locale, locale)
}

export function readStoredBoolean(key: BooleanStorageKey): boolean {
  const value = localStorage.getItem(key)
  if (value === null || value === 'false') return false
  if (value === 'true') return true
  localStorage.removeItem(key)
  return false
}

export function writeStoredBoolean(key: BooleanStorageKey, value: boolean): void {
  localStorage.setItem(key, value ? 'true' : 'false')
}
