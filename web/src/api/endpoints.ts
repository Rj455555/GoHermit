import { apiRequest, apiRequestNoContent, type ApiRequestOptions } from './client'
import {
  decodeApprovals,
  decodeCancellation,
  decodeCodexLogin,
  decodeConfigured,
  decodeBoundedProjection,
  decodeDryRun,
  decodeEmployeeActivity,
  decodeEmployeeDryRun,
  decodeEmployeeKnowledge,
  decodeEmployeeList,
  decodeEmployeeRecord,
  decodeEmployeeMemory,
  decodeEmployeeMemoryCandidates,
  decodeEmployeeSkills,
  decodeEmployeeTask,
  decodeEmployeeTasks,
  decodeTaskBoard,
  decodeDecision,
  decodeHealth,
  decodeInfo,
  decodeInvocations,
  decodeLoopDefinition,
  decodeLoopInvocation,
  decodeLoopInvocationList,
  decodeLoopRuntimeState,
  decodeLoopDefinitions,
  decodeNotificationStatus,
  decodeReport,
  decodeReports,
  decodeOwnerProfile,
  decodeProjects,
  decodeSkillCatalog,
  decodeMemoryFact,
  decodeTeamTemplate,
  decodeRunReference,
  decodeSessionCreated,
  decodeSessionDetail,
  decodeSessionList,
} from './decoders'
import type {
  Employee,
  LoopDefinition,
  OwnerProfile,
  PlanMode,
  ProjectBinding,
  SkillBinding,
  TeamTemplate,
  TaskBoardCardInput,
  TaskBoardNoteInput,
  TaskBoardSettingsInput,
} from './types'

type ReadOptions = Pick<ApiRequestOptions, 'signal'>
type ProjectBindingDraft = Omit<
  ProjectBinding,
  'employee_id' | 'workspace_fingerprint' | 'created_at' | 'updated_at'
> & Partial<Pick<ProjectBinding, 'employee_id' | 'workspace_fingerprint' | 'created_at' | 'updated_at'>>
type WritableEmployee = Omit<Employee, 'project_count'>

function segment(value: string): string {
  return encodeURIComponent(value)
}

function writableEmployee(value: Employee): WritableEmployee {
  const employee: WritableEmployee & { project_count?: number } = { ...value }
  delete employee.project_count
  return employee
}

export const getHealth = (options: ReadOptions = {}) =>
  apiRequest('/api/health', decodeHealth, options)
export const getInfo = (options: ReadOptions = {}) =>
  apiRequest('/api/info', decodeInfo, options)
export const listLoops = (options: ReadOptions = {}) =>
  apiRequest('/api/loops', decodeLoopDefinitions, options)
export const getLoopRuntime = (loopId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/loops/${segment(loopId)}/runtime`, decodeLoopRuntimeState, options)
export const getNotificationStatus = (options: ReadOptions = {}) =>
  apiRequest('/api/settings/notifications', decodeNotificationStatus, options)
export const listReports = (options: ReadOptions = {}) =>
  apiRequest('/api/reports?limit=100', decodeReports, options)
export const retryReport = (reportId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/reports/${segment(reportId)}/retry`, decodeReport, {
    ...options,
    method: 'POST',
  })
export const listLoopInvocations = (loopId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/loops/${segment(loopId)}/invocations?limit=50`, decodeInvocations, options)

export const listEmployees = (
  query: { state?: string; cursor?: string; limit?: number } = {},
  options: ReadOptions = {},
) => {
  const values = new URLSearchParams()
  values.set('limit', String(Math.min(100, Math.max(1, query.limit ?? 100))))
  if (query.state) values.set('state', query.state)
  if (query.cursor) values.set('cursor', query.cursor)
  return apiRequest(`/api/employees?${values.toString()}`, decodeEmployeeList, options)
}
export const getEmployee = (employeeId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/employees/${segment(employeeId)}`, decodeEmployeeRecord, options)
export const createEmployee = (
  input: { employee: Employee; project_bindings: ProjectBindingDraft[] },
  options: ReadOptions = {},
) => {
  return apiRequest('/api/employees', decodeEmployeeRecord, {
    ...options,
    method: 'POST',
    body: { ...input, employee: writableEmployee(input.employee) },
  })
}
export const updateEmployee = (
  employeeId: string,
  input: { expected_revision: number; employee: Employee; project_bindings: ProjectBinding[] },
  options: ReadOptions = {},
) => apiRequest(`/api/employees/${segment(employeeId)}`, decodeEmployeeRecord, {
  ...options,
  method: 'PUT',
  body: { ...input, employee: writableEmployee(input.employee) },
})
export const mutateEmployeeLifecycle = (
  employeeId: string,
  action: 'disable' | 'enable' | 'archive',
  expectedRevision: number,
  options: ReadOptions = {},
) => apiRequest(`/api/employees/${segment(employeeId)}/${action}`, decodeEmployeeRecord, {
  ...options,
  method: 'POST',
  body: { expected_revision: expectedRevision },
})
export const dryRunEmployee = (employeeId: string, options: ReadOptions = {}) =>
  apiRequest(
    `/api/employees/${segment(employeeId)}/dry-run`,
    decodeEmployeeDryRun,
    { ...options, method: 'POST' },
  )
