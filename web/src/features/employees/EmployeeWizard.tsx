import { useEffect, useState } from 'react'
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
  const { t } = useTranslation()
  const { actions } = useUI()
  const [step, setStep] = useState(0)
  const [employee, setEmployee] = useState(initialEmployee)
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

  const patch = (value: Partial<Employee>) =>
    setEmployee((current) => ({ ...current, ...value }))
  const lines = (value: string) =>
    value.split(/\r?\n/u).map((item) => item.trim()).filter(Boolean)

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

  function projectBinding() {
    const project = projects.find((item) => item.id === selectedProject)
    if (!project) throw new Error('project')
    const id = `project-${employee.id}`
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

  async function prepareReview() {
    if (persisted) return
    setBusy(true)
    try {
      const project = projectBinding()
      const draft = {
        ...employee,
        skill_bindings: bindings(),
        project_binding_ids: [project.id],
      }
      const record = await createEmployee({ employee: draft, project_bindings: [project] })
      setPersisted(record)
      if (knowledge.kind) {
        await addEmployeeKnowledge(record.employee.id, {
          id: knowledge.id,
          kind: knowledge.kind,
          title: knowledge.title,
          ...(knowledge.kind === 'manual_text'
            ? { manual_text: knowledge.content }
            : { relative_path: knowledge.content }),
        })
      }
      const [report, skillProjection, knowledgeProjection] = await Promise.all([
        dryRunEmployee(record.employee.id),
        getEmployeeSkills(record.employee.id),
        getEmployeeKnowledge(record.employee.id),
      ])
      setReadiness(report)
      setPostCreate({
        skills: skillProjection.bindings.length,
        knowledge: knowledgeProjection.sources.length,
      })
    } catch (error) {
      actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
    } finally {
      setBusy(false)
    }
  }

  async function next() {
    if (step === 7) {
      await prepareReview()
      setStep(8)
    } else {
      setStep((current) => Math.min(8, current + 1))
    }
  }

  const company = companies.find((item) => item.id === employee.default_selection.company)
  const access = company?.access.find((item) => item.id === employee.default_selection.access)

  return (
    <section className="projection-card" aria-label={t('employees.create')}>
      <h2>{t('employees.create')}</h2>
      <p data-testid="employee-wizard-step">
        {t('employees.step', { current: step + 1, total: STEPS.length })}
        {' · '}
        {t(`employees.steps.${STEPS[step]}`)}
      </p>

      {step === 0 ? (
        <div className="form-grid">
          <label>{t('employees.id')}<input value={employee.id} onChange={(event) => patch({ id: event.target.value })} /></label>
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
          <label><input type="checkbox" checked={employee.memory_policy.candidate_generation} onChange={(event) => patch({ memory_policy: { ...employee.memory_policy, candidate_generation: event.target.checked, promotion: event.target.checked ? 'owner_confirmation' : 'disabled' } })} />{t('employees.memoryCandidates')}</label>
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
          <label><input type="checkbox" checked={employee.permission_policy.network_allowed} onChange={(event) => patch({ permission_policy: { ...employee.permission_policy, network_allowed: event.target.checked } })} />{t('employees.network')}</label>
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
        </>
      ) : null}

      <div className="button-row">
        <button type="button" onClick={onClose}>{t('actions.cancel')}</button>
        {step > 0 && !persisted ? <button type="button" onClick={() => setStep((current) => current - 1)}>{t('employees.previous')}</button> : null}
        {step < 8 ? <button type="button" disabled={busy || !catalogReady} onClick={() => void next()}>{t('employees.next')}</button> : <button type="button" disabled={busy || !persisted || !readiness} onClick={() => persisted && onCreated(persisted)}>{readiness?.ready ? t('employees.openEmployee') : t('employees.openRepair')}</button>}
      </div>
    </section>
  )
}
