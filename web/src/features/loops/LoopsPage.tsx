import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Button } from 'antd'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useParams } from 'react-router-dom'

import {
  cancelLoopInvocation,
  createLoop,
  decideApproval,
  dryRunEmployee,
  dryRunLoop,
  getInfo,
  getLoop,
  getLoopRuntime,
  getLoopInvocation,
  getSession,
  getTeamTemplate,
  importLoop,
  importTeamTemplate,
  listApprovals,
  listEmployees,
  listLoopInvocations,
  listLoops,
  startLoopInvocation,
  updateLoop,
} from '../../api/endpoints'
import { ApiError } from '../../api/errors'
import type {
  ApprovalRequest,
  DryRunReport,
  EmployeeDryRun,
  EmployeeSummary,
  Info,
  LoopDefinition,
  LoopInvocation,
  LoopRuntimeState,
  RuntimeSelection,
  SessionDetailResponse,
  TeamRoleSelection,
  TeamTemplate,
} from '../../api/types'
import { useConnectivity } from '../../components/ConnectivityProvider'
import { ErrorState } from '../../components/ErrorState'
import { PageHeader } from '../../components/PageHeader'
import { useSessionEvents } from '../../hooks/useSessionEvents'
import { translatedEnum } from '../../i18n/enumLabel'
import { useUI } from '../../state/UIContext'

const TEAM_ROLES = ['lead', 'explorer', 'builder', 'reviewer', 'verifier'] as const
const TERMINAL_INVOCATIONS = new Set(['completed', 'skipped', 'blocked', 'failed', 'cancelled'])

function mutationKey(error: unknown) {
  if (error instanceof ApiError && error.code === 'network_error') return 'mutation.offline'
  if (error instanceof ApiError && error.status === 409) return 'mutation.conflict'
  return 'mutation.failed'
}

function parsePositive(value: string, fallback: number) {
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) && parsed >= 0 ? parsed : fallback
}

function newDefinition(): LoopDefinition {
  const now = new Date(0).toISOString()
  return {
    id: '',
    schema_version: 1,
    name: '',
    description: '',
    employee_id: '',
    contract: {
      goal: '',
      boundaries: ['不保存凭据或未经确认的敏感信息。', '信息不明确时停止并请求 Owner 确认。'],
      sop: ['检查本次新增材料。', '去重、分类并保留来源。', '验证结果并生成本次报告。'],
      definition_of_done: ['每条归档信息都有来源。', '本次运行有可审阅的报告。'],
      stop_conditions: ['来源冲突或分类不明确时停止。'],
    },
    schedule: { kind: 'manual', local_time: '', timezone: '' },
    workspace_identity: '',
    enabled: true,
    task_source: { type: 'fixed_prompt', prompt: '' },
    agent_selection: { company: '', access: '', model: '', agent: 'team' },
    team_template_ref: 'default',
    plan_mode: 'review',
    verification_recipe: { checks: [], independent_verifier: true, max_repair_attempts: 0 },
    budget: { max_model_calls: 12, max_tokens: 120_000, timeout_seconds: 1_200 },
    approval_policy: { require_for_mutation: true },
    workspace_policy: { read_only: true, require_clean_git: false },
    output_policy: { include_diff: false, max_report_bytes: 65_536 },
    created_at: now,
    updated_at: now,
    revision: 0,
  }
}

function normalizedDefinition(definition: LoopDefinition): LoopDefinition {
  return {
    ...definition,
    task_source: definition.employee_id
      ? { type: 'fixed_prompt', prompt: definition.contract.goal }
      : definition.task_source,
    verification_recipe: {
      ...definition.verification_recipe,
      checks: definition.verification_recipe.checks.map((check) => ({
        ...check,
        command: [...check.command],
      })),
    },
  }
}

async function loadAllActiveEmployees(signal: AbortSignal) {
  const employees: EmployeeSummary[] = []
  let cursor = ''
  do {
    const query = cursor
      ? { state: 'active', cursor, limit: 100 }
      : { state: 'active', limit: 100 }
    const page = await listEmployees(
      query,
      { signal },
    )
    employees.push(...page.employees)
    cursor = page.next_cursor ?? ''
  } while (cursor)
  return employees
}

function RuntimeSelectionFields({
  info,
  value,
  onChange,
  prefix,
}: {
  info: Info | null
  value: Pick<RuntimeSelection, 'company' | 'access' | 'model'>
  onChange: (next: Pick<RuntimeSelection, 'company' | 'access' | 'model'>) => void
  prefix: string
}) {
  const { t } = useTranslation()
  const companies = info?.available_companies ?? []
  const company = companies.find((item) => item.id === value.company)
  const access = company?.access.find((item) => item.id === value.access)
  return (
    <div className="form-grid">
      <label>{t('loops.company')}
        <select
          aria-label={`${prefix} ${t('loops.company')}`}
          value={value.company}
          onChange={(event) => onChange({ company: event.target.value, access: '', model: '' })}
        >
          <option value="">{t('common.select')}</option>
          {companies.map((item) => <option key={item.id} value={item.id}>{item.label}</option>)}
        </select>
      </label>
      <label>{t('loops.access')}
        <select
          aria-label={`${prefix} ${t('loops.access')}`}
          value={value.access}
          onChange={(event) => onChange({ ...value, access: event.target.value, model: '' })}
        >
          <option value="">{t('common.select')}</option>
          {company?.access.map((item) => <option key={item.id} value={item.id}>{item.label}</option>)}
        </select>
      </label>
      <label>{t('loops.model')}
        <select
          aria-label={`${prefix} ${t('loops.model')}`}
          value={value.model}
          onChange={(event) => onChange({ ...value, model: event.target.value })}
        >
          <option value="">{t('common.select')}</option>
          {access?.models.map((item) => <option key={item.id} value={item.id}>{item.label}</option>)}
        </select>
      </label>
    </div>
  )
}