export const listProjects = (options: ReadOptions = {}) =>
  apiRequest('/api/projects', decodeProjects, options)
export const listSkills = (options: ReadOptions = {}) =>
  apiRequest('/api/skills', decodeSkillCatalog, options)
export const getEmployeeSkills = (employeeId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/employees/${segment(employeeId)}/skills`, decodeEmployeeSkills, options)
export const updateEmployeeSkills = (
  employeeId: string,
  expectedRevision: number,
  bindings: SkillBinding[],
  options: ReadOptions = {},
) => apiRequest(`/api/employees/${segment(employeeId)}/skills`, decodeEmployeeRecord, {
  ...options,
  method: 'PUT',
  body: { expected_revision: expectedRevision, bindings },
})
export const getEmployeeKnowledge = (employeeId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/employees/${segment(employeeId)}/knowledge?limit=32`, decodeEmployeeKnowledge, options)
export const addEmployeeKnowledge = (
  employeeId: string,
  source: {
    id: string
    kind: 'manual_text' | 'file' | 'project_docs'
    title: string
    manual_text?: string | undefined
    relative_path?: string | undefined
  },
  options: ReadOptions = {},
) => apiRequest(`/api/employees/${segment(employeeId)}/knowledge`, decodeEmployeeKnowledge, {
  ...options,
  method: 'POST',
  body: source,
})
export const refreshEmployeeKnowledge = (
  employeeId: string,
  sourceId: string,
  options: ReadOptions = {},
) => apiRequest(
  `/api/employees/${segment(employeeId)}/knowledge/${segment(sourceId)}/refresh`,
  decodeEmployeeKnowledge,
  { ...options, method: 'POST' },
)
export const deleteEmployeeKnowledge = (
  employeeId: string,
  sourceId: string,
  options: ReadOptions = {},
) => apiRequestNoContent(
  `/api/employees/${segment(employeeId)}/knowledge/${segment(sourceId)}`,
  { ...options, method: 'DELETE' },
)
export const getEmployeeMemory = (employeeId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/employees/${segment(employeeId)}/memory`, decodeEmployeeMemory, options)
export const getEmployeeMemoryCandidates = (employeeId: string, options: ReadOptions = {}) =>
  apiRequest(
    `/api/employees/${segment(employeeId)}/memory-candidates`,
    decodeEmployeeMemoryCandidates,
    options,
  )
export const acceptEmployeeMemoryCandidate = (
  employeeId: string,
  candidateId: string,
  options: ReadOptions = {},
) => apiRequest(
  `/api/employees/${segment(employeeId)}/memory-candidates/${segment(candidateId)}/accept`,
  decodeMemoryFact,
  { ...options, method: 'POST' },
)
export const rejectEmployeeMemoryCandidate = (
  employeeId: string,
  candidateId: string,
  options: ReadOptions = {},
) => apiRequestNoContent(
  `/api/employees/${segment(employeeId)}/memory-candidates/${segment(candidateId)}`,
  { ...options, method: 'DELETE' },
)
export const editEmployeeMemory = (
  employeeId: string,
  factId: string,
  value: string,
  options: ReadOptions = {},
) => apiRequest(
  `/api/employees/${segment(employeeId)}/memory/${segment(factId)}`,
  decodeMemoryFact,
  { ...options, method: 'PUT', body: { value } },
)
export const forgetEmployeeMemory = (
  employeeId: string,
  factId: string,
  options: ReadOptions = {},
) => apiRequestNoContent(
  `/api/employees/${segment(employeeId)}/memory/${segment(factId)}`,
  { ...options, method: 'DELETE' },
)
export const getEmployeeActivity = (
  employeeId: string,
  query: { limit?: number; cursor?: string } = {},
  options: ReadOptions = {},
) => {
  const values = new URLSearchParams()
  values.set('limit', String(Math.min(100, Math.max(1, query.limit ?? 100))))
  if (query.cursor) values.set('cursor', query.cursor)
  return apiRequest(
    `/api/employees/${segment(employeeId)}/activity?${values.toString()}`,
    decodeEmployeeActivity,
    options,
  )
}
export const listEmployeeTasks = (
  employeeId: string,
  query: { limit?: number } = {},
  options: ReadOptions = {},
) => apiRequest(
  `/api/employees/${segment(employeeId)}/tasks?limit=${Math.min(100, Math.max(1, query.limit ?? 100))}`,
  decodeEmployeeTasks,
  options,
)
export const createEmployeeTask = (
  employeeId: string,
  input: Record<string, unknown>,
  options: ReadOptions = {},
) => apiRequest(`/api/employees/${segment(employeeId)}/tasks`, decodeEmployeeTask, {
  ...options,
  method: 'POST',
  body: input,
})
export const getEmployeeTask = (taskId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/employee-tasks/${segment(taskId)}`, decodeEmployeeTask, options)
export const startEmployeeTask = (taskId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/employee-tasks/${segment(taskId)}/start`, decodeEmployeeTask, {
    ...options,
    method: 'POST',
  })
export const cancelEmployeeTask = (taskId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/employee-tasks/${segment(taskId)}/cancel`, decodeEmployeeTask, {
    ...options,
    method: 'POST',
  })
