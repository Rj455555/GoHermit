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
    expect(styles).toContain('.employee-directory-grid')
    expect(styles).toContain('.dashboard-content-stack')
    expect(styles).toContain('.task-prompt-text')
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

  it('contains a mobile breakpoint without masking page overflow', () => {
    expect(styles).toMatch(/max-width:\s*900px/)
    expect(styles).toContain('overflow-wrap: anywhere')
    expect(styles).not.toMatch(/body\s*\{[^}]*overflow-x:\s*(hidden|clip)/s)
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

  it('fits the Task Board to the viewport on desktop and snaps columns on small screens', () => {
    expect(styles).toContain('grid-auto-columns:minmax(0,1fr)')
    expect(styles).not.toContain('minmax(280px,1fr)')
    expect(styles).toContain('grid-auto-columns:minmax(240px,62vw)')
    expect(styles).toContain('scroll-snap-type:x proximity')
    expect(styles).toContain('.task-board-card__title')
    expect(styles).toContain('-webkit-line-clamp:2')
    expect(styles).toContain('.task-board-card.is-dragging')
    expect(styles).toContain('.task-board-card:focus-visible')
    expect(styles).toContain('.task-board-column.is-drop-target')
    expect(styles).toContain('.task-board-card .ant-tag{max-width:100%;overflow:hidden;text-overflow:ellipsis')
    expect(styles).not.toContain('.dashboard-task-board-card')
  })
})
