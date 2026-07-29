import { DecodeError } from './errors'
import type {
  Access,
  AgentCatalogItem,
  ApprovalRequest,
  CodexLoginSession,
  Company,
  Handoff,
  Health,
  Info,
  InvocationStatus,
  InvocationSummary,
  LoopSummary,
  Message,
  Mission,
  ModelOption,
  OwnerEnvironment,
  OwnerFact,
  OwnerIdentity,
  OwnerPreferences,
  OwnerProfile,
  Plan,
  PlanMode,
  PlanStatus,
  PlanStep,
  PlanStepStatus,
  ProviderReadiness,
  Run,
  RunStatus,
  RuntimeEvent,
  RuntimeEventType,
  RuntimeSelection,
  SessionDetail,
  SessionDetailResponse,
  SessionSelection,
  SessionStatus,
  SessionSummary,
  TestResult,
  ToolRecord,
  WorkItem,
} from './types'

const MAX_TEXT = 64 << 10
const MAX_STREAM_CHUNK = 32 << 10
const MAX_COLLECTION = 500
const MAX_SMALL_COLLECTION = 100
const ID_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$/u

function fail(): never {
  throw new DecodeError()
}

function object(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) fail()
  return value as Record<string, unknown>
}

function string(value: unknown, max = MAX_TEXT): string {
  if (typeof value !== 'string' || new TextEncoder().encode(value).byteLength > max) fail()
  return value
}

function optionalString(value: unknown, max = MAX_TEXT): string | undefined {
  return value === undefined || value === null ? undefined : string(value, max)
}

function boolean(value: unknown): boolean {
  if (typeof value !== 'boolean') fail()
  return value
}

function integer(value: unknown): number {
  if (!Number.isSafeInteger(value) || (value as number) < 0) fail()
  return value as number
}

function optionalInteger(value: unknown): number {
  return value === undefined || value === null ? 0 : integer(value)
}

function id(value: unknown): string {
  const result = string(value, 256)
  if (!ID_PATTERN.test(result) || result.includes('..')) fail()
  return result
}

function optionalID(value: unknown): string | undefined {
  return value === undefined || value === null || value === '' ? undefined : id(value)
}

function time(value: unknown): string {
  const result = string(value, 128)
  if (!Number.isFinite(Date.parse(result))) fail()
  return result
}

function optionalTime(value: unknown): string | undefined {
  return value === undefined || value === null ? undefined : time(value)
}

function enumeration<T extends string>(value: unknown, allowed: readonly T[]): T {
  if (typeof value !== 'string' || !allowed.includes(value as T)) fail()
  return value as T
}

function array<T>(
  value: unknown,
  decode: (item: unknown) => T,
  max = MAX_COLLECTION,
): T[] {
  if (!Array.isArray(value) || value.length > max) fail()
  return value.map(decode)
}

function optionalArray<T>(
  value: unknown,
  decode: (item: unknown) => T,
  max = MAX_COLLECTION,
): T[] {
  return value === undefined || value === null ? [] : array(value, decode, max)
}

function stringRecord(value: unknown): Record<string, string> {
  const source = object(value)
  if (Object.keys(source).length > MAX_COLLECTION) fail()
  const result: Record<string, string> = {}
  for (const [key, entry] of Object.entries(source)) {
    if (!ID_PATTERN.test(key) && key.length > 1024) fail()
    result[key] = string(entry)
  }
  return result
}

const SESSION_STATUSES = ['open', 'archived', 'running', 'completed', 'failed', 'cancelled'] as const
const RUN_STATUSES = ['queued', 'running', 'verifying', 'completed', 'failed', 'cancelled', 'interrupted'] as const
const PLAN_MODES = ['auto', 'review'] as const
const PLAN_STATUSES = ['active', 'completed', 'failed', 'cancelled'] as const
const PLAN_STEP_STATUSES = ['pending', 'in_progress', 'completed', 'failed', 'cancelled'] as const
const TEAM_ROLES = ['lead', 'explorer', 'builder', 'reviewer', 'verifier', 'operator'] as const
const MISSION_STATUSES = ['queued', 'running', 'completed', 'failed', 'cancelled', 'interrupted'] as const
const WORK_STATUSES = ['queued', 'running', 'completed', 'failed', 'cancelled', 'interrupted', 'skipped'] as const
const APPROVAL_STATUSES = ['pending', 'approved', 'denied', 'expired', 'consumed'] as const
const INVOCATION_STATUSES = ['prepared', 'dispatched', 'attached', 'completed', 'skipped', 'blocked', 'failed', 'cancelled'] as const
export const RUNTIME_EVENT_TYPES = [
  'task_started', 'turn_started', 'model_started', 'model_delta', 'model_completed',
  'tool_started', 'tool_completed', 'permission_required', 'checkpoint_saved',
  'run_verifying', 'run_interrupted', 'workspace_changed', 'memory_updated',
  'session_updated', 'task_completed', 'task_failed', 'task_cancelled',
  'mission_started', 'mission_completed', 'mission_failed', 'work_item_started',
  'work_item_completed', 'work_item_failed', 'plan_created', 'plan_updated',
  'approval_requested', 'approval_decided', 'approval_expired', 'approval_consumed',
  'provider_fallback',
] as const

