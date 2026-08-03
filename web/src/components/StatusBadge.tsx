import type { ReactNode } from 'react'
import { Tag } from 'antd'

import type { FeedbackTone } from '../state/UIContext'

export function StatusBadge({
  children,
  tone = 'info',
}: {
  children: ReactNode
  tone?: FeedbackTone
}) {
  const color = tone === 'success' ? 'success' : tone === 'error' ? 'error' : tone === 'warning' ? 'warning' : 'processing'
  return <Tag color={color} className={`status-badge status-badge--${tone}`}>{children}</Tag>
}
