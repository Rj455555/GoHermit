import type { ReactNode } from 'react'

import type { FeedbackTone } from '../state/UIContext'

export function StatusBadge({
  children,
  tone = 'info',
}: {
  children: ReactNode
  tone?: FeedbackTone
}) {
  return <span className={`status-badge status-badge--${tone}`}>{children}</span>
}
