import { useEffect, useState } from 'react'
import { ArrowRight, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  addEmployeeKnowledge,
  createEmployee,
  dryRunEmployee,
  getEmployeeKnowledge,
  getEmployeeSkills,
  getInfo,
  listProjects,
  listSkills,
  updateEmployeeSkills,
} from '../../api/endpoints'
import { ApiError } from '../../api/errors'
import type {
  Employee,
  EmployeeDryRun,
  EmployeeRecord,
  ProjectCatalogItem,
  SkillBinding,
  SkillCatalogItem,
} from '../../api/types'
import { useUI } from '../../state/UIContext'
import {
  ensureEmployeeId,
  generateEmployeeDraft,
  isValidEmployeeId,
  type EmployeePreset,
} from './employeeDraft'

const STEPS = [
  'identity', 'modelAgent', 'charter', 'skills', 'knowledge',
  'memory', 'projects', 'policy', 'review',
] as const

function mutationKey(error: unknown) {
  if (error instanceof ApiError && error.code === 'network_error') return 'mutation.offline'
  if (error instanceof ApiError && error.status === 409) return 'mutation.conflict'
  return 'mutation.failed'
}

function initialEmployee(): Employee {
  const epoch = new Date(0).toISOString()
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
    default_selection: { company: '', access: '', model: '' },
    agent_profile: '',
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
    created_at: epoch,
    updated_at: epoch,
  }
}

