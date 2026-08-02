import { useCallback, useEffect, useState } from 'react'
import { Button, Card, Col, Row, Select, Tag, Typography } from 'antd'
import { Plus, SlidersHorizontal, UsersRound } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import {
  createEmployee,
  dryRunEmployee,
  getEmployee,
  getEmployeeActivity,
  getEmployeeKnowledge,
  getEmployeeMemory,
  getEmployeeMemoryCandidates,
  getEmployeeSkills,
  listEmployeeTasks,
  listEmployees,
  listProjects,
  listSkills,
  mutateEmployeeLifecycle,
  updateEmployee,
} from '../../api/endpoints'
import { ApiError } from '../../api/errors'
import type {
  Employee,
  EmployeeRecord,
  EmployeeState,
  EmployeeSummary,
  SkillCatalogItem,
} from '../../api/types'
import { useConnectivity } from '../../components/ConnectivityProvider'
import { EmptyState } from '../../components/EmptyState'
import { ErrorState } from '../../components/ErrorState'
import { PageHeader } from '../../components/PageHeader'
import { translatedEnum } from '../../i18n/enumLabel'
import { useUI } from '../../state/UIContext'
import { EmployeeDetailPage as Phase4EmployeeDetailPage } from './EmployeeDetailPage'
import { EmployeeWizard as Phase4EmployeeWizard } from './EmployeeWizard'

const WIZARD_STEPS = [
  'identity', 'modelAgent', 'charter', 'skills', 'knowledge',
  'memory', 'projects', 'policy', 'review',
] as const

function mutationKey(error: unknown) {
  if (error instanceof ApiError && error.code === 'network_error') return 'mutation.offline'
  if (error instanceof ApiError && error.status === 409) return 'mutation.conflict'
  return 'mutation.failed'
}

function statusLabel(
  t: ReturnType<typeof useTranslation>['t'],
  state: EmployeeState,
) {
  return translatedEnum(t, 'employeeStatus', state)
}

function defaultEmployee(): Employee {
  const now = new Date(0).toISOString()
  return {
    id: '',
    schema_version: 1,
    revision: 0,
    state: 'active',
    name: '',
    avatar: { kind: 'initials', value: '?' },
    job_title: '',
    charter: '',
    responsibilities: [],
    behavior_boundaries: [],
    default_selection: { company: 'openai', access: 'codex', model: '' },
    agent_profile: 'coding',
    skill_bindings: [],
    project_binding_ids: [],
    permission_policy: { allowed_capabilities: ['read'], network_allowed: false },
    budget_policy: { max_model_calls: 8, max_tokens: 32_000, timeout_seconds: 1_200 },
    concurrency_policy: { max_running_tasks: 1 },
    memory_policy: {
      candidate_generation: true,
      promotion: 'owner_confirmation',
      max_context_facts: 16,
      max_context_bytes: 32_768,
    },
    project_count: 0,
    created_at: now,
    updated_at: now,
  }
}

