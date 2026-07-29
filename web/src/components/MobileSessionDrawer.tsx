import { useEffect, useRef } from 'react'
import { X } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { trapFocus } from './focusTrap'
import { SessionList } from './SessionList'

export function MobileSessionDrawer({
  open,
  onClose,
  returnFocus,
}: {
  open: boolean
  onClose: () => void
  returnFocus: React.RefObject<HTMLButtonElement | null>
}) {
  const { t } = useTranslation()
  const closeRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    if (!open) return
    const previousOverflow = document.body.style.overflow
    const returnFocusElement = returnFocus.current
    document.body.style.overflow = 'hidden'
    closeRef.current?.focus()
    return () => {
      document.body.style.overflow = previousOverflow
      returnFocusElement?.focus()
    }
  }, [open, returnFocus])

  if (!open) return null
  return (
    <div className="session-drawer-layer">
      <button
        type="button"
        className="session-drawer-overlay"
        data-testid="session-drawer-overlay"
        aria-hidden="true"
        tabIndex={-1}
        onClick={onClose}
      />
      <aside
        className="session-drawer"
        role="dialog"
        aria-modal="true"
        aria-labelledby="session-drawer-title"
        onKeyDown={(event) => {
          if (event.key === 'Escape') {
            event.preventDefault()
            onClose()
          } else {
            trapFocus(event)
          }
        }}
      >
        <header>
          <h2 id="session-drawer-title">{t('session.label')}</h2>
          <button ref={closeRef} type="button" className="icon-button" aria-label={t('session.closeDrawer')} onClick={onClose}>
            <X size={18} aria-hidden="true" />
          </button>
        </header>
        <SessionList onSelect={onClose} />
      </aside>
    </div>
  )
}