function DefinitionForm({
  value,
  onChange,
  info,
  includeId,
}: {
  value: LoopDefinition
  onChange: (next: LoopDefinition) => void
  info: Info | null
  includeId: boolean
}) {
  const { t } = useTranslation()
  const checks = value.verification_recipe.checks
  const updateSelection = (selection: Pick<RuntimeSelection, 'company' | 'access' | 'model'>) => {
    onChange({ ...value, agent_selection: { ...value.agent_selection, ...selection } })
  }
  return (
    <>
      <div className="form-grid">
        {includeId ? <label>{t('loops.id')}<input value={value.id} onChange={(event) => onChange({ ...value, id: event.target.value })} /></label> : null}
        <label>{t('loops.name')}<input value={value.name} onChange={(event) => onChange({ ...value, name: event.target.value })} /></label>
        <label>{t('loops.descriptionLabel')}<textarea value={value.description} onChange={(event) => onChange({ ...value, description: event.target.value })} /></label>
        <label>{t('loops.workspace')}<input value={value.workspace_identity} onChange={(event) => onChange({ ...value, workspace_identity: event.target.value })} /></label>
        <label>{t('loops.mission')}<textarea value={value.task_source.prompt} onChange={(event) => onChange({ ...value, task_source: { type: 'fixed_prompt', prompt: event.target.value } })} /></label>
        <label>{t('loops.agent')}
          <select value={value.agent_selection.agent} onChange={(event) => onChange({ ...value, agent_selection: { ...value.agent_selection, agent: event.target.value } })}>
            {info?.agents.map((agent) => <option key={agent.id} value={agent.id}>{agent.label}</option>)}
          </select>
        </label>
        <label>{t('loops.planMode')}
          <select value={value.plan_mode} onChange={(event) => onChange({ ...value, plan_mode: event.target.value === 'auto' ? 'auto' : 'review' })}>
            <option value="review">{t('agent.planReview')}</option>
            <option value="auto">{t('agent.planAuto')}</option>
          </select>
        </label>
        <label>{t('loops.teamReference')}<input value={value.team_template_ref} onChange={(event) => onChange({ ...value, team_template_ref: event.target.value })} /></label>
      </div>
      <RuntimeSelectionFields info={info} value={value.agent_selection} onChange={updateSelection} prefix={t('loops.definitionModel')} />
      <fieldset>
        <legend>{t('loops.policies')}</legend>
        <label><input type="checkbox" checked={value.enabled} onChange={(event) => onChange({ ...value, enabled: event.target.checked })} /> {t('loops.enabled')}</label>
        <label><input type="checkbox" checked={value.workspace_policy.read_only} onChange={(event) => onChange({ ...value, workspace_policy: { ...value.workspace_policy, read_only: event.target.checked } })} /> {t('loops.readOnly')}</label>
        <label><input type="checkbox" checked={value.workspace_policy.require_clean_git} onChange={(event) => onChange({ ...value, workspace_policy: { ...value.workspace_policy, require_clean_git: event.target.checked } })} /> {t('loops.cleanGit')}</label>
        <label><input type="checkbox" checked={value.approval_policy.require_for_mutation} onChange={(event) => onChange({ ...value, approval_policy: { require_for_mutation: event.target.checked } })} /> {t('loops.requireApproval')}</label>
        <label><input type="checkbox" checked={value.output_policy.include_diff} onChange={(event) => onChange({ ...value, output_policy: { ...value.output_policy, include_diff: event.target.checked } })} /> {t('loops.includeDiff')}</label>
      </fieldset>
      <fieldset>
        <legend>{t('loops.budget')}</legend>
        <label>{t('loops.maxCalls')}<input type="number" min="0" value={value.budget.max_model_calls} onChange={(event) => onChange({ ...value, budget: { ...value.budget, max_model_calls: parsePositive(event.target.value, value.budget.max_model_calls) } })} /></label>
        <label>{t('loops.maxTokens')}<input type="number" min="0" value={value.budget.max_tokens} onChange={(event) => onChange({ ...value, budget: { ...value.budget, max_tokens: parsePositive(event.target.value, value.budget.max_tokens) } })} /></label>
        <label>{t('loops.timeout')}<input type="number" min="0" value={value.budget.timeout_seconds} onChange={(event) => onChange({ ...value, budget: { ...value.budget, timeout_seconds: parsePositive(event.target.value, value.budget.timeout_seconds) } })} /></label>
        <label>{t('loops.maxReport')}<input type="number" min="0" value={value.output_policy.max_report_bytes} onChange={(event) => onChange({ ...value, output_policy: { ...value.output_policy, max_report_bytes: parsePositive(event.target.value, value.output_policy.max_report_bytes) } })} /></label>
      </fieldset>
      <section>
        <h2>{t('loops.verificationChecks')}</h2>
        <label><input type="checkbox" checked={value.verification_recipe.independent_verifier} onChange={(event) => onChange({ ...value, verification_recipe: { ...value.verification_recipe, independent_verifier: event.target.checked } })} /> {t('loops.independentVerifier')}</label>
        <label>{t('loops.maxRepair')}<input type="number" min="0" value={value.verification_recipe.max_repair_attempts} onChange={(event) => onChange({ ...value, verification_recipe: { ...value.verification_recipe, max_repair_attempts: parsePositive(event.target.value, value.verification_recipe.max_repair_attempts) } })} /></label>
        {checks.map((check, index) => (
          <fieldset key={index}>
            <legend>{t('loops.checkNumber', { number: index + 1 })}</legend>
            <label>{t('loops.checkId')}<input aria-label={t('loops.checkId')} value={check.id} onChange={(event) => onChange({ ...value, verification_recipe: { ...value.verification_recipe, checks: checks.map((item, itemIndex) => itemIndex === index ? { ...item, id: event.target.value } : item) } })} /></label>
            <fieldset>
              <legend>{t('loops.commandArguments')}</legend>
              {check.command.map((argument, argumentIndex) => (
                <div className="button-row" key={argumentIndex}>
                  <label>{t('loops.commandArgument', { number: argumentIndex + 1 })}
                    <input
                      aria-label={t('loops.commandArgument', { number: argumentIndex + 1 })}
                      value={argument}
                      onChange={(event) => onChange({
                        ...value,
                        verification_recipe: {
                          ...value.verification_recipe,
                          checks: checks.map((item, itemIndex) => itemIndex === index
                            ? {
                                ...item,
                                command: item.command.map((current, currentIndex) =>
                                  currentIndex === argumentIndex ? event.target.value : current),
                              }
                            : item),
                        },
                      })}
                    />
                  </label>
                  <button type="button" onClick={() => onChange({
                    ...value,
                    verification_recipe: {
                      ...value.verification_recipe,
                      checks: checks.map((item, itemIndex) => itemIndex === index
                        ? { ...item, command: item.command.filter((_, currentIndex) => currentIndex !== argumentIndex) }
                        : item),
                    },
                  })}>{t('common.remove')}</button>
                </div>
              ))}
              <button type="button" disabled={check.command.length >= 32} onClick={() => onChange({
                ...value,
                verification_recipe: {
                  ...value.verification_recipe,
                  checks: checks.map((item, itemIndex) => itemIndex === index
                    ? { ...item, command: [...item.command, ''] }
                    : item),
                },
              })}>{t('loops.addArgument')}</button>
            </fieldset>
            <label>{t('loops.checkTimeout')}<input type="number" min="0" value={check.timeout_seconds} onChange={(event) => onChange({ ...value, verification_recipe: { ...value.verification_recipe, checks: checks.map((item, itemIndex) => itemIndex === index ? { ...item, timeout_seconds: parsePositive(event.target.value, item.timeout_seconds) } : item) } })} /></label>
            <label><input type="checkbox" checked={check.required} onChange={(event) => onChange({ ...value, verification_recipe: { ...value.verification_recipe, checks: checks.map((item, itemIndex) => itemIndex === index ? { ...item, required: event.target.checked } : item) } })} /> {t('loops.required')}</label>
            <button type="button" onClick={() => onChange({ ...value, verification_recipe: { ...value.verification_recipe, checks: checks.filter((_, itemIndex) => itemIndex !== index) } })}>{t('common.remove')}</button>
          </fieldset>
        ))}
        <button type="button" disabled={checks.length >= 16} onClick={() => onChange({ ...value, verification_recipe: { ...value.verification_recipe, checks: [...checks, { id: '', command: [], required: true, timeout_seconds: 300 }] } })}>{t('loops.addCheck')}</button>
      </section>
    </>
  )
}

