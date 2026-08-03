import { Navigate, Route, Routes } from 'react-router-dom'

import { AgentLandingPage, AgentSessionPage } from '../features/agent/AgentPage'
import { DashboardPage } from '../features/dashboard/DashboardPage'
import { EmployeeDetailPage, EmployeesPage } from '../features/employees/EmployeesPage'
import { LoopDetailPage, LoopInvocationPage, LoopsPage } from '../features/loops/LoopsPage'
import { SettingsPage } from '../features/settings/SettingsPage'
import { ReportsPage } from '../features/reports/ReportsPage'
import { TaskDetailPage, TasksPage } from '../features/tasks/TasksPage'
import { NotFoundPage } from './NotFoundPage'

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/" element={<Navigate replace to="/dashboard" />} />
      <Route path="/dashboard" element={<DashboardPage />} />
      <Route path="/employees" element={<EmployeesPage />} />
      <Route path="/employees/:employeeId" element={<EmployeeDetailPage />} />
      <Route path="/tasks" element={<TasksPage />} />
      <Route path="/tasks/:taskId" element={<TaskDetailPage />} />
      <Route path="/agent" element={<AgentLandingPage />} />
      <Route path="/agent/sessions/:sessionId" element={<AgentSessionPage />} />
      <Route path="/loops" element={<LoopsPage />} />
      <Route path="/loops/:loopId" element={<LoopDetailPage />} />
      <Route
        path="/loops/:loopId/invocations/:invocationId"
        element={<LoopInvocationPage />}
      />
      <Route path="/settings" element={<SettingsPage />} />
      <Route path="/reports" element={<ReportsPage />} />
      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  )
}