export function LegacyEmployeeWizard({ onClose, onCreated }: {
  onClose: () => void
  onCreated: (record: EmployeeRecord) => void
}) {
  const { t } = useTranslation()
  const { actions } = useUI()
  const [step, setStep] = useState(0)
  const [employee, setEmployee] = useState(defaultEmployee)
  const [skills, setSkills] = useState<SkillCatalogItem[]>([])
  const [catalogReady, setCatalogReady] = useState(false)
  const [busy, setBusy] = useState(false)
  const [readiness, setReadiness] = useState<Awaited<ReturnType<typeof dryRunEmployee>> | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    Promise.all([
      listProjects({ signal: controller.signal }),
      listSkills({ signal: controller.signal }),
    ]).then(([, skillPage]) => {
      setSkills(skillPage.skills)
      setCatalogReady(true)
    }).catch(() => setCatalogReady(false))
    return () => controller.abort()
  }, [])

  const patch = (value: Partial<Employee>) => setEmployee((current) => ({ ...current, ...value }))
  async function finish() {
    if (!employee.id || !employee.name || !employee.charter || !catalogReady) return
    setBusy(true)
    try {
      const record = await createEmployee({ employee, project_bindings: [] })
      const report = await dryRunEmployee(record.employee.id)
      setReadiness(report)
      if (report.ready === true) {
        onCreated(record)
        actions.showToast({ messageKey: 'employees.created', tone: 'success' })
      }
    } catch (error) {
      actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="projection-card" aria-label={t('employees.create')}>
      <h2>{t('employees.create')}</h2>
      <p>{t('employees.step', { current: step + 1, total: WIZARD_STEPS.length })} · {t(`employees.steps.${WIZARD_STEPS[step]}`)}</p>
      {step === 0 ? (
        <div className="form-grid">
          <label>{t('employees.id')}<input value={employee.id} onChange={(event) => patch({ id: event.target.value })} /></label>
          <label>{t('employees.name')}<input value={employee.name} onChange={(event) => patch({ name: event.target.value })} /></label>
          <label>{t('employees.jobTitle')}<input value={employee.job_title} onChange={(event) => patch({ job_title: event.target.value })} /></label>
        </div>
      ) : null}
      {step === 1 ? (
        <label>{t('employees.charter')}<textarea value={employee.charter} onChange={(event) => patch({ charter: event.target.value })} /></label>
      ) : null}
      {step === 2 ? (
        <div className="form-grid">
          <label>{t('employees.company')}<input value={employee.default_selection.company} onChange={(event) => patch({ default_selection: { ...employee.default_selection, company: event.target.value } })} /></label>
          <label>{t('employees.access')}<input value={employee.default_selection.access} onChange={(event) => patch({ default_selection: { ...employee.default_selection, access: event.target.value } })} /></label>
          <label>{t('employees.model')}<input value={employee.default_selection.model} onChange={(event) => patch({ default_selection: { ...employee.default_selection, model: event.target.value } })} /></label>
        </div>
      ) : null}
      {step === 3 ? <p>{catalogReady ? t('employees.catalogReady') : t('common.loading')}</p> : null}
      {step === 4 ? (
        <>
          <p>{t('employees.skillBindingContract')}</p>
          <ul>
            {skills.map((skill) => {
              const key = `${skill.skill_id}:${skill.version}:${skill.digest}`
              const checked = employee.skill_bindings.some((binding) =>
                binding.skill_id === skill.skill_id
                && binding.version === skill.version
                && binding.digest === skill.digest)
              return (
                <li key={key}>
                  <label>
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={(event) => patch({
                        skill_bindings: event.target.checked
                          ? [...employee.skill_bindings, {
                            skill_id: skill.skill_id,
                            version: skill.version,
                            digest: skill.digest,
                            configuration: {},
                            enabled: true,
                          }]
                          : employee.skill_bindings.filter((binding) =>
                            `${binding.skill_id}:${binding.version}:${binding.digest}` !== key),
                      })}
                    />
                    {skill.title} · {skill.skill_id}@{skill.version} · {skill.digest}
                    {skill.kind === 'skill_md_adapter' ? ` · ${t('employees.adapterZeroCapability')}` : ''}
                  </label>
                </li>
              )
            })}
          </ul>
        </>
      ) : null}
      {step === 5 ? (
        <label><input type="checkbox" checked={employee.permission_policy.network_allowed} onChange={(event) => patch({ permission_policy: { ...employee.permission_policy, network_allowed: event.target.checked } })} /> {t('employees.network')}</label>
      ) : null}
      {step === 6 ? (
        <label>{t('employees.maxCalls')}<input type="number" min="1" value={employee.budget_policy.max_model_calls} onChange={(event) => patch({ budget_policy: { ...employee.budget_policy, max_model_calls: Number(event.target.value) } })} /></label>
      ) : null}
      {step === 7 ? (
        <label><input type="checkbox" checked={employee.memory_policy.candidate_generation} onChange={(event) => patch({ memory_policy: { ...employee.memory_policy, candidate_generation: event.target.checked, promotion: event.target.checked ? 'owner_confirmation' : 'disabled' } })} /> {t('employees.memoryCandidates')}</label>
      ) : null}
      {step === 8 ? (
        <>
          <dl>
            <dt>{t('employees.name')}</dt><dd>{employee.name}</dd>
            <dt>{t('employees.charter')}</dt><dd>{employee.charter}</dd>
          </dl>
          <p>{t('employees.serverReadiness')}</p>
          {readiness ? <pre>{JSON.stringify(readiness, null, 2)}</pre> : null}
        </>
      ) : null}
      <div className="button-row">
        <button type="button" onClick={onClose}>{t('actions.cancel')}</button>
        {step > 0 ? <button type="button" onClick={() => setStep((value) => value - 1)}>{t('employees.previous')}</button> : null}
        {step < WIZARD_STEPS.length - 1 ? (
          <button type="button" onClick={() => setStep((value) => value + 1)}>{t('employees.next')}</button>
        ) : (
          <button type="button" disabled={busy || !catalogReady} onClick={() => void finish()}>{t('employees.createAndCheck')}</button>
        )}
      </div>
    </section>
  )
}

