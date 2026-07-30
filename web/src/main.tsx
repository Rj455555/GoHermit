import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'

import { App } from './App'
import { BootstrapErrorBoundary } from './BootstrapErrorBoundary'

const rootElement = document.getElementById('root')
if (rootElement === null) {
  throw new Error('React bootstrap root is missing')
}

createRoot(rootElement).render(
  <StrictMode>
    <BrowserRouter>
      <BootstrapErrorBoundary>
        <App />
      </BootstrapErrorBoundary>
    </BrowserRouter>
  </StrictMode>,
)
