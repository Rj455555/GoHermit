import { describe, expect, it } from 'vitest'

import {
  MAX_RUN_MESSAGE_BYTES,
  normalizedRunMessage,
  runMessageByteLength,
} from './messageLimits'

describe('Run message byte contract', () => {
  it('accepts exactly 16 KiB and rejects one byte more', () => {
    expect(runMessageByteLength('x'.repeat(MAX_RUN_MESSAGE_BYTES))).toBe(16 << 10)
    expect(runMessageByteLength('x'.repeat(MAX_RUN_MESSAGE_BYTES + 1))).toBe((16 << 10) + 1)
  })

  it('counts Chinese and Emoji by their real UTF-8 byte length', () => {
    expect(runMessageByteLength('中')).toBe(3)
    expect(runMessageByteLength('😀')).toBe(4)
    expect(runMessageByteLength('中😀')).toBe(7)
  })

  it('matches the Go trim-before-byte-count contract', () => {
    expect(normalizedRunMessage('  中😀  ')).toBe('中😀')
    expect(runMessageByteLength('  中😀  ')).toBe(7)
  })
})