export function LoopsPage() {
  const { t } = useTranslation()
  const { actions } = useUI()
  const navigate = useNavigate()
  const connectivity = useConnectivity()
  const [loops, setLoops] = useState<LoopDefinition[]>([])
  const [employees, setEmployees] = useState<EmployeeSummary[]>([])
  const [creating, setCreating] = useState(false)
  const [draft, setDraft] = useState(newDefinition)
  const [info, setInfo] = useState<Info | null>(null)
  const [importText, setImportText] = useState('')
  const [strictError, setStrictError] = useState('')
  const pageEpoch = useRef(0)

  useEffect(() => {
    const epoch = ++pageEpoch.current
    const controller = new AbortController()
    void Promise.all([
      listLoops({ signal: controller.signal }),
      getInfo({ signal: controller.signal }),
      loadAllActiveEmployees(controller.signal),
    ])
      .then(([page, catalog, activeEmployees]) => {
        if (epoch !== pageEpoch.current) return
        setLoops(page.loops)
        setEmployees(activeEmployees)
        setInfo(catalog)
        setDraft((current) => current.agent_selection.company ? current : {
          ...current,
          workspace_identity: catalog.workspace,
          agent_selection: { ...catalog.selection, agent: 'team' },
        })
      })
      .catch(() => { if (!controller.signal.aborted) actions.showToast({ messageKey: 'mutation.failed', tone: 'error' }) })
    return () => {
      pageEpoch.current += 1
      controller.abort()
    }
  }, [actions, connectivity.generation])

  async function saveNew() {
    if (!connectivity.canMutate) return
    const epoch = pageEpoch.current
    setCreating(true)
    try {
      const saved = await createLoop(normalizedDefinition(draft))
      if (epoch !== pageEpoch.current) return
      void navigate(`/loops/${encodeURIComponent(saved.id)}`)
    } catch (error) {
      if (epoch !== pageEpoch.current) return
      actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
    } finally {
      if (epoch === pageEpoch.current) setCreating(false)
    }
  }

  async function importStrict() {
    const epoch = pageEpoch.current
    setStrictError('')
    let value: unknown
    try {
      value = JSON.parse(importText) as unknown
      if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('shape')
    } catch {
      setStrictError(t('loops.invalidImport'))
      return
    }
    try {
      const saved = await importLoop(value)
      if (epoch !== pageEpoch.current) return
      void navigate(`/loops/${encodeURIComponent(saved.id)}`)
    } catch (error) {
      if (epoch !== pageEpoch.current) return
      setStrictError(error instanceof ApiError && error.status === 409 ? t('mutation.conflict') : t('loops.rejectedImport'))
    }
  }

  return (
    <article className="feature-page loop-workbench">
      <header className="loop-hero">
        <span className="loop-kicker">{t('loops.kicker')}</span>
        <h1>{t('loops.heroTitle')}</h1>
        <p>{t('loops.heroDescription')}</p>
      </header>
      <section className="loop-card-grid" aria-label={t('loops.activeLoops')}>
        {loops.map((definition) => (
          <article className="loop-card" key={definition.id}>
            <div className="loop-card__topline">
              <span className="loop-pill">{definition.employee_id ? t('loops.employeeOwned') : t('loops.legacy')}</span>
              <span>{definition.enabled ? t('loops.enabled') : t('loops.disabled')}</span>
            </div>
            <h2><Link to={`/loops/${encodeURIComponent(definition.id)}`}>{definition.name}</Link></h2>
            <p>{definition.contract.goal || definition.description || definition.task_source.prompt}</p>
            <dl className="loop-card__facts">
              <div><dt>{t('loops.when')}</dt><dd>{definition.schedule.kind === 'daily' ? `${definition.schedule.local_time} · ${definition.schedule.timezone}` : t('loops.manual')}</dd></div>
              <div><dt>{t('loops.does')}</dt><dd>{definition.contract.sop[0] ?? definition.task_source.prompt}</dd></div>
              <div><dt>{t('loops.youGet')}</dt><dd>{definition.contract.definition_of_done[0] ?? t('loops.verifiedReport')}</dd></div>
            </dl>
            <Link className="loop-card__action" to={`/loops/${encodeURIComponent(definition.id)}`}>{t('loops.openLoop')} →</Link>
          </article>
        ))}
      </section>
      <section className="projection-card loop-quick-create">
        <div>
          <span className="loop-kicker">{t('loops.newLoop')}</span>
          <h2>{t('loops.describeJob')}</h2>
          <p>{t('loops.quickCreateDescription')}</p>
        </div>
        <div className="form-grid">
          <label>{t('loops.employee')}
            <select value={draft.employee_id ?? ''} onChange={(event) => setDraft({ ...draft, employee_id: event.target.value })}>
              <option value="">{t('loops.selectEmployee')}</option>
              {employees.map((employee) => <option key={employee.id} value={employee.id}>{employee.name} · {employee.job_title}</option>)}
            </select>
          </label>
          <label>{t('loops.name')}<input value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value, id: draft.id || `loop-${Date.now().toString(36)}` })} /></label>
          <label className="wide">{t('loops.goal')}<textarea value={draft.contract.goal} onChange={(event) => setDraft({ ...draft, contract: { ...draft.contract, goal: event.target.value }, task_source: { type: 'fixed_prompt', prompt: event.target.value } })} /></label>
          <label>{t('loops.scheduleKind')}
            <select value={draft.schedule.kind || 'manual'} onChange={(event) => {
              const daily = event.target.value === 'daily'
              setDraft({ ...draft, schedule: daily ? { kind: 'daily', local_time: draft.schedule.local_time || '02:00', timezone: draft.schedule.timezone || 'Asia/Shanghai' } : { kind: 'manual', local_time: '', timezone: '' } })
            }}>
              <option value="manual">{t('loops.manual')}</option>
              <option value="daily">{t('loops.daily')}</option>
            </select>
          </label>
          {draft.schedule.kind === 'daily' ? <label>{t('loops.runTime')}<input type="time" value={draft.schedule.local_time} onChange={(event) => setDraft({ ...draft, schedule: { ...draft.schedule, local_time: event.target.value } })} /></label> : null}
        </div>
        <Button type="primary" loading={creating} disabled={!connectivity.canMutate || !draft.employee_id || !draft.name.trim() || !draft.contract.goal.trim()} onClick={() => void saveNew()}>{t('loops.createAndConfigure')}</Button>
        <details className="advanced-panel">
          <summary>{t('loops.advanced')}</summary>
          <DefinitionForm value={draft} onChange={setDraft} info={info} includeId />
          <section>
            <h3>{t('loops.import')}</h3>
            <textarea aria-label={t('loops.import')} value={importText} onChange={(event) => setImportText(event.target.value)} />
            {strictError ? <p role="alert">{strictError}</p> : null}
            <Button type="default" disabled={!connectivity.canMutate} onClick={() => void importStrict()}>{t('loops.import')}</Button>
          </section>
        </details>
      </section>
    </article>
  )
}