function decodeSelection(value: unknown): RuntimeSelection {
  const source = object(value)
  return {
    company: id(source.company),
    access: id(source.access),
    model: id(source.model),
    agent: id(source.agent),
  }
}

function decodeModel(value: unknown): ModelOption {
  const source = object(value)
  return { id: id(source.id), label: string(source.label, 1024), provider: id(source.provider) }
}

function decodeAccess(value: unknown): Access {
  const source = object(value)
  return {
    id: id(source.id),
    label: string(source.label, 1024),
    auth_type: enumeration(source.auth_type, ['api_key', 'oauth_external'] as const),
    description: string(source.description),
    api_key_env: optionalString(source.api_key_env, 256),
    supported: boolean(source.supported),
    models: array(source.models, decodeModel, MAX_SMALL_COLLECTION),
  }
}

function decodeCompany(value: unknown): Company {
  const source = object(value)
  return {
    id: id(source.id),
    label: string(source.label, 1024),
    access: array(source.access, decodeAccess, MAX_SMALL_COLLECTION),
  }
}

function decodeAgent(value: unknown): AgentCatalogItem {
  const source = object(value)
  return {
    id: id(source.id),
    label: string(source.label, 1024),
    description: string(source.description),
    read_only: boolean(source.read_only),
    tool_policy: id(source.tool_policy),
  }
}

export function decodeHealth(value: unknown): Health {
  const source = object(value)
  return {
    status: enumeration(source.status, ['ok'] as const),
    version: string(source.version, 256),
    active: boolean(source.active),
  }
}

export function decodeInfo(value: unknown): Info {
  const source = object(value)
  const model = object(source.model)
  const owner = object(source.owner)
  const readinessSource = object(source.auth_status)
  if (Object.keys(readinessSource).length > MAX_SMALL_COLLECTION) fail()
  const auth_status: Record<string, ProviderReadiness> = {}
  for (const [key, entry] of Object.entries(readinessSource)) {
    if (!ID_PATTERN.test(key)) fail()
    const item = object(entry)
    auth_status[key] = {
      configured: boolean(item.configured),
      source: optionalString(item.source, 1024) ?? '',
      detail: optionalString(item.detail, 4096) ?? '',
    }
  }
  return {
    version: string(source.version, 256),
    workspace: string(source.workspace, 4096),
    model: {
      provider: id(model.provider),
      protocol: id(model.protocol),
      base_url: string(model.base_url, 4096),
      model: id(model.model),
      api_key_env: optionalString(model.api_key_env, 256) ?? '',
      api_key_configured: boolean(model.api_key_configured),
    },
    selection: decodeSelection(source.selection),
    companies: array(source.companies, decodeCompany, MAX_SMALL_COLLECTION),
    available_companies: array(source.available_companies, decodeCompany, MAX_SMALL_COLLECTION),
    agents: array(source.agents, decodeAgent, MAX_SMALL_COLLECTION),
    auth_status,
    active: boolean(source.active),
    owner: {
      configured: boolean(owner.configured),
      display_name: optionalString(owner.display_name, 1024),
    },
  }
}

function decodeOwnerIdentity(value: unknown): OwnerIdentity {
  const source = object(value)
  return {
    display_name: optionalString(source.display_name, 8192) ?? '',
    timezone: optionalString(source.timezone, 8192) ?? '',
    language: optionalString(source.language, 8192) ?? '',
  }
}

