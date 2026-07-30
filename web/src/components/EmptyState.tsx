export function EmptyState({
  title,
  description,
}: {
  title: string
  description: string
}) {
  return (
    <section className="state-panel state-panel--empty">
      <h2>{title}</h2>
      <p>{description}</p>
    </section>
  )
}
