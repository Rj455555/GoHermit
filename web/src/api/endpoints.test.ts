import { describe, expect, it, vi } from 'vitest'

const client = vi.hoisted(() => ({
  apiRequest: vi.fn().mockResolvedValue({}),
  apiRequestNoContent: vi.fn().mockResolvedValue(undefined),
}))
vi.mock('./client', () => client)

import {
  approvePlan,
  acceptEmployeeMemoryCandidate,
  addEmployeeKnowledge,
  cancelEmployeeTask,
  cancelLoopInvocation,
  cancelRun,
  createEmployee,
  createEmployeeTask,
  createLoop,
  createSession,
  createTaskBoardNote,
  decideApproval,
  deleteEmployeeKnowledge,
  deleteProviderCredentials,
  forgetOwnerFact,
  getEmployee,
  getEmployeeActivity,
  getEmployeeKnowledge,
  getEmployeeMemory,
  getEmployeeMemoryCandidates,
  getEmployeeSkills,
  getEmployeeTask,
  getCodexLogin,
  getHealth,
  getInfo,
  getLoop,
  getLoopInvocation,
  getNotificationStatus,
  getOwner,
  getSession,
  getTaskBoard,
  getTeamTemplate,
  importLoop,
  importTeamTemplate,
  editEmployeeMemory,
  listApprovals,
  listEmployees,
  listEmployeeTasks,
  listLoopInvocationDetails,
  listLoopInvocations,
  listLoops,
  listReports,
  listSessions,
  listProjects,
  listSkills,
  mutateEmployeeLifecycle,
  forgetEmployeeMemory,
  refreshEmployeeKnowledge,
  rejectEmployeeMemoryCandidate,
  resumeEmployeeTask,
  resumeRun,
  retryReport,
  saveOwner,
  saveOwnerFact,
  saveProviderAPIKey,
  startCodexLogin,
  startEmployeeTask,
  startLoopInvocation,
  startRun,
  dryRunEmployee,
  dryRunLoop,
  updateEmployee,
  updateEmployeeSkills,
  updateLoop,
  updateTaskBoardCard,
  updateTaskBoardSettings,
} from './endpoints'
import type { Employee, LoopDefinition } from './types'

const profile = {
  schema_version: 1,
  identity: { display_name: '', timezone: '', language: '' },
  preferences: { communication: '', coding: '', git: '', verification: '', risk: '' },
  environments: [],
  facts: [],
}

