export type SessionStatus =
  | 'open'
  | 'archived'
  | 'running'
  | 'completed'
  | 'failed'
  | 'cancelled'
export type RunStatus =
  | 'queued'
  | 'running'
  | 'verifying'
  | 'completed'
  | 'failed'
  | 'cancelled'
  | 'interrupted'
export type PlanMode = 'auto' | 'review'
export type PlanStatus = 'active' | 'completed' | 'failed' | 'cancelled'
export type PlanStepStatus = 'pending' | 'in_progress' | 'completed' | 'failed' | 'cancelled'
export type ApprovalStatus = 'pending' | 'approved' | 'denied' | 'expired' | 'consumed'
export type InvocationStatus =
  | 'prepared'
  | 'dispatched'
  | 'attached'
  | 'completed'
  | 'skipped'
  | 'blocked'
  | 'failed'
  | 'cancelled'

export interface Health {
  status: 'ok'
  version: string
  active: boolean
}

export interface ModelOption {
  id: string
  label: string
  provider: string
}

export interface Access {
  id: string
  label: string
  auth_type: 'api_key' | 'oauth_external'
  description: string
  api_key_env?: string | undefined
  supported: boolean
  models: ModelOption[]
}

export interface Company {
  id: string
  label: string
  access: Access[]
}

export interface AgentCatalogItem {
  id: string
  label: string
  description: string
  read_only: boolean
  tool_policy: string
}

export interface RuntimeSelection {
  company: string
  access: string
  model: string
  agent: string
}

export interface ProviderReadiness {
  configured: boolean
  source: string
  detail: string
}

export interface Info {
  version: string
  workspace: string
  model: {
    provider: string
    protocol: string
    base_url: string
    model: string
    api_key_env: string
    api_key_configured: boolean
  }
  selection: RuntimeSelection
  companies: Company[]
  available_companies: Company[]
  agents: AgentCatalogItem[]
  auth_status: Record<string, ProviderReadiness>
  active: boolean
  owner: { configured: boolean; display_name?: string | undefined }
}

export interface OwnerIdentity {
  display_name: string
  timezone: string
  language: string
}

export interface OwnerPreferences {
  communication: string
  coding: string
  git: string
  verification: string
  risk: string
}

export interface OwnerEnvironment {
  id: string
  label: string
  kind: string
  alias: string
  notes: string
}

export interface OwnerFact {
  id: string
  category: string
  value: string
  source: string
  confirmed: boolean
  created_at: string
  updated_at: string
}

export interface OwnerProfile {
  schema_version: number
  identity: OwnerIdentity
  preferences: OwnerPreferences
  environments: OwnerEnvironment[]
  facts: OwnerFact[]
  updated_at?: string | undefined
}

export interface CodexLoginSession {
  id: string
  status: 'pending' | 'approved' | 'error' | 'expired' | 'cancelled'
  user_code?: string | undefined
  verification_url?: string | undefined
  error?: string | undefined
  expires_at: string
}

export interface SessionSelection {
  company: string
  access: string
  model: string
  agent: string
}

export interface SessionSummary {
  id: string
  title: string
  status: SessionStatus
  updated_at: string
  active_run_id?: string | undefined
  last_run_status?: RunStatus | undefined
  selection: SessionSelection
}

export interface Message {
  id: string
  run_id: string
  role: 'system' | 'user' | 'assistant' | 'tool'
  content: string
  created_at: string
}

export interface PlanStep {
  id: string
  title: string
  status: PlanStepStatus
  detail?: string | undefined
  started_at?: string | undefined
  completed_at?: string | undefined
  updated_at: string
}

export interface Plan {
  schema_version: number
  id: string
  status: PlanStatus
  revision: number
  allow_parallel: boolean
  steps: PlanStep[]
  created_at: string
  updated_at: string
}

export interface Run {
  id: string
  message: string
  status: RunStatus
  started_at: string
  updated_at: string
  completed_at?: string | undefined
  start_turn: number
  end_turn: number
  last_mutation_turn: number
  last_verification_turn: number
  verification_attempts: number
  model_calls: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  plan?: Plan | undefined
  plan_mode: PlanMode
  plan_approved: boolean
  plan_approved_at?: string | undefined
  modified_files: string[]
  final_message: string
  error: string
}

export interface ToolRecord {
  time: string
  run_id?: string | undefined
  turn: number
  call_id: string
  name: string
  args_digest?: string | undefined
  summary: string
  is_error: boolean
  status: 'started' | 'completed' | 'uncertain' | ''
  started_at?: string | undefined
  completed_at?: string | undefined
}

