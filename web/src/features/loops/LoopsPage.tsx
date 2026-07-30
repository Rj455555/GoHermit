import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
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
  LoopSummary,
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
  const [loops, setLoops] = useState<LoopSummary[]>([])
  const [creating, setCreating] = useState(false)
  const [draft, setDraft] = useState(newDefinition)
  const [info, setInfo] = useState<Info | null>(null)
  const [importText, setImportText] = useState('')
  const [strictError, setStrictError] = useState('')
  const pageEpoch = useRef(0)

  useEffect(() => {
    const epoch = ++pageEpoch.current
    const controller = new AbortController()
    void Promise.all([listLoops({ signal: controller.signal }), getInfo({ signal: controller.signal })])
      .then(([page, catalog]) => {
        if (epoch !== pageEpoch.current) return
        setLoops(page.loops)
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
    <article className="feature-page">
      <PageHeader title={t('pages.loops.title')} description={t('loops.description')} />
      <ul className="entity-list">
        {loops.map((loop) => <li key={loop.id}><Link to={`/loops/${encodeURIComponent(loop.id)}`}>{loop.name}</Link><span>r{loop.revision}</span></li>)}
      </ul>
      <section className="projection-card">
        <h2>{t('loops.create')}</h2>
        <DefinitionForm value={draft} onChange={setDraft} info={info} includeId />
        <button type="button" disabled={creating || !connectivity.canMutate} onClick={() => void saveNew()}>{t('loops.create')}</button>
      </section>
      <section className="projection-card">
        <h2>{t('loops.import')}</h2>
        <textarea aria-label={t('loops.import')} value={importText} onChange={(event) => setImportText(event.target.value)} />
        {strictError ? <p role="alert">{strictError}</p> : null}
        <button type="button" disabled={!connectivity.canMutate} onClick={() => void importStrict()}>{t('loops.import')}</button>
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

export function LoopDetailPage() {
  const { loopId } = useParams()
  const { t } = useTranslation()
  const { actions } = useUI()
  const connectivity = useConnectivity()
  const [definition, setDefinition] = useState<LoopDefinition | null>(null)
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
      const [next, invocations, activeEmployees, nextTeam, catalog] = await Promise.all([
        getLoop(loopId, { signal: controller.signal }),
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
  if (!definition || !team) return <p role="status">{t('common.loading')}</p>
  return (
    <article className="feature-page">
      <PageHeader title={definition.name} description={`${definition.id} · r${definition.revision}`} />
      <section className="projection-card">
        <DefinitionForm value={definition} onChange={(next) => { setDefinition(next); setReport(null) }} info={info} includeId={false} />
        <div className="button-row">
          <button type="button" disabled={busy || !connectivity.canMutate} onClick={() => void mutate('save')}>{t('loops.save')}</button>
          <button type="button" disabled={busy || !connectivity.canMutate} onClick={() => void mutate('dry-run')}>{t('loops.dryRun')}</button>
          <button type="button" disabled={busy || !connectivity.canMutate || !report?.ready} onClick={() => void mutate('start')}>{t('loops.start')}</button>
        </div>
        {report ? <DryRunProjection report={report} /> : <p>{t('loops.runDryFirst')}</p>}
      </section>
      <TeamEditor team={team} setTeam={setTeam} employees={employees} readiness={readiness} info={info} onSave={() => void saveTeam()} busy={busy || !connectivity.canMutate} />
      <section className="projection-card">
        <h2>{t('loops.history')}</h2>
        <ul>{history.map((invocation) => <li key={invocation.id}><Link to={`/loops/${encodeURIComponent(definition.id)}/invocations/${encodeURIComponent(invocation.id)}`}>{invocation.id}</Link> · {translatedEnum(t, 'invocationStatus', invocation.status)}</li>)}</ul>
      </section>
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
