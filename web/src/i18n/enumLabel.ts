import type { TFunction } from 'i18next'

import { i18n } from './i18n'

export function translatedEnum(
  t: TFunction,
  namespace: string,
  value: string | undefined,
): string {
  const fallback = t('status.unknown')
  if (!value) return fallback
  const key = `${namespace}.${value}`
  return i18n.exists(key) ? t(key) : fallback
}