export function EmployeesPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const connectivity = useConnectivity()
  const [searchParams, setSearchParams] = useSearchParams()
  const state = searchParams.get('state') ?? ''
  const [items, setItems] = useState<EmployeeSummary[]>([])
  const [cursor, setCursor] = useState<string>()
  const [error, setError] = useState(false)
  const [wizard, setWizard] = useState(false)

  const load = useCallback(async (nextCursor?: string) => {
    try {
      const query: { state?: string; cursor?: string; limit: number } = { limit: 100 }
      if (state) query.state = state
      if (nextCursor) query.cursor = nextCursor
      const page = await listEmployees(query, {})
      setItems((current) => {
        if (!nextCursor) return page.employees
        const combined = new Map(current.map((employee) => [employee.id, employee]))
        for (const employee of page.employees) combined.set(employee.id, employee)
        return [...combined.values()]
      })
      setCursor(page.next_cursor)
      setError(false)
    } catch {
      setError(true)
    }
  }, [state])

  useEffect(() => { void load() }, [load, connectivity.generation])

  if (error && items.length === 0) {
    return (
      <ErrorState
        title={t('employees.loadError')}
        description={t('common.retryDescription')}
        action={<Button type="primary" onClick={() => void load()}>{t('actions.retry')}</Button>}
      />
    )
  }
  return (
    <article className="feature-page employees-page">
      <PageHeader
        title={t('pages.employees.title')}
        description={t('employees.description')}
        actions={(
          <Button type="primary" icon={<Plus size={16} aria-hidden="true" />} disabled={!connectivity.canMutate} onClick={() => setWizard(true)}>
            {t('employees.create')}
          </Button>
        )}
      />
      {wizard ? <Phase4EmployeeWizard onClose={() => setWizard(false)} onCreated={(record) => { void navigate(`/employees/${encodeURIComponent(record.employee.id)}`) }} /> : null}
      <div className="filter-field">
        <Typography.Text strong><SlidersHorizontal size={16} aria-hidden="true" />{t('employees.state')}</Typography.Text>
        <Select
          aria-label={t('employees.state')}
          value={state || undefined}
          placeholder={t('employees.all')}
          allowClear
          options={[
            { value: 'active', label: statusLabel(t, 'active') },
            { value: 'disabled', label: statusLabel(t, 'disabled') },
            { value: 'archived', label: statusLabel(t, 'archived') },
          ]}
          onChange={(value) => {
            const next = new URLSearchParams(searchParams)
            if (value) next.set('state', value)
            else next.delete('state')
            setSearchParams(next)
          }}
        />
      </div>
      {items.length === 0 ? (
        <EmptyState
          title={t('employees.emptyTitle')}
          description={t('employees.emptyDescription')}
          action={(
            <Button type="primary" icon={<UsersRound size={17} aria-hidden="true" />} disabled={!connectivity.canMutate} onClick={() => setWizard(true)}>
              {t('employees.createFirst')}
            </Button>
          )}
        />
      ) : (
        <Row gutter={[16, 16]} className="employee-grid">
          {items.map((employee) => (
            <Col key={employee.id} xs={24} sm={12} xl={8} xxl={6}>
              <Link className="employee-card-link" to={`/employees/${encodeURIComponent(employee.id)}`}>
                <Card hoverable className="employee-card">
                  <Typography.Title level={4} ellipsis={{ tooltip: employee.name }}>{employee.name}</Typography.Title>
                  <Typography.Paragraph type="secondary" ellipsis={{ rows: 2 }}>{employee.job_title || t('employees.jobTitle')}</Typography.Paragraph>
                  <Tag color={employee.state === 'active' ? 'success' : employee.state === 'archived' ? 'default' : 'warning'}>{statusLabel(t, employee.state)}</Tag>
                </Card>
              </Link>
            </Col>
          ))}
        </Row>
      )}
      {cursor ? <Button onClick={() => void load(cursor)}>{t('employees.loadMore')}</Button> : null}
    </article>
  )
}

type EmployeeTab = 'overview' | 'skills' | 'knowledge' | 'memory' | 'projects' | 'tasks' | 'activity'
const TABS: EmployeeTab[] = ['overview', 'skills', 'knowledge', 'memory', 'projects', 'tasks', 'activity']

