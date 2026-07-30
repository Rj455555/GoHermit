import { describe, expect, it } from 'vitest'

import styles from 'virtual:gohermit-styles-contract'

describe('responsive shell CSS contract', () => {
  it('centralizes the approved shell dimensions and transition duration', () => {
    expect(styles).toContain('--navigation-rail-expanded: 228px')
    expect(styles).toContain('--navigation-rail-collapsed: 68px')
    expect(styles).toContain('--session-sidebar-expanded: 320px')
    expect(styles).toContain('--shell-transition-duration: 180ms')
  })

  it('uses one restrained accent and styles the new workbench surfaces', () => {
    expect(styles).toContain('--color-accent: #c45a3a')
    expect(styles).toContain('.dashboard-hero')
    expect(styles).toContain('.wizard-progress')
    expect(styles).toContain('.employee-grid')
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
