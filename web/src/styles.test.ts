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
    expect(styles).toContain('.guided-employee-card')
  })

  it('keeps native choice controls compact and gives selects a deliberate affordance', () => {
    expect(styles).toMatch(/select\s*\{[^}]*appearance:\s*none/s)
    expect(styles).toMatch(/input\[type="checkbox"\][^{]*\{[^}]*width:\s*16px/s)
    expect(styles).toMatch(/input\[type="radio"\][^{]*\{[^}]*width:\s*16px/s)
    expect(styles).toMatch(/input\[type="checkbox"\]:checked::after/)
  })

  it('disables non-essential motion for reduced-motion users', () => {
    expect(styles).toMatch(/prefers-reduced-motion:\s*reduce/)
    expect(styles).toContain('--shell-transition-duration: 0ms')
  })

  it('contains a mobile breakpoint that prevents horizontal overflow', () => {
    expect(styles).toMatch(/max-width:\s*900px/)
    expect(styles).toMatch(/overflow-x:\s*hidden/)
  })

  it('uses explicit mobile section controls and safe-area surfaces', () => {
    expect(styles).toContain('.employee-mobile-tab-select')
    expect(styles).toContain('.loop-mobile-tab-select')
    expect(styles).toContain('.mobile-navigation-drawer')
    expect(styles).toContain('env(safe-area-inset-bottom)')
  })

  it('keeps deep Employee, Task, and Loop pages in bounded responsive surfaces', () => {
    expect(styles).toContain('.employee-settings-page')
    expect(styles).toContain('.employee-knowledge-page')
    expect(styles).toContain('.task-sticky-action-bar')
    expect(styles).toContain('.team-role-card')
    expect(styles).toContain('overflow-wrap: anywhere')
  })
})