function decodeOwnerPreferences(value: unknown): OwnerPreferences {
  const source = object(value)
  return {
    communication: optionalString(source.communication, 8192) ?? '',
    coding: optionalString(source.coding, 8192) ?? '',
    git: optionalString(source.git, 8192) ?? '',
    verification: optionalString(source.verification, 8192) ?? '',
    risk: optionalString(source.risk, 8192) ?? '',
  }
}

function decodeOwnerEnvironment(value: unknown): OwnerEnvironment {
  const source = object(value)
  return {
    id: id(source.id),
    label: string(source.label, 8192),
    kind: optionalString(source.kind, 8192) ?? '',
    alias: optionalString(source.alias, 8192) ?? '',
    notes: optionalString(source.notes, 8192) ?? '',
  }
}

function decodeOwnerFact(value: unknown): OwnerFact {
  const source = object(value)
  return {
    id: id(source.id),
    category: string(source.category, 8192),
    value: string(source.value, 8192),
    source: optionalString(source.source, 8192) ?? '',
    confirmed: boolean(source.confirmed),
    created_at: time(source.created_at),
    updated_at: time(source.updated_at),
  }
}

export function decodeOwnerProfile(value: unknown): OwnerProfile {
  const source = object(value)
  return {
    schema_version: integer(source.schema_version),
    identity: decodeOwnerIdentity(source.identity),
    preferences: decodeOwnerPreferences(source.preferences),
    environments: optionalArray(source.environments, decodeOwnerEnvironment, 64),
    facts: optionalArray(source.facts, decodeOwnerFact, 256),
    updated_at: optionalTime(source.updated_at),
  }
}

export function decodeCodexLogin(value: unknown): CodexLoginSession {
  const source = object(value)
  const verification_url = optionalString(source.verification_url, 4096)
  if (verification_url !== undefined) {
    const parsed = new URL(verification_url)
    if (parsed.protocol !== 'https:') fail()
  }
  return {
    id: id(source.id),
    status: enumeration(source.status, ['pending', 'approved', 'error', 'expired', 'cancelled'] as const),
    user_code: optionalString(source.user_code, 256),
    verification_url,
    error: optionalString(source.error, 4096),
    expires_at: time(source.expires_at),
  }
}

function decodeSessionSelection(value: unknown): SessionSelection {
  return decodeSelection(value)
}

function decodeSessionSummary(value: unknown): SessionSummary {
  const source = object(value)
  return {
    id: id(source.id),
    title: string(source.title, 4096),
    status: enumeration(source.status, SESSION_STATUSES),
    updated_at: time(source.updated_at),
    active_run_id: optionalID(source.active_run_id),
    last_run_status: source.last_run_status === undefined || source.last_run_status === ''
      ? undefined
      : enumeration(source.last_run_status, RUN_STATUSES),
    selection: decodeSessionSelection(source.selection),
  }
}

export function decodeSessionList(value: unknown): { sessions: SessionSummary[] } {
  const source = object(value)
  return { sessions: array(source.sessions, decodeSessionSummary, MAX_COLLECTION) }
}

function decodeMessage(value: unknown): Message {
  const source = object(value)
  return {
    id: id(source.id),
    run_id: id(source.run_id),
    role: enumeration(source.role, ['system', 'user', 'assistant', 'tool'] as const),
    content: string(source.content),
    created_at: time(source.created_at),
  }
}

function decodePlanStep(value: unknown): PlanStep {
  const source = object(value)
  return {
    id: id(source.id),
    title: string(source.title, 8192),
    status: enumeration<PlanStepStatus>(source.status, PLAN_STEP_STATUSES),
    detail: optionalString(source.detail),
    started_at: optionalTime(source.started_at),
    completed_at: optionalTime(source.completed_at),
    updated_at: time(source.updated_at),
  }
}

function decodePlan(value: unknown): Plan {
  const source = object(value)
  return {
    schema_version: integer(source.schema_version),
    id: id(source.id),
    status: enumeration<PlanStatus>(source.status, PLAN_STATUSES),
    revision: integer(source.revision),
    allow_parallel: source.allow_parallel === undefined ? false : boolean(source.allow_parallel),
    steps: array(source.steps, decodePlanStep, MAX_SMALL_COLLECTION),
    created_at: time(source.created_at),
    updated_at: time(source.updated_at),
  }
}

