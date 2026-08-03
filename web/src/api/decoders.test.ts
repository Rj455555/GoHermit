import { describe, expect, it } from 'vitest'

import {
  decodeApprovals,
  decodeCancellation,
  decodeBoundedProjection,
  decodeCodexLogin,
  decodeConfigured,
  decodeDecision,
  decodeDryRun,
  decodeEmployeeList,
  decodeEmployeeRecord,
  decodeEmployeeSkills,
  decodeEmployeeTask,
  decodeEmployeeTasks,
  decodeHealth,
  decodeInfo,
  decodeInvocations,
  decodeLoopDefinition,
  decodeLoopDefinitions,
  decodeLoopInvocation,
  decodeLoopInvocationList,
  decodeLoopRuntimeState,
  decodeNotificationStatus,
  decodeLoops,
  decodeOwnerProfile,
  decodeRunReference,
  decodeSessionCreated,
  decodeRuntimeEvent,
  decodeSessionDetail,
  decodeSessionList,
  decodeSkillCatalog,
  decodeTeamTemplate,
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

  it('decodes the bounded notification readiness projection without credentials', () => {
    expect(decodeNotificationStatus({
      configured: true,
      recipient: '1143130628@qq.com',
      from: 'sender@qq.com',
      host: 'smtp.qq.com',
      last_sent_at: now,
    })).toEqual({
      configured: true,
      recipient: '1143130628@qq.com',
      from: 'sender@qq.com',
      host: 'smtp.qq.com',
      last_sent_at: now,
    })
    expect(() => decodeNotificationStatus({ configured: false, recipient: 123 })).toThrow()
  })

  it('strictly decodes the complete bounded Phase 4 projections', () => {
    const summary = {
      id: 'employee-1',
      revision: 2,
      state: 'active',
      name: 'Literal Employee',
      job_title: 'Builder',
      agent_profile: 'coding',
      project_count: 1,
      created_at: now,
      updated_at: now,
    }
    const employee = {
      ...summary,
      schema_version: 1,
      avatar: { kind: 'initials', value: 'L' },
      charter: 'Literal charter',
      responsibilities: ['Build'],
      behavior_boundaries: ['No secrets'],
      default_selection: { company: 'openai', access: 'codex', model: 'gpt' },
      skill_bindings: [{
        skill_id: 'review',
        version: '1.0.0',
        digest: 'digest',
        configuration: {},
        enabled: true,
      }],
      project_binding_ids: ['project-1'],
      permission_policy: { allowed_capabilities: ['read'], network_allowed: false },
      budget_policy: { max_model_calls: 4, max_tokens: 4000, timeout_seconds: 600 },
      concurrency_policy: { max_running_tasks: 1 },
      memory_policy: {
        candidate_generation: true,
        promotion: 'owner_confirmation',
        max_context_facts: 8,
        max_context_bytes: 8192,
      },
    }
    const project = {
      id: 'project-1',
      employee_id: 'employee-1',
      label: 'Literal project',
      workspace_real_path: '/literal/path',
      workspace_fingerprint: 'fingerprint',
      read_allowed: true,
      mutation_allowed: false,
      allowed_tool_capabilities: ['read'],
      network_allowed: false,
      budget_override: { max_model_calls: 3, max_tokens: 3000, timeout_seconds: 300 },
      created_at: now,
      updated_at: now,
    }
    const task = {
      schema_version: 1,
      id: 'task-1',
      employee_id: 'employee-1',
      employee_revision: 2,
      prompt: 'Literal prompt',
      state: 'queued',
      created_at: now,
      updated_at: now,
      cancelled_at: now,
      employee_snapshot: {
        schema_version: 1,
        employee_id: 'employee-1',
        revision: 2,
        captured_at: now,
        digest: 'employee-snapshot-digest',
      },
      skills: [{
        skill_id: 'review',
        version: '1.0.0',
        digest: 'skill-digest',
        configuration: { mode: 'strict' },
        enabled: true,
      }],
      knowledge: [{
        source_id: 'source-1',
        source_digest: 'source-digest',
        citations: [{
          citation_id: 'citation-1',
          path: 'docs/guide.md',
          digest: 'citation-digest',
          start_line: 10,
          end_line: 14,
        }],
      }],
      memory_facts: [{ fact_id: 'fact-1', digest: 'memory-digest' }],
      project_binding: {
        id: project.id,
        label: project.label,
        workspace_fingerprint: project.workspace_fingerprint,
        read_allowed: project.read_allowed,
        mutation_allowed: project.mutation_allowed,
        allowed_tool_capabilities: project.allowed_tool_capabilities,
        network_allowed: project.network_allowed,
        budget_override: project.budget_override,
      },
      policy: {
        allowed_capabilities: ['read'],
        network_allowed: false,
        budget: { max_model_calls: 4, max_tokens: 4000, timeout_seconds: 600 },
      },
      snapshot_digest: 'snapshot',
      artifacts: [{
        schema_version: 1,
        id: 'artifact-1',
        employee_id: 'employee-1',
        task_id: 'task-1',
        session_id: 'session-1',
        run_id: 'run-1',
        path: 'literal/file.go',
        digest: 'artifact-digest',
        verified_at: now,
      }],
    }
    const definition = {
      id: 'loop-1',
      schema_version: 1,
      name: 'Literal Loop',
      description: 'Description',
      workspace_identity: '/literal/path',
      enabled: true,
      task_source: { type: 'fixed_prompt', prompt: 'Literal mission' },
      agent_selection: { company: 'openai', access: 'codex', model: 'gpt', agent: 'team' },
      team_template_ref: 'default',
      plan_mode: 'review',
      verification_recipe: {
        checks: [{
          id: 'unit',
          command: ['go', 'test', './...'],
          required: true,
          timeout_seconds: 120,
        }],
        independent_verifier: true,
        max_repair_attempts: 1,
      },
      budget: { max_model_calls: 8, max_tokens: 8000, timeout_seconds: 1200 },
      approval_policy: { require_for_mutation: true },
      workspace_policy: { read_only: true, require_clean_git: false },
      output_policy: { include_diff: false, max_report_bytes: 65536 },
      created_at: now,
      updated_at: now,
      revision: 3,
    }
    const invocation = {
      id: 'invocation-1',
      loop_id: 'loop-1',
      definition_revision: 3,
      definition_snapshot: definition,
      trigger: 'manual',
      task_snapshot: 'Literal mission',
      session_id: 'session-1',
      run_id: 'run-1',
      status: 'attached',
      created_at: now,
      started_at: now,
    }

    expect(decodeEmployeeList({ employees: [summary], next_cursor: 'next' }).employees).toHaveLength(1)
    expect(decodeEmployeeRecord({ employee, project_bindings: [project] }).employee.name).toBe('Literal Employee')
    const decodedCatalog = decodeSkillCatalog({ skills: [{
      skill_id: 'review',
      version: '1.0.0',
      digest: 'digest',
      kind: 'native',
      title: 'Review',
      description: 'Literal',
      requested_capabilities: ['read'],
      configuration_schema: {},
    }, {
      skill_id: 'adapter',
      version: 'synthetic-1',
      digest: 'adapter-digest',
      kind: 'skill_md_adapter',
      title: 'Adapter',
      description: 'Zero-capability adapter',
      requested_capabilities: null,
      configuration_schema: {},
    }] }).skills
    expect(decodedCatalog).toHaveLength(2)
    expect(decodedCatalog[1]?.requested_capabilities).toEqual([])
    expect(decodeEmployeeSkills({
      employee_id: 'employee-1',
      revision: 2,
      bindings: [{ binding: employee.skill_bindings[0], status: 'current', kind: 'native' }],
    }).bindings).toHaveLength(1)
    expect(decodeBoundedProjection({ facts: [] })).toEqual({ facts: [] })
    const decodedTask = decodeEmployeeTask(task)
    expect(decodedTask.prompt).toBe('Literal prompt')
    expect(decodedTask.skills[0]).toEqual(task.skills[0])
    expect(decodedTask.knowledge[0]?.citations[0]).toEqual(task.knowledge[0]?.citations[0])
    expect(decodedTask.memory_facts[0]).toEqual({ fact_id: 'fact-1', digest: 'memory-digest' })
    expect(decodedTask.employee_snapshot.digest).toBe('employee-snapshot-digest')
    expect(decodedTask.cancelled_at).toBe(now)
    expect(decodedTask.project_binding.budget_override).toEqual(project.budget_override)
    expect(decodeEmployeeTasks({ tasks: [task] }).tasks).toHaveLength(1)
    const decodedDefinition = decodeLoopDefinition(definition)
    expect(decodedDefinition.revision).toBe(3)
    expect(decodedDefinition.verification_recipe.checks[0]).toEqual({
      id: 'unit',
      command: ['go', 'test', './...'],
      required: true,
      timeout_seconds: 120,
    })
    const legacyDefinition = {
      ...definition,
      team_template_ref: undefined,
      verification_recipe: {
        independent_verifier: true,
        max_repair_attempts: 0,
      },
    }
    expect(decodeLoopDefinition(legacyDefinition)).toMatchObject({
      team_template_ref: '',
      verification_recipe: { checks: [] },
    })
    expect(decodeLoopDefinitions({ loops: [definition] }).loops).toHaveLength(1)
    expect(decodeLoopRuntimeState({
      schema_version: 1,
      loop_id: 'loop-1',
      definition_revision: 3,
      last_invocation_id: 'invocation-1',
      last_status: 'completed',
      last_run_at: now,
      next_run_at: now,
      consecutive_failures: 0,
      total_runs: 4,
      successful_runs: 3,
      updated_at: now,
    })).toMatchObject({
      loop_id: 'loop-1',
      last_invocation_id: 'invocation-1',
      total_runs: 4,
      successful_runs: 3,
    })
    expect(decodeLoopInvocation(invocation).id).toBe('invocation-1')
    expect(decodeLoopInvocationList({ invocations: [invocation], limit: 50 }).limit).toBe(50)
    expect(decodeDryRun({
      loop_id: 'loop-1',
      definition_revision: 3,
      definition_valid: true,
      workspace_identity: '/literal/path',
      workspace_matches: true,
      git_clean: true,
      task_prompt: 'Literal mission',
      agent: definition.agent_selection,
      roles: [],
      write_scope: 'read-only',
      checks: [],
      budget: definition.budget,
      requires_approval: false,
      ready: true,
      reasons: [],
    }).ready).toBe(true)
    expect(decodeDryRun({
      loop_id: 'loop-1',
      definition_revision: 3,
      definition_valid: true,
      workspace_identity: '/literal/path',
      workspace_matches: true,
      git_clean: true,
      task_prompt: 'Literal mission',
      agent: definition.agent_selection,
      write_scope: 'read-only',
      budget: definition.budget,
      requires_approval: false,
      ready: true,
    })).toMatchObject({ roles: [], checks: [], reasons: [] })
    expect(decodeTeamTemplate({
      schema_version: 2,
      name: 'default',
      default: { company: 'openai', access: 'codex', model: 'gpt' },
      roles: {
        builder: {
          company: '',
          access: '',
          model: '',
          employee_id: 'employee-builder',
        },
      },
      updated_at: now,
    }).roles.builder?.employee_id).toBe('employee-builder')
  })

  it('decodes the canonical Go Employee record when empty slices are omitted', () => {
    const record = decodeEmployeeRecord({
      employee: {
        id: 'employee-quick',
        schema_version: 1,
        revision: 1,
        state: 'active',
        name: '档案管理员',
        avatar: { kind: 'initials', value: '档' },
        job_title: '岗位待配置',
        charter: '角色细节尚未配置。',
        default_selection: {
          company: 'openai',
          access: 'openai-codex',
          model: 'gpt-5.6-sol',
        },
        agent_profile: 'team',
        project_binding_ids: ['project-employee-quick'],
        permission_policy: {
          allowed_capabilities: ['read'],
          network_allowed: false,
        },
        budget_policy: {
          max_model_calls: 8,
          max_tokens: 32_000,
          timeout_seconds: 1_200,
        },
        concurrency_policy: { max_running_tasks: 1 },
        memory_policy: {
          candidate_generation: false,
          promotion: 'disabled',
          max_context_facts: 0,
          max_context_bytes: 0,
        },
        created_at: now,
        updated_at: now,
      },
      project_bindings: [{
        id: 'project-employee-quick',
        employee_id: 'employee-quick',
        label: 'workspace',
        workspace_real_path: '/workspace',
        workspace_fingerprint: 'fingerprint',
        read_allowed: true,
        mutation_allowed: false,
        allowed_tool_capabilities: ['read'],
        network_allowed: false,
        created_at: now,
        updated_at: now,
      }],
    })

    expect(record.employee.project_count).toBe(1)
    expect(record.employee.responsibilities).toEqual([])
    expect(record.employee.behavior_boundaries).toEqual([])
    expect(record.employee.skill_bindings).toEqual([])
  })

  it('defaults a missing next_event_sequence to zero', () => {
    expect(
      decodeSessionDetail({ session: session(), messages: [] }).session.next_event_sequence,
    ).toBe(0)
  })

  it('keeps legacy Sessions with an empty selection readable', () => {
    const decoded = decodeSessionList({
      sessions: [{
        id: 'legacy-session',
        title: 'Legacy session',
        status: 'open',
        updated_at: now,
        selection: {},
      }],
    })
    expect(decoded.sessions[0]?.selection).toEqual({
      company: '',
      access: '',
      model: '',
      agent: '',
    })
    expect(() => decodeSessionList({
      sessions: [{
        id: 'partial-session',
        title: 'Partial session',
        status: 'open',
        updated_at: now,
        selection: { company: 'openai' },
      }],
    })).toThrow()
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
