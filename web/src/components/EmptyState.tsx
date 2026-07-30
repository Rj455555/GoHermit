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
      <h2>{title}</h2>
      <p>{description}</p>
      {action ? <div className="state-panel__actions">{action}</div> : null}
    </section>
  )
}