function TeamEditor({
  team,
  setTeam,
  employees,
  readiness,
  info,
  onSave,
  busy,
}: {
  team: TeamTemplate
  setTeam: (next: TeamTemplate) => void
  employees: EmployeeSummary[]
  readiness: Record<string, EmployeeDryRun>
  info: Info | null
  onSave: () => void
  busy: boolean
}) {
  const { t } = useTranslation()
  const setDefault = (next: Pick<RuntimeSelection, 'company' | 'access' | 'model'>) => setTeam({ ...team, default: { ...team.default, ...next } })
  const setRole = (role: string, next: TeamRoleSelection) => setTeam({ ...team, roles: { ...team.roles, [role]: next } })
  return (
    <section className="projection-card">
      <h2>{t('loops.teamRoles')}</h2>
      <label>{t('loops.teamName')}<input value={team.name} onChange={(event) => setTeam({ ...team, name: event.target.value })} /></label>
      <h3>{t('loops.defaultSelection')}</h3>
      <RuntimeSelectionFields info={info} value={team.default} onChange={setDefault} prefix={t('loops.defaultSelection')} />
      <p>{t('loops.modelOverrideRule')}</p>
      {TEAM_ROLES.map((role) => {
        const selection = team.roles[role] ?? { ...team.default }
        const employee = employees.find((item) => item.id === selection.employee_id)
        const employeeReady = selection.employee_id ? readiness[selection.employee_id] : undefined
        const override = Boolean(selection.employee_id && selection.company && selection.access && selection.model)
        return (
          <fieldset key={role}>
            <legend>{translatedEnum(t, 'teamRole', role)}</legend>
            <label>{t('loops.employee')}
              <select
                data-testid={`team-role-${role}`}
                value={selection.employee_id ?? ''}
                onChange={(event) => setRole(role, event.target.value
                  ? { company: '', access: '', model: '', employee_id: event.target.value }
                  : { ...team.default })}
              >
                <option value="">{t('loops.noEmployee')}</option>
                {employees.map((item) => <option key={item.id} value={item.id}>{item.name} · r{item.revision}</option>)}
                {selection.employee_id && !employee ? <option value={selection.employee_id}>{selection.employee_id} · {t('loops.unavailable')}</option> : null}
              </select>
            </label>
            {selection.employee_id ? (
              <>
                <p>{employee ? `${employee.name} · r${employee.revision} · ${employeeReady?.ready ? t('loops.ready') : t('loops.notReady')}` : `${selection.employee_id} · ${t('loops.unavailable')}`}</p>
                {employeeReady?.checks.map((check) => <p key={check.name}>{check.name}: {check.ready ? t('loops.ready') : t('loops.notReady')} · {check.detail}</p>)}
                <label>
                  <input
                    type="checkbox"
                    aria-label={t('loops.useMissionOverride', { role })}
                    checked={override}
                    onChange={(event) => setRole(role, event.target.checked
                      ? { ...team.default, employee_id: selection.employee_id }
                      : { company: '', access: '', model: '', employee_id: selection.employee_id })}
                  />
                  {t('loops.missionOverride')}
                </label>
                {override ? <RuntimeSelectionFields info={info} value={selection} onChange={(next) => setRole(role, { ...selection, ...next })} prefix={role} /> : <p>{t('loops.employeeDefaultPath')}</p>}
              </>
            ) : <p>{t('loops.teamDefaultPath')}</p>}
          </fieldset>
        )
      })}
      <button type="button" disabled={busy} onClick={onSave}>{t('loops.saveTeam')}</button>
    </section>
  )
}