export const resumeEmployeeTask = (taskId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/employee-tasks/${segment(taskId)}/resume`, decodeEmployeeTask, {
    ...options,
    method: 'POST',
  })

export const getTaskBoard = (options: ReadOptions = {}) =>
  apiRequest('/api/task-board', decodeTaskBoard, options)

export const updateTaskBoardSettings = (
  input: TaskBoardSettingsInput,
  options: ReadOptions = {},
) => apiRequest('/api/task-board', decodeTaskBoard, {
  ...options,
  method: 'PUT',
  body: input,
})

export const updateTaskBoardCard = (
  taskId: string,
  input: TaskBoardCardInput,
  options: ReadOptions = {},
) => apiRequest(`/api/task-board/cards/${segment(taskId)}`, decodeTaskBoard, {
  ...options,
  method: 'PUT',
  body: input,
})

export const createTaskBoardNote = (
  input: TaskBoardNoteInput,
  options: ReadOptions = {},
) => apiRequest('/api/task-board/notes', decodeTaskBoard, {
  ...options,
  method: 'POST',
  body: input,
})

export const getLoop = (loopId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/loops/${segment(loopId)}`, decodeLoopDefinition, options)
export const createLoop = (definition: LoopDefinition, options: ReadOptions = {}) =>
  apiRequest('/api/loops', decodeLoopDefinition, { ...options, method: 'POST', body: definition })
export const updateLoop = (
  loopId: string,
  definition: LoopDefinition,
  options: ReadOptions = {},
) => apiRequest(`/api/loops/${segment(loopId)}`, decodeLoopDefinition, {
  ...options,
  method: 'PUT',
  body: definition,
})
export const importLoop = (definition: unknown, options: ReadOptions = {}) =>
  apiRequest('/api/loops/import', decodeLoopDefinition, {
    ...options,
    method: 'POST',
    body: definition,
  })
export const dryRunLoop = (loopId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/loops/${segment(loopId)}/dry-run`, decodeDryRun, {
    ...options,
    method: 'POST',
  })
export const listLoopInvocationDetails = (loopId: string, options: ReadOptions = {}) =>
  apiRequest(
    `/api/loops/${segment(loopId)}/invocations?limit=50`,
    decodeLoopInvocationList,
    options,
  )
export const startLoopInvocation = (loopId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/loops/${segment(loopId)}/invocations`, decodeLoopInvocation, {
    ...options,
    method: 'POST',
  })
