import { DecodeError } from './errors'
import type {
  Access,
  AgentCatalogItem,
  ApprovalRequest,
  CodexLoginSession,
  Company,
  DryRunReport,
  EmployeeActivity,
  EmployeeDryRun,
  EmployeeKnowledge,
  Employee,
  EmployeeRecord,
  EmployeeSkillStatus,
  EmployeeState,
  EmployeeSummary,
  EmployeeTask,
  EmployeeTaskState,
  Handoff,
  Health,
  Info,
  InvocationStatus,
  InvocationSummary,
  LoopDefinition,
  LoopInvocation,
  LoopRuntimeState,
  LoopSummary,
  NotificationStatus,
  MemoryCandidate,
  MemoryFact,
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
  ProjectCatalogItem,
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
  SkillCatalogItem,
  TeamTemplate,
  TestResult,
  ToolRecord,
  WorkItem,
} from './types'

const MAX_TEXT = 64 << 10
const MAX_STREAM_CHUNK = 32 << 10
const MAX_COLLECTION = 500
const MAX_SMALL_COLLECTION = 100
const MAX_SESSION_RECORDS = 8_192
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

function stringRecord(value: unknown, max = MAX_COLLECTION): Record<string, string> {
  const source = object(value)
  if (Object.keys(source).length > max) fail()
  const result: Record<string, string> = {}
  for (const [key, entry] of Object.entries(source)) {
    if (!ID_PATTERN.test(key) && key.length > 1024) fail()
    result[key] = string(entry)
  }
  return result
}

function boundedRecord(value: unknown, maxBytes = MAX_TEXT): Record<string, unknown> {
  const result = object(value)
  let encoded: string
  try {
    encoded = JSON.stringify(result)
  } catch {
    fail()
  }
  if (new TextEncoder().encode(encoded).byteLength > maxBytes) fail()
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
  const source = object(value)
  const values = [
    optionalID(source.company),
    optionalID(source.access),
    optionalID(source.model),
    optionalID(source.agent),
  ]
  const populated = values.filter((entry) => entry !== undefined).length
  // Sessions created before the runtime-selection contract was introduced
  // legitimately persist an empty selection object. Keep those sessions
  // readable without weakening validation for partially populated or malformed
  // modern selections.
  if (populated === 0) {
    return { company: '', access: '', model: '', agent: '' }
  }
  if (populated !== values.length) fail()
  return {
    company: values[0] ?? '',
    access: values[1] ?? '',
    model: values[2] ?? '',
    agent: values[3] ?? '',
  }
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
    work_items: array(source.work_items, decodeWorkItem, MAX_SESSION_RECORDS),
    handoffs: array(source.handoffs, decodeHandoff, MAX_SESSION_RECORDS),
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
    runs: array(source.runs, decodeRun, MAX_SESSION_RECORDS),
    active_run_id: optionalID(source.active_run_id),
    next_event_sequence: optionalInteger(source.next_event_sequence),
    summary: string(source.summary),
    tool_calls: array(source.tool_calls, decodeToolRecord, MAX_SESSION_RECORDS),
    modified_files: stringRecord(source.modified_files, MAX_SESSION_RECORDS),
    completed_steps: array(source.completed_steps, (item) => string(item), MAX_SESSION_RECORDS),
    pending_steps: array(source.pending_steps, (item) => string(item), MAX_SESSION_RECORDS),
    test_results: array(source.test_results, decodeTestResult, MAX_SESSION_RECORDS),
    last_error: optionalString(source.last_error, 4096),
    workspace: string(source.workspace, 4096),
    git_state: optionalString(source.git_state, 4096),
    config_digest: string(source.config_digest, 1024),
    workspace_changed: source.workspace_changed === undefined ? false : boolean(source.workspace_changed),
    mission: source.mission === undefined || source.mission === null ? undefined : decodeMission(source.mission),
    approval_requests: optionalArray(source.approval_requests, decodeApproval, MAX_SESSION_RECORDS),
  }
}