describe('Phase 3 endpoint map', () => {
  it('uses only scoped API routes and exercises every typed endpoint wrapper', async () => {
    await Promise.all([
      getHealth(),
      getInfo(),
      getNotificationStatus(),
      listLoops(),
      listLoopInvocations('loop one'),
      getOwner(),
      saveOwner(profile),
      saveOwnerFact('fact one', { category: 'c', value: 'v', source: 'owner', confirmed: true }),
      forgetOwnerFact('fact one'),
      saveProviderAPIKey('openai api', 'transient'),
      deleteProviderCredentials('openai api'),
      startCodexLogin(),
      getCodexLogin('login one'),
      listSessions(),
      getSession('session one'),
      createSession({
        title: 'title',
        company: 'openai',
        access: 'openai-codex',
        model: 'gpt-5.6',
        agent: 'coding',
        plan_mode: 'auto',
      }),
      startRun('session one', 'message'),
      cancelRun('session one', 'run one'),
      resumeRun('session one', 'run one'),
      approvePlan('session one', 'run one'),
      listApprovals('session one'),
      decideApproval('session one', 'approval one', 'approve'),
    ])

    const paths = client.apiRequest.mock.calls.map(([path]) => path as string)
    expect(paths).toHaveLength(22)
    expect(paths.every((path) => path.startsWith('/api/'))).toBe(true)
    expect(paths).not.toContain('/api/run')
    expect(paths).toContain('/api/sessions?limit=100')
    expect(paths).not.toContain('/api/sessions?limit=200')
    expect(paths).toContain('/api/sessions/session%20one/runs/run%20one/cancel')
    expect(paths).toContain('/api/settings/providers/openai%20api/api-key')
  })

  it('maps every Phase 4 wrapper without introducing a second execution API', async () => {
    const employee = {
      id: 'employee-1',
      revision: 1,
      skill_bindings: [],
      project_count: 3,
    } as unknown as Employee
    const definition = {
      id: 'loop-1',
      revision: 2,
    } as unknown as LoopDefinition
    await Promise.all([
      listEmployees({ state: 'active', cursor: 'next', limit: 500 }),
      getEmployee('employee one'),
      createEmployee({ employee, project_bindings: [] }),
      updateEmployee('employee one', {
        expected_revision: 1,
        employee,
        project_bindings: [],
      }),
      mutateEmployeeLifecycle('employee one', 'disable', 1),
      mutateEmployeeLifecycle('employee one', 'enable', 2),
      mutateEmployeeLifecycle('employee one', 'archive', 3),
      dryRunEmployee('employee one'),
      listProjects(),
      listSkills(),
      getEmployeeSkills('employee one'),
      updateEmployeeSkills('employee one', 1, []),
      getEmployeeKnowledge('employee one'),
      addEmployeeKnowledge('employee one', {
        id: 'source-1',
        kind: 'manual_text',
        title: 'Guide',
        manual_text: 'bounded text',
      }),
      refreshEmployeeKnowledge('employee one', 'source one'),
      deleteEmployeeKnowledge('employee one', 'source one'),
      getEmployeeMemory('employee one'),
      getEmployeeMemoryCandidates('employee one'),
      acceptEmployeeMemoryCandidate('employee one', 'candidate one'),
      rejectEmployeeMemoryCandidate('employee one', 'candidate one'),
      editEmployeeMemory('employee one', 'fact one', 'updated'),
      forgetEmployeeMemory('employee one', 'fact one'),
      getEmployeeActivity('employee one'),
      listEmployeeTasks('employee one', { limit: 500 }),
      createEmployeeTask('employee one', { prompt: 'literal' }),
      getEmployeeTask('task one'),
      startEmployeeTask('task one'),
      cancelEmployeeTask('task one'),
      resumeEmployeeTask('task one'),
      getLoop('loop one'),
      createLoop(definition),
      updateLoop('loop one', definition),
      importLoop(definition),
      dryRunLoop('loop one'),
      listLoopInvocationDetails('loop one'),
      startLoopInvocation('loop one'),
      getLoopInvocation('invocation one'),
      cancelLoopInvocation('invocation one'),
      listReports(),
      retryReport('report one'),
      getTaskBoard(),
      updateTaskBoardSettings({
        definition: {
          id: 'default',
          name: 'Task Board',
          columns: [{ id: 'queued', title: 'Queued', color: 'blue', hidden: false }],
        },
        view: { view: 'board', column_width: 320, wip_enabled: true },
        filters: { states: ['queued'], labels: ['memory'] },
      }),
      updateTaskBoardCard('task one', {
        column_id: 'queued',
        rank: 1,
        labels: ['memory'],
        priority: 2,
        due_at: null,
        pinned: true,
        blocked: false,
        blocker_reason: '',
        depends_on: [],
        source_url: '/tasks/task-one',
        loop_id: '',
      }),
      createTaskBoardNote({
        title: 'Verify Memory P0.1',
        body: 'Run the isolated acceptance suite.',
        column_id: 'queued',
        rank: 2,
        labels: ['memory'],
        priority: 1,
        due_at: null,
        pinned: false,
        source_url: '',
        blocker_reason: '',
      }),
      getTeamTemplate(),
      importTeamTemplate({
        schema_version: 2,
        name: 'Team',
        default: { company: 'openai', access: 'codex', model: 'gpt' },
        roles: {},
      }),
    ])

    const paths = client.apiRequest.mock.calls.map(([path]) => path as string)
    expect(paths).toContain('/api/employees?limit=100&state=active&cursor=next')
    expect(paths).toContain('/api/employees/employee%20one/tasks?limit=100')
    expect(paths).toContain('/api/employee-tasks/task%20one/start')
    expect(paths).toContain('/api/loops/loop%20one/dry-run')
    expect(paths).toContain('/api/loop-invocations/invocation%20one/cancel')
    expect(paths).toContain('/api/reports?limit=100')
    expect(paths).toContain('/api/reports/report%20one/retry')
    expect(paths).toContain('/api/task-board')
    expect(paths).toContain('/api/task-board/cards/task%20one')
    expect(paths).toContain('/api/task-board/notes')
    expect(paths).not.toContain('/api/tasks/task%20one/events')
    const retryCall = client.apiRequest.mock.calls.find(([path]) =>
      path === '/api/reports/report%20one/retry')
    expect(retryCall?.[2]).toMatchObject({ method: 'POST' })
    const boardCardCall = client.apiRequest.mock.calls.find(([path]) =>
      path === '/api/task-board/cards/task%20one')
    expect(boardCardCall?.[2]).toMatchObject({
      method: 'PUT',
      body: { column_id: 'queued', rank: 1, priority: 2 },
    })
    const createCall = client.apiRequest.mock.calls.find(([path]) => path === '/api/employees')
    const createOptions = createCall?.[2] as unknown as {
      method: string
      body: { employee: Record<string, unknown>; project_bindings: unknown[] }
    }
    expect(createOptions).toMatchObject({
      method: 'POST',
      body: {
        employee: {
          id: 'employee-1',
          revision: 1,
          skill_bindings: [],
        },
        project_bindings: [],
      },
    })
    expect(createOptions.body.employee).not.toHaveProperty('project_count')
    const endpointCalls = client.apiRequest.mock.calls as unknown as Array<[
      string,
      unknown,
      { method?: string; body?: { employee: Record<string, unknown> } },
    ]>
    const updateCall = endpointCalls.find(([path, , options]) =>
      path === '/api/employees/employee%20one' && options.method === 'PUT')
    expect(updateCall?.[2].body?.employee).not.toHaveProperty('project_count')
    const emptyPaths = client.apiRequestNoContent.mock.calls.map(([path]) => path as string)
    expect(emptyPaths).toContain('/api/employees/employee%20one/knowledge/source%20one')
    expect(emptyPaths).toContain('/api/employees/employee%20one/memory/fact%20one')
  })
})
