import { describe, expect, it } from 'vitest'

import styles from 'virtual:gohermit-styles-contract'

describe('responsive shell CSS contract', () => {
  it('centralizes the approved shell dimensions and transition duration', () => {
    expect(styles).toContain('--navigation-rail-expanded: 184px')
    expect(styles).toContain('--navigation-rail-collapsed: 56px')
    expect(styles).toContain('--session-sidebar-expanded: 292px')
    expect(styles).toContain('--shell-transition-duration: 180ms')
  })

  it('disables non-essential motion for reduced-motion users', () => {
    expect(styles).toMatch(/prefers-reduced-motion:\s*reduce/)
    expect(styles).toContain('--shell-transition-duration: 0ms')
  })

  it('contains a mobile breakpoint that prevents horizontal overflow', () => {
    expect(styles).toMatch(/max-width:\s*900px/)
    expect(styles).toMatch(/overflow-x:\s*hidden/)
  })
})
