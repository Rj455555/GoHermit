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
    <section className="state-panel state-panel--error" role="alert">
      <h2>{title}</h2>
      <p>{description}</p>
      {action ? <div className="state-panel__actions">{action}</div> : null}
    </section>
  )
}