function DryRunProjection({ report }: { report: DryRunReport }) {
  const { t } = useTranslation()
  return (
    <section aria-label={t('loops.dryRunResult')}>
      <h3>{t('loops.dryRunResult')}</h3>
      <dl>
        <dt>{t('loops.readiness')}</dt><dd>{report.ready ? t('loops.ready') : t('loops.notReady')}</dd>
        <dt>{t('loops.definitionValid')}</dt><dd>{report.definition_valid ? t('common.yes') : t('common.no')}</dd>
        <dt>{t('loops.workspaceMatch')}</dt><dd>{report.workspace_matches ? t('common.yes') : t('common.no')}</dd>
        <dt>{t('loops.gitClean')}</dt><dd>{report.git_clean ? t('common.yes') : t('common.no')}</dd>
        <dt>{t('loops.writeScope')}</dt><dd>{report.write_scope}</dd>
      </dl>
      {report.reasons.length ? <ul>{report.reasons.map((reason) => <li key={reason}>{reason}</li>)}</ul> : null}
      <h4>{t('loops.verificationChecks')}</h4>
      <p>{t('loops.checkCount', { count: report.checks.length })}</p>
      <h4>{t('loops.teamRoles')}</h4>
      <p>{t('loops.roleCount', { count: report.roles.length })}</p>
    </section>
  )
}

type FlowStatus = 'idle' | 'active' | 'success' | 'blocked' | 'failed'

function orchestrationStatus(invocation: LoopInvocation | undefined): FlowStatus {
  if (!invocation) return 'idle'
  if (invocation.status === 'completed') return 'success'
  if (invocation.status === 'failed') return 'failed'
  if (invocation.status === 'blocked' || invocation.status === 'skipped' || invocation.status === 'cancelled') return 'blocked'
  return 'active'
}

function stageStatus(stage: 'trigger' | 'orchestrator' | 'executor' | 'verifier' | 'evidence' | 'evolve', flow: FlowStatus): FlowStatus {
  if (flow === 'idle') return 'idle'
  if (flow === 'failed') return stage === 'verifier' || stage === 'evidence' ? 'failed' : 'success'
  if (flow === 'blocked') return stage === 'orchestrator' ? 'blocked' : 'idle'
  if (flow === 'active') {
    if (stage === 'trigger') return 'success'
    if (stage === 'orchestrator') return 'active'
    return 'idle'
  }
  return stage === 'evolve' ? 'active' : 'success'
}

function FlowNode({ label, detail, status, icon }: { label: string; detail: string; status: FlowStatus; icon: string }) {
  return (
    <div className={`loop-flow-node loop-flow-node--${status}`} data-status={status}>
      <span className="loop-flow-node__icon" aria-hidden="true">{icon}</span>
      <strong>{label}</strong>
      <small>{detail}</small>
    </div>
  )
}

function LoopOrchestrationBoard({
  definition,
  runtime,
  history,
  current,
  evidence,
}: {
  definition: LoopDefinition
  runtime?: LoopRuntimeState | null
  history: LoopInvocation[]
  current?: LoopInvocation | null
  evidence?: { plan: number; tools: number; checks: number; artifacts: number }
}) {
  const { t } = useTranslation()
  const latest = current ?? history[history.length - 1]
  const flow = orchestrationStatus(latest)
  const chips = [
    { label: t('loops.contractSharpened'), value: definition.contract.goal },
    { label: t('loops.boundariesRedrawn'), value: definition.contract.boundaries.length ? `${definition.contract.boundaries.length}` : '—' },
    { label: t('loops.repeatedSteps'), value: definition.contract.sop.length ? `${definition.contract.sop.length}` : '—' },
    { label: t('loops.triggerRetuned'), value: definition.schedule.kind || t('loops.manual') },
    { label: t('loops.dashboardUpdated'), value: runtime?.last_status ? translatedEnum(t, 'invocationStatus', runtime.last_status) : t('loops.neverRun') },
  ]
  return (
    <section className="loop-orchestration projection-card" data-testid="loop-orchestration" aria-label={t('loops.orchestrationTitle')}>
      <div className="loop-orchestration__heading">
        <div>
          <span className="loop-kicker">LOOPANY WORKFLOW</span>
          <h2>{t('loops.orchestrationTitle')}</h2>
          <p>{t('loops.orchestrationDescription')}</p>
        </div>
        <span className={`status-badge status-badge--${flow === 'success' ? 'success' : flow === 'failed' ? 'error' : flow === 'blocked' ? 'warning' : 'muted'}`}>
          {t(`loops.flowStatus.${flow}`)}
        </span>
      </div>
      <div className="loop-role-grid">
        <article className="loop-role-card loop-role-card--orchestrator"><span aria-hidden="true">⤴</span><div><strong>{t('loops.orchestrator')}</strong><p>{t('loops.orchestratorDescription')}</p></div></article>
        <article className="loop-role-card loop-role-card--executor"><span aria-hidden="true">◇</span><div><strong>{t('loops.executor')}</strong><p>{t('loops.executorDescription')}</p></div></article>
        <article className="loop-role-card loop-role-card--verifier"><span aria-hidden="true">✓</span><div><strong>{t('loops.verifier')}</strong><p>{t('loops.verifierDescription')}</p></div></article>
      </div>
      <div className="loop-flow" role="list" aria-label={t('loops.orchestrationStages')}>
        <FlowNode label={t('loops.triggerNode')} detail={latest?.trigger ?? t('loops.manual')} status={stageStatus('trigger', flow)} icon="⏰" />
        <span className="loop-flow-connector" aria-hidden="true">→</span>
        <FlowNode label={t('loops.orchestratorNode')} detail={t('loops.dispatchDetail')} status={stageStatus('orchestrator', flow)} icon="↗" />
        <span className="loop-flow-connector" aria-hidden="true">→</span>
        <FlowNode label={t('loops.executorNode')} detail={latest?.session_id ? t('loops.sessionBound') : t('loops.waitingForRun')} status={stageStatus('executor', flow)} icon="□" />
        <span className="loop-flow-connector" aria-hidden="true">→</span>
        <FlowNode label={t('loops.verifierNode')} detail={definition.verification_recipe.checks.length ? t('loops.checkCount', { count: definition.verification_recipe.checks.length }) : t('loops.noChecks')} status={stageStatus('verifier', flow)} icon="✓" />
        <span className="loop-flow-connector" aria-hidden="true">→</span>
        <FlowNode label={t('loops.evidenceNode')} detail={evidence ? t('loops.evidenceSummary', evidence) : t('loops.evidencePending')} status={stageStatus('evidence', flow)} icon="▣" />
        <span className="loop-flow-connector" aria-hidden="true">→</span>
        <FlowNode label={t('loops.evolveNode')} detail={t('loops.evolveDetail')} status={stageStatus('evolve', flow)} icon="↻" />
      </div>
      <div className="loop-flow-chips" aria-label={t('loops.evolutionSignals')}>
        {chips.map((chip) => <span className="loop-flow-chip" key={chip.label}><b>{chip.label}</b><small>{chip.value}</small></span>)}
      </div>
    </section>
  )
}

