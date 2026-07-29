import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'

import '../../web/src/styles.css'
import { AppShell } from '../../web/src/layouts/AppShell'
import { AppProviders } from '../../web/src/state/AppProviders'
import { useUI } from '../../web/src/state/UIContext'

function DialogActions() {
  const { actions } = useUI()
  const [confirmations, setConfirmations] = useState(0)
  return (
    <>
      <button
        type="button"
        onClick={() => actions.showToast({ messageKey: 'toast.saved', tone: 'success' })}
      >
        显示通知
      </button>
      <button
        type="button"
        onClick={() =>
          actions.openDialog({
            titleKey: 'dialog.sampleTitle',
            descriptionKey: 'dialog.sampleDescription',
            confirmKey: 'actions.confirm',
            onConfirm: () => setConfirmations((value) => value + 1),
          })
        }
      >
        打开确认框
      </button>
      <output aria-label="确认次数">{confirmations}</output>
    </>
  )
}

const rootElement = document.getElementById('root')
if (rootElement === null) throw new Error('Dialog harness root is missing')

createRoot(rootElement).render(
  <MemoryRouter initialEntries={['/dashboard']}>
    <AppProviders>
      <AppShell>
        <DialogActions />
      </AppShell>
    </AppProviders>
  </MemoryRouter>,
)
