import type { ReactNode } from 'react'
import { Flex, Typography } from 'antd'

export function PageHeader({
  title,
  description,
  actions,
}: {
  title: string
  description?: string
  actions?: ReactNode
}) {
  return (
    <Flex className="page-header" align="start" justify="space-between" gap={24} wrap>
      <div>
        <Typography.Title level={1}>{title}</Typography.Title>
        {description ? <Typography.Paragraph type="secondary">{description}</Typography.Paragraph> : null}
      </div>
      {actions ? <div className="page-header__actions">{actions}</div> : null}
    </Flex>
  )
}
