import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { useUI } from '../state/UIContext'
import { trapFocus } from './focusTrap'

export function ConfirmDialog() {
  const { t } = useTranslation()
  const { state, actions } = useUI()
  const cancelRef = useRef<HTMLButtonElement>(null)
  const returnFocusRef = useRef<HTMLElement | null>(null)

  useEffect(() => {
    if (state.dialog === null) return
    returnFocusRef.current =
      document.activeElement instanceof HTMLElement ? document.activeElement : null
    cancelRef.current?.focus()
    return () => returnFocusRef.current?.focus()
  }, [state.dialog])

  if (state.dialog === null) return null
  const dialog = state.dialog
  const close = () => actions.closeDialog()
  const confirm = () => {
    close()
    dialog.onConfirm()
  }

  return (
    <div className="modal-layer">
      <button
        className="modal-overlay"
        type="button"
        aria-label={t('actions.dismiss')}
        onClick={close}
      />
      <section
        className="confirm-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="confirm-dialog-title"
        onKeyDown={(event) => {
          if (event.key === 'Escape') {
            event.preventDefault()
            close()
          } else {
            trapFocus(event)
          }
        }}
      >
        <h2 id="confirm-dialog-title">{t(dialog.titleKey)}</h2>
        <p>{t(dialog.descriptionKey)}</p>
        <div className="confirm-dialog__actions">
          <button ref={cancelRef} type="button" className="button button--secondary" onClick={close}>
            {t('actions.cancel')}
          </button>
          <button
            type="button"
            className={`button button--${dialog.tone === 'error' ? 'danger' : 'primary'}`}
            onClick={confirm}
          >
            {t(dialog.confirmKey)}
          </button>
        </div>
      </section>
    </div>
  )
}
