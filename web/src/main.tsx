import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import { App } from './App'
import { BootstrapErrorBoundary } from './BootstrapErrorBoundary'

const rootElement = document.getElementById('root')
if (rootElement === null) {
  throw new Error('React bootstrap root is missing')
}

createRoot(rootElement).render(
  <StrictMode>
    <BootstrapErrorBoundary>
      <App />
    </BootstrapErrorBoundary>
  </StrictMode>,
)