export function EmployeeWizard({ onClose, onCreated }: {
  onClose: () => void
  onCreated: (record: EmployeeRecord) => void
}) {
  const { t, i18n } = useTranslation()
  const { actions } = useUI()
  const [step, setStep] = useState(0)
  const [employee, setEmployee] = useState(initialEmployee)
  const [guided, setGuided] = useState<{
    preset: EmployeePreset
    displayName: string
    brief: string
  }>({ preset: 'developer', displayName: '', brief: '' })
  const [guidedGenerated, setGuidedGenerated] = useState(false)
  const [draftSuffix] = useState(() => Date.now().toString(36).slice(-6))
  const [skills, setSkills] = useState<SkillCatalogItem[]>([])
  const [projects, setProjects] = useState<ProjectCatalogItem[]>([])
  const [selectedProject, setSelectedProject] = useState('')
  const [companies, setCompanies] = useState<Array<{
    id: string
    access: Array<{ id: string; models: Array<{ id: string }> }>
  }>>([])
  const [agents, setAgents] = useState<Array<{ id: string }>>([])
  const [skillConfiguration, setSkillConfiguration] = useState<Record<string, string>>({})
  const [knowledge, setKnowledge] = useState({
    id: '',
    kind: '' as '' | 'manual_text' | 'file' | 'project_docs',
    title: '',
    content: '',
  })
  const [catalogReady, setCatalogReady] = useState(false)
  const [busy, setBusy] = useState(false)
  const [persisted, setPersisted] = useState<EmployeeRecord | null>(null)
  const [readiness, setReadiness] = useState<EmployeeDryRun | null>(null)
  const [postCreate, setPostCreate] = useState({ skills: 0, knowledge: 0 })
  const [skillsPersisted, setSkillsPersisted] = useState(false)
  const [knowledgePersisted, setKnowledgePersisted] = useState(false)
  const [stepError, setStepError] = useState<string | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    Promise.all([
      getInfo({ signal: controller.signal }),
      listProjects({ signal: controller.signal }),
      listSkills({ signal: controller.signal }),
    ]).then(([info, projectPage, skillPage]) => {
      setCompanies(info.available_companies)
      setAgents(info.agents)
      const company = info.available_companies[0]
      const access = company?.access[0]
      setEmployee((current) => ({
        ...current,
        default_selection: {
          company: company?.id ?? '',
          access: access?.id ?? '',
          model: access?.models[0]?.id ?? '',
        },
        agent_profile: info.agents[0]?.id ?? '',
      }))
      setProjects(projectPage.projects)
      setSelectedProject(projectPage.projects[0]?.id ?? '')
      setSkills(skillPage.skills)
      setCatalogReady(true)
    }).catch((error: unknown) => {
      if (!(error instanceof ApiError && error.code === 'aborted')) {
        actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
      }
    })
    return () => controller.abort()
  }, [actions])

  const patch = (value: Partial<Employee>) => {
    setStepError(null)
    setEmployee((current) => ({ ...current, ...value }))
  }
  const lines = (value: string) =>
    value.split(/\r?\n/u).map((item) => item.trim()).filter(Boolean)

  function generateGuidedDraft() {
    patch(generateEmployeeDraft({
      ...guided,
      locale: i18n.language,
      uniqueSuffix: draftSuffix,
    }))
    setGuidedGenerated(true)
  }

  function safeEmployeeId(): string {
    return ensureEmployeeId(employee.id, employee.name || guided.displayName, draftSuffix)
  }

  function reviewGeneratedDraft() {
    if (!guidedGenerated) return
    if (!catalogReady
      || employee.default_selection.company === ''
      || employee.default_selection.access === ''
      || employee.default_selection.model === ''
      || employee.agent_profile === ''
      || selectedProject === '') {
      setStepError('employees.validation.runtime')
      return
    }
    setStep(7)
  }

  function validateStep(currentStep: number): boolean {
    let key: string | null = null
    if (currentStep === 0 && (
      employee.name.trim() === ''
      || employee.job_title.trim() === ''
    )) key = 'employees.validation.identity'
    if (currentStep === 0 && key === null && !isValidEmployeeId(employee.id)) {
      patch({ id: safeEmployeeId() })
    }
    if (currentStep === 1 && (
      employee.default_selection.company === ''
      || employee.default_selection.access === ''
      || employee.default_selection.model === ''
      || employee.agent_profile === ''
    )) key = 'employees.validation.runtime'
    if (currentStep === 2 && employee.charter.trim() === '') {
      key = 'employees.validation.charter'
    }
    if (currentStep === 3) {
      try {
        bindings()
      } catch {
        key = 'employees.validation.skills'
      }
    }
    if (currentStep === 4 && knowledge.kind && (
      knowledge.id.trim() === ''
      || knowledge.title.trim() === ''
      || knowledge.content.trim() === ''
    )) key = 'employees.validation.knowledge'
    if (currentStep === 6 && selectedProject === '') {
      key = 'employees.validation.project'
    }
    if (currentStep === 7 && (
      employee.permission_policy.allowed_capabilities.length === 0
      || employee.budget_policy.max_model_calls < 1
      || employee.budget_policy.max_tokens < 1
      || employee.budget_policy.timeout_seconds < 1
    )) key = 'employees.validation.policy'
    setStepError(key)
    return key === null
  }

  function bindings(): SkillBinding[] {
    return employee.skill_bindings.map((binding) => {
      const catalog = skills.find((item) =>
        item.skill_id === binding.skill_id && item.version === binding.version)
      if (catalog?.kind === 'skill_md_adapter') return { ...binding, configuration: {} }
      const key = `${binding.skill_id}:${binding.version}:${binding.digest}`
      const parsed = JSON.parse(skillConfiguration[key] ?? '{}') as unknown
      if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
        throw new Error('configuration')
      }
      return { ...binding, configuration: parsed as Record<string, unknown> }
    })
  }

  function projectBinding(employeeId = employee.id) {
    const project = projects.find((item) => item.id === selectedProject)
    if (!project) throw new Error('project')
    const id = `project-${employeeId}`
    return {
      id,
      label: project.label,
      workspace_real_path: project.workspace_real_path,
      read_allowed: true,
      mutation_allowed: employee.permission_policy.allowed_capabilities.some((item) =>
        item === 'write' || item === 'filesystem.write'),
      allowed_tool_capabilities: employee.permission_policy.allowed_capabilities,
      network_allowed: employee.permission_policy.network_allowed,
    }
  }

  async function validatePersisted(
    initialRecord: EmployeeRecord,
    skillsAlreadySaved: boolean,
    knowledgeAlreadySaved: boolean,
  ) {
    let current = initialRecord
    try {
      const selectedBindings = bindings()
      if (!skillsAlreadySaved) {
        current = await updateEmployeeSkills(
          current.employee.id,
          current.employee.revision,
          selectedBindings,
        )
        setPersisted(current)
        setSkillsPersisted(true)
      }
      if (knowledge.kind && !knowledgeAlreadySaved) {
        await addEmployeeKnowledge(current.employee.id, {
          id: knowledge.id,
          kind: knowledge.kind,
          title: knowledge.title,
          ...(knowledge.kind === 'manual_text'
            ? { manual_text: knowledge.content }
            : { relative_path: knowledge.content }),
        })
        setKnowledgePersisted(true)
      }
      const [report, skillProjection, knowledgeProjection] = await Promise.all([
        dryRunEmployee(current.employee.id),
        getEmployeeSkills(current.employee.id),
        getEmployeeKnowledge(current.employee.id),
      ])
      const skillReady = selectedBindings.length === skillProjection.bindings.length
        && selectedBindings.every((selected) => skillProjection.bindings.some((item) =>
          item.status === 'current'
          && item.binding.skill_id === selected.skill_id
          && item.binding.version === selected.version
          && item.binding.digest === selected.digest))
      const knowledgeReady = knowledgeProjection.sources.every((source) => source.status === 'ready')
      setReadiness({
        ...report,
        revision: current.employee.revision,
        ready: report.ready && skillReady && knowledgeReady,
        checks: [
          ...report.checks,
          {
            name: 'skills',
            ready: skillReady,
            detail: skillReady ? 'validated by server' : 'missing or stale Skill binding',
          },
          {
            name: 'knowledge',
            ready: knowledgeReady,
            detail: knowledgeReady ? 'ready' : 'Knowledge source failed',
          },
        ],
      })
      setPostCreate({
        skills: skillProjection.bindings.length,
        knowledge: knowledgeProjection.sources.length,
      })
    } catch (error) {
      setReadiness({
        employee_id: current.employee.id,
        revision: current.employee.revision,
        ready: false,
        checks: [{
          name: 'server_validation',
          ready: false,
          detail: 'The persisted Employee still requires repair.',
        }],
      })
      actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
    }
  }

  async function prepareReview(): Promise<boolean> {
    if (busy) return false
    setBusy(true)
    try {
      let record = persisted
      if (!record) {
        const employeeId = safeEmployeeId()
        const project = projectBinding(employeeId)
        const draft = {
          ...employee,
          id: employeeId,
          skill_bindings: [],
          project_binding_ids: [project.id],
        }
        record = await createEmployee({ employee: draft, project_bindings: [project] })
        setPersisted(record)
      }
      await validatePersisted(
        record,
        persisted ? skillsPersisted : false,
        persisted ? knowledgePersisted : false,
      )
      return true
    } catch (error) {
      setStepError('employees.validation.server')
      actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
      return false
    } finally {
      setBusy(false)
    }
  }

  async function next() {
    if (!validateStep(step)) return
    if (step === 7) {
      if (await prepareReview()) setStep(8)
    } else {
      setStep((current) => Math.min(8, current + 1))
    }
  }

  const company = companies.find((item) => item.id === employee.default_selection.company)
  const access = company?.access.find((item) => item.id === employee.default_selection.access)

  return (
    <section className="projection-card employee-wizard" aria-label={t('employees.create')}>
      <div className="wizard-heading">
        <div>
          <span className="section-kicker">{t('employees.setup')}</span>
          <h2>{t('employees.create')}</h2>
        </div>
        <span>{step + 1}/{STEPS.length}</span>
      </div>
      <p data-testid="employee-wizard-step">
        {t('employees.step', { current: step + 1, total: STEPS.length })}
        {' · '}
        {t(`employees.steps.${STEPS[step]}`)}
      </p>
      <ol className="wizard-progress" aria-label={t('employees.progress')}>
        {STEPS.map((value, index) => (
          <li key={value} data-state={index < step ? 'complete' : index === step ? 'current' : 'pending'}>
            <span>{index + 1}</span>
            <small>{t(`employees.steps.${value}`)}</small>
          </li>
        ))}
      </ol>
      {stepError ? <p className="form-error" role="alert">{t(stepError)}</p> : null}

      {step === 0 ? (
        <div className="form-grid">
          <section className="guided-employee-card wide" aria-labelledby="guided-employee-title">
            <div className="guided-employee-card__heading">
              <span className="guided-employee-card__icon"><Sparkles size={18} /></span>
              <div>
                <h3 id="guided-employee-title">{t('employees.guided.title')}</h3>
                <p>{t('employees.guided.description')}</p>
              </div>
            </div>
            <div className="form-grid">
              <label>{t('employees.guided.preset')}
                <select value={guided.preset} onChange={(event) => {
                  setGuidedGenerated(false)
                  setGuided((current) => ({
                    ...current,
                    preset: event.target.value as EmployeePreset,
                  }))
                }}>
                  {(['developer', 'researcher', 'operations', 'writer'] as const).map((preset) => (
                    <option key={preset} value={preset}>
                      {t(`employees.guided.presets.${preset}`)}
                    </option>
                  ))}
                </select>
              </label>
              <label>{t('employees.guided.name')}
                <input value={guided.displayName} onChange={(event) => {
                  setGuidedGenerated(false)
                  setGuided((current) => ({
                    ...current,
                    displayName: event.target.value,
                  }))
                }} />
              </label>
              <label className="wide">{t('employees.guided.brief')}
                <textarea
                  rows={3}
                  value={guided.brief}
                  onChange={(event) => {
                    setGuidedGenerated(false)
                    setGuided((current) => ({
                      ...current,
                      brief: event.target.value,
                    }))
                  }}
                  placeholder={t('employees.guided.briefPlaceholder')}
                />
              </label>
            </div>
            <div className="guided-employee-card__actions">
              <button className="button button--primary" type="button" onClick={generateGuidedDraft}>
                <Sparkles size={16} />
                {t('employees.guided.generate')}
              </button>
              {guidedGenerated ? (
                <>
                  <span className="guided-employee-card__success">{t('employees.guided.generated')}</span>
                  <button
                    className="button button--secondary"
                    type="button"
                    disabled={!catalogReady}
                    onClick={reviewGeneratedDraft}
                  >
                    {t('employees.guided.review')}
                    <ArrowRight size={16} />
                  </button>
                </>
              ) : null}
            </div>
          </section>
          <label>{t('employees.id')}
            <input
              value={employee.id}
              aria-label={t('employees.id')}
              aria-describedby="employee-id-help"
              onChange={(event) => patch({ id: event.target.value })}
            />
            <small id="employee-id-help" className={employee.id && !isValidEmployeeId(employee.id) ? 'field-help field-help--notice' : 'field-help'}>
              {employee.id && !isValidEmployeeId(employee.id)
                ? t('employees.idWillGenerate', { id: safeEmployeeId() })
                : t('employees.idHelp')}
            </small>
          </label>
          <label>{t('employees.name')}<input value={employee.name} onChange={(event) => patch({ name: event.target.value })} /></label>
          <label>{t('employees.jobTitle')}<input value={employee.job_title} onChange={(event) => patch({ job_title: event.target.value })} /></label>
          <label>{t('employees.avatar')}<select value={employee.avatar.kind} onChange={(event) => patch({ avatar: { kind: event.target.value as 'initials' | 'emoji', value: '' } })}><option value="initials">{t('employees.initials')}</option><option value="emoji">{t('employees.emoji')}</option></select></label>
          {employee.avatar.kind === 'emoji' ? <label>{t('employees.emoji')}<input value={employee.avatar.value} onChange={(event) => patch({ avatar: { kind: 'emoji', value: event.target.value } })} /></label> : null}
        </div>
      ) : null}

      {step === 1 ? (
        <div className="form-grid">
          <label>{t('employees.company')}<select value={employee.default_selection.company} onChange={(event) => {
            const selected = companies.find((item) => item.id === event.target.value)
            const selectedAccess = selected?.access[0]
            patch({ default_selection: { company: event.target.value, access: selectedAccess?.id ?? '', model: selectedAccess?.models[0]?.id ?? '' } })
          }}>{companies.map((item) => <option key={item.id} value={item.id}>{item.id}</option>)}</select></label>
          <label>{t('employees.access')}<select value={employee.default_selection.access} onChange={(event) => {
            const selected = company?.access.find((item) => item.id === event.target.value)
            patch({ default_selection: { ...employee.default_selection, access: event.target.value, model: selected?.models[0]?.id ?? '' } })
          }}>{company?.access.map((item) => <option key={item.id} value={item.id}>{item.id}</option>)}</select></label>
          <label>{t('employees.model')}<select value={employee.default_selection.model} onChange={(event) => patch({ default_selection: { ...employee.default_selection, model: event.target.value } })}>{access?.models.map((item) => <option key={item.id} value={item.id}>{item.id}</option>)}</select></label>
          <label>{t('employees.agent')}<select value={employee.agent_profile} onChange={(event) => patch({ agent_profile: event.target.value })}>{agents.map((item) => <option key={item.id} value={item.id}>{item.id}</option>)}</select></label>
        </div>
      ) : null}

      {step === 2 ? (
        <div className="form-grid">
          <label className="wide">{t('employees.charter')}<textarea value={employee.charter} onChange={(event) => patch({ charter: event.target.value })} /></label>
          <label>{t('employees.responsibilities')}<textarea value={employee.responsibilities.join('\n')} onChange={(event) => patch({ responsibilities: lines(event.target.value) })} /></label>
          <label>{t('employees.behaviorBoundaries')}<textarea value={employee.behavior_boundaries.join('\n')} onChange={(event) => patch({ behavior_boundaries: lines(event.target.value) })} /></label>
        </div>
      ) : null}

      {step === 3 ? (
        <>
          <p>{t('employees.skillBindingContract')}</p>
          <ul>{skills.map((skill) => {
            const key = `${skill.skill_id}:${skill.version}:${skill.digest}`
            const selected = employee.skill_bindings.some((item) =>
              item.skill_id === skill.skill_id && item.version === skill.version)
            return (
              <li key={key}>
                <label><input type="checkbox" checked={selected} onChange={(event) => patch({
                  skill_bindings: event.target.checked
                    ? [...employee.skill_bindings, { skill_id: skill.skill_id, version: skill.version, digest: skill.digest, configuration: {}, enabled: true }]
                    : employee.skill_bindings.filter((item) => item.skill_id !== skill.skill_id || item.version !== skill.version),
                })} />{skill.title} · {skill.skill_id}@{skill.version} · {skill.digest}{skill.kind === 'skill_md_adapter' ? ` · ${t('employees.adapterZeroCapability')}` : ''}</label>
                {selected && skill.kind === 'native' ? <label>{t('employees.configurationJSON')}<textarea value={skillConfiguration[key] ?? '{}'} onChange={(event) => setSkillConfiguration((current) => ({ ...current, [key]: event.target.value }))} /></label> : null}
              </li>
            )
          })}</ul>
        </>
      ) : null}

      {step === 4 ? (
        <div className="form-grid">
          <label>{t('employees.knowledgeKind')}<select value={knowledge.kind} onChange={(event) => setKnowledge({ ...knowledge, kind: event.target.value as typeof knowledge.kind })}><option value="">{t('common.none')}</option><option value="manual_text">{t('knowledgeKind.manual_text')}</option><option value="file">{t('knowledgeKind.file')}</option><option value="project_docs">{t('knowledgeKind.project_docs')}</option></select></label>
          {knowledge.kind ? <>
            <label>{t('employees.sourceId')}<input value={knowledge.id} onChange={(event) => setKnowledge({ ...knowledge, id: event.target.value })} /></label>
            <label>{t('employees.sourceTitle')}<input value={knowledge.title} onChange={(event) => setKnowledge({ ...knowledge, title: event.target.value })} /></label>
            <label className="wide">{knowledge.kind === 'manual_text' ? t('knowledgeKind.manual_text') : t('employees.relativePath')}<textarea value={knowledge.content} onChange={(event) => setKnowledge({ ...knowledge, content: event.target.value })} /></label>
          </> : null}
        </div>
      ) : null}

      {step === 5 ? (
        <div className="form-grid">
          <label className="choice-field wide"><input type="checkbox" checked={employee.memory_policy.candidate_generation} onChange={(event) => patch({ memory_policy: { ...employee.memory_policy, candidate_generation: event.target.checked, promotion: event.target.checked ? 'owner_confirmation' : 'disabled' } })} />{t('employees.memoryCandidates')}</label>
          <label>{t('employees.maxContextFacts')}<input type="number" min="0" value={employee.memory_policy.max_context_facts} onChange={(event) => patch({ memory_policy: { ...employee.memory_policy, max_context_facts: Number(event.target.value) } })} /></label>
          <label>{t('employees.maxContextBytes')}<input type="number" min="0" value={employee.memory_policy.max_context_bytes} onChange={(event) => patch({ memory_policy: { ...employee.memory_policy, max_context_bytes: Number(event.target.value) } })} /></label>
        </div>
      ) : null}

      {step === 6 ? (
        <fieldset><legend>{t('employees.steps.projects')}</legend>{projects.map((project) => <label key={project.id}><input type="radio" name="project" checked={selectedProject === project.id} onChange={() => setSelectedProject(project.id)} />{project.label} · {project.workspace_real_path}</label>)}</fieldset>
      ) : null}

      {step === 7 ? (
        <div className="form-grid">
          <label>{t('employees.capabilities')}<textarea value={employee.permission_policy.allowed_capabilities.join('\n')} onChange={(event) => patch({ permission_policy: { ...employee.permission_policy, allowed_capabilities: lines(event.target.value) } })} /></label>
          <label className="choice-field"><input type="checkbox" checked={employee.permission_policy.network_allowed} onChange={(event) => patch({ permission_policy: { ...employee.permission_policy, network_allowed: event.target.checked } })} />{t('employees.network')}</label>
          <label>{t('employees.maxCalls')}<input type="number" min="1" value={employee.budget_policy.max_model_calls} onChange={(event) => patch({ budget_policy: { ...employee.budget_policy, max_model_calls: Number(event.target.value) } })} /></label>
          <label>{t('employees.maxTokens')}<input type="number" min="1" value={employee.budget_policy.max_tokens} onChange={(event) => patch({ budget_policy: { ...employee.budget_policy, max_tokens: Number(event.target.value) } })} /></label>
          <label>{t('employees.timeoutSeconds')}<input type="number" min="1" value={employee.budget_policy.timeout_seconds} onChange={(event) => patch({ budget_policy: { ...employee.budget_policy, timeout_seconds: Number(event.target.value) } })} /></label>
          <p>{t('employees.concurrencyOne')}</p>
        </div>
      ) : null}

      {step === 8 ? (
        <>
          <dl>
            <dt>{t('employees.name')}</dt><dd>{employee.name}</dd>
            <dt>{t('employees.charter')}</dt><dd>{employee.charter}</dd>
            <dt>{t('employees.defaultModel')}</dt><dd>{employee.default_selection.company}/{employee.default_selection.access}/{employee.default_selection.model}</dd>
            <dt>{t('employees.project')}</dt><dd>{persisted?.project_bindings[0]?.label ?? selectedProject}</dd>
            <dt>{t('employees.tabs.skills')}</dt><dd>{postCreate.skills}</dd>
            <dt>{t('employees.tabs.knowledge')}</dt><dd>{postCreate.knowledge}</dd>
          </dl>
          {busy ? <p role="status">{t('common.loading')}</p> : null}
          {readiness ? <>
            <p data-testid="employee-readiness">{readiness.ready ? t('employees.ready') : t('employees.blocked')}</p>
            <ul>{readiness.checks.map((check) => <li key={check.name}>{check.name}: {check.ready ? t('employees.ready') : t('employees.blocked')} · {check.detail}</li>)}</ul>
            {!readiness.ready ? <p>{t('employees.persistedRepair')}</p> : null}
          </> : <p>{t('employees.serverReadiness')}</p>}
          {persisted && !readiness?.ready ? <button type="button" disabled={busy} onClick={() => void prepareReview()}>{t('employees.retryValidation')}</button> : null}
        </>
      ) : null}

      <div className="button-row">
        <button type="button" className="button button--secondary" onClick={onClose}>{t('actions.cancel')}</button>
        {step > 0 && !persisted ? <button type="button" className="button button--secondary" onClick={() => { setStepError(null); setStep((current) => current - 1) }}>{t('employees.previous')}</button> : null}
        {step < 8 ? <button type="button" className="button button--primary" disabled={busy || !catalogReady} onClick={() => void next()}>{t('employees.next')}</button> : <button type="button" className="button button--primary" disabled={busy || !persisted} onClick={() => persisted && onCreated(persisted)}>{readiness?.ready ? t('employees.openEmployee') : t('employees.openRepair')}</button>}
      </div>
    </section>
  )
}
