import { describe, expect, it } from 'vitest'

import {
  readStoredBoolean,
  readStoredLocale,
  STORAGE_KEYS,
  writeStoredBoolean,
} from './storage'

describe('untrusted UI preference storage', () => {
  it.each(['1', 'yes', 'TRUE', 'false ', 'null'])(
    'rejects the non-canonical boolean value %s',
    (value) => {
      localStorage.setItem(STORAGE_KEYS.navigationCollapsed, value)

      expect(readStoredBoolean(STORAGE_KEYS.navigationCollapsed)).toBe(false)
      expect(localStorage.getItem(STORAGE_KEYS.navigationCollapsed)).toBeNull()
    },
  )

  it('accepts only exact canonical booleans', () => {
    localStorage.setItem(STORAGE_KEYS.navigationCollapsed, 'true')
    expect(readStoredBoolean(STORAGE_KEYS.navigationCollapsed)).toBe(true)
    localStorage.setItem(STORAGE_KEYS.navigationCollapsed, 'false')
    expect(readStoredBoolean(STORAGE_KEYS.navigationCollapsed)).toBe(false)
  })

  it('writes a canonical boolean value', () => {
    writeStoredBoolean(STORAGE_KEYS.sessionSidebarCollapsed, true)
    expect(localStorage.getItem(STORAGE_KEYS.sessionSidebarCollapsed)).toBe('true')
  })

  it('accepts only the two exact locale values', () => {
    localStorage.setItem(STORAGE_KEYS.locale, 'en-US')
    expect(readStoredLocale()).toBe('en-US')
    localStorage.setItem(STORAGE_KEYS.locale, 'en-us')
    expect(readStoredLocale()).toBe('zh-CN')
    expect(localStorage.getItem(STORAGE_KEYS.locale)).toBeNull()
  })
})