export const getLoopInvocation = (invocationId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/loop-invocations/${segment(invocationId)}`, decodeLoopInvocation, options)
export const cancelLoopInvocation = (invocationId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/loop-invocations/${segment(invocationId)}/cancel`, decodeLoopInvocation, {
    ...options,
    method: 'POST',
  })
export const getTeamTemplate = (options: ReadOptions = {}) =>
  apiRequest('/api/team-template/export', decodeTeamTemplate, options)
export const importTeamTemplate = (
  input: TeamTemplate | Record<string, unknown>,
  options: ReadOptions = {},
) =>
  apiRequest('/api/team-template/import', decodeBoundedProjection, {
    ...options,
    method: 'POST',
    body: input,
  })

export const getOwner = (options: ReadOptions = {}) =>
  apiRequest('/api/owner', decodeOwnerProfile, options)
export const saveOwner = (profile: OwnerProfile, options: ReadOptions = {}) =>
  apiRequest('/api/owner', decodeOwnerProfile, { ...options, method: 'PUT', body: profile })
export const saveOwnerFact = (
  factId: string,
  fact: { category: string; value: string; source: string; confirmed: boolean },
  options: ReadOptions = {},
) => apiRequest(`/api/owner/facts/${segment(factId)}`, decodeOwnerProfile, {
  ...options,
  method: 'PUT',
  body: fact,
})
export const forgetOwnerFact = (factId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/owner/facts/${segment(factId)}`, decodeOwnerProfile, {
    ...options,
    method: 'DELETE',
  })

export const saveProviderAPIKey = (
  provider: string,
  apiKey: string,
  options: ReadOptions = {},
) => apiRequest(`/api/settings/providers/${segment(provider)}/api-key`, decodeConfigured, {
  ...options,
  method: 'PUT',
  body: { api_key: apiKey },
})
export const deleteProviderCredentials = (provider: string, options: ReadOptions = {}) =>
  apiRequest(
    `/api/settings/providers/${segment(provider)}/credentials`,
    decodeConfigured,
    { ...options, method: 'DELETE' },
  )
export const startCodexLogin = (options: ReadOptions = {}) =>
  apiRequest('/api/settings/providers/openai-codex/login', decodeCodexLogin, {
    ...options,
    method: 'POST',
  })
export const getCodexLogin = (loginId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/settings/logins/${segment(loginId)}`, decodeCodexLogin, options)

export const listSessions = (options: ReadOptions = {}) =>
  apiRequest('/api/sessions?limit=100', decodeSessionList, options)
export const getSession = (sessionId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/sessions/${segment(sessionId)}`, decodeSessionDetail, options)
export const createSession = (
  input: {
    title: string
    company: string
    access: string
    model: string
    agent: string
    plan_mode: PlanMode
  },
  options: ReadOptions = {},
) => apiRequest('/api/sessions', decodeSessionCreated, {
  ...options,
  method: 'POST',
  body: input,
})
export const startRun = (sessionId: string, message: string, options: ReadOptions = {}) =>
  apiRequest(`/api/sessions/${segment(sessionId)}/runs`, decodeRunReference, {
    ...options,
    method: 'POST',
    body: { message },
  })
export const cancelRun = (sessionId: string, runId: string, options: ReadOptions = {}) =>
  apiRequest(
    `/api/sessions/${segment(sessionId)}/runs/${segment(runId)}/cancel`,
    decodeCancellation,
    { ...options, method: 'POST' },
  )
export const resumeRun = (sessionId: string, runId: string, options: ReadOptions = {}) =>
  apiRequest(
    `/api/sessions/${segment(sessionId)}/runs/${segment(runId)}/resume`,
    decodeRunReference,
    { ...options, method: 'POST' },
  )
export const approvePlan = (sessionId: string, runId: string, options: ReadOptions = {}) =>
  apiRequest(
    `/api/sessions/${segment(sessionId)}/runs/${segment(runId)}/approve`,
    decodeRunReference,
    { ...options, method: 'POST' },
  )
export const listApprovals = (sessionId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/sessions/${segment(sessionId)}/approvals?status=pending`, decodeApprovals, options)
export const decideApproval = (
  sessionId: string,
  requestId: string,
  decision: 'approve' | 'deny',
  options: ReadOptions = {},
) => apiRequest(
  `/api/sessions/${segment(sessionId)}/approvals/${segment(requestId)}/decide`,
  decodeDecision,
  { ...options, method: 'POST', body: { decision } },
)