export function LoopDetailPage() {
  const { loopId } = useParams()
  const { t } = useTranslation()
  const { actions } = useUI()
  const connectivity = useConnectivity()
  const [definition, setDefinition] = useState<LoopDefinition | null>(null)
  const [runtime, setRuntime] = useState<LoopRuntimeState | null>(null)
  const [history, setHistory] = useState<LoopInvocation[]>([])
  const [employees, setEmployees] = useState<EmployeeSummary[]>([])
  const [readiness, setReadiness] = useState<Record<string, EmployeeDryRun>>({})
  const [info, setInfo] = useState<Info | null>(null)
  const [team, setTeam] = useState<TeamTemplate | null>(null)
  const [report, setReport] = useState<DryRunReport | null>(null)
  const [busy, setBusy] = useState(false)
  const [notFound, setNotFound] = useState(false)
  const requestEpoch = useRef(0)
  const requestController = useRef<AbortController | null>(null)

  const refresh = useCallback(async () => {
    if (!loopId) return
    const epoch = ++requestEpoch.current
    requestController.current?.abort()
    const controller = new AbortController()
    requestController.current = controller
    try {
      const [next, runtimeState, invocations, activeEmployees, nextTeam, catalog] = await Promise.all([
        getLoop(loopId, { signal: controller.signal }),
        getLoopRuntime(loopId, { signal: controller.signal }),
        listLoopInvocations(loopId, { signal: controller.signal }),
        loadAllActiveEmployees(controller.signal),
        getTeamTemplate({ signal: controller.signal }),
        getInfo({ signal: controller.signal }),
      ])
      const reports = await Promise.all(activeEmployees.map(async (employee) => {
        try {
          return await dryRunEmployee(employee.id, { signal: controller.signal })
        } catch {
          return { employee_id: employee.id, revision: employee.revision, ready: false, checks: [] }
        }
      }))
      if (epoch !== requestEpoch.current) return
      setDefinition(next)
      setRuntime(runtimeState)
      setHistory(invocations.invocations as LoopInvocation[])
      setEmployees(activeEmployees)
      setTeam(nextTeam)
      setInfo(catalog)
      setReadiness(Object.fromEntries(reports.map((item) => [item.employee_id, item])))
      setNotFound(false)
    } catch (error) {
      if (epoch !== requestEpoch.current) return
      setNotFound(error instanceof ApiError && error.status === 404)
    }
  }, [loopId])

  useEffect(() => {
    setBusy(false)
    void refresh()
    return () => {
      requestEpoch.current += 1
      requestController.current?.abort()
    }
  }, [connectivity.generation, refresh])

  async function mutate(action: 'save' | 'start' | 'dry-run') {
    if (!loopId || !definition || !connectivity.canMutate) return
    const owner = loopId
    const epoch = requestEpoch.current
    setBusy(true)
    try {
      if (action === 'save') {
        const updated = await updateLoop(loopId, normalizedDefinition(definition))
        if (epoch !== requestEpoch.current || owner !== loopId) return
        setDefinition(updated)
        setReport(null)
      } else if (action === 'start') {
        await startLoopInvocation(loopId)
        if (epoch !== requestEpoch.current || owner !== loopId) return
        await refresh()
      } else {
        const nextReport = await dryRunLoop(loopId)
        if (epoch !== requestEpoch.current || owner !== loopId) return
        setReport(nextReport)
      }
    } catch (error) {
      if (epoch !== requestEpoch.current || owner !== loopId) return
      actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
      if (error instanceof ApiError && error.status === 409) await refresh()
    } finally {
      if (owner === loopId) setBusy(false)
    }
  }

  async function saveTeam() {
    if (!team || !connectivity.canMutate) return
    const owner = loopId
    const epoch = requestEpoch.current
    const incomplete = Object.values(team.roles).some((selection) => selection.employee_id && (
      [selection.company, selection.access, selection.model].some(Boolean)
      && ![selection.company, selection.access, selection.model].every(Boolean)
    ))
    if (incomplete) {
      actions.showToast({ messageKey: 'loops.incompleteOverride', tone: 'error' })
      return
    }
    setBusy(true)
    try {
      await importTeamTemplate(team)
      if (epoch !== requestEpoch.current || owner !== loopId) return
      actions.showToast({ messageKey: 'toast.saved', tone: 'success' })
    } catch (error) {
      if (epoch !== requestEpoch.current || owner !== loopId) return
      actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
    } finally {
      if (owner === loopId) setBusy(false)
    }
  }

  if (notFound) return <ErrorState title={t('loops.notFound')} description={t('loops.notFoundDescription')} />
  if (!definition || !team || !runtime) return <p role="status">{t('common.loading')}</p>
  return (
    <article className="feature-page loop-workbench">
      <PageHeader title={definition.name} description={`${definition.employee_id ?? t('loops.legacy')} · ${definition.id} · r${definition.revision}`} />
      <LoopOrchestrationBoard definition={definition} runtime={runtime} history={history} />
      <div className="loop-command-bar">
        <div><span>{t('loops.nextRun')}</span><strong>{runtime.next_run_at ? new Date(runtime.next_run_at).toLocaleString() : t('loops.manual')}</strong></div>
        <div><span>{t('loops.lastStatus')}</span><strong>{runtime.last_status ? translatedEnum(t, 'invocationStatus', runtime.last_status) : t('loops.neverRun')}</strong></div>
        <div><span>{t('loops.successRate')}</span><strong>{runtime.total_runs ? `${runtime.successful_runs}/${runtime.total_runs}` : '—'}</strong></div>
        <div className="button-row">
          <button type="button" disabled={busy || !connectivity.canMutate} onClick={() => void mutate('dry-run')}>{t('loops.dryRun')}</button>
          <button className="button button--primary" type="button" disabled={busy || !connectivity.canMutate || !report?.ready} onClick={() => void mutate('start')}>{t('loops.runNow')}</button>
        </div>
      </div>
      {report ? <DryRunProjection report={report} /> : <p className="stale-notice">{t('loops.runDryFirst')}</p>}
      <div className="loop-detail-grid">
        <section className="projection-card loop-contract-panel">
          <span className="loop-kicker">LOOP.md</span>
          <h2>{t('loops.contract')}</h2>
          <label>{t('loops.goal')}<textarea value={definition.contract.goal} onChange={(event) => setDefinition({ ...definition, contract: { ...definition.contract, goal: event.target.value } })} /></label>
          <label>{t('loops.boundaries')}<textarea value={definition.contract.boundaries.join('\n')} onChange={(event) => setDefinition({ ...definition, contract: { ...definition.contract, boundaries: event.target.value.split(/\r?\n/u).map((item) => item.trim()).filter(Boolean) } })} /></label>
          <label>{t('loops.sop')}<textarea value={definition.contract.sop.join('\n')} onChange={(event) => setDefinition({ ...definition, contract: { ...definition.contract, sop: event.target.value.split(/\r?\n/u).map((item) => item.trim()).filter(Boolean) } })} /></label>
          <label>{t('loops.definitionOfDone')}<textarea value={definition.contract.definition_of_done.join('\n')} onChange={(event) => setDefinition({ ...definition, contract: { ...definition.contract, definition_of_done: event.target.value.split(/\r?\n/u).map((item) => item.trim()).filter(Boolean) } })} /></label>
          <label>{t('loops.stopConditions')}<textarea value={definition.contract.stop_conditions.join('\n')} onChange={(event) => setDefinition({ ...definition, contract: { ...definition.contract, stop_conditions: event.target.value.split(/\r?\n/u).map((item) => item.trim()).filter(Boolean) } })} /></label>
          <div className="form-grid">
            <label>{t('loops.scheduleKind')}<select value={definition.schedule.kind || 'manual'} onChange={(event) => setDefinition({ ...definition, schedule: event.target.value === 'daily' ? { kind: 'daily', local_time: definition.schedule.local_time || '02:00', timezone: definition.schedule.timezone || 'Asia/Shanghai' } : { kind: 'manual', local_time: '', timezone: '' } })}><option value="manual">{t('loops.manual')}</option><option value="daily">{t('loops.daily')}</option></select></label>
            {definition.schedule.kind === 'daily' ? <label>{t('loops.runTime')}<input type="time" value={definition.schedule.local_time} onChange={(event) => setDefinition({ ...definition, schedule: { ...definition.schedule, local_time: event.target.value } })} /></label> : null}
          </div>
          <div className="button-row">
            <button className="button button--primary" type="button" disabled={busy || !connectivity.canMutate} onClick={() => void mutate('save')}>{t('loops.saveContract')}</button>
            <a className="button button--secondary" href={`/api/loops/${encodeURIComponent(definition.id)}/contract.md`} target="_blank" rel="noreferrer">{t('loops.openMarkdown')}</a>
          </div>
        </section>
        <section className="projection-card loop-state-panel">
          <span className="loop-kicker">state.json</span>
          <h2>{t('loops.state')}</h2>
          <dl>
            <dt>{t('loops.nextRun')}</dt><dd>{runtime.next_run_at ?? '—'}</dd>
            <dt>{t('loops.lastRun')}</dt><dd>{runtime.last_run_at ?? '—'}</dd>
            <dt>{t('loops.lastStatus')}</dt><dd>{runtime.last_status ? translatedEnum(t, 'invocationStatus', runtime.last_status) : '—'}</dd>
            <dt>{t('loops.failures')}</dt><dd>{runtime.consecutive_failures}</dd>
            <dt>{t('loops.totalRuns')}</dt><dd>{runtime.total_runs}</dd>
          </dl>
          <p>{t('loops.stateExplanation')}</p>
        </section>
        <section className="projection-card loop-log-panel">
          <span className="loop-kicker">runs/</span>
          <h2>{t('loops.logs')}</h2>
          {history.length ? <ol className="loop-run-list">{history.map((invocation) => <li key={invocation.id}><Link to={`/loops/${encodeURIComponent(definition.id)}/invocations/${encodeURIComponent(invocation.id)}`}><strong>{translatedEnum(t, 'invocationStatus', invocation.status)}</strong><span>{new Date(invocation.created_at).toLocaleString()}</span><small>{invocation.failure_summary || invocation.id}</small></Link></li>)}</ol> : <p>{t('loops.noRuns')}</p>}
        </section>
      </div>
      <details className="advanced-panel projection-card">
        <summary>{t('loops.advanced')}</summary>
        <DefinitionForm value={definition} onChange={(next) => { setDefinition(next); setReport(null) }} info={info} includeId={false} />
        {!definition.employee_id ? <TeamEditor team={team} setTeam={setTeam} employees={employees} readiness={readiness} info={info} onSave={() => void saveTeam()} busy={busy || !connectivity.canMutate} /> : null}
      </details>
    </article>
  )
}

