import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useParams, useSearchParams } from 'react-router-dom'

import {
  acceptEmployeeMemoryCandidate,
  addEmployeeKnowledge,
  deleteEmployeeKnowledge,
  dryRunEmployee,
  editEmployeeMemory,
  forgetEmployeeMemory,
  getEmployee,
  getEmployeeActivity,
  getEmployeeKnowledge,
  getEmployeeMemory,
  getEmployeeMemoryCandidates,
  getEmployeeSkills,
  listEmployeeTasks,
  listSkills,
  mutateEmployeeLifecycle,
  refreshEmployeeKnowledge,
  rejectEmployeeMemoryCandidate,
  updateEmployee,
  updateEmployeeSkills,
} from '../../api/endpoints'
import { ApiError } from '../../api/errors'
import type {
  EmployeeActivity,
  EmployeeDryRun,
  EmployeeKnowledge,
  EmployeeRecord,
  EmployeeTask,
  MemoryCandidate,
  MemoryFact,
  SkillBinding,
  SkillCatalogItem,
} from '../../api/types'
import { useConnectivity } from '../../components/ConnectivityProvider'
import { ErrorState } from '../../components/ErrorState'
import { PageHeader } from '../../components/PageHeader'
import { translatedEnum } from '../../i18n/enumLabel'
import { useUI } from '../../state/UIContext'

type Tab = 'overview' | 'skills' | 'knowledge' | 'memory' | 'projects' | 'tasks' | 'activity'
const TABS: Tab[] = ['overview', 'skills', 'knowledge', 'memory', 'projects', 'tasks', 'activity']

function errorKey(error: unknown) {
  if (error instanceof ApiError && error.code === 'network_error') return 'mutation.offline'
  if (error instanceof ApiError && error.status === 409) return 'mutation.conflict'
  return 'mutation.failed'
}

