import { Empty, Typography } from 'antd'

export function EmptyState({
  title,
  description,
  action,
}: {
  title: string
  description: string
  action?: React.ReactNode
}) {
  return (
    <section className="state-panel state-panel--empty">
      <Typography.Title level={2}>{title}</Typography.Title>
      <Empty description={description} image={Empty.PRESENTED_IMAGE_SIMPLE}>
        {action ? <div className="state-panel__actions">{action}</div> : null}
      </Empty>
    </section>
  )
}
