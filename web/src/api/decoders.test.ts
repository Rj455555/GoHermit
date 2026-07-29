import { describe, expect, it } from 'vitest'

import {
  decodeApprovals,
  decodeCancellation,
  decodeCodexLogin,
  decodeConfigured,
  decodeDecision,
  decodeHealth,
  decodeInfo,
  decodeInvocations,
  decodeLoops,
  decodeOwnerProfile,
  decodeRunReference,
  decodeSessionCreated,
  decodeRuntimeEvent,
  decodeSessionDetail,
  decodeSessionList,
} from './decoders'

const now = '2026-07-29T08:00:00Z'

function session(overrides: Record<string, unknown> = {}) {
  return {
    schema_version: 6,
    id: 'session-1',
    title: 'Untranslated title',
    goal: 'User prompt',
    status: 'open',
    selection: { company: 'openai', access: 'openai-codex', model: 'gpt-5.6', agent: 'coding' },
    created_at: now,
    updated_at: now,
    turns: 0,
    runs: [],
    recent_messages: [],
    summary: '',
    tool_calls: [],
    modified_files: {},
    completed_steps: [],
    pending_steps: [],
    test_results: [],
    workspace: '/workspace',
    config_digest: 'digest',
    ...overrides,
  }
}

describe('endpoint decoders', () => {
  it('validates Health fields and enums', () => {
    expect(decodeHealth({ status: 'ok', version: '0.3', active: false })).toEqual({
      status: 'ok',
      version: '0.3',
      active: false,
    })
    expect(() => decodeHealth({ status: 'maybe', version: '0.3', active: false })).toThrow()
  })

  it('defaults a missing next_event_sequence to zero', () => {
    expect(
      decodeSessionDetail({ session: session(), messages: [] }).session.next_event_sequence,
    ).toBe(0)
  })

  it.each([-1, 1.5, Number.MAX_SAFE_INTEGER + 1])(
    'rejects unsafe next_event_sequence %s',
    (nextEventSequence) => {
      expect(() =>
        decodeSessionDetail({
          session: session({ next_event_sequence: nextEventSequence }),
          messages: [],
        }),
      ).toThrow()
    },
  )

  it('rejects invalid IDs, timestamps, enums, and oversized arrays', () => {
    expect(() =>
      decodeSessionList({
        sessions: [
          {
            id: '../hidden',
            title: 'bad',
            status: 'open',
            updated_at: now,
            selection: { company: '', access: '', model: '', agent: '' },
          },
        ],
      }),
    ).toThrow()
    expect(() =>
      decodeSessionDetail({ session: session({ updated_at: 'not-time' }), messages: [] }),
    ).toThrow()
    expect(() =>
      decodeSessionDetail({ session: session({ status: 'invented' }), messages: [] }),
    ).toThrow()
    const messages = Array.from({ length: 8_192 }, (_, index) => ({
      id: `message-${index}`,
      run_id: 'run-1',
      role: 'assistant',
      content: 'bounded',
      created_at: now,
    }))
    expect(decodeSessionDetail({ session: session(), messages }).messages).toHaveLength(8_192)
    expect(() =>
      decodeSessionDetail({
        session: session(),
        messages: [...messages, messages[0]],
      }),
    ).toThrow()
  })

  it('accepts model_delta only with sequence zero and validates its scope', () => {
    expect(
      decodeRuntimeEvent({
        type: 'model_delta',
        time: now,
        session_id: 'session-1',
        run_id: 'run-1',
        sequence: 0,
        turn: 1,
        message: '<b>text only</b>',
      }),
    ).toMatchObject({ type: 'model_delta', message: '<b>text only</b>' })
    expect(() =>
      decodeRuntimeEvent({
        type: 'task_started',
        time: now,
        session_id: 'session-1',
        sequence: 0,
      }),
    ).toThrow()
    expect(() =>
      decodeRuntimeEvent({
        type: 'model_delta',
        time: now,
        session_id: 'session-1',
        sequence: 0,
        run_id: '',
        turn: 0,
        message: 'delta',
      }),
    ).toThrow()
  })

  it('decodes the complete Phase 3 authoritative projection surface', () => {
    const model = { id: 'gpt-5.6', label: 'GPT-5.6', provider: 'openai-codex' }
    const access = {
      id: 'openai-codex',
      label: 'Codex',
      auth_type: 'oauth_external',
      description: 'Login',
      supported: true,
      models: [model],
    }
    const company = { id: 'openai', label: 'OpenAI', access: [access] }
    expect(decodeInfo({
      version: '0.3',
      workspace: '/workspace',
      model: {
        provider: 'openai-codex',
        protocol: 'responses',
        base_url: 'https://api.openai.com',
        model: 'gpt-5.6',
        api_key_env: '',
        api_key_configured: true,
      },
      selection: { company: 'openai', access: 'openai-codex', model: 'gpt-5.6', agent: 'coding' },
      companies: [company],
      available_companies: [company],
      agents: [{
        id: 'coding',
        label: 'Coding',
        description: 'Agent',
        read_only: false,
        tool_policy: 'workspace',
      }],
      auth_status: { 'openai-codex': { configured: true, source: 'test', detail: 'ready' } },
      active: true,
      owner: { configured: true, display_name: 'Owner' },
    })).toMatchObject({ workspace: '/workspace', active: true })

    const owner = decodeOwnerProfile({
      schema_version: 1,
      identity: { display_name: 'Owner', timezone: 'UTC', language: 'en-US' },
      preferences: {
        communication: 'brief',
        coding: 'strict',
        git: 'safe',
        verification: 'full',
        risk: 'low',
      },
      environments: [{ id: 'mac', label: 'Mac', kind: 'host', alias: 'mini', notes: 'local' }],
      facts: [{
        id: 'fact-1',
        category: 'preference',
        value: 'text',
        source: 'owner',
        confirmed: true,
        created_at: now,
        updated_at: now,
      }],
      updated_at: now,
    })
    expect(owner.facts).toHaveLength(1)
    expect(decodeCodexLogin({
      id: 'login-1',
      status: 'pending',
      user_code: 'ABCD',
      verification_url: 'https://example.test/login',
      expires_at: now,
    }).verification_url).toBe('https://example.test/login')

    const approval = {
      request_id: 'approval-1',
      session_id: 'session-1',
      run_id: 'run-1',
      mission_id: 'mission-1',
      work_item_id: 'work-1',
      role: 'coding',
      tool: 'write_file',
      resource_paths: ['/workspace/file.go'],
      args_summary: 'write one file',
      args_digest: 'digest',
      policy_fingerprint: 'policy',
      plan_revision: 1,
      created_at: now,
      expires_at: now,
      status: 'pending',
    }
    const projection = session({
      status: 'running',
      next_event_sequence: 4,
      active_run_id: 'run-1',
      runs: [{
        id: 'run-1',
        message: 'Implement',
        status: 'running',
        started_at: now,
        updated_at: now,
        start_turn: 1,
        end_turn: 2,
        plan_mode: 'review',
        plan_approved: true,
        modified_files: ['file.go'],
        final_message: '',
        error: '',
        plan: {
          schema_version: 1,
          id: 'plan-1',
          status: 'active',
          revision: 1,
          allow_parallel: false,
          steps: [{ id: 'step-1', title: 'Edit', status: 'in_progress', detail: 'detail', updated_at: now }],
          created_at: now,
          updated_at: now,
        },
      }],
      tool_calls: [{
        time: now,
        run_id: 'run-1',
        turn: 1,
        call_id: 'call-1',
        name: 'write_file',
        args_digest: 'digest',
        summary: 'wrote file',
        is_error: false,
        status: 'completed',
        started_at: now,
        completed_at: now,
      }],
      test_results: [{
        command: 'go test ./...',
        passed: true,
        summary: 'pass',
        exit_code: 0,
        duration_ms: 12,
        time: now,
        run_id: 'run-1',
        turn: 2,
      }],
      mission: {
        id: 'mission-1',
        run_id: 'run-1',
        goal: 'Ship',
        template: 'default',
        status: 'running',
        budget: {
          max_work_items: 8,
          max_model_calls: 20,
          max_tokens: 10000,
          timeout: 60000000000,
          role_limits: { builder: { model_calls: 5, tokens: 2000 } },
        },
        usage: { model_calls: 1, tokens: 100 },
        usage_by_role: { builder: { model_calls: 1, tokens: 100 } },
        work_items: [{
          id: 'work-1',
          title: 'Work',
          goal: 'Edit',
          role: 'builder',
          status: 'running',
          depends_on: [],
          mutates_workspace: true,
          attempt: 1,
          updated_at: now,
        }],
        handoffs: [{
          id: 'handoff-1',
          work_item_id: 'work-1',
          role: 'builder',
          summary: 'done',
          evidence: ['test'],
          modified_files: ['file.go'],
          checks: [{ command: 'go test ./...', passed: true, summary: 'pass', exit_code: 0, duration_ms: 1 }],
          issues: [],
          next_steps: [],
          substeps: [],
          findings: [{ severity: 'advisory', summary: 'none' }],
          created_at: now,
        }],
        created_at: now,
        updated_at: now,
      },
      approval_requests: [approval],
    })
    const detail = decodeSessionDetail({
      session: projection,
      messages: [{
        id: 'message-1',
        run_id: 'run-1',
        role: 'assistant',
        content: 'text',
        created_at: now,
      }],
    })
    expect(detail.session.runs[0]?.plan?.steps).toHaveLength(1)
    expect(detail.session.mission?.handoffs).toHaveLength(1)
    expect(detail.session.tool_calls).toHaveLength(1)
    expect(detail.session.test_results).toHaveLength(1)
    expect(decodeSessionCreated(projection).id).toBe('session-1')
    expect(decodeApprovals({ approvals: [approval] }).approvals).toHaveLength(1)
    expect(decodeDecision({ request: approval }).request.request_id).toBe('approval-1')
    expect(decodeLoops({ loops: [{
      id: 'loop-1',
      name: 'Loop',
      enabled: true,
      created_at: now,
      updated_at: now,
      revision: 1,
    }] }).loops).toHaveLength(1)
    expect(decodeInvocations({
      invocations: [{
        id: 'invocation-1',
        loop_id: 'loop-1',
        definition_revision: 1,
        trigger: 'manual',
        task_snapshot: 'Task',
        session_id: 'session-1',
        run_id: 'run-1',
        status: 'completed',
        created_at: now,
        started_at: now,
        finished_at: now,
      }],
      limit: 50,
    }).invocations).toHaveLength(1)
    expect(decodeRunReference({ session_id: 'session-1', run_id: 'run-1' })).toEqual({
      session_id: 'session-1',
      run_id: 'run-1',
    })
    expect(decodeConfigured({ configured: true, provider: 'openai-codex' }).configured).toBe(true)
    expect(decodeCancellation({ cancelled: true, status: 'cancelled' })).toEqual({
      cancelled: true,
      status: 'cancelled',
    })
  })
})
