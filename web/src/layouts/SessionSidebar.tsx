import { PanelRightClose } from 'lucide-react'
import { Button } from 'antd'
import { useTranslation } from 'react-i18next'

import { SessionList } from '../components/SessionList'

export function SessionSidebar({ onCollapse }: { onCollapse: () => void }) {
  const { t } = useTranslation()
  return (
    <aside className="session-sidebar" aria-label={t('session.label')}>
      <header>
        <h2>{t('session.label')}</h2>
        <Button
          type="text"
          className="icon-button"
          aria-label={t('session.collapse')}
          title={t('session.collapse')}
          onClick={onCollapse}
        >
          <PanelRightClose size={18} aria-hidden="true" />
        </Button>
      </header>
      <SessionList />
    </aside>
  )
}