export interface TestResult {
  command: string
  passed: boolean
  summary: string
  exit_code: number
  duration_ms: number
  time: string
  run_id?: string | undefined
  turn: number
}

export interface WorkItem {
  id: string
  title: string
  goal: string
  role: string
  status: string
  depends_on: string[]
  mutates_workspace: boolean
  attempt: number
  started_at?: string | undefined
  updated_at: string
  completed_at?: string | undefined
  handoff_id?: string | undefined
  execution_session_id?: string | undefined
  error?: string | undefined
}

export interface MissionUsage {
  model_calls: number
  tokens: number
}

export interface MissionBudget {
  max_work_items: number
  max_model_calls: number
  max_tokens: number
  timeout: number
  role_limits: Record<string, MissionUsage>
}

export interface HandoffCheck {
  command: string
  passed: boolean
  summary: string
  exit_code: number
  duration_ms: number
}

export interface HandoffSubstep {
  id: string
  title: string
  goal: string
  role: string
  depends_on: string[]
}

export interface HandoffFinding {
  severity: 'blocking' | 'advisory'
  summary: string
}

export interface Handoff {
  id: string
  work_item_id: string
  role: string
  summary: string
  evidence: string[]
  modified_files: string[]
  checks: HandoffCheck[]
  issues: string[]
  next_steps: string[]
  substeps: HandoffSubstep[]
  findings: HandoffFinding[]
  created_at: string
}

export interface Mission {
  id: string
  run_id: string
  goal: string
  template: string
  status: string
  budget: MissionBudget
  usage: MissionUsage
  usage_by_role: Record<string, MissionUsage>
  work_items: WorkItem[]
  handoffs: Handoff[]
  created_at: string
  updated_at: string
  error?: string | undefined
}

export interface ApprovalRequest {
  request_id: string
  session_id: string
  run_id: string
  mission_id?: string | undefined
  work_item_id?: string | undefined
  role?: string | undefined
  tool: string
  resource_paths: string[]
  args_summary: string
  args_digest: string
  policy_fingerprint: string
  plan_revision: number
  created_at: string
  expires_at: string
  status: ApprovalStatus
}

export interface SessionDetail {
  schema_version: number
  id: string
  title: string
  goal: string
  status: SessionStatus
  selection: SessionSelection
  plan_mode: PlanMode
  created_at: string
  updated_at: string
  turns: number
  runs: Run[]
  active_run_id?: string | undefined
  next_event_sequence: number
  summary: string
  tool_calls: ToolRecord[]
  modified_files: Record<string, string>
  completed_steps: string[]
  pending_steps: string[]
  test_results: TestResult[]
  last_error?: string | undefined
  workspace: string
  git_state?: string | undefined
  config_digest: string
  workspace_changed: boolean
  mission?: Mission | undefined
  approval_requests: ApprovalRequest[]
}

export interface SessionDetailResponse {
  session: SessionDetail
  messages: Message[]
}

export type RuntimeEventType =
  | 'task_started'
  | 'turn_started'
  | 'model_started'
  | 'model_delta'
  | 'model_completed'
  | 'tool_started'
  | 'tool_completed'
  | 'permission_required'
  | 'checkpoint_saved'
  | 'run_verifying'
  | 'run_interrupted'
  | 'workspace_changed'
  | 'memory_updated'
  | 'session_updated'
  | 'task_completed'
  | 'task_failed'
  | 'task_cancelled'
  | 'mission_started'
  | 'mission_completed'
  | 'mission_failed'
  | 'work_item_started'
  | 'work_item_completed'
  | 'work_item_failed'
  | 'plan_created'
  | 'plan_updated'
  | 'approval_requested'
  | 'approval_decided'
  | 'approval_expired'
  | 'approval_consumed'
  | 'provider_fallback'

export interface RuntimeEvent {
  type: RuntimeEventType
  time: string
  session_id: string
  run_id?: string | undefined
  mission_id?: string | undefined
  work_item_id?: string | undefined
  agent_id?: string | undefined
  plan_step_id?: string | undefined
  sequence: number
  turn: number
  tool?: string | undefined
  message?: string | undefined
  error?: string | undefined
}

export interface LoopSummary {
  id: string
  name: string
  enabled: boolean
  created_at: string
  updated_at: string
  revision: number
}

export interface InvocationSummary {
  id: string
  loop_id: string
  definition_revision: number
  trigger: string
  task_snapshot: string
  session_id?: string | undefined
  run_id?: string | undefined
  status: InvocationStatus
  created_at: string
  started_at?: string | undefined
  finished_at?: string | undefined
  failure_code?: string | undefined
  failure_summary?: string | undefined
}