export function decodeSessionDetail(value: unknown): SessionDetailResponse {
  const source = object(value)
  return {
    session: decodeSession(source.session),
    messages: array(source.messages, decodeMessage, MAX_SESSION_RECORDS),
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
        employee_task_id: optionalID(invocation.employee_task_id),
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

const EMPLOYEE_STATES = ['active', 'disabled', 'archived'] as const
const EMPLOYEE_TASK_STATES = [
  'queued', 'prepared', 'waiting_owner', 'running', 'verifying',
  'completed', 'failed', 'cancelled', 'interrupted',
] as const

function decodeEmployeeSummaryValue(value: unknown): EmployeeSummary {
  const source = object(value)
  return {
    id: id(source.id),
    revision: integer(source.revision),
    state: enumeration<EmployeeState>(source.state, EMPLOYEE_STATES),
    name: string(source.name, 8192),
    job_title: string(source.job_title, 8192),
    agent_profile: id(source.agent_profile),
    project_count: integer(source.project_count),
    created_at: time(source.created_at),
    updated_at: time(source.updated_at),
  }
}

function decodeSkillBinding(value: unknown) {
  const source = object(value)
  return {
    skill_id: id(source.skill_id),
    version: string(source.version, 256),
    digest: string(source.digest, 256),
    configuration: boundedRecord(source.configuration ?? {}),
    enabled: boolean(source.enabled),
  }
}

function decodeProjectBinding(value: unknown) {
  const source = object(value)
  return {
    id: id(source.id),
    employee_id: id(source.employee_id),
    label: string(source.label, 8192),
    workspace_real_path: string(source.workspace_real_path, 4096),
    workspace_fingerprint: string(source.workspace_fingerprint, 256),
    read_allowed: boolean(source.read_allowed),
    mutation_allowed: boolean(source.mutation_allowed),
    allowed_tool_capabilities: array(
      source.allowed_tool_capabilities === undefined ? [] : source.allowed_tool_capabilities,
      (item) => string(item, 256),
      MAX_SMALL_COLLECTION,
    ),
    network_allowed: boolean(source.network_allowed),
    budget_override: source.budget_override === undefined || source.budget_override === null
      ? undefined
      : decodeBudget(source.budget_override),
    created_at: time(source.created_at),
    updated_at: time(source.updated_at),
  }
}

function decodeEmployeeValue(value: unknown): Employee {
  const source = object(value)
  const summary: EmployeeSummary = {
    id: id(source.id),
    revision: integer(source.revision),
    state: enumeration<EmployeeState>(source.state, EMPLOYEE_STATES),
    name: string(source.name, 8192),
    job_title: string(source.job_title, 8192),
    agent_profile: id(source.agent_profile),
    project_count: source.project_count === undefined ? 0 : integer(source.project_count),
    created_at: time(source.created_at),
    updated_at: time(source.updated_at),
  }
  const avatar = object(source.avatar)
  const selection = object(source.default_selection)
  const permission = object(source.permission_policy)
  const budget = object(source.budget_policy)
  const concurrency = object(source.concurrency_policy)
  const memory = object(source.memory_policy)
  return {
    ...summary,
    schema_version: integer(source.schema_version),
    avatar: {
      kind: enumeration(avatar.kind, ['initials', 'emoji'] as const),
      value: string(avatar.value, 64),
    },
    charter: string(source.charter),
    responsibilities: array(
      source.responsibilities === undefined ? [] : source.responsibilities,
      (item) => string(item),
      MAX_SMALL_COLLECTION,
    ),
    behavior_boundaries: array(
      source.behavior_boundaries === undefined ? [] : source.behavior_boundaries,
      (item) => string(item),
      MAX_SMALL_COLLECTION,
    ),
    default_selection: {
      company: id(selection.company),
      access: id(selection.access),
      model: id(selection.model),
    },
    skill_bindings: array(
      source.skill_bindings === undefined ? [] : source.skill_bindings,
      decodeSkillBinding,
      MAX_SMALL_COLLECTION,
    ),
    project_binding_ids: array(
      source.project_binding_ids === undefined ? [] : source.project_binding_ids,
      id,
      MAX_SMALL_COLLECTION,
    ),
    permission_policy: {
      allowed_capabilities: array(
        permission.allowed_capabilities,
        (item) => string(item, 256),
        MAX_SMALL_COLLECTION,
      ),
      network_allowed: boolean(permission.network_allowed),
    },
    budget_policy: {
      max_model_calls: integer(budget.max_model_calls),
      max_tokens: integer(budget.max_tokens),
      timeout_seconds: integer(budget.timeout_seconds),
    },
    concurrency_policy: { max_running_tasks: integer(concurrency.max_running_tasks) },
    memory_policy: {
      candidate_generation: boolean(memory.candidate_generation),
      promotion: enumeration(memory.promotion, ['disabled', 'owner_confirmation'] as const),
      max_context_facts: integer(memory.max_context_facts),
      max_context_bytes: integer(memory.max_context_bytes),
    },
  }
}

export function decodeEmployeeList(value: unknown): {
  employees: EmployeeSummary[]
  next_cursor?: string | undefined
} {
  const source = object(value)
  return {
    employees: array(source.employees, decodeEmployeeSummaryValue, MAX_SMALL_COLLECTION),
    next_cursor: optionalString(source.next_cursor, 1024),
  }
}

export function decodeEmployeeRecord(value: unknown): EmployeeRecord {
  const source = object(value)
  const projectBindings = array(
    source.project_bindings,
    decodeProjectBinding,
    MAX_SMALL_COLLECTION,
  )
  const employee = decodeEmployeeValue(source.employee)
  return {
    employee: { ...employee, project_count: projectBindings.length },
    project_bindings: projectBindings,
  }
}

export function decodeSkillCatalog(value: unknown): { skills: SkillCatalogItem[] } {
  const source = object(value)
  return {
    skills: array(source.skills, (entry): SkillCatalogItem => {
      const skill = object(entry)
      return {
        skill_id: id(skill.skill_id),
        version: string(skill.version, 256),
        digest: string(skill.digest, 256),
        kind: enumeration(skill.kind, ['native', 'skill_md_adapter'] as const),
        title: string(skill.title, 8192),
        description: string(skill.description),
        requested_capabilities: array(
          skill.requested_capabilities,
          (item) => string(item, 256),
          MAX_SMALL_COLLECTION,
        ),
        configuration_schema: boundedRecord(skill.configuration_schema ?? {}),
      }
    }, MAX_SMALL_COLLECTION),
  }
}

export function decodeEmployeeSkills(value: unknown): {
  employee_id: string
  revision: number
  bindings: EmployeeSkillStatus[]
} {
  const source = object(value)
  return {
    employee_id: id(source.employee_id),
    revision: integer(source.revision),
    bindings: array(source.bindings, (entry): EmployeeSkillStatus => {
      const binding = object(entry)
      return {
        binding: decodeSkillBinding(binding.binding),
        status: enumeration(binding.status, ['current', 'missing', 'digest_drift'] as const),
        kind: binding.kind === undefined
          ? undefined
          : enumeration(binding.kind, ['native', 'skill_md_adapter'] as const),
      }
    }, MAX_SMALL_COLLECTION),
  }
}

export function decodeEmployeeDryRun(value: unknown): EmployeeDryRun {
  const source = object(value)
  return {
    employee_id: id(source.employee_id),
    revision: integer(source.revision),
    ready: boolean(source.ready),
    checks: array(source.checks, (entry) => {
      const check = object(entry)
      return {
        name: id(check.name),
        ready: boolean(check.ready),
        detail: string(check.detail, 8192),
      }
    }, MAX_SMALL_COLLECTION),
  }
}

export function decodeProjects(value: unknown): { projects: ProjectCatalogItem[] } {
  const source = object(value)
  return {
    projects: array(source.projects, (entry) => {
      const project = object(entry)
      return {
        id: id(project.id),
        label: string(project.label, 8192),
        workspace_real_path: string(project.workspace_real_path, 4096),
        workspace_fingerprint: string(project.workspace_fingerprint, 256),
      }
    }, MAX_SMALL_COLLECTION),
  }
}

function decodeCitation(value: unknown) {
  const source = object(value)
  return {
    schema_version: integer(source.schema_version),
    id: id(source.id),
    employee_id: id(source.employee_id),
    source_id: id(source.source_id),
    path: string(source.path, 4096),
    heading: optionalString(source.heading, 8192),
    start_line: integer(source.start_line),
    end_line: integer(source.end_line),
    digest: string(source.digest, 256),
    snippet: string(source.snippet, 16 << 10),
  }
}

export function decodeEmployeeKnowledge(value: unknown): EmployeeKnowledge {
  const source = object(value)
  return {
    employee_id: id(source.employee_id),
    sources: array(source.sources, (entry) => {
      const item = object(entry)
      return {
        schema_version: integer(item.schema_version),
        id: id(item.id),
        employee_id: id(item.employee_id),
        kind: enumeration(item.kind, ['manual_text', 'file', 'project_docs'] as const),
        title: string(item.title, 8192),
        relative_path: optionalString(item.relative_path, 4096),
        manual_text: optionalString(item.manual_text, 64 << 10),
        digest: string(item.digest, 256),
        status: enumeration(item.status, ['ready', 'failed'] as const),
        error: optionalString(item.error, 8192),
      }
    }, 128),
    indexes: array(source.indexes, (entry) => {
      const item = object(entry)
      return {
        schema_version: integer(item.schema_version),
        employee_id: id(item.employee_id),
        source_id: id(item.source_id),
        source_digest: string(item.source_digest, 256),
        documents: array(item.documents, (documentValue) => {
          const document = object(documentValue)
          return {
            path: string(document.path, 4096),
            digest: string(document.digest, 256),
            terms: array(document.terms, (term) => string(term, 64), 1024),
            citations: array(document.citations, decodeCitation, 1024),
          }
        }, 256),
      }
    }, 128),
    results: optionalArray(source.results, (entry) => {
      const result = object(entry)
      return {
        source_id: id(result.source_id),
        title: string(result.title, 8192),
        score: integer(result.score),
        citation: decodeCitation(result.citation),
      }
    }, 32),
  }
}

function decodeProvenance(value: unknown) {
  const source = object(value)
  return {
    source_type: id(source.source_type),
    source_id: id(source.source_id),
    source_task_id: optionalID(source.source_task_id),
    source_session_id: optionalID(source.source_session_id),
    source_run_id: optionalID(source.source_run_id),
    verified_at: time(source.verified_at),
  }
}

function decodeMemoryCandidateValue(value: unknown): MemoryCandidate {
  const source = object(value)
  return {
    schema_version: integer(source.schema_version),
    id: id(source.id),
    employee_id: id(source.employee_id),
    category: string(source.category, 8192),
    value: string(source.value, 8192),
    provenance: array(source.provenance, decodeProvenance, 16),
    created_at: time(source.created_at),
    digest: string(source.digest, 256),
  }
}

function decodeMemoryFactValue(value: unknown): MemoryFact {
  const source = object(value)
  return {
    ...decodeMemoryCandidateValue(source),
    candidate_id: id(source.candidate_id),
    updated_at: time(source.updated_at),
    owner_edited: boolean(source.owner_edited),
  }
}

export function decodeEmployeeMemory(value: unknown): { employee_id: string; facts: MemoryFact[] } {
  const source = object(value)
  return {
    employee_id: id(source.employee_id),
    facts: array(source.facts, decodeMemoryFactValue, 512),
  }
}

export function decodeEmployeeMemoryCandidates(
  value: unknown,
): { employee_id: string; candidates: MemoryCandidate[] } {
  const source = object(value)
  return {
    employee_id: id(source.employee_id),
    candidates: array(source.candidates, decodeMemoryCandidateValue, 128),
  }
}

export const decodeMemoryFact = decodeMemoryFactValue

export function decodeEmployeeActivity(value: unknown): EmployeeActivity {
  const source = object(value)
  return {
    events: array(source.events, (entry) => {
      const event = object(entry)
      return {
        schema_version: integer(event.schema_version),
        id: id(event.id),
        employee_id: id(event.employee_id),
        type: id(event.type),
        time: time(event.time),
        employee_revision: event.employee_revision === undefined
          ? undefined
          : integer(event.employee_revision),
        subject_id: optionalID(event.subject_id),
        task_id: optionalID(event.task_id),
        session_id: optionalID(event.session_id),
        run_id: optionalID(event.run_id),
      }
    }, MAX_SMALL_COLLECTION),
    next_cursor: optionalString(source.next_cursor, 1024),
  }
}

export function decodeTeamTemplate(value: unknown): TeamTemplate {
  const source = object(value)
  const decodeRole = (entry: unknown) => {
    const role = object(entry)
    return {
      company: string(role.company, 256),
      access: string(role.access, 256),
      model: string(role.model, 256),
      employee_id: optionalID(role.employee_id),
      max_model_calls: role.max_model_calls === undefined ? undefined : integer(role.max_model_calls),
      max_tokens: role.max_tokens === undefined ? undefined : integer(role.max_tokens),
    }
  }
  const roles = object(source.roles ?? {})
  return {
    schema_version: integer(source.schema_version),
    name: string(source.name, 8192),
    default: decodeRole(source.default),
    roles: Object.fromEntries(Object.entries(roles).map(([key, entry]) => [key, decodeRole(entry)])),
    updated_at: optionalTime(source.updated_at),
  }
}

export function decodeBoundedProjection(value: unknown): Record<string, unknown> {
  return boundedRecord(value)
}

function decodeTaskProject(value: unknown): EmployeeTask['project_binding'] {
  const source = object(value)
  return {
    id: id(source.id),
    label: string(source.label, 8192),
    workspace_fingerprint: string(source.workspace_fingerprint, 256),
    read_allowed: boolean(source.read_allowed),
    mutation_allowed: boolean(source.mutation_allowed),
    allowed_tool_capabilities: array(
      source.allowed_tool_capabilities,
      (item) => string(item, 256),
      MAX_SMALL_COLLECTION,
    ),
    network_allowed: boolean(source.network_allowed),
    budget_override: source.budget_override === undefined || source.budget_override === null
      ? undefined
      : decodeBudget(source.budget_override),
  }
}

export function decodeEmployeeTask(value: unknown): EmployeeTask {
  const source = object(value)
  const policy = object(source.policy)
  const budget = object(policy.budget)
  return {
    schema_version: integer(source.schema_version),
    id: id(source.id),
    employee_id: id(source.employee_id),
    employee_revision: integer(source.employee_revision),
    prompt: string(source.prompt),
    state: enumeration<EmployeeTaskState>(source.state, EMPLOYEE_TASK_STATES),
    created_at: time(source.created_at),
    updated_at: time(source.updated_at),
    cancelled_at: optionalTime(source.cancelled_at),
    employee_snapshot: (() => {
      const snapshot = object(source.employee_snapshot)
      return {
        schema_version: integer(snapshot.schema_version),
        employee_id: id(snapshot.employee_id),
        revision: integer(snapshot.revision),
        captured_at: time(snapshot.captured_at),
        digest: string(snapshot.digest, 256),
      }
    })(),
    skills: array(source.skills, decodeSkillBinding, MAX_SMALL_COLLECTION),
    knowledge: array(source.knowledge, (entry) => {
      const knowledge = object(entry)
      return {
        source_id: id(knowledge.source_id),
        source_digest: string(knowledge.source_digest, 256),
        citations: array(knowledge.citations, (citationValue) => {
          const citation = object(citationValue)
          return {
            citation_id: id(citation.citation_id),
            path: string(citation.path, 4096),
            digest: string(citation.digest, 256),
            start_line: integer(citation.start_line),
            end_line: integer(citation.end_line),
          }
        }, MAX_SMALL_COLLECTION),
      }
    }, MAX_SMALL_COLLECTION),
    memory_facts: array(source.memory_facts, (entry) => {
      const fact = object(entry)
      return {
        fact_id: id(fact.fact_id),
        digest: string(fact.digest, 256),
      }
    }, MAX_SMALL_COLLECTION),
    project_binding: decodeTaskProject(source.project_binding),
    policy: {
      allowed_capabilities: array(
        policy.allowed_capabilities,
        (item) => string(item, 256),
        MAX_SMALL_COLLECTION,
      ),
      network_allowed: boolean(policy.network_allowed),
      budget: {
        max_model_calls: integer(budget.max_model_calls),
        max_tokens: integer(budget.max_tokens),
        timeout_seconds: integer(budget.timeout_seconds),
      },
    },
    snapshot_digest: string(source.snapshot_digest, 256),
    session_id: optionalID(source.session_id),
    run_id: optionalID(source.run_id),
    artifacts: optionalArray(source.artifacts, (entry) => {
      const artifact = object(entry)
      return {
        schema_version: integer(artifact.schema_version),
        id: id(artifact.id),
        employee_id: id(artifact.employee_id),
        task_id: id(artifact.task_id),
        session_id: id(artifact.session_id),
        run_id: id(artifact.run_id),
        path: string(artifact.path, 4096),
        digest: string(artifact.digest, 256),
        verified_at: time(artifact.verified_at),
      }
    }, 128),
  }
}

export function decodeEmployeeTasks(value: unknown): { tasks: EmployeeTask[] } {
  const source = object(value)
  return { tasks: array(source.tasks, decodeEmployeeTask, MAX_SMALL_COLLECTION) }
}

function decodeBudget(value: unknown) {
  const source = object(value)
  return {
    max_model_calls: integer(source.max_model_calls),
    max_tokens: integer(source.max_tokens),
    timeout_seconds: integer(source.timeout_seconds),
  }
}

export function decodeLoopDefinition(value: unknown): LoopDefinition {
  const source = object(value)
  const task = object(source.task_source)
  const contract = source.contract === undefined ? {} : object(source.contract)
  const schedule = source.schedule === undefined ? {} : object(source.schedule)
  const verification = object(source.verification_recipe)
  const approval = object(source.approval_policy)
  const workspace = object(source.workspace_policy)
  const output = object(source.output_policy)
  return {
    id: id(source.id),
    schema_version: integer(source.schema_version),
    name: string(source.name, 8192),
    description: string(source.description),
    employee_id: optionalID(source.employee_id),
    contract: {
      goal: source.employee_id === undefined ? '' : string(contract.goal ?? '', 8192),
      boundaries: array(contract.boundaries ?? [], (item) => string(item, 8192), 32),
      sop: array(contract.sop ?? [], (item) => string(item, 8192), 32),
      definition_of_done: array(contract.definition_of_done ?? [], (item) => string(item, 8192), 32),
      stop_conditions: array(contract.stop_conditions ?? [], (item) => string(item, 8192), 32),
    },
    schedule: {
      kind: enumeration(schedule.kind ?? '', ['', 'manual', 'daily'] as const),
      local_time: string(schedule.local_time ?? '', 16),
      timezone: string(schedule.timezone ?? '', 256),
    },
    workspace_identity: string(source.workspace_identity, 4096),
    enabled: boolean(source.enabled),
    task_source: {
      type: enumeration(task.type, ['fixed_prompt'] as const),
      prompt: string(task.prompt),
    },
    agent_selection: decodeSelection(source.agent_selection),
    team_template_ref: optionalString(source.team_template_ref, 256) ?? '',
    plan_mode: enumeration<PlanMode>(source.plan_mode, PLAN_MODES),
    verification_recipe: {
      checks: array(verification.checks ?? [], (item) => {
        const check = object(item)
        return {
          id: id(check.id),
          command: array(check.command, (argument) => string(argument, 4096), 8),
          required: boolean(check.required),
          timeout_seconds: integer(check.timeout_seconds),
        }
      }, 16),
      independent_verifier: boolean(verification.independent_verifier),
      max_repair_attempts: integer(verification.max_repair_attempts),
    },
    budget: decodeBudget(source.budget),
    approval_policy: { require_for_mutation: boolean(approval.require_for_mutation) },
    workspace_policy: {
      read_only: boolean(workspace.read_only),
      require_clean_git: boolean(workspace.require_clean_git),
    },
    output_policy: {
      include_diff: boolean(output.include_diff),
      max_report_bytes: integer(output.max_report_bytes),
    },
    created_at: time(source.created_at),
    updated_at: time(source.updated_at),
    revision: integer(source.revision),
  }
}

export function decodeLoopDefinitions(value: unknown): { loops: LoopDefinition[] } {
  const source = object(value)
  return { loops: array(source.loops, decodeLoopDefinition, MAX_SMALL_COLLECTION) }
}

function decodeInvocationValue(value: unknown): LoopInvocation {
  const source = object(value)
  return {
    id: id(source.id),
    loop_id: id(source.loop_id),
    definition_revision: integer(source.definition_revision),
    definition_snapshot: decodeLoopDefinition(source.definition_snapshot),
    trigger: id(source.trigger),
    task_snapshot: string(source.task_snapshot),
    employee_task_id: optionalID(source.employee_task_id),
    session_id: optionalID(source.session_id),
    run_id: optionalID(source.run_id),
    status: enumeration<InvocationStatus>(source.status, INVOCATION_STATUSES),
    created_at: time(source.created_at),
    started_at: optionalTime(source.started_at),
    finished_at: optionalTime(source.finished_at),
    failure_code: optionalString(source.failure_code, 256),
    failure_summary: optionalString(source.failure_summary, 4096),
  }
}

export function decodeLoopRuntimeState(value: unknown): LoopRuntimeState {
  const source = object(value)
  return {
    schema_version: integer(source.schema_version),
    loop_id: id(source.loop_id),
    definition_revision: integer(source.definition_revision),
    last_invocation_id: optionalID(source.last_invocation_id),
    last_status: source.last_status === undefined || source.last_status === ''
      ? undefined
      : enumeration<InvocationStatus>(source.last_status, INVOCATION_STATUSES),
    last_run_at: optionalTime(source.last_run_at),
    next_run_at: optionalTime(source.next_run_at),
    consecutive_failures: integer(source.consecutive_failures),
    total_runs: integer(source.total_runs),
    successful_runs: integer(source.successful_runs),
    updated_at: time(source.updated_at),
  }
}

export function decodeLoopInvocation(value: unknown): LoopInvocation {
  return decodeInvocationValue(value)
}

export function decodeLoopInvocationList(value: unknown): {
  invocations: LoopInvocation[]
  limit: number
} {
  const source = object(value)
  return {
    invocations: array(source.invocations, decodeInvocationValue, MAX_SMALL_COLLECTION),
    limit: integer(source.limit),
  }
}

export function decodeNotificationStatus(value: unknown): NotificationStatus {
  const source = object(value)
  return {
    configured: boolean(source.configured),
    recipient: string(source.recipient, 320),
    from: optionalString(source.from, 320),
    host: optionalString(source.host, 256),
    last_error: optionalString(source.last_error, 512),
    last_sent_at: optionalTime(source.last_sent_at),
  }
}

export function decodeDryRun(value: unknown): DryRunReport {
  const source = object(value)
  return {
    loop_id: id(source.loop_id),
    definition_revision: integer(source.definition_revision),
    definition_valid: boolean(source.definition_valid),
    workspace_identity: string(source.workspace_identity, 4096),
    workspace_matches: boolean(source.workspace_matches),
    git_clean: boolean(source.git_clean),
    task_prompt: string(source.task_prompt),
    agent: decodeSelection(source.agent),
    roles: array(source.roles, (entry) => boundedRecord(entry), MAX_SMALL_COLLECTION),
    write_scope: string(source.write_scope, 256),
    checks: array(source.checks, (entry) => boundedRecord(entry), MAX_SMALL_COLLECTION),
    budget: decodeBudget(source.budget),
    requires_approval: boolean(source.requires_approval),
    ready: boolean(source.ready),
    reasons: array(source.reasons, (item) => string(item, 4096), MAX_SMALL_COLLECTION),
  }
}
