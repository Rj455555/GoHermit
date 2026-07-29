import { Navigate, Route, Routes } from 'react-router-dom'

import { AgentLandingPage, AgentSessionPage } from '../features/agent/AgentPage'
import { DashboardPage } from '../features/dashboard/DashboardPage'
import { SettingsPage } from '../features/settings/SettingsPage'
import { NotFoundPage } from './NotFoundPage'
import { PlaceholderPage } from './PlaceholderPage'

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/" element={<Navigate replace to="/dashboard" />} />
      <Route path="/dashboard" element={<DashboardPage />} />
      <Route path="/employees" element={<PlaceholderPage page="employees" />} />
      <Route path="/employees/:employeeId" element={<PlaceholderPage page="employees" />} />
      <Route path="/tasks" element={<PlaceholderPage page="tasks" />} />
      <Route path="/tasks/:taskId" element={<PlaceholderPage page="tasks" />} />
      <Route path="/agent" element={<AgentLandingPage />} />
      <Route path="/agent/sessions/:sessionId" element={<AgentSessionPage />} />
      <Route path="/loops" element={<PlaceholderPage page="loops" />} />
      <Route path="/loops/:loopId" element={<PlaceholderPage page="loops" />} />
      <Route
        path="/loops/:loopId/invocations/:invocationId"
        element={<PlaceholderPage page="loops" />}
      />
      <Route path="/settings" element={<SettingsPage />} />
      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  )
}
