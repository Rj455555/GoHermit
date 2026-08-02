import { Alert } from 'antd'

export function ErrorState({
  title,
  description,
  action,
}: {
  title: string
  description: string
  action?: React.ReactNode
}) {
  return (
    <section className="state-panel state-panel--error">
      <Alert type="error" showIcon message={title} description={description} action={action} />
    </section>
  )
}