function decodeRun(value: unknown): Run {
  const source = object(value)
  return {
    id: id(source.id),
    message: string(source.message),
    status: enumeration<RunStatus>(source.status, RUN_STATUSES),
    started_at: time(source.started_at),
    updated_at: time(source.updated_at),
    completed_at: optionalTime(source.completed_at),
    start_turn: integer(source.start_turn),
    end_turn: optionalInteger(source.end_turn),
    last_mutation_turn: optionalInteger(source.last_mutation_turn),
    last_verification_turn: optionalInteger(source.last_verification_turn),
    verification_attempts: optionalInteger(source.verification_attempts),
    model_calls: optionalInteger(source.model_calls),
    prompt_tokens: optionalInteger(source.prompt_tokens),
    completion_tokens: optionalInteger(source.completion_tokens),
    total_tokens: optionalInteger(source.total_tokens),
    plan: source.plan === undefined || source.plan === null ? undefined : decodePlan(source.plan),
    plan_mode: source.plan_mode === undefined || source.plan_mode === ''
      ? 'auto'
      : enumeration<PlanMode>(source.plan_mode, PLAN_MODES),
    plan_approved: source.plan_approved === undefined ? false : boolean(source.plan_approved),
    plan_approved_at: optionalTime(source.plan_approved_at),
    modified_files: optionalArray(source.modified_files, (item) => string(item, 4096)),
    final_message: optionalString(source.final_message) ?? '',
    error: optionalString(source.error, 4096) ?? '',
  }
}

function decodeToolRecord(value: unknown): ToolRecord {
  const source = object(value)
  const status = optionalString(source.status, 64) ?? ''
  if (!['', 'started', 'completed', 'uncertain'].includes(status)) fail()
  return {
    time: time(source.time),
    run_id: optionalID(source.run_id),
    turn: optionalInteger(source.turn),
    call_id: id(source.call_id),
    name: string(source.name, 1024),
    args_digest: optionalString(source.args_digest, 256),
    summary: string(source.summary),
    is_error: boolean(source.is_error),
    status: status as ToolRecord['status'],
    started_at: optionalTime(source.started_at),
    completed_at: optionalTime(source.completed_at),
  }
}

function decodeTestResult(value: unknown): TestResult {
  const source = object(value)
  return {
    command: string(source.command, 8192),
    passed: boolean(source.passed),
    summary: string(source.summary),
    exit_code: optionalInteger(source.exit_code),
    duration_ms: optionalInteger(source.duration_ms),
    time: time(source.time),
    run_id: optionalID(source.run_id),
    turn: optionalInteger(source.turn),
  }
}

function decodeWorkItem(value: unknown): WorkItem {
  const source = object(value)
  return {
    id: id(source.id),
    title: string(source.title, 8192),
    goal: string(source.goal),
    role: enumeration(source.role, TEAM_ROLES),
    status: enumeration(source.status, WORK_STATUSES),
    depends_on: optionalArray(source.depends_on, id, MAX_SMALL_COLLECTION),
    mutates_workspace: source.mutates_workspace === undefined ? false : boolean(source.mutates_workspace),
    attempt: optionalInteger(source.attempt),
    started_at: optionalTime(source.started_at),
    updated_at: time(source.updated_at),
    completed_at: optionalTime(source.completed_at),
    handoff_id: optionalID(source.handoff_id),
    execution_session_id: optionalID(source.execution_session_id),
    error: optionalString(source.error, 4096),
  }
}

function decodeMissionUsage(value: unknown) {
  const source = object(value)
  return { model_calls: integer(source.model_calls), tokens: integer(source.tokens) }
}

function decodeRoleUsage(value: unknown) {
  if (value === undefined || value === null) return {}
  const source = object(value)
  if (Object.keys(source).length > TEAM_ROLES.length) fail()
  const result: Record<string, ReturnType<typeof decodeMissionUsage>> = {}
  for (const [role, usage] of Object.entries(source)) {
    if (!TEAM_ROLES.includes(role as (typeof TEAM_ROLES)[number])) fail()
    result[role] = decodeMissionUsage(usage)
  }
  return result
}

