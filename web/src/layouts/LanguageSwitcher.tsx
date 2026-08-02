import { Languages } from 'lucide-react'
import { Button } from 'antd'
import { useTranslation } from 'react-i18next'

import { useUI } from '../state/UIContext'

export function LanguageSwitcher({ compact = false }: { compact?: boolean }) {
  const { t } = useTranslation()
  const { state, actions } = useUI()
  const nextLocale = state.locale === 'zh-CN' ? 'en-US' : 'zh-CN'
  const label = state.locale === 'zh-CN' ? t('language.toEnglish') : t('language.toChinese')

  return (
    <Button
      type="text"
      className="language-switcher"
      aria-label={label}
      title={label}
      onClick={() => actions.setLocale(nextLocale)}
    >
      <Languages aria-hidden="true" size={17} />
      {compact ? null : <span>{state.locale === 'zh-CN' ? 'EN' : '简中'}</span>}
    </Button>
  )
}
