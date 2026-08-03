import { describe, expect, it } from 'vitest'
import { formatDateTime } from './dateTime'

describe('formatDateTime', () => {
  it('formats valid timestamps in the requested locale', () => {
    expect(formatDateTime('2026-08-03T09:38:00Z', 'en-US')).toContain('2026')
  })

  it('preserves invalid timestamps for a safe fallback', () => {
    expect(formatDateTime('not-a-timestamp', 'en-US')).toBe('not-a-timestamp')
  })
})
