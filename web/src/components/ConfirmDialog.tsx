import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from 'antd'

import { useUI } from '../state/UIContext'
import { trapFocus } from './focusTrap'

export function ConfirmDialog() {
  const { t } = useTranslation()
  const { state, actions } = useUI()
  const cancelRef = useRef<HTMLButtonElement>(null)
  const returnFocusRef = useRef<HTMLElement | null>(null)
  const lastTriggerRef = useRef<HTMLElement | null>(null)

  useEffect(() => {
    const rememberTrigger = (event: PointerEvent) => {
      if (!(event.target instanceof HTMLElement) || event.target.closest('.modal-layer')) return
      lastTriggerRef.current = event.target.closest<HTMLElement>(
        'button, a[href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
      )
    }
    document.addEventListener('pointerdown', rememberTrigger, true)
    return () => document.removeEventListener('pointerdown', rememberTrigger, true)
  }, [])

  useEffect(() => {
    if (state.dialog === null) return
    const previousOverflow = document.body.style.overflow
    const activeElement = document.activeElement instanceof HTMLElement
      && document.activeElement !== document.body
      && document.activeElement !== document.documentElement
      ? document.activeElement
      : null
    returnFocusRef.current = lastTriggerRef.current ?? activeElement
    document.body.style.overflow = 'hidden'
    cancelRef.current?.focus()
    return () => {
      document.body.style.overflow = previousOverflow
      returnFocusRef.current?.focus()
    }
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
        data-testid="confirm-dialog-overlay"
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
          <Button ref={cancelRef} type="default" aria-label={t('actions.cancel')} className="button button--secondary" onClick={close}>
            {t('actions.cancel')}
          </Button>
          <Button
            type="primary"
            danger={dialog.tone === 'error'}
            aria-label={t(dialog.confirmKey)}
            className={`button button--${dialog.tone === 'error' ? 'danger' : 'primary'}`}
            onClick={confirm}
          >
            {t(dialog.confirmKey)}
          </Button>
        </div>
      </section>
    </div>
  )
}
