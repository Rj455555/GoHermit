import { useTranslation } from 'react-i18next'

import { useUI } from '../state/UIContext'

export function ToastRegion() {
  const { t } = useTranslation()
  const { state, actions } = useUI()
  if (state.toast === null) return null

  const isError = state.toast.tone === 'error'
  return (
    <div
      className={`toast toast--${state.toast.tone}`}
      role={isError ? 'alert' : 'status'}
      aria-live={isError ? 'assertive' : 'polite'}
    >
      <span>{t(state.toast.messageKey)}</span>
      <button type="button" onClick={actions.dismissToast} aria-label={t('actions.dismiss')}>
        ×
      </button>
    </div>
  )
}