export function LegacyEmployeeDetailPage() {
  const { employeeId } = useParams()
  const { t } = useTranslation()
  const { actions } = useUI()
  const connectivity = useConnectivity()
  const [record, setRecord] = useState<EmployeeRecord | null>(null)
  const [tab, setTab] = useState<EmployeeTab>('overview')
  const [projection, setProjection] = useState<unknown>(null)
  const [notFound, setNotFound] = useState(false)
  const [busy, setBusy] = useState(false)

  const refresh = useCallback(async () => {
    if (!employeeId) return
    try {
      setRecord(await getEmployee(employeeId))
      setNotFound(false)
    } catch (error) {
      setNotFound(error instanceof ApiError && error.status === 404)
    }
  }, [employeeId])
  useEffect(() => { void refresh() }, [refresh, connectivity.generation])

  useEffect(() => {
    if (!employeeId || tab === 'overview' || tab === 'projects') return
    const loaders = {
      skills: getEmployeeSkills,
      knowledge: getEmployeeKnowledge,
      memory: async (id: string) => {
        const [facts, candidates] = await Promise.all([
          getEmployeeMemory(id), getEmployeeMemoryCandidates(id),
        ])
        return { facts, candidates }
      },
      tasks: (id: string) => listEmployeeTasks(id, { limit: 100 }),
      activity: getEmployeeActivity,
    }
    void loaders[tab](employeeId).then(setProjection).catch(() => setProjection(null))
  }, [employeeId, tab])

  async function save() {
    if (!employeeId || !record) return
    setBusy(true)
    try {
      setRecord(await updateEmployee(employeeId, {
        expected_revision: record.employee.revision,
        employee: record.employee,
        project_bindings: record.project_bindings,
      }))
      actions.showToast({ messageKey: 'toast.saved', tone: 'success' })
    } catch (error) {
      actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
      if (error instanceof ApiError && error.status === 409) await refresh()
    } finally {
      setBusy(false)
    }
  }

  function lifecycle(action: 'disable' | 'enable' | 'archive') {
    if (!employeeId || !record) return
    actions.openDialog({
      titleKey: `employees.${action}Title`,
      descriptionKey: `employees.${action}Description`,
      confirmKey: `employees.${action}`,
      tone: action === 'archive' ? 'warning' : 'info',
      onConfirm: () => {
        setBusy(true)
        void mutateEmployeeLifecycle(employeeId, action, record.employee.revision)
          .then(setRecord)
          .catch((error: unknown) => {
            actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
            if (error instanceof ApiError && error.status === 409) void refresh()
          })
          .finally(() => setBusy(false))
      },
    })
  }

  if (notFound) return <ErrorState title={t('employees.notFound')} description={t('employees.notFoundDescription')} />
  if (!record) return <p role="status">{t('common.loading')}</p>
  const employee = record.employee
  const archived = employee.state === 'archived'
  const displayProjection = tab === 'projects' ? record.project_bindings : projection
  return (
    <article className="feature-page">
      <PageHeader title={employee.name} description={`${employee.id} · r${employee.revision}`} />
      <p data-testid="employee-status">{statusLabel(t, employee.state)}</p>
      {archived ? <p className="stale-notice">{t('employees.archivedReadOnly')}</p> : null}
      <nav className="tab-list" aria-label={t('employees.sections')}>
        {TABS.map((value) => <button key={value} type="button" aria-current={tab === value ? 'page' : undefined} onClick={() => setTab(value)}>{t(`employees.tabs.${value}`)}</button>)}
      </nav>
      {tab === 'overview' ? (
        <section className="projection-card">
          <label>{t('employees.name')}<input disabled={archived} value={employee.name} onChange={(event) => setRecord({ ...record, employee: { ...employee, name: event.target.value } })} /></label>
          <label>{t('employees.charter')}<textarea disabled={archived} value={employee.charter} onChange={(event) => setRecord({ ...record, employee: { ...employee, charter: event.target.value } })} /></label>
          <dl>
            <dt>{t('employees.defaultModel')}</dt><dd>{employee.default_selection.model}</dd>
            <dt>{t('employees.memory')}</dt><dd>{employee.memory_policy.promotion}</dd>
          </dl>
          {!archived ? (
            <div className="button-row">
              <button type="button" disabled={busy || !connectivity.canMutate} onClick={() => void save()}>{t('employees.save')}</button>
              {employee.state === 'active' ? <button type="button" disabled={busy} onClick={() => lifecycle('disable')}>{t('employees.disable')}</button> : null}
              {employee.state === 'disabled' ? <button type="button" disabled={busy} onClick={() => lifecycle('enable')}>{t('employees.enable')}</button> : null}
              <button type="button" disabled={busy} onClick={() => lifecycle('archive')}>{t('employees.archive')}</button>
              <button type="button" disabled={busy} onClick={() => void dryRunEmployee(employee.id).then(setProjection)}>{t('employees.dryRun')}</button>
            </div>
          ) : null}
        </section>
      ) : (
        <section className="projection-card">
          {tab === 'skills' && projection && typeof projection === 'object' && 'bindings' in projection ? (
            <ul>{(projection as { bindings: Array<{ binding: { skill_id: string; version: string; digest: string }; status: string; kind?: string }> }).bindings.map((item) => (
              <li key={`${item.binding.skill_id}:${item.binding.version}:${item.binding.digest}`}>
                {item.binding.skill_id}@{item.binding.version} · {item.binding.digest} · {item.kind === 'skill_md_adapter' ? t('employees.adapterZeroCapability') : item.status === 'digest_drift' ? t('employees.staleDigest') : item.status}
              </li>
            ))}</ul>
          ) : <pre>{JSON.stringify(displayProjection, null, 2)}</pre>}
        </section>
      )}
    </article>
  )
}

export function EmployeeDetailPage() {
  return <Phase4EmployeeDetailPage />
}
