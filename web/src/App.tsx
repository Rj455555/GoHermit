import './styles.css'

import { useLocation } from 'react-router-dom'

import { RouteErrorBoundary } from './components/RouteErrorBoundary'
import { AppShell } from './layouts/AppShell'
import { AppRoutes } from './routes/AppRoutes'
import { AppProviders } from './state/AppProviders'

function RoutedApplication() {
  const location = useLocation()
  return (
    <AppShell>
      <RouteErrorBoundary key={location.pathname}>
        <AppRoutes />
      </RouteErrorBoundary>
    </AppShell>
  )
}

export function App() {
  return (
    <AppProviders>
      <RoutedApplication />
    </AppProviders>
  )
}
