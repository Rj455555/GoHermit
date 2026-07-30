import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

export function NotFoundPage() {
  const { t } = useTranslation()
  return (
    <article className="not-found">
      <p className="not-found__code">404</p>
      <h1>{t('notFound.title')}</h1>
      <p>{t('notFound.description')}</p>
      <Link className="button button--primary" to="/dashboard">
        {t('actions.backDashboard')}
      </Link>
    </article>
  )
}