export type EmployeeState = 'active' | 'disabled' | 'archived'
export type EmployeeTaskState =
  | 'queued'
  | 'running'
  | 'completed'
  | 'failed'
  | 'cancelled'
  | 'interrupted'

export interface EmployeeSummary {
  id: string
  revision: number
  state: EmployeeState
  name: string
  job_title: string
  agent_profile: string
  project_count: number
  created_at: string
  updated_at: string
}

export interface SkillBinding {
  skill_id: string
  version: string
  digest: string
  configuration: Record<string, unknown>
  enabled: boolean
}

export interface ProjectBinding {
  id: string
  label: string
  workspace_real_path: string
  workspace_fingerprint: string
  read_allowed: boolean
  mutation_allowed: boolean
  allowed_tool_capabilities: string[]
  network_allowed: boolean
}

export interface Employee extends EmployeeSummary {
  schema_version: number
  avatar: { kind: 'initials' | 'emoji'; value: string }
  charter: string
  responsibilities: string[]
  behavior_boundaries: string[]
  default_selection: { company: string; access: string; model: string }
  skill_bindings: SkillBinding[]
  project_binding_ids: string[]
  permission_policy: { allowed_capabilities: string[]; network_allowed: boolean }
  budget_policy: { max_model_calls: number; max_tokens: number; timeout_seconds: number }
  concurrency_policy: { max_running_tasks: number }
  memory_policy: {
    candidate_generation: boolean
    promotion: 'disabled' | 'owner_confirmation'
    max_context_facts: number
    max_context_bytes: number
  }
}

export interface EmployeeRecord {
  employee: Employee
  project_bindings: ProjectBinding[]
}

export interface SkillCatalogItem {
  skill_id: string
  version: string
  digest: string
  kind: 'native' | 'skill_md_adapter'
  title: string
  description: string
  requested_capabilities: string[]
  configuration_schema: Record<string, unknown>
}

export interface EmployeeSkillStatus {
  binding: SkillBinding
  status: 'current' | 'missing' | 'digest_drift'
  kind?: 'native' | 'skill_md_adapter' | undefined
}

export interface EmployeeTask {
  schema_version: number
  id: string
  employee_id: string
  employee_revision: number
  prompt: string
  state: EmployeeTaskState
  created_at: string
  updated_at: string
  skills: Array<{ skill_id: string; version: string }>
  knowledge: Array<{ source_id: string; citation_ids: string[] }>
  memory_facts: Array<{
    id: string
    category?: string | undefined
    value?: string | undefined
  }>
  project_binding: {
    id: string
    label: string
    workspace_fingerprint: string
    read_allowed: boolean
    mutation_allowed: boolean
    allowed_tool_capabilities: string[]
    network_allowed: boolean
  }
  policy: {
    allowed_capabilities: string[]
    network_allowed: boolean
    budget: { max_model_calls: number; max_tokens: number; timeout_seconds: number }
  }
  snapshot_digest: string
  session_id?: string | undefined
  run_id?: string | undefined
  artifacts: Array<{ id?: string; kind?: string; path?: string; digest?: string }>
}

export interface LoopDefinition extends LoopSummary {
  schema_version: number
  description: string
  workspace_identity: string
  task_source: { type: 'fixed_prompt'; prompt: string }
  agent_selection: RuntimeSelection
  team_template_ref: string
  plan_mode: PlanMode
  verification_recipe: {
    checks: string[]
    independent_verifier: boolean
    max_repair_attempts: number
  }
  budget: { max_model_calls: number; max_tokens: number; timeout_seconds: number }
  approval_policy: { require_for_mutation: boolean }
  workspace_policy: { read_only: boolean; require_clean_git: boolean }
  output_policy: { include_diff: boolean; max_report_bytes: number }
}

export interface LoopInvocation extends InvocationSummary {
  definition_snapshot: LoopDefinition
}

export interface DryRunReport {
  loop_id: string
  definition_revision: number
  definition_valid: boolean
  workspace_identity: string
  workspace_matches: boolean
  git_clean: boolean
  task_prompt: string
  agent: RuntimeSelection
  roles: Array<Record<string, unknown>>
  write_scope: string
  checks: Array<Record<string, unknown>>
  budget: { max_model_calls: number; max_tokens: number; timeout_seconds: number }
  requires_approval: boolean
  ready: boolean
  reasons: string[]
}
