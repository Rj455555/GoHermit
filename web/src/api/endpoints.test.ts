import { describe, expect, it, vi } from 'vitest'

const client = vi.hoisted(() => ({ apiRequest: vi.fn().mockResolvedValue({}) }))
vi.mock('./client', () => client)

import {
  approvePlan,
  cancelRun,
  createSession,
  decideApproval,
  deleteProviderCredentials,
  forgetOwnerFact,
  getCodexLogin,
  getHealth,
  getInfo,
  getOwner,
  getSession,
  listApprovals,
  listLoopInvocations,
  listLoops,
  listSessions,
  resumeRun,
  saveOwner,
  saveOwnerFact,
  saveProviderAPIKey,
  startCodexLogin,
  startRun,
} from './endpoints'

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
    expect(paths).toHaveLength(21)
    expect(paths.every((path) => path.startsWith('/api/'))).toBe(true)
    expect(paths).not.toContain('/api/run')
    expect(paths).toContain('/api/sessions/session%20one/runs/run%20one/cancel')
    expect(paths).toContain('/api/settings/providers/openai%20api/api-key')
  })
})