export function EmployeeDetailPage() {
  const { employeeId } = useParams()
  const { t } = useTranslation()
  const { actions } = useUI()
  const connectivity = useConnectivity()
  const [searchParams, setSearchParams] = useSearchParams()
  const requestedTab = searchParams.get('tab')
  const tab: Tab = TABS.includes(requestedTab as Tab) ? requestedTab as Tab : 'overview'
  const requestEpoch = useRef(0)
  const [record, setRecord] = useState<EmployeeRecord | null>(null)
  const [notFound, setNotFound] = useState(false)
  const [busy, setBusy] = useState(false)
  const [skills, setSkills] = useState<Awaited<ReturnType<typeof getEmployeeSkills>> | null>(null)
  const [catalog, setCatalog] = useState<SkillCatalogItem[]>([])
  const [skillDraft, setSkillDraft] = useState<SkillBinding[]>([])
  const [skillConfiguration, setSkillConfiguration] = useState<Record<string, string>>({})
  const [knowledge, setKnowledge] = useState<EmployeeKnowledge | null>(null)
  const [memory, setMemory] = useState<{ facts: MemoryFact[]; candidates: MemoryCandidate[] } | null>(null)
  const [tasks, setTasks] = useState<EmployeeTask[] | null>(null)
  const [activity, setActivity] = useState<EmployeeActivity | null>(null)
  const [dryRun, setDryRun] = useState<EmployeeDryRun | null>(null)
  const [sourceDraft, setSourceDraft] = useState({
    id: '', kind: 'manual_text' as 'manual_text' | 'file' | 'project_docs', title: '', content: '',
  })
  const [factDraft, setFactDraft] = useState<Record<string, string>>({})

  const refresh = useCallback(async (signal?: AbortSignal) => {
    if (!employeeId) return
    const epoch = ++requestEpoch.current
    setRecord(null)
    setNotFound(false)
    try {
      const value = await getEmployee(employeeId, signal ? { signal } : {})
      if (epoch !== requestEpoch.current) return
      setRecord(value)
      setSkillDraft(value.employee.skill_bindings)
      setSkillConfiguration(Object.fromEntries(value.employee.skill_bindings.map((binding) => [
        `${binding.skill_id}:${binding.version}`,
        JSON.stringify(binding.configuration, null, 2),
      ])))
    } catch (error) {
      if (epoch !== requestEpoch.current) return
      setNotFound(error instanceof ApiError && error.status === 404)
    }
  }, [employeeId])

  useEffect(() => {
    const controller = new AbortController()
    setSkills(null)
    setKnowledge(null)
    setMemory(null)
    setTasks(null)
    setActivity(null)
    setDryRun(null)
    void refresh(controller.signal)
    return () => {
      requestEpoch.current += 1
      controller.abort()
    }
  }, [refresh, connectivity.generation])

  useEffect(() => {
    if (!employeeId || !record || tab === 'overview' || tab === 'projects') return
    const controller = new AbortController()
    const epoch = ++requestEpoch.current
    const apply = <T,>(setter: (value: T) => void) => (value: T) => {
      if (epoch === requestEpoch.current) setter(value)
    }
    if (tab === 'skills') {
      void Promise.all([
        getEmployeeSkills(employeeId, { signal: controller.signal }),
        listSkills({ signal: controller.signal }),
      ]).then(([projection, skillCatalog]) => {
        if (epoch !== requestEpoch.current) return
        setSkills(projection)
        setCatalog(skillCatalog.skills)
      }).catch(() => undefined)
    } else if (tab === 'knowledge') {
      void getEmployeeKnowledge(employeeId, { signal: controller.signal }).then(apply(setKnowledge)).catch(() => undefined)
    } else if (tab === 'memory') {
      void Promise.all([
        getEmployeeMemory(employeeId, { signal: controller.signal }),
        getEmployeeMemoryCandidates(employeeId, { signal: controller.signal }),
      ]).then(([facts, candidates]) => apply(setMemory)({
        facts: facts.facts, candidates: candidates.candidates,
      })).catch(() => undefined)
    } else if (tab === 'tasks') {
      void listEmployeeTasks(employeeId, { limit: 100 }, { signal: controller.signal })
        .then((value) => apply(setTasks)(value.tasks)).catch(() => undefined)
    } else if (tab === 'activity') {
      void getEmployeeActivity(employeeId, { limit: 100 }, { signal: controller.signal })
        .then(apply(setActivity)).catch(() => undefined)
    }
    return () => controller.abort()
  }, [employeeId, record, tab])

  async function mutate(action: () => Promise<unknown>, refreshAfter = true) {
    if (!connectivity.canMutate) return
    setBusy(true)
    try {
      await action()
      actions.showToast({ messageKey: 'toast.saved', tone: 'success' })
      if (refreshAfter) await refresh()
    } catch (error) {
      actions.showToast({ messageKey: errorKey(error), tone: 'error' })
      if (error instanceof ApiError && error.status === 409) await refresh()
    } finally {
      setBusy(false)
    }
  }

  if (notFound) return <ErrorState title={t('employees.notFound')} description={t('employees.notFoundDescription')} />
  if (!record) return <p role="status">{t('common.loading')}</p>

  const employee = record.employee
  const archived = employee.state === 'archived'
  const active = employee.state === 'active'
  const canMutate = connectivity.canMutate && !archived && !busy

  function saveEmployee(nextRecord: EmployeeRecord | null = record) {
    if (!nextRecord) return Promise.resolve()
    return mutate(async () => {
      const updated = await updateEmployee(employee.id, {
        expected_revision: employee.revision,
        employee: nextRecord.employee,
        project_bindings: nextRecord.project_bindings,
      })
      setRecord(updated)
    })
  }

  function lifecycle(action: 'disable' | 'enable' | 'archive') {
    actions.openDialog({
      titleKey: `employees.${action}Title`,
      descriptionKey: `employees.${action}Description`,
      confirmKey: `employees.${action}`,
      tone: action === 'archive' ? 'warning' : 'info',
      onConfirm: () => void mutate(async () => {
        setRecord(await mutateEmployeeLifecycle(employee.id, action, employee.revision))
      }, false),
    })
  }

  return (
    <article className="feature-page">
      <PageHeader title={employee.name} description={`${employee.id} · r${employee.revision}`} />
      <p data-testid="employee-status">{translatedEnum(t, 'employeeStatus', employee.state)}</p>
      {archived ? <p className="stale-notice">{t('employees.archivedReadOnly')}</p> : null}
      <nav className="tab-list" aria-label={t('employees.sections')}>
        {TABS.map((value) => <button key={value} type="button" aria-current={tab === value ? 'page' : undefined} onClick={() => setSearchParams(value === 'overview' ? {} : { tab: value })}>{t(`employees.tabs.${value}`)}</button>)}
      </nav>

      {tab === 'overview' ? (
        <section className="projection-card">
          <div className="form-grid">
            <label>{t('employees.name')}<input disabled={archived} value={employee.name} onChange={(event) => setRecord({ ...record, employee: { ...employee, name: event.target.value } })} /></label>
            <label>{t('employees.jobTitle')}<input disabled={archived} value={employee.job_title} onChange={(event) => setRecord({ ...record, employee: { ...employee, job_title: event.target.value } })} /></label>
            <label className="wide">{t('employees.charter')}<textarea disabled={archived} value={employee.charter} onChange={(event) => setRecord({ ...record, employee: { ...employee, charter: event.target.value } })} /></label>
          </div>
          <dl>
            <dt>{t('employees.responsibilities')}</dt><dd>{employee.responsibilities.join(' · ') || '—'}</dd>
            <dt>{t('employees.behaviorBoundaries')}</dt><dd>{employee.behavior_boundaries.join(' · ') || '—'}</dd>
            <dt>{t('employees.defaultModel')}</dt><dd>{employee.default_selection.company}/{employee.default_selection.access}/{employee.default_selection.model}</dd>
            <dt>{t('employees.agent')}</dt><dd>{employee.agent_profile}</dd>
            <dt>{t('employees.capabilities')}</dt><dd>{employee.permission_policy.allowed_capabilities.join(', ')}</dd>
            <dt>{t('employees.network')}</dt><dd>{employee.permission_policy.network_allowed ? t('common.yes') : t('common.no')}</dd>
            <dt>{t('employees.budget')}</dt><dd>{t('employees.budgetSummary', { calls: employee.budget_policy.max_model_calls, tokens: employee.budget_policy.max_tokens, seconds: employee.budget_policy.timeout_seconds })}</dd>
            <dt>{t('employees.concurrency')}</dt><dd>{employee.concurrency_policy.max_running_tasks}</dd>
            <dt>{t('employees.memory')}</dt><dd>{employee.memory_policy.promotion} · {employee.memory_policy.max_context_facts} facts / {employee.memory_policy.max_context_bytes} bytes</dd>
          </dl>
          {!archived ? <div className="button-row">
            <button type="button" disabled={!canMutate} onClick={() => void saveEmployee()}>{t('employees.save')}</button>
            {active ? <button type="button" disabled={!canMutate} onClick={() => lifecycle('disable')}>{t('employees.disable')}</button> : <button type="button" disabled={!canMutate} onClick={() => lifecycle('enable')}>{t('employees.enable')}</button>}
            <button type="button" disabled={!canMutate} onClick={() => lifecycle('archive')}>{t('employees.archive')}</button>
            <button type="button" disabled={!connectivity.canMutate || busy} onClick={() => void dryRunEmployee(employee.id).then(setDryRun)}>{t('employees.dryRun')}</button>
          </div> : null}
          {dryRun ? <section><h3>{t('employees.readiness')}</h3><p>{dryRun.ready ? t('employees.ready') : t('employees.blocked')}</p><ul>{dryRun.checks.map((check) => <li key={check.name}>{check.name}: {check.detail}</li>)}</ul></section> : null}
        </section>
      ) : null}

      {tab === 'skills' ? (
        <section className="projection-card">
          <h2>{t('employees.tabs.skills')}</h2>
          {skills ? <ul>{skills.bindings.map((item) => <li key={`${item.binding.skill_id}:${item.binding.version}`}><strong>{item.binding.skill_id}@{item.binding.version}</strong> · {item.binding.digest} · {translatedEnum(t, 'skillStatus', item.status)} · {translatedEnum(t, 'bindingStatus', item.binding.enabled ? 'enabled' : 'disabled')}{item.kind === 'skill_md_adapter' ? ` · ${t('employees.adapterZeroCapability')}` : ''}</li>)}</ul> : <p role="status">{t('common.loading')}</p>}
          {active ? <>
            <fieldset><legend>{t('employees.catalogBindings')}</legend>{catalog.map((item) => {
              const key = `${item.skill_id}:${item.version}`
              const binding = skillDraft.find((candidate) =>
                candidate.skill_id === item.skill_id && candidate.version === item.version)
              return <section key={key}><label><input type="checkbox" checked={Boolean(binding)} onChange={(event) => {
                setSkillDraft((current) => event.target.checked
                  ? [...current, { skill_id: item.skill_id, version: item.version, digest: item.digest, configuration: {}, enabled: true }]
                  : current.filter((candidate) => candidate.skill_id !== item.skill_id || candidate.version !== item.version))
                if (event.target.checked) {
                  setSkillConfiguration((current) => ({ ...current, [key]: '{}' }))
                }
              }} />{item.title} · {item.digest}</label>
              {binding ? <label><input type="checkbox" checked={binding.enabled} onChange={(event) => setSkillDraft((current) => current.map((candidate) =>
                candidate.skill_id === item.skill_id && candidate.version === item.version
                  ? { ...candidate, enabled: event.target.checked }
                  : candidate))} />{t('bindingStatus.enabled')} {item.title}</label> : null}
              {binding && item.kind === 'native' ? <label>{t('employees.configurationJSON')} {item.title}<textarea value={skillConfiguration[key] ?? '{}'} onChange={(event) => setSkillConfiguration((current) => ({ ...current, [key]: event.target.value }))} /></label> : null}
              {binding && item.kind === 'skill_md_adapter' ? <p>{t('employees.adapterZeroCapability')}</p> : null}
              </section>
            })}</fieldset>
            <button type="button" disabled={!canMutate} onClick={() => void mutate(async () => {
              const normalized = skillDraft.map((binding) => {
                const item = catalog.find((candidate) =>
                  candidate.skill_id === binding.skill_id && candidate.version === binding.version)
                if (item?.kind === 'skill_md_adapter') return { ...binding, configuration: {} }
                const parsed = JSON.parse(skillConfiguration[`${binding.skill_id}:${binding.version}`] ?? '{}') as unknown
                if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') throw new Error('configuration')
                return { ...binding, configuration: parsed as Record<string, unknown> }
              })
              const updated = await updateEmployeeSkills(employee.id, employee.revision, normalized)
              setRecord(updated)
              setSkills(await getEmployeeSkills(employee.id))
            }, false)}>{t('employees.saveSkills')}</button>
          </> : <p>{t('employees.skillsActiveOnly')}</p>}
        </section>
      ) : null}

      {tab === 'knowledge' ? (
        <section className="projection-card">
          <h2>{t('employees.tabs.knowledge')}</h2>
          {knowledge ? knowledge.sources.map((source) => {
            const index = knowledge.indexes.find((item) => item.source_id === source.id)
            return <section key={source.id} data-testid="knowledge-source"><h3>{source.title}</h3><p>{source.id} · {translatedEnum(t, 'knowledgeKind', source.kind)} · {translatedEnum(t, 'knowledgeStatus', source.status)} · {source.digest}</p><ul>{index?.documents.flatMap((document) => document.citations).map((citation) => <li key={citation.id} data-testid="knowledge-citation">{citation.path}:{citation.start_line}-{citation.end_line} · {citation.digest}</li>)}</ul>{!archived ? <div className="button-row"><button type="button" disabled={!canMutate} onClick={() => void mutate(async () => setKnowledge(await refreshEmployeeKnowledge(employee.id, source.id)), false)}>{t('employees.refresh')}</button><button type="button" disabled={!canMutate} onClick={() => void mutate(async () => { await deleteEmployeeKnowledge(employee.id, source.id); setKnowledge(await getEmployeeKnowledge(employee.id)) }, false)}>{t('employees.delete')}</button></div> : null}</section>
          }) : <p role="status">{t('common.loading')}</p>}
          {!archived ? <div className="form-grid"><label>{t('employees.knowledgeKind')}<select value={sourceDraft.kind} onChange={(event) => setSourceDraft({ ...sourceDraft, kind: event.target.value as typeof sourceDraft.kind })}><option value="manual_text">{t('knowledgeKind.manual_text')}</option><option value="file">{t('knowledgeKind.file')}</option><option value="project_docs">{t('knowledgeKind.project_docs')}</option></select></label><label>{t('employees.sourceId')}<input value={sourceDraft.id} onChange={(event) => setSourceDraft({ ...sourceDraft, id: event.target.value })} /></label><label>{t('employees.sourceTitle')}<input value={sourceDraft.title} onChange={(event) => setSourceDraft({ ...sourceDraft, title: event.target.value })} /></label><label className="wide">{sourceDraft.kind === 'manual_text' ? t('knowledgeKind.manual_text') : t('employees.relativePath')}<textarea value={sourceDraft.content} onChange={(event) => setSourceDraft({ ...sourceDraft, content: event.target.value })} /></label><button type="button" disabled={!canMutate} onClick={() => void mutate(async () => setKnowledge(await addEmployeeKnowledge(employee.id, { id: sourceDraft.id, kind: sourceDraft.kind, title: sourceDraft.title, ...(sourceDraft.kind === 'manual_text' ? { manual_text: sourceDraft.content } : { relative_path: sourceDraft.content }) })), false)}>{t('employees.addKnowledge')}</button></div> : null}
        </section>
      ) : null}

      {tab === 'memory' ? (
        <section className="projection-card">
          <h2>{t('employees.pendingCandidates')}</h2>
          {memory?.candidates.map((candidate) => <article key={candidate.id} data-testid="memory-candidate"><strong>{candidate.category}</strong><p>{candidate.value}</p><small>{candidate.provenance.map((item) => `${item.source_type}:${item.source_id}`).join(' · ')}</small>{!archived ? <div className="button-row"><button type="button" disabled={!canMutate} onClick={() => void mutate(async () => { await acceptEmployeeMemoryCandidate(employee.id, candidate.id); const value = await getEmployeeMemory(employee.id); const pending = await getEmployeeMemoryCandidates(employee.id); setMemory({ facts: value.facts, candidates: pending.candidates }) }, false)}>{t('employees.accept')}</button><button type="button" disabled={!canMutate} onClick={() => void mutate(async () => { await rejectEmployeeMemoryCandidate(employee.id, candidate.id); setMemory({ facts: memory.facts, candidates: (await getEmployeeMemoryCandidates(employee.id)).candidates }) }, false)}>{t('employees.reject')}</button></div> : null}</article>)}
          <h2>{t('employees.acceptedMemory')}</h2>
          {memory?.facts.map((fact) => <article key={fact.id}><strong>{fact.category}</strong><input disabled={archived} value={factDraft[fact.id] ?? fact.value} onChange={(event) => setFactDraft((current) => ({ ...current, [fact.id]: event.target.value }))} /><small>{fact.digest}</small>{!archived ? <div className="button-row"><button type="button" disabled={!canMutate} onClick={() => void mutate(async () => { await editEmployeeMemory(employee.id, fact.id, factDraft[fact.id] ?? fact.value); setMemory({ ...memory, facts: (await getEmployeeMemory(employee.id)).facts }) }, false)}>{t('employees.edit')}</button><button type="button" disabled={!canMutate} onClick={() => void mutate(async () => { await forgetEmployeeMemory(employee.id, fact.id); setMemory({ ...memory, facts: (await getEmployeeMemory(employee.id)).facts }) }, false)}>{t('employees.forget')}</button></div> : null}</article>)}
        </section>
      ) : null}

      {tab === 'projects' ? (
        <section className="projection-card">
          {record.project_bindings.map((binding, index) => <fieldset key={binding.id}><legend>{binding.label}</legend><p>{binding.workspace_fingerprint}</p><label><input type="checkbox" disabled={archived} checked={binding.read_allowed} onChange={(event) => setRecord({ ...record, project_bindings: record.project_bindings.map((item, itemIndex) => itemIndex === index ? { ...item, read_allowed: event.target.checked } : item) })} />{t('employees.readAllowed')}</label><label><input type="checkbox" disabled={archived} checked={binding.mutation_allowed} onChange={(event) => setRecord({ ...record, project_bindings: record.project_bindings.map((item, itemIndex) => itemIndex === index ? { ...item, mutation_allowed: event.target.checked } : item) })} />{t('employees.mutationAllowed')}</label><label><input type="checkbox" disabled={archived} checked={binding.network_allowed} onChange={(event) => setRecord({ ...record, project_bindings: record.project_bindings.map((item, itemIndex) => itemIndex === index ? { ...item, network_allowed: event.target.checked } : item) })} />{t('employees.networkAllowed')}</label><p>{t('employees.capabilities')}: {binding.allowed_tool_capabilities.join(', ')}</p><p>{t('employees.budgetOverride')}: {binding.budget_override ? `${binding.budget_override.max_model_calls}/${binding.budget_override.max_tokens}/${binding.budget_override.timeout_seconds}s` : t('employees.employeeDefault')}</p></fieldset>)}
          {!archived ? <button type="button" disabled={!canMutate} onClick={() => void saveEmployee()}>{t('employees.save')}</button> : null}
        </section>
      ) : null}

      {tab === 'tasks' ? <section className="projection-card"><p>{t('tasks.listBoundary')}</p><ul>{tasks?.map((task) => <li key={task.id}><Link to={`/tasks/${encodeURIComponent(task.id)}`}>{task.prompt}</Link> · {translatedEnum(t, 'employeeTaskStatus', task.state)} · {task.session_id || 'not started'}</li>)}</ul></section> : null}
      {tab === 'activity' ? <section className="projection-card"><ul>{activity?.events.map((event) => <li key={event.id}><time>{event.time}</time> · {translatedEnum(t, 'employeeActivityType', event.type)} · {event.task_id || event.subject_id || `r${event.employee_revision ?? '—'}`}{event.session_id ? ` · ${event.session_id}/${event.run_id ?? ''}` : ''}</li>)}</ul>{activity?.next_cursor ? <button type="button" onClick={() => {
        const cursor = activity.next_cursor
        if (!cursor) return
        void getEmployeeActivity(employee.id, { limit: 100, cursor }).then((page) => setActivity({ events: [...activity.events, ...page.events], ...(page.next_cursor ? { next_cursor: page.next_cursor } : {}) }))
      }}>{t('employees.loadMore')}</button> : null}</section> : null}
    </article>
  )
}
