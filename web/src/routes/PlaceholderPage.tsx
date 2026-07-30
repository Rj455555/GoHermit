import { useTranslation } from 'react-i18next'

import { PageHeader } from '../components/PageHeader'

export function PlaceholderPage({ page }: { page: string }) {
  const { t } = useTranslation()
  return (
    <article className="placeholder-page" data-testid="placeholder-page">
      <PageHeader
        title={t(`pages.${page}.title`)}
        description={t(`pages.${page}.description`)}
      />
      <div className="placeholder-canvas" aria-hidden="true">
        <span />
        <span />
        <span />
      </div>
    </article>
  )
}
