import { PanelRightClose } from 'lucide-react'
import { useTranslation } from 'react-i18next'

export function SessionSidebar({ onCollapse }: { onCollapse: () => void }) {
  const { t } = useTranslation()
  return (
    <aside className="session-sidebar" aria-label={t('session.label')}>
      <header>
        <h2>{t('session.label')}</h2>
        <button
          type="button"
          className="icon-button"
          aria-label={t('session.collapse')}
          title={t('session.collapse')}
          onClick={onCollapse}
        >
          <PanelRightClose size={18} aria-hidden="true" />
        </button>
      </header>
      <p>{t('session.placeholder')}</p>
    </aside>
  )
}