function decodeHandoff(value: unknown): Handoff {
  const source = object(value)
  return {
    id: id(source.id),
    work_item_id: id(source.work_item_id),
    role: enumeration(source.role, TEAM_ROLES),
    summary: string(source.summary),
    evidence: optionalArray(source.evidence, (item) => string(item), MAX_SMALL_COLLECTION),
    modified_files: optionalArray(source.modified_files, (item) => string(item, 4096), MAX_SMALL_COLLECTION),
    checks: optionalArray(source.checks, (item) => {
      const check = object(item)
      return {
        command: string(check.command, 8192),
        passed: boolean(check.passed),
        summary: optionalString(check.summary) ?? '',
        exit_code: optionalInteger(check.exit_code),
        duration_ms: optionalInteger(check.duration_ms),
      }
    }, MAX_SMALL_COLLECTION),
    issues: optionalArray(source.issues, (item) => string(item), MAX_SMALL_COLLECTION),
    next_steps: optionalArray(source.next_steps, (item) => string(item), MAX_SMALL_COLLECTION),
    substeps: optionalArray(source.substeps, (item) => {
      const substep = object(item)
      return {
        id: id(substep.id),
        title: string(substep.title, 8192),
        goal: string(substep.goal),
        role: enumeration(substep.role, TEAM_ROLES),
        depends_on: optionalArray(substep.depends_on, id, MAX_SMALL_COLLECTION),
      }
    }, 8),
    findings: optionalArray(source.findings, (item) => {
      const finding = object(item)
      return {
        severity: enumeration(finding.severity, ['blocking', 'advisory'] as const),
        summary: string(finding.summary),
      }
    }, MAX_SMALL_COLLECTION),
    created_at: time(source.created_at),
  }
}

function decodeMission(value: unknown): Mission {
  const source = object(value)
  return {
    id: id(source.id),
    run_id: id(source.run_id),
    goal: string(source.goal),
    template: id(source.template),
    status: enumeration(source.status, MISSION_STATUSES),
    budget: (() => {
      const budget = object(source.budget)
      return {
        max_work_items: integer(budget.max_work_items),
        max_model_calls: integer(budget.max_model_calls),
        max_tokens: integer(budget.max_tokens),
        timeout: integer(budget.timeout),
        role_limits: decodeRoleUsage(budget.role_limits),
      }
    })(),
    usage: decodeMissionUsage(source.usage),
    usage_by_role: decodeRoleUsage(source.usage_by_role),
    work_items: array(source.work_items, decodeWorkItem, MAX_SMALL_COLLECTION),
    handoffs: array(source.handoffs, decodeHandoff, MAX_SMALL_COLLECTION),
    created_at: time(source.created_at),
    updated_at: time(source.updated_at),
    error: optionalString(source.error, 4096),
  }
}

function decodeApproval(value: unknown): ApprovalRequest {
  const source = object(value)
  return {
    request_id: id(source.request_id),
    session_id: id(source.session_id),
    run_id: id(source.run_id),
    mission_id: optionalID(source.mission_id),
    work_item_id: optionalID(source.work_item_id),
    role: optionalID(source.role),
    tool: string(source.tool, 1024),
    resource_paths: array(source.resource_paths, (item) => string(item, 1024), 16),
    args_summary: string(source.args_summary, 2048),
    args_digest: string(source.args_digest, 256),
    policy_fingerprint: string(source.policy_fingerprint, 256),
    plan_revision: integer(source.plan_revision),
    created_at: time(source.created_at),
    expires_at: time(source.expires_at),
    status: enumeration(source.status, APPROVAL_STATUSES),
  }
}

function decodeSession(value: unknown): SessionDetail {
  const source = object(value)
  return {
    schema_version: integer(source.schema_version),
    id: id(source.id),
    title: string(source.title, 4096),
    goal: string(source.goal),
    status: enumeration<SessionStatus>(source.status, SESSION_STATUSES),
    selection: decodeSessionSelection(source.selection),
    plan_mode: source.plan_mode === undefined || source.plan_mode === ''
      ? 'auto'
      : enumeration<PlanMode>(source.plan_mode, PLAN_MODES),
    created_at: time(source.created_at),
    updated_at: time(source.updated_at),
    turns: integer(source.turns),
    runs: array(source.runs, decodeRun, MAX_COLLECTION),
    active_run_id: optionalID(source.active_run_id),
    next_event_sequence: optionalInteger(source.next_event_sequence),
    summary: string(source.summary),
    tool_calls: array(source.tool_calls, decodeToolRecord, MAX_COLLECTION),
    modified_files: stringRecord(source.modified_files),
    completed_steps: array(source.completed_steps, (item) => string(item), MAX_COLLECTION),
    pending_steps: array(source.pending_steps, (item) => string(item), MAX_COLLECTION),
    test_results: array(source.test_results, decodeTestResult, MAX_COLLECTION),
    last_error: optionalString(source.last_error, 4096),
    workspace: string(source.workspace, 4096),
    git_state: optionalString(source.git_state, 4096),
    config_digest: string(source.config_digest, 1024),
    workspace_changed: source.workspace_changed === undefined ? false : boolean(source.workspace_changed),
    mission: source.mission === undefined || source.mission === null ? undefined : decodeMission(source.mission),
    approval_requests: optionalArray(source.approval_requests, decodeApproval, MAX_COLLECTION),
  }
}