function ProjectionList({ title, empty, children }: { title: string; empty: string; children: ReactNode }) {
  return <section className="projection-card"><h2>{title}</h2>{children ?? <p>{empty}</p>}</section>
}

export function LoopInvocationPage() {
  const { invocationId } = useParams()
  const { t } = useTranslation()
  const { actions } = useUI()
  const connectivity = useConnectivity()
  const [invocation, setInvocation] = useState<LoopInvocation | null>(null)
  const [session, setSession] = useState<SessionDetailResponse | null>(null)
  const [approvals, setApprovals] = useState<ApprovalRequest[]>([])
  const [notFound, setNotFound] = useState(false)
  const requestEpoch = useRef(0)
  const requestController = useRef<AbortController | null>(null)

  const refresh = useCallback(async () => {
    if (!invocationId) return
    const epoch = ++requestEpoch.current
    requestController.current?.abort()
    const controller = new AbortController()
    requestController.current = controller
    try {
      const next = await getLoopInvocation(invocationId, { signal: controller.signal })
      const [nextSession, pending] = next.session_id
        ? await Promise.all([
            getSession(next.session_id, { signal: controller.signal }),
            listApprovals(next.session_id, { signal: controller.signal }),
          ])
        : [null, { approvals: [] }]
      if (epoch !== requestEpoch.current) return
      setInvocation(next)
      setSession(nextSession)
      setApprovals(pending.approvals)
      setNotFound(false)
    } catch (error) {
      if (epoch !== requestEpoch.current) return
      setNotFound(error instanceof ApiError && error.status === 404)
    }
  }, [invocationId])
  useEffect(() => {
    void refresh()
    return () => {
      requestEpoch.current += 1
      requestController.current?.abort()
    }
  }, [connectivity.generation, refresh])
  const events = useSessionEvents({
    sessionId: invocation?.session_id,
    frontier: session?.session.next_event_sequence ?? 0,
    runId: invocation?.run_id,
    onRefresh: () => { void refresh() },
  })
  const boundRun = useMemo(() => session?.session.runs.find((run) => run.id === invocation?.run_id), [invocation?.run_id, session])
  const tools = session?.session.tool_calls.filter((tool) => !invocation?.run_id || tool.run_id === invocation.run_id) ?? []
  const verification = session?.session.test_results.filter((test) => !invocation?.run_id || test.run_id === invocation.run_id) ?? []
  const invocationEvidence = {
    plan: boundRun?.plan?.steps.length ?? 0,
    tools: tools.length,
    checks: verification.length,
    artifacts: 0,
  }

  function cancel() {
    if (!invocation || !connectivity.canMutate) return
    actions.openDialog({
      titleKey: 'loops.cancelTitle',
      descriptionKey: 'loops.cancelDescription',
      confirmKey: 'loops.cancel',
      tone: 'warning',
      onConfirm: () => {
        const owner = invocation.id
        const epoch = requestEpoch.current
        void (async () => {
          try {
            const updated = await cancelLoopInvocation(invocation.id)
            if (
              epoch !== requestEpoch.current ||
              owner !== invocationId
            ) return
            setInvocation(updated)
            await refresh()
          } catch (error) {
            if (epoch !== requestEpoch.current || owner !== invocationId) return
            actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
          }
        })()
      },
    })
  }

  async function decide(requestId: string, decision: 'approve' | 'deny') {
    if (!invocation?.session_id || !connectivity.canMutate) return
    const owner = invocation.id
    const epoch = requestEpoch.current
    try {
      await decideApproval(invocation.session_id, requestId, decision)
      if (epoch !== requestEpoch.current || owner !== invocationId) return
      await refresh()
    } catch (error) {
      if (epoch !== requestEpoch.current || owner !== invocationId) return
      actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
    }
  }

  if (notFound) return <ErrorState title={t('loops.invocationNotFound')} description={t('loops.notFoundDescription')} />
  if (!invocation) return <p role="status">{t('common.loading')}</p>
  const active = !TERMINAL_INVOCATIONS.has(invocation.status)
  return (
    <article className="feature-page">
      <PageHeader title={invocation.id} description={`${invocation.loop_id} · ${translatedEnum(t, 'invocationStatus', invocation.status)}`} />
      <LoopOrchestrationBoard definition={invocation.definition_snapshot} history={[invocation]} current={invocation} evidence={invocationEvidence} />
      <dl>
        <dt>{t('loops.definitionRevision')}</dt><dd>{invocation.definition_revision}</dd>
        <dt>{t('loops.trigger')}</dt><dd>{invocation.trigger}</dd>
        <dt>{t('loops.created')}</dt><dd>{invocation.created_at}</dd>
        <dt>{t('loops.started')}</dt><dd>{invocation.started_at ?? '—'}</dd>
        <dt>{t('loops.finished')}</dt><dd>{invocation.finished_at ?? '—'}</dd>
        {invocation.failure_code ? <><dt>{t('loops.failure')}</dt><dd>{invocation.failure_code} · {invocation.failure_summary}</dd></> : null}
      </dl>
      {active ? <button type="button" disabled={!connectivity.canMutate} onClick={cancel}>{t('loops.cancel')}</button> : null}
      <ProjectionList title={t('loops.definitionSnapshot')} empty={t('common.empty')}>
        <dl>
          <dt>{t('loops.name')}</dt><dd>{invocation.definition_snapshot.name}</dd>
          <dt>{t('loops.workspace')}</dt><dd>{invocation.definition_snapshot.workspace_identity}</dd>
          <dt>{t('loops.mission')}</dt><dd>{invocation.task_snapshot}</dd>
          <dt>{t('loops.model')}</dt><dd>{invocation.definition_snapshot.agent_selection.model}</dd>
          <dt>{t('loops.verificationChecks')}</dt><dd>{invocation.definition_snapshot.verification_recipe.checks.length}</dd>
        </dl>
      </ProjectionList>
      <ProjectionList title={t('session.plan')} empty={t('common.empty')}>
        {boundRun?.plan ? <ol>{boundRun.plan.steps.map((step) => <li key={step.id}>{step.title} · {translatedEnum(t, 'planStatus', step.status)}</li>)}</ol> : null}
      </ProjectionList>
      <ProjectionList title={t('loops.tools')} empty={t('common.empty')}>
        {tools.length ? <ul>{tools.map((tool) => <li key={tool.call_id}>{tool.name} · {tool.summary} · {translatedEnum(t, 'toolStatus', tool.status || (tool.is_error ? 'error' : 'completed'))}</li>)}</ul> : null}
      </ProjectionList>
      <ProjectionList title={t('session.verification')} empty={t('common.empty')}>
        {verification.length ? <ul>{verification.map((test) => <li key={`${test.command}-${test.turn}`}>{test.command} · {test.passed ? t('common.passed') : t('common.failed')} · {test.summary}</li>)}</ul> : null}
      </ProjectionList>
      <ProjectionList title={t('session.approvals')} empty={t('common.empty')}>
        {approvals.length ? <ul>{approvals.map((approval) => <li key={approval.request_id}>{approval.tool} · {approval.args_summary}<button type="button" disabled={!connectivity.canMutate} onClick={() => void decide(approval.request_id, 'approve')}>{t('approval.approve')}</button><button type="button" disabled={!connectivity.canMutate} onClick={() => void decide(approval.request_id, 'deny')}>{t('approval.deny')}</button></li>)}</ul> : null}
      </ProjectionList>
      <section className="projection-card" data-testid="loop-timeline">
        <h2>{t('loops.timeline')}</h2>
        <p>{invocation.id}</p>
        {events.fatal ? <button type="button" onClick={events.reconnect}>{t('session.reconnectEvents')}</button> : null}
        {events.status === 'reconnecting' ? <p role="status">{t('session.reconnecting')}</p> : null}
        {events.truncated ? <p role="status">{t('session.streamingTruncated')}</p> : null}
        <ul>{events.events.map((event) => <li key={event.sequence}>{event.sequence} · {translatedEnum(t, 'runtimeEventType', event.type)}</li>)}</ul>
      </section>
    </article>
  )
}
