import { describe, expect, it } from 'vitest'

import {
  decodeEmployeeActivity,
  decodeEmployeeDryRun,
  decodeEmployeeKnowledge,
  decodeEmployeeMemory,
  decodeEmployeeMemoryCandidates,
  decodeProjects,
  decodeWeixinAccounts,
  decodeWeixinBindings,
  decodeWeixinInbox,
  decodeWeixinLoginAttempt,
} from './decoders'

const now = '2026-07-29T08:00:00Z'

describe('bounded channel and employee projection decoders', () => {
  it('decodes Weixin account, login, binding, and inbox projections', () => {
    const account = {
      id: 'account-1', label: 'Owner Weixin', state: 'connected',
      weixin_user_id: 'wx-user', created_at: now, updated_at: now,
    }
    expect(decodeWeixinAccounts({ accounts: [account] }).accounts[0]?.state).toBe('connected')
    expect(decodeWeixinLoginAttempt({
      id: 'attempt-1', account_id: 'account-1', state: 'qr_pending',
      expires_at: now, created_at: now, updated_at: now, qr_available: true,
    }).qr_available).toBe(true)
    expect(decodeWeixinBindings({ bindings: [{
      id: 'binding-1', account_id: 'account-1', peer_id: 'peer-1',
      employee_id: 'employee-1', enabled: true, mention_required: false,
      created_at: now, updated_at: now,
    }] }).bindings).toHaveLength(1)
    expect(decodeWeixinInbox({ items: [{
      id: 'inbox-1', account_id: 'account-1', peer_id: 'peer-1',
      message_id: 'message-1', sequence: 1, text: 'hello', state: 'unbound',
      received_at: now,
    }, {
      id: 'inbox-2', account_id: 'account-1', peer_id: 'peer-2', group_id: 'group-1',
      message_id: 'message-2', sequence: 2, text: null, state: 'queued', task_id: 'task-1',
      received_at: now,
    }] }).items[0]?.text).toBe('hello')
  })

  it('decodes readiness, knowledge, memory, project, and bounded activity records', () => {
    expect(decodeEmployeeDryRun({
      employee_id: 'employee-1', revision: 2, ready: true,
      checks: [{ name: 'model', ready: true, detail: 'ready' }],
    }).ready).toBe(true)
    expect(decodeProjects({ projects: [{
      id: 'project-1', label: 'Workspace', workspace_real_path: '/workspace',
      workspace_fingerprint: 'fingerprint',
    }] }).projects[0]?.id).toBe('project-1')

    const citation = {
      schema_version: 1, id: 'citation-1', employee_id: 'employee-1',
      source_id: 'source-1', path: 'docs/guide.md', heading: 'Guide',
      start_line: 1, end_line: 2, digest: 'digest', snippet: 'bounded',
    }
    expect(decodeEmployeeKnowledge({
      employee_id: 'employee-1',
      sources: [{ schema_version: 1, id: 'source-1', employee_id: 'employee-1',
        kind: 'manual_text', title: 'Guide', manual_text: 'text', digest: 'digest', status: 'ready' }],
      indexes: [{ schema_version: 1, employee_id: 'employee-1', source_id: 'source-1',
        source_digest: 'digest', documents: [{ path: 'docs/guide.md', digest: 'digest', terms: ['guide'], citations: [citation] }] }],
      results: [{ source_id: 'source-1', title: 'Guide', score: 1, citation }],
    }).indexes).toHaveLength(1)

    const provenance = [{ source_type: 'task', source_id: 'task-1', verified_at: now }]
    const candidate = {
      schema_version: 1, id: 'candidate-1', employee_id: 'employee-1', category: 'preference',
      value: 'bounded fact', provenance, created_at: now, digest: 'digest',
    }
    expect(decodeEmployeeMemoryCandidates({ employee_id: 'employee-1', candidates: [candidate] }).candidates).toHaveLength(1)
    expect(decodeEmployeeMemory({ employee_id: 'employee-1', facts: [{
      ...candidate, candidate_id: 'candidate-1', updated_at: now, owner_edited: false,
    }] }).facts).toHaveLength(1)
    expect(decodeEmployeeActivity({ events: [{
      schema_version: 1, id: 'activity-1', employee_id: 'employee-1', type: 'updated',
      time: now, employee_revision: 2, subject_id: 'subject-1',
    }, {
      schema_version: 1, id: 'activity-2', employee_id: 'employee-1', type: 'created', time: now,
    }] }).events[0]?.type).toBe('updated')
  })
})