export function decodeSessionDetail(value: unknown): SessionDetailResponse {
  const source = object(value)
  return {
    session: decodeSession(source.session),
    messages: array(source.messages, decodeMessage, MAX_COLLECTION),
  }
}

export function decodeApprovals(value: unknown): { approvals: ApprovalRequest[] } {
  const source = object(value)
  return { approvals: array(source.approvals, decodeApproval, MAX_COLLECTION) }
}

export function decodeRuntimeEvent(value: unknown): RuntimeEvent {
  const source = object(value)
  const type = enumeration<RuntimeEventType>(source.type, RUNTIME_EVENT_TYPES)
  const sequence = optionalInteger(source.sequence)
  const run_id = optionalID(source.run_id)
  const turn = optionalInteger(source.turn)
  const message = optionalString(source.message, type === 'model_delta' ? MAX_STREAM_CHUNK : MAX_TEXT)
  if (type === 'model_delta') {
    if (sequence !== 0 || run_id === undefined || turn < 1 || message === undefined || message.length === 0) fail()
  } else if (sequence < 1) {
    fail()
  }
  if (source.data !== undefined && JSON.stringify(source.data).length > MAX_TEXT) fail()
  return {
    type,
    time: time(source.time),
    session_id: id(source.session_id),
    run_id,
    mission_id: optionalID(source.mission_id),
    work_item_id: optionalID(source.work_item_id),
    agent_id: optionalID(source.agent_id),
    plan_step_id: optionalID(source.plan_step_id),
    sequence,
    turn,
    tool: optionalString(source.tool, 1024),
    message,
    error: optionalString(source.error, 4096),
  }
}

export function decodeLoops(value: unknown): { loops: LoopSummary[] } {
  const source = object(value)
  return {
    loops: array(source.loops, (item): LoopSummary => {
      const loop = object(item)
      return {
        id: id(loop.id),
        name: string(loop.name, 8192),
        enabled: boolean(loop.enabled),
        created_at: time(loop.created_at),
        updated_at: time(loop.updated_at),
        revision: integer(loop.revision),
      }
    }, MAX_SMALL_COLLECTION),
  }
}

export function decodeInvocations(value: unknown): { invocations: InvocationSummary[]; limit: number } {
  const source = object(value)
  return {
    invocations: array(source.invocations, (item): InvocationSummary => {
      const invocation = object(item)
      return {
        id: id(invocation.id),
        loop_id: id(invocation.loop_id),
        definition_revision: integer(invocation.definition_revision),
        trigger: id(invocation.trigger),
        task_snapshot: string(invocation.task_snapshot),
        session_id: optionalID(invocation.session_id),
        run_id: optionalID(invocation.run_id),
        status: enumeration<InvocationStatus>(invocation.status, INVOCATION_STATUSES),
        created_at: time(invocation.created_at),
        started_at: optionalTime(invocation.started_at),
        finished_at: optionalTime(invocation.finished_at),
        failure_code: optionalString(invocation.failure_code, 256),
        failure_summary: optionalString(invocation.failure_summary, 4096),
      }
    }, MAX_SMALL_COLLECTION),
    limit: integer(source.limit),
  }
}

export function decodeRunReference(value: unknown): { session_id: string; run_id: string } {
  const source = object(value)
  return { session_id: id(source.session_id), run_id: id(source.run_id) }
}

export function decodeSessionCreated(value: unknown): SessionDetail {
  return decodeSession(value)
}

export function decodeConfigured(value: unknown): { configured: boolean; provider: string } {
  const source = object(value)
  return { configured: boolean(source.configured), provider: id(source.provider) }
}

export function decodeDecision(value: unknown): { request: ApprovalRequest } {
  const source = object(value)
  return { request: decodeApproval(source.request) }
}

export function decodeCancellation(value: unknown): {
  cancelled?: boolean | undefined
  status?: string | undefined
} {
  const source = object(value)
  return {
    cancelled: source.cancelled === undefined ? undefined : boolean(source.cancelled),
    status: optionalString(source.status, 64),
  }
}
