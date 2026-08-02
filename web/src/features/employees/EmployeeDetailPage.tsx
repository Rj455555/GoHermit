import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import {
  Bot,
  Check,
  CircleCheck,
  Database,
  FileText,
  FolderKanban,
  FolderOpen,
  History,
  ListChecks,
  Search,
  ShieldCheck,
  Settings2,
  Workflow,
} from 'lucide-react'

import {
  acceptEmployeeMemoryCandidate,
  addEmployeeKnowledge,
  deleteEmployeeKnowledge,
  dryRunEmployee,
  editEmployeeMemory,
  forgetEmployeeMemory,
  getEmployee,
  getEmployeeActivity,
  getInfo,
  getEmployeeKnowledge,
  getEmployeeMemory,
  getEmployeeMemoryCandidates,
  getEmployeeSkills,
  listEmployeeTasks,
  listLoops,
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
  Info,
  MemoryCandidate,
  MemoryFact,
  LoopDefinition,
  SkillBinding,
  SkillCatalogItem,
} from '../../api/types'
import { useConnectivity } from '../../components/ConnectivityProvider'
import { ErrorState } from '../../components/ErrorState'
import { translatedEnum } from '../../i18n/enumLabel'
import { useUI } from '../../state/UIContext'

type Tab = 'overview' | 'skills' | 'knowledge' | 'memory' | 'projects' | 'loops' | 'tasks' | 'activity'
const TABS: Tab[] = ['overview', 'skills', 'knowledge', 'memory', 'projects', 'loops', 'tasks', 'activity']

function errorKey(error: unknown) {
  if (error instanceof ApiError && error.code === 'network_error') return 'mutation.offline'
  if (error instanceof ApiError && error.status === 409) return 'mutation.conflict'
  return 'mutation.failed'
}

export function EmployeeDetailPage() {
  const { employeeId } = useParams()
  const { t, i18n } = useTranslation()
  const { actions } = useUI()
  const connectivity = useConnectivity()
  const [searchParams, setSearchParams] = useSearchParams()
  const requestedTab = searchParams.get('tab')
  const tab: Tab = TABS.includes(requestedTab as Tab) ? requestedTab as Tab : 'overview'
  const requestEpoch = useRef(0)
  const definitionRef = useRef<HTMLDetailsElement>(null)
  const tabsRef = useRef<HTMLElement>(null)
  const [record, setRecord] = useState<EmployeeRecord | null>(null)
  const [info, setInfo] = useState<Info | null>(null)
  const [notFound, setNotFound] = useState(false)
  const [busy, setBusy] = useState(false)
  const [skills, setSkills] = useState<Awaited<ReturnType<typeof getEmployeeSkills>> | null>(null)
  const [catalog, setCatalog] = useState<SkillCatalogItem[]>([])
  const [skillDraft, setSkillDraft] = useState<SkillBinding[]>([])
  const [skillConfiguration, setSkillConfiguration] = useState<Record<string, string>>({})
  const [skillQuery, setSkillQuery] = useState('')
  const [selectedSkillKey, setSelectedSkillKey] = useState<string | null>(null)
  const [knowledge, setKnowledge] = useState<EmployeeKnowledge | null>(null)
  const [memory, setMemory] = useState<{ facts: MemoryFact[]; candidates: MemoryCandidate[] } | null>(null)
  const [loops, setLoops] = useState<LoopDefinition[] | null>(null)
  const [tasks, setTasks] = useState<EmployeeTask[] | null>(null)
  const [activity, setActivity] = useState<EmployeeActivity | null>(null)
  const [dryRun, setDryRun] = useState<EmployeeDryRun | null>(null)
  const [knowledgeComposerOpen, setKnowledgeComposerOpen] = useState(false)
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
      const [value, catalogInfo] = await Promise.all([
        getEmployee(employeeId, signal ? { signal } : {}),
        getInfo(signal ? { signal } : {}),
      ])
      if (epoch !== requestEpoch.current) return
      setRecord(value)
      setInfo(catalogInfo)
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
    setLoops(null)
    setTasks(null)
    setActivity(null)
    setDryRun(null)
    setKnowledgeComposerOpen(false)
    setSkillQuery('')
    setSelectedSkillKey(null)
    setBusy(false)
    void refresh(controller.signal)
    return () => {
      requestEpoch.current += 1
      controller.abort()
    }
  }, [refresh, connectivity.generation])

  useEffect(() => {
    const activeTab = tabsRef.current?.querySelector<HTMLElement>('[aria-current="page"]')
    if (activeTab && typeof activeTab.scrollIntoView === 'function') {
      activeTab.scrollIntoView({ block: 'nearest', inline: 'center' })
    }
  }, [tab, record?.employee.id])

  useEffect(() => {
    if (
      !employeeId ||
      !record ||
      record.employee.id !== employeeId ||
      tab === 'overview' ||
      tab === 'projects'
    ) return
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
        setSelectedSkillKey((current) => {
          if (current) return current
          const firstBinding = projection.bindings[0]?.binding
          if (firstBinding) return `${firstBinding.skill_id}:${firstBinding.version}`
          const firstCatalogItem = skillCatalog.skills[0]
          return firstCatalogItem ? `${firstCatalogItem.skill_id}:${firstCatalogItem.version}` : null
        })
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
    } else if (tab === 'loops') {
      void listLoops({ signal: controller.signal })
        .then((value) => apply(setLoops)(value.loops.filter((item) => item.employee_id === employeeId)))
        .catch(() => undefined)
    } else if (tab === 'tasks') {
      void listEmployeeTasks(employeeId, { limit: 100 }, { signal: controller.signal })
        .then((value) => apply(setTasks)(value.tasks)).catch(() => undefined)
    } else if (tab === 'activity') {
      void getEmployeeActivity(employeeId, { limit: 100 }, { signal: controller.signal })
        .then(apply(setActivity)).catch(() => undefined)
    }
    return () => controller.abort()
  }, [employeeId, record, tab])

  async function mutate<T>(
    action: (signal: AbortSignal) => Promise<T>,
    onSuccess?: (value: T) => void,
    refreshAfter = true,
  ) {
    if (!connectivity.canMutate) return
    const epoch = requestEpoch.current
    const owner = employeeId
    const controller = new AbortController()
    setBusy(true)
    try {
      const value = await action(controller.signal)
      if (epoch !== requestEpoch.current || owner !== employeeId) return
      onSuccess?.(value)
      actions.showToast({ messageKey: 'toast.saved', tone: 'success' })
      if (refreshAfter) await refresh()
    } catch (error) {
      if (epoch !== requestEpoch.current || owner !== employeeId) return
      actions.showToast({ messageKey: errorKey(error), tone: 'error' })
      if (error instanceof ApiError && error.status === 409) await refresh()
    } finally {
      if (owner === employeeId) setBusy(false)
    }
  }

  if (notFound) return <ErrorState title={t('employees.notFound')} description={t('employees.notFoundDescription')} />
  if (!record) return <p role="status">{t('common.loading')}</p>

  const employee = record.employee
  const archived = employee.state === 'archived'
  const active = employee.state === 'active'
  const canMutate = connectivity.canMutate && !archived && !busy
  const companies = info?.available_companies ?? []
  const company = companies.find((item) => item.id === employee.default_selection.company)
  const access = company?.access.find((item) => item.id === employee.default_selection.access)
  const model = access?.models.find((item) => item.id === employee.default_selection.model)
  const avatarLabel = employee.avatar.value.trim() || employee.name.trim().slice(0, 2).toUpperCase()
  const statusTone = archived ? 'muted' : active ? 'success' : 'warning'
  const readinessTone = dryRun ? (dryRun.ready ? 'success' : 'warning') : 'muted'
  const zh = i18n.resolvedLanguage?.startsWith('zh') ?? true
  const overviewCopy = zh ? {
    title: '员工工作台',
    description: '先回答“这个员工能否安全开始下一项工作”，再呈现完整定义。',
    edit: '编辑定义',
    createTask: '创建任务',
    revision: '定义版本',
    concurrency: '并发上限',
    projects: '项目绑定',
    charterHint: '不可被单次 Prompt 覆盖的长期行为边界',
    recent: '最近工作',
    recentHint: 'Task 仍是执行真相入口',
    beforeWork: '开始工作前',
    beforeWorkHint: '基于当前 DTO 可确认的就绪检查',
    projectBoundary: '项目边界',
    skillBinding: 'Skill 绑定',
    modelAuth: '模型认证',
    noRecent: '暂无最近任务。',
  } : {
    title: 'Employee workspace',
    description: 'First confirm this Employee can safely start the next task, then inspect the complete definition.',
    edit: 'Edit definition',
    createTask: 'Create task',
    revision: 'Definition revision',
    concurrency: 'Concurrency',
    projects: 'Project bindings',
    charterHint: 'Long-lived behavior boundaries that a single Prompt cannot override',
    recent: 'Recent work',
    recentHint: 'Task remains the execution source of truth',
    beforeWork: 'Before starting work',
    beforeWorkHint: 'Readiness checks supported by the current DTOs',
    projectBoundary: 'Project boundary',
    skillBinding: 'Skill bindings',
    modelAuth: 'Model authentication',
    noRecent: 'No recent task.',
  }
  const tabCopy = zh ? {
    knowledge: { kicker: 'EMPLOYEE / KNOWLEDGE', title: '知识', description: '来源、索引、文档与 Citation 保持可追溯层级，失败不会被空状态吞掉。', add: '添加来源', inspector: 'Citation 检查器', inspectorHint: '选择树节点后显示不可变证据', citationNote: '引用只展示服务端返回的行号与 Digest，不从浏览器重新推断。' },
    memory: { kicker: 'EMPLOYEE / MEMORY', title: '记忆', description: '候选与已确认事实分层：只有 Owner 明确接受后，候选才能成为可注入上下文的 Fact。', candidatesHint: '附带 Task / Session / Run 来源链', factsHint: '仅这些内容可进入后续快照', boundary: '记忆边界', candidateGeneration: '候选生成', enabled: '启用', contextLimit: '上下文上限', byteLimit: '字节上限', warning: '不要把模型输出直接写成长期记忆；必须保留验证来源。' },
    projects: { kicker: 'EMPLOYEE / PROJECTS', title: '项目与权限', description: '用真实路径指纹与能力清单界定执行边界；高风险能力不使用含糊的“完全访问”开关。', add: '添加项目', bound: '已绑定', realPath: '真实路径', allowed: '允许', blocked: '禁止', allowedCapabilities: '允许能力', rootOnly: '限定在已验证 Workspace Root 内', pathNote: '路径指纹由服务端生成；界面不允许手动编辑，也不显示未验证的符号链接解析结果。' },
    loops: { kicker: 'EMPLOYEE / LOOPS', title: '员工 Loops', description: '只显示归属于该员工的持久工作契约，执行详情仍回到全局 Loop 与 Task 真相。', create: '创建 Loop', schedule: 'Schedule', last: 'Last', next: 'Next', open: '打开 Loop', nextTitle: '创建下一个 Loop', nextHint: '从 Goal、Boundaries、SOP、Definition of Done 与 Stop Conditions 建立可审计契约。', invocationNote: '每次 Invocation 都创建普通 EmployeeTask；Loop 不引入第二套执行系统。', goTo: '前往 Loops' },
    tasks: { kicker: 'EMPLOYEE / TASKS', title: '员工任务', description: '以该员工为作用域查看任务快照；状态、Session 与 Run 仍与全局任务详情一致。', create: '新建任务', task: '任务', employee: '员工', status: '状态', sessionRun: 'Session / Run', updated: '更新时间', snapshot: 'snapshot' },
    activity: { kicker: 'EMPLOYEE / ACTIVITY', title: '员工活动', description: '按时间展示可审计事件，并保留 Employee revision、Task、Session 与 Run 的交叉链接。', open: '打开' },
  } : {
    knowledge: { kicker: 'EMPLOYEE / KNOWLEDGE', title: 'Knowledge', description: 'Keep sources, indexes, documents, and Citations in a traceable hierarchy; failures never disappear into an empty state.', add: 'Add source', inspector: 'Citation inspector', inspectorHint: 'Select a tree node to inspect immutable evidence', citationNote: 'Citations display only server-returned lines and digests; the browser does not infer them.' },
    memory: { kicker: 'EMPLOYEE / MEMORY', title: 'Memory', description: 'Candidates and confirmed facts stay separate. Only explicit Owner acceptance promotes a candidate into injectable context.', candidatesHint: 'Includes Task / Session / Run provenance', factsHint: 'Only these facts may enter later snapshots', boundary: 'Memory boundary', candidateGeneration: 'Candidate generation', enabled: 'Enabled', contextLimit: 'Context limit', byteLimit: 'Byte limit', warning: 'Never write model output directly as long-term memory; verified provenance is required.' },
    projects: { kicker: 'EMPLOYEE / PROJECTS', title: 'Projects & permissions', description: 'Use verified workspace identity and explicit capabilities to bound execution; high-risk access is never a vague full-access switch.', add: 'Add project', bound: 'Bound', realPath: 'Real path', allowed: 'Allowed', blocked: 'Blocked', allowedCapabilities: 'Allowed capabilities', rootOnly: 'Limited to the verified Workspace Root', pathNote: 'Workspace fingerprints are generated by the service; the UI cannot edit them or infer unverified symlink resolution.' },
    loops: { kicker: 'EMPLOYEE / LOOPS', title: 'Employee Loops', description: 'Show only persistent work contracts owned by this Employee; execution truth remains in global Loops and Tasks.', create: 'Create Loop', schedule: 'Schedule', last: 'Last', next: 'Next', open: 'Open Loop', nextTitle: 'Create the next Loop', nextHint: 'Build an auditable contract from Goal, Boundaries, SOP, Definition of Done, and Stop Conditions.', invocationNote: 'Every Invocation creates an ordinary EmployeeTask; Loops do not add a second execution system.', goTo: 'Go to Loops' },
    tasks: { kicker: 'EMPLOYEE / TASKS', title: 'Employee tasks', description: 'Review task snapshots in this Employee scope; state, Session, and Run remain consistent with global task details.', create: 'New task', task: 'Task', employee: 'Employee', status: 'Status', sessionRun: 'Session / Run', updated: 'Updated', snapshot: 'snapshot' },
    activity: { kicker: 'EMPLOYEE / ACTIVITY', title: 'Employee activity', description: 'Show auditable events over time while preserving Employee revision, Task, Session, and Run cross-links.', open: 'Open' },
  }
  const lines = (value: string) =>
    value.split(/\r?\n/u).map((item) => item.trim()).filter(Boolean)
  const patchEmployee = (value: Partial<typeof employee>) =>
    setRecord({ ...record, employee: { ...employee, ...value } })
  const skillKey = (item: Pick<SkillCatalogItem, 'skill_id' | 'version'> | SkillBinding) =>
    `${item.skill_id}:${item.version}`
  const filteredCatalog = catalog.filter((item) => {
    const query = skillQuery.trim().toLocaleLowerCase()
    if (!query) return true
    return `${item.title} ${item.skill_id} ${item.description} ${item.version}`
      .toLocaleLowerCase().includes(query)
  })
  const fallbackSkill = catalog.find((item) => skillDraft.some((binding) => skillKey(binding) === skillKey(item))) ?? catalog[0]
  const activeSkillKey = selectedSkillKey ?? (fallbackSkill ? skillKey(fallbackSkill) : null)
  const selectedSkill = activeSkillKey
    ? catalog.find((item) => skillKey(item) === activeSkillKey) ?? null
    : null
  const selectedBinding = activeSkillKey
    ? skillDraft.find((item) => skillKey(item) === activeSkillKey) ?? null
    : null
  const selectedSkillStatus = activeSkillKey
    ? skills?.bindings.find((item) => skillKey(item.binding) === activeSkillKey) ?? null
    : null
  const firstKnowledgeIndex = knowledge?.indexes.find((item) =>
    item.documents.some((document) => document.citations.length > 0),
  )
  const firstKnowledgeCitation = firstKnowledgeIndex?.documents
    .flatMap((document) => document.citations)[0]
  const firstKnowledgeSource = knowledge?.sources.find((source) =>
    source.id === firstKnowledgeIndex?.source_id,
  )

  function saveEmployee(nextRecord: EmployeeRecord | null = record) {
    if (!nextRecord) return Promise.resolve()
    return mutate((signal) => updateEmployee(employee.id, {
        expected_revision: employee.revision,
        employee: nextRecord.employee,
        project_bindings: nextRecord.project_bindings,
      }, { signal }), setRecord)
  }

  function lifecycle(action: 'disable' | 'enable' | 'archive') {
    actions.openDialog({
      titleKey: `employees.${action}Title`,
      descriptionKey: `employees.${action}Description`,
      confirmKey: `employees.${action}`,
      tone: action === 'archive' ? 'warning' : 'info',
      onConfirm: () => void mutate(
        (signal) => mutateEmployeeLifecycle(employee.id, action, employee.revision, { signal }),
        setRecord,
        false,
      ),
    })
  }

  function runDry() {
    void mutate(
      (signal) => dryRunEmployee(employee.id, { signal }),
      setDryRun,
      false,
    )
  }

  function confirmKnowledgeDelete(sourceId: string) {
    actions.openDialog({
      titleKey: 'employees.deleteKnowledgeTitle',
      descriptionKey: 'employees.deleteKnowledgeDescription',
      confirmKey: 'employees.delete',
      tone: 'warning',
      onConfirm: () => void mutate(async (signal) => {
        await deleteEmployeeKnowledge(employee.id, sourceId, { signal })
        return getEmployeeKnowledge(employee.id, { signal })
      }, setKnowledge, false),
    })
  }

  function confirmMemoryReject(candidateId: string) {
    actions.openDialog({
      titleKey: 'employees.rejectMemoryTitle',
      descriptionKey: 'employees.rejectMemoryDescription',
      confirmKey: 'employees.reject',
      tone: 'warning',
      onConfirm: () => void mutate(async (signal) => {
        await rejectEmployeeMemoryCandidate(employee.id, candidateId, { signal })
        const [facts, candidates] = await Promise.all([
          getEmployeeMemory(employee.id, { signal }),
          getEmployeeMemoryCandidates(employee.id, { signal }),
        ])
        return { facts: facts.facts, candidates: candidates.candidates }
      }, setMemory, false),
    })
  }

  function confirmMemoryForget(factId: string) {
    actions.openDialog({
      titleKey: 'employees.forgetMemoryTitle',
      descriptionKey: 'employees.forgetMemoryDescription',
      confirmKey: 'employees.forget',
      tone: 'warning',
      onConfirm: () => void mutate(async (signal) => {
        await forgetEmployeeMemory(employee.id, factId, { signal })
        const [facts, candidates] = await Promise.all([
          getEmployeeMemory(employee.id, { signal }),
          getEmployeeMemoryCandidates(employee.id, { signal }),
        ])
        return { facts: facts.facts, candidates: candidates.candidates }
      }, setMemory, false),
    })
  }

  return (
    <article className="feature-page employee-detail-page">
      <header className="employee-detail-hero">
        <div className="employee-detail-hero__identity">
          <div className="employee-avatar" aria-hidden="true">{avatarLabel}</div>
          <div className="employee-detail-hero__copy">
            <h1 aria-label={employee.name}>{employee.name} · {employee.job_title}</h1>
            <p className="mono">{employee.id} · rev {employee.revision}</p>
          </div>
        </div>
        <div className="employee-detail-hero__actions">
          <span className={`status-badge status-badge--${statusTone}`} data-testid="employee-status">
            <CircleCheck size={14} aria-hidden="true" />
            {translatedEnum(t, 'employeeStatus', employee.state)}
          </span>
          {!archived ? <button type="button" className="button button--secondary" onClick={() => {
            if (definitionRef.current) definitionRef.current.open = true
            definitionRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
          }}>{overviewCopy.edit}</button> : null}
        </div>
      </header>

      {tab === 'overview' ? <section className="employee-insights" aria-label={t('employees.sections')}>
        <article className="employee-insight-card">
          <span className={`employee-insight-card__icon employee-insight-card__icon--${readinessTone}`}><ShieldCheck size={18} aria-hidden="true" /></span>
          <span className="employee-insight-card__label">{t('employees.readiness')}</span>
          <strong>{dryRun ? (dryRun.ready ? t('employees.ready') : t('employees.blocked')) : t('employees.notRun')}</strong>
          <small>{dryRun ? t('employees.checksPassed', { passed: dryRun.checks.filter((check) => check.ready).length, total: dryRun.checks.length }) : t('employees.runDryRunHint')}</small>
        </article>
        <article className="employee-insight-card">
          <span className="employee-insight-card__icon"><Bot size={18} aria-hidden="true" /></span>
          <span className="employee-insight-card__label">{t('employees.defaultModel')}</span>
          <strong>{model?.label ?? employee.default_selection.model}</strong>
          <small>{company?.label ?? employee.default_selection.company} · {access?.label ?? employee.default_selection.access}</small>
        </article>
        <article className="employee-insight-card">
          <span className="employee-insight-card__icon"><Database size={18} aria-hidden="true" /></span>
          <span className="employee-insight-card__label">{t('employees.memory')}</span>
          <strong>{employee.memory_policy.max_context_facts} facts</strong>
          <small>{t('employees.contextBudget', { bytes: employee.memory_policy.max_context_bytes.toLocaleString() })}</small>
        </article>
        <article className="employee-insight-card">
          <span className="employee-insight-card__icon"><FolderKanban size={18} aria-hidden="true" /></span>
          <span className="employee-insight-card__label">{t('employees.tabs.projects')}</span>
          <strong>{record.project_bindings.length}</strong>
          <small>{employee.skill_bindings.length} skills · {employee.responsibilities.length} responsibilities</small>
        </article>
      </section> : null}

      {archived ? <p className="stale-notice employee-detail-notice">{t('employees.archivedReadOnly')}</p> : null}
      <nav className="tab-list employee-detail-tabs" aria-label={t('employees.sections')} ref={tabsRef}>
        <span className="employee-detail-tabs__label"><Workflow size={15} aria-hidden="true" />{t('employees.sections')}</span>
        <div className="employee-detail-tabs__items">
          {TABS.map((value) => <button key={value} type="button" aria-current={tab === value ? 'page' : undefined} onClick={() => setSearchParams(value === 'overview' ? {} : { tab: value })}>{t(`employees.tabs.${value}`)}</button>)}
        </div>
      </nav>

      {tab === 'overview' ? (
        <>
        <header className="page-header employee-prototype-page-head">
          <div>
            <span className="section-kicker">EMPLOYEE / OVERVIEW</span>
            <h1>{overviewCopy.title}</h1>
            <p>{overviewCopy.description}</p>
          </div>
          {!archived ? <Link className="button button--primary" to="/tasks"><ListChecks size={16} aria-hidden="true" />{overviewCopy.createTask}</Link> : null}
        </header>
        <div className="status-strip employee-prototype-status">
          <div className="status-item"><span>{overviewCopy.revision}</span><strong>rev {employee.revision}</strong></div>
          <div className="status-item"><span>{t('employees.defaultModel')}</span><strong>{model?.label ?? employee.default_selection.model}</strong></div>
          <div className="status-item"><span>{overviewCopy.concurrency}</span><strong>{employee.concurrency_policy.max_running_tasks}</strong></div>
          <div className="status-item"><span>{overviewCopy.projects}</span><strong>{record.project_bindings.length}</strong></div>
        </div>
        <div className="split-8-4 employee-prototype-overview">
          <div className="stack">
            <section className="panel">
              <div className="panel-head"><div><h2>{t('employees.charter')}</h2><p>{overviewCopy.charterHint}</p></div></div>
              <p>{employee.charter || t('employees.description')}</p>
              <div className="grid-2">
                <div><h3>{t('employees.responsibilities')}</h3><ul>{employee.responsibilities.slice(0, 3).map((item) => <li key={item}>{item}</li>)}</ul></div>
                <div><h3>{t('employees.behaviorBoundaries')}</h3><ul>{employee.behavior_boundaries.slice(0, 2).map((item) => <li key={item}>{item}</li>)}</ul></div>
              </div>
            </section>
            <section className="panel"><div className="panel-head"><div><h2>{overviewCopy.recent}</h2><p>{overviewCopy.recentHint}</p></div></div><p className="muted">{overviewCopy.noRecent}</p></section>
          </div>
          <aside className="panel">
            <div className="panel-head"><div><h2>{overviewCopy.beforeWork}</h2><p>{overviewCopy.beforeWorkHint}</p></div></div>
            <ul className="readiness-list">
              <li className="readiness-item"><span className="readiness-icon"><Check size={13} aria-hidden="true" /></span><div><strong>{overviewCopy.projectBoundary}</strong><div className="muted tiny">{record.project_bindings.length} · {employee.permission_policy.network_allowed ? t('common.yes') : t('common.no')}</div></div></li>
              <li className="readiness-item"><span className="readiness-icon"><Check size={13} aria-hidden="true" /></span><div><strong>{overviewCopy.skillBinding}</strong><div className="muted tiny">{employee.skill_bindings.length} current</div></div></li>
              <li className="readiness-item"><span className={`readiness-icon${dryRun?.ready === false ? ' fail' : ''}`}>{dryRun?.ready === false ? '!' : <Check size={13} aria-hidden="true" />}</span><div><strong>{overviewCopy.modelAuth}</strong><div className="muted tiny">{model?.label ?? employee.default_selection.model}</div></div></li>
            </ul>
            <div className="notice info">{dryRun ? (dryRun.ready ? t('employees.ready') : t('employees.blocked')) : t('employees.runDryRunHint')}</div>
          </aside>
        </div>
        <details ref={definitionRef} className="employee-definition-editor" open>
          <summary>{t('employees.setup')}</summary>
        <section className="projection-card employee-overview-card">
          <div className="employee-card-heading">
            <div>
              <span className="section-kicker">EMPLOYEE PROFILE</span>
              <h2>{t('employees.setup')}</h2>
              <p>{employee.charter || t('employees.description')}</p>
            </div>
            <span className={`status-badge status-badge--${statusTone}`}>{translatedEnum(t, 'employeeStatus', employee.state)}</span>
          </div>
          <div className="form-grid employee-settings-grid">
            <fieldset className="employee-settings-section">
              <legend><span>01</span><span><strong>{t('employees.steps.identity')}</strong><small>{t('employees.name')} · {t('employees.avatar')}</small></span></legend>
              <div className="employee-settings-section__grid">
            <label>{t('employees.name')}<input disabled={archived} value={employee.name} onChange={(event) => setRecord({ ...record, employee: { ...employee, name: event.target.value } })} /></label>
            <label>{t('employees.jobTitle')}<input disabled={archived} value={employee.job_title} onChange={(event) => setRecord({ ...record, employee: { ...employee, job_title: event.target.value } })} /></label>
            <label className="wide">{t('employees.charter')}<textarea disabled={archived} value={employee.charter} onChange={(event) => setRecord({ ...record, employee: { ...employee, charter: event.target.value } })} /></label>
            <label>{t('employees.avatar')}<select disabled={archived} value={employee.avatar.kind} onChange={(event) => patchEmployee({ avatar: { kind: event.target.value as 'initials' | 'emoji', value: '' } })}><option value="initials">{t('employees.initials')}</option><option value="emoji">{t('employees.emoji')}</option></select></label>
            <label>{t('employees.avatarValue')}<input disabled={archived} value={employee.avatar.value} onChange={(event) => patchEmployee({ avatar: { ...employee.avatar, value: event.target.value } })} /></label>
              </div>
            </fieldset>
            <fieldset className="employee-settings-section">
              <legend><span>02</span><span><strong>{t('employees.steps.charter')}</strong><small>{t('employees.responsibilities')} · {t('employees.behaviorBoundaries')}</small></span></legend>
              <div className="employee-settings-section__grid">
            <label>{t('employees.responsibilities')}<textarea disabled={archived} value={employee.responsibilities.join('\n')} onChange={(event) => patchEmployee({ responsibilities: lines(event.target.value) })} /></label>
            <label>{t('employees.behaviorBoundaries')}<textarea disabled={archived} value={employee.behavior_boundaries.join('\n')} onChange={(event) => patchEmployee({ behavior_boundaries: lines(event.target.value) })} /></label>
              </div>
            </fieldset>
            <fieldset className="employee-settings-section">
              <legend><span>03</span><span><strong>{t('employees.steps.modelAgent')}</strong><small>{t('employees.defaultModel')} · {t('employees.agent')}</small></span></legend>
              <div className="employee-settings-section__grid">
            <label>{t('employees.company')}<select disabled={archived} value={employee.default_selection.company} onChange={(event) => {
              const selected = companies.find((item) => item.id === event.target.value)
              const selectedAccess = selected?.access[0]
              patchEmployee({ default_selection: { company: event.target.value, access: selectedAccess?.id ?? '', model: selectedAccess?.models[0]?.id ?? '' } })
            }}>{companies.map((item) => <option key={item.id} value={item.id}>{item.label}</option>)}</select></label>
            <label>{t('employees.access')}<select disabled={archived} value={employee.default_selection.access} onChange={(event) => {
              const selected = company?.access.find((item) => item.id === event.target.value)
              patchEmployee({ default_selection: { ...employee.default_selection, access: event.target.value, model: selected?.models[0]?.id ?? '' } })
            }}>{company?.access.map((item) => <option key={item.id} value={item.id}>{item.label}</option>)}</select></label>
            <label>{t('employees.model')}<select disabled={archived} value={employee.default_selection.model} onChange={(event) => patchEmployee({ default_selection: { ...employee.default_selection, model: event.target.value } })}>{access?.models.map((item) => <option key={item.id} value={item.id}>{item.label}</option>)}</select></label>
            <label>{t('employees.agent')}<select disabled={archived} value={employee.agent_profile} onChange={(event) => patchEmployee({ agent_profile: event.target.value })}>{info?.agents.map((item) => <option key={item.id} value={item.id}>{item.label}</option>)}</select></label>
              </div>
            </fieldset>
            <fieldset className="employee-settings-section">
              <legend><span>04</span><span><strong>{t('employees.steps.policy')}</strong><small>{t('employees.capabilities')} · {t('employees.budget')}</small></span></legend>
              <div className="employee-settings-section__grid">
            <label>{t('employees.capabilities')}<textarea disabled={archived} value={employee.permission_policy.allowed_capabilities.join('\n')} onChange={(event) => patchEmployee({ permission_policy: { ...employee.permission_policy, allowed_capabilities: lines(event.target.value) } })} /></label>
            <label className="form-toggle"><input type="checkbox" disabled={archived} checked={employee.permission_policy.network_allowed} onChange={(event) => patchEmployee({ permission_policy: { ...employee.permission_policy, network_allowed: event.target.checked } })} /><span>{t('employees.network')}</span></label>
            <label>{t('employees.maxCalls')}<input type="number" min="1" disabled={archived} value={employee.budget_policy.max_model_calls} onChange={(event) => patchEmployee({ budget_policy: { ...employee.budget_policy, max_model_calls: Number(event.target.value) } })} /></label>
            <label>{t('employees.maxTokens')}<input type="number" min="1" disabled={archived} value={employee.budget_policy.max_tokens} onChange={(event) => patchEmployee({ budget_policy: { ...employee.budget_policy, max_tokens: Number(event.target.value) } })} /></label>
            <label>{t('employees.timeoutSeconds')}<input type="number" min="1" disabled={archived} value={employee.budget_policy.timeout_seconds} onChange={(event) => patchEmployee({ budget_policy: { ...employee.budget_policy, timeout_seconds: Number(event.target.value) } })} /></label>
            <label>{t('employees.maxRunningTasks')}<input type="number" min="1" disabled={archived} value={employee.concurrency_policy.max_running_tasks} onChange={(event) => patchEmployee({ concurrency_policy: { max_running_tasks: Number(event.target.value) } })} /></label>
              </div>
            </fieldset>
            <fieldset className="employee-settings-section">
              <legend><span>05</span><span><strong>{t('employees.steps.memory')}</strong><small>{t('employees.memoryCandidates')} · {t('employees.maxContextFacts')}</small></span></legend>
              <div className="employee-settings-section__grid">
            <label className="form-toggle"><input type="checkbox" disabled={archived} checked={employee.memory_policy.candidate_generation} onChange={(event) => patchEmployee({ memory_policy: { ...employee.memory_policy, candidate_generation: event.target.checked, promotion: event.target.checked ? 'owner_confirmation' : 'disabled' } })} /><span>{t('employees.memoryCandidates')}</span></label>
            <label>{t('employees.maxContextFacts')}<input type="number" min="0" disabled={archived} value={employee.memory_policy.max_context_facts} onChange={(event) => patchEmployee({ memory_policy: { ...employee.memory_policy, max_context_facts: Number(event.target.value) } })} /></label>
            <label>{t('employees.maxContextBytes')}<input type="number" min="0" disabled={archived} value={employee.memory_policy.max_context_bytes} onChange={(event) => patchEmployee({ memory_policy: { ...employee.memory_policy, max_context_bytes: Number(event.target.value) } })} /></label>
              </div>
            </fieldset>
          </div>
          <dl>
            <dt className="employee-summary-kicker">{t('employees.currentConfiguration')}</dt>
            <dt>{t('employees.defaultModel')}</dt><dd>{employee.default_selection.company}/{employee.default_selection.access}/{employee.default_selection.model}</dd>
            <dt>{t('employees.agent')}</dt><dd>{employee.agent_profile}</dd>
            <dt>{t('employees.network')}</dt><dd>{employee.permission_policy.network_allowed ? t('common.yes') : t('common.no')}</dd>
            <dt>{t('employees.budget')}</dt><dd>{t('employees.budgetSummary', { calls: employee.budget_policy.max_model_calls, tokens: employee.budget_policy.max_tokens, seconds: employee.budget_policy.timeout_seconds })}</dd>
            <dt>{t('employees.concurrency')}</dt><dd>{employee.concurrency_policy.max_running_tasks}</dd>
            <dt>{t('employees.memory')}</dt><dd>{employee.memory_policy.promotion} · {employee.memory_policy.max_context_facts} facts / {employee.memory_policy.max_context_bytes} bytes</dd>
          </dl>
          {!archived ? <div className="button-row employee-action-bar">
            <button className="button button--primary" type="button" disabled={!canMutate} onClick={() => void saveEmployee()}>{t('employees.save')}</button>
            {active ? <button type="button" disabled={!canMutate} onClick={() => lifecycle('disable')}>{t('employees.disable')}</button> : <button type="button" disabled={!canMutate} onClick={() => lifecycle('enable')}>{t('employees.enable')}</button>}
            <button type="button" disabled={!canMutate} onClick={() => lifecycle('archive')}>{t('employees.archive')}</button>
            <button type="button" disabled={!connectivity.canMutate || busy} onClick={runDry}>{t('employees.dryRun')}</button>
          </div> : null}
          {dryRun ? <section className="employee-readiness-card" data-testid="employee-detail-readiness"><div className="employee-readiness-card__heading"><div><span className="section-kicker">SERVER CHECK</span><h3>{t('employees.readiness')}</h3></div><span className={`status-badge status-badge--${dryRun.ready ? 'success' : 'warning'}`}>{dryRun.ready ? t('employees.ready') : t('employees.blocked')}</span></div><ul>{dryRun.checks.map((check) => <li className={check.ready ? 'is-ready' : 'is-blocked'} key={check.name}><CircleCheck size={16} aria-hidden="true" /><span><strong>{check.name}: {check.detail}</strong><small>{check.ready ? t('employees.ready') : t('employees.blocked')}</small></span></li>)}</ul></section> : null}
        </section>
        </details>
        </>
      ) : null}

      {tab === 'skills' ? (
        <section className="projection-card employee-tab-page employee-skills-tab">
          <header className="employee-tab-header">
            <div>
              <span className="section-kicker">SKILL DIRECTORY</span>
              <h2>{t('employees.tabs.skills')}</h2>
              <p>{t('employees.skillDirectoryDescription')}</p>
            </div>
            <div className="employee-tab-actions"><span className="status-badge status-badge--success"><FolderOpen size={14} aria-hidden="true" />{t('employees.catalogBindings')}</span></div>
          </header>
          <div className="employee-tab-metrics">
            <div><span>{t('employees.boundSkills')}</span><strong>{skillDraft.length}</strong></div>
            <div><span>{t('employees.availableSkills')}</span><strong>{catalog.length}</strong></div>
            <div><span>{t('employees.skillContract')}</span><strong>{t('employees.skillContractValue')}</strong></div>
          </div>
          {skills ? <section className="employee-subsection employee-current-bindings">
            <div className="employee-section-heading"><div><span className="section-kicker">ACTIVE CONFIGURATION</span><h3>{t('employees.currentSkills')}</h3></div><span className="employee-section-heading__meta">{skillDraft.length} / {catalog.length}</span></div>
            {skills.bindings.length ? <div className="skill-binding-grid">{skills.bindings.map((item) => {
              const label = `${item.binding.skill_id}@${item.binding.version}`
              return <article className={`skill-binding-card skill-binding-card--${item.status}`} key={label}>
                <div className="skill-binding-card__topline"><span className="skill-kind-icon"><Settings2 size={15} aria-hidden="true" /></span><span className={`status-badge status-badge--${item.status === 'current' ? 'success' : 'warning'}`}>{translatedEnum(t, 'skillStatus', item.status)}</span></div>
                <h4>{label}</h4>
                <code>{item.binding.digest.slice(0, 12)}…</code>
                <p>{translatedEnum(t, 'bindingStatus', item.binding.enabled ? 'enabled' : 'disabled')}{item.kind === 'skill_md_adapter' ? ` · ${t('employees.adapterZeroCapability')}` : ''}</p>
              </article>
            })}</div> : <div className="employee-empty-panel"><FolderOpen size={22} aria-hidden="true" /><p>{t('employees.noSkillsBound')}</p></div>}
          </section> : <p role="status">{t('common.loading')}</p>}
          {active && skills ? <div className="employee-inline-alert">{skills.bindings.filter((item) => item.status !== 'current').map((item) => {
            const label = `${item.binding.skill_id}@${item.binding.version}`
            const current = catalog.find((candidate) => candidate.skill_id === item.binding.skill_id && candidate.version === item.binding.version)
            return item.status === 'digest_drift' && current
              ? <button type="button" key={label} disabled={!canMutate} onClick={() => setSkillDraft((draft) => draft.map((binding) => skillKey(binding) === skillKey(item.binding) ? { ...binding, digest: current.digest } : binding))}>{t('employees.upgradeSkill')} {label}</button>
              : <button type="button" key={label} disabled={!canMutate} onClick={() => setSkillDraft((draft) => draft.filter((binding) => skillKey(binding) !== skillKey(item.binding)))}>{t('employees.removeSkill')} {label}</button>
          })}</div> : null}
          {active ? <div className="skill-workbench">
            <section className="skill-directory-panel">
              <div className="employee-section-heading"><div><span className="section-kicker">CATALOG</span><h3>{t('employees.catalogBindings')}</h3><p>{t('employees.selectSkillHint')}</p></div><label className="skill-search"><Search size={16} aria-hidden="true" /><span className="sr-only">{t('employees.searchSkills')}</span><input aria-label={t('employees.searchSkills')} placeholder={t('employees.searchSkills')} value={skillQuery} onChange={(event) => setSkillQuery(event.target.value)} /></label></div>
              <fieldset className="skill-catalog-fieldset"><legend className="sr-only">{t('employees.catalogBindings')}</legend><div className="skill-directory-grid">
                {filteredCatalog.map((item) => {
                  const key = skillKey(item)
                  const binding = skillDraft.find((candidate) => skillKey(candidate) === key)
                  const selected = selectedSkillKey === key
                  return <label className={`skill-directory-card${selected ? ' is-selected' : ''}${binding ? ' is-bound' : ''}`} key={key}>
                    <input type="checkbox" checked={Boolean(binding)} aria-label={item.title} onChange={(event) => {
                      setSelectedSkillKey(key)
                      setSkillDraft((current) => event.target.checked
                        ? current.some((candidate) => skillKey(candidate) === key) ? current : [...current, { skill_id: item.skill_id, version: item.version, digest: item.digest, configuration: {}, enabled: true }]
                        : current.filter((candidate) => skillKey(candidate) !== key))
                      if (event.target.checked) setSkillConfiguration((current) => ({ ...current, [key]: current[key] ?? '{}' }))
                    }} />
                    <span className="skill-directory-card__check" aria-hidden="true"><Check size={14} /></span>
                    <span className="skill-directory-card__main"><span className="skill-directory-card__title">{item.title}</span><span className="skill-directory-card__id">{item.skill_id} · v{item.version}</span><span className="skill-directory-card__description">{item.description}</span><span className="skill-directory-card__footer"><span>{item.kind === 'native' ? t('employees.nativeSkill') : t('employees.adapterSkill')}</span><code>{item.digest.slice(0, 10)}…</code></span></span>
                  </label>
                })}
                {!filteredCatalog.length ? <div className="employee-empty-panel"><Search size={22} aria-hidden="true" /><p>{t('employees.noSkillsFound')}</p></div> : null}
              </div></fieldset>
            </section>
            <aside className="skill-config-panel" aria-label={t('employees.selectedSkill')}>
              {selectedSkill ? <>
                <div className="skill-config-panel__heading"><span className="skill-kind-icon"><Settings2 size={17} aria-hidden="true" /></span><div><span className="section-kicker">SELECTED SKILL</span><h3>{selectedSkill.title}</h3><p>{selectedSkill.skill_id}@{selectedSkill.version}</p></div></div>
                <div className="skill-config-panel__meta"><span className={`status-badge status-badge--${selectedSkillStatus?.status === 'current' ? 'success' : 'warning'}`}>{selectedSkillStatus ? translatedEnum(t, 'skillStatus', selectedSkillStatus.status) : t('employees.notBound')}</span><code>{selectedSkill.digest}</code></div>
                {selectedBinding ? <label className="skill-enabled-toggle"><input type="checkbox" checked={selectedBinding.enabled} aria-label={`${t('bindingStatus.enabled')} ${selectedSkill.title}`} onChange={(event) => setSkillDraft((current) => current.map((candidate) => skillKey(candidate) === activeSkillKey ? { ...candidate, enabled: event.target.checked } : candidate))} /><span><strong>{t('bindingStatus.enabled')}</strong><small>{t('employees.enabledSkillHint')}</small></span></label> : <div className="skill-config-panel__empty"><FolderOpen size={18} aria-hidden="true" /><p>{t('employees.selectSkillToBind')}</p></div>}
                {selectedBinding && selectedSkill.kind === 'native' ? <label className="skill-config-field">{t('employees.configurationJSON')} {selectedSkill.title}<textarea aria-label={`${t('employees.configurationJSON')} ${selectedSkill.title}`} value={skillConfiguration[activeSkillKey ?? ''] ?? '{}'} onChange={(event) => setSkillConfiguration((current) => ({ ...current, [activeSkillKey ?? '']: event.target.value }))} /><small>{t('employees.configurationHint')}</small></label> : null}
                {selectedBinding && selectedSkill.kind === 'skill_md_adapter' ? <p className="skill-adapter-note"><FileText size={16} aria-hidden="true" />{t('employees.adapterZeroCapability')}</p> : null}
              </> : <div className="skill-config-panel__empty"><FolderOpen size={24} aria-hidden="true" /><h3>{t('employees.selectSkill')}</h3><p>{t('employees.selectSkillHint')}</p></div>}
            </aside>
          </div> : <p className="stale-notice">{t('employees.skillsActiveOnly')}</p>}
          {active ? <div className="employee-sticky-actions"><span>{t('employees.saveSkillsHint')}</span><button className="button button--primary" type="button" disabled={!canMutate} onClick={() => void mutate(async (signal) => {
            const normalized = skillDraft.map((binding) => {
              const item = catalog.find((candidate) => skillKey(candidate) === skillKey(binding))
              if (item?.kind === 'skill_md_adapter') return { ...binding, configuration: {} }
              const parsed = JSON.parse(skillConfiguration[skillKey(binding)] ?? '{}') as unknown
              if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') throw new Error('configuration')
              return { ...binding, configuration: parsed as Record<string, unknown> }
            })
            const updated = await updateEmployeeSkills(employee.id, employee.revision, normalized, { signal })
            const projection = await getEmployeeSkills(employee.id, { signal })
            return { updated, projection }
          }, ({ updated, projection }) => { setRecord(updated); setSkillDraft(updated.employee.skill_bindings); setSkills(projection) }, false)}>{t('employees.saveSkills')}</button></div> : null}
        </section>
      ) : null}

      {tab === 'knowledge' ? (
        <section className="employee-tab-page employee-knowledge-tab">
          <header className="page-header employee-tab-page-head"><div><span className="section-kicker">{tabCopy.knowledge.kicker}</span><h1>{tabCopy.knowledge.title}</h1><p>{tabCopy.knowledge.description}</p></div>{!archived ? <button className="button button--primary" type="button" onClick={() => setKnowledgeComposerOpen((open) => !open)}><Database size={16} aria-hidden="true" />{tabCopy.knowledge.add}</button> : null}</header>
          {knowledge ? <div className="split-7-5 employee-knowledge-workspace"><section><div className="knowledge-source-grid">{knowledge.sources.map((source) => {
            const index = knowledge.indexes.find((item) => item.source_id === source.id)
            const citations = index?.documents.flatMap((document) => document.citations) ?? []
            return <article className="knowledge-source-card" key={source.id} data-testid="knowledge-source"><div className="knowledge-source-card__header"><span className="knowledge-source-card__icon"><FileText size={18} aria-hidden="true" /></span><div><h3>{source.title}</h3><p>{source.id} · {translatedEnum(t, 'knowledgeKind', source.kind)}</p></div><span className={`status-badge status-badge--${source.status === 'ready' ? 'success' : 'warning'}`}>{translatedEnum(t, 'knowledgeStatus', source.status)}</span></div><div className="knowledge-source-card__digest"><span>{t('employees.sourceDigest')}</span><code>{source.digest.slice(0, 16)}…</code></div>{citations.length ? <div className="citation-list"><div className="citation-list__heading"><span>{t('employees.citations')}</span><strong>{citations.length}</strong></div><ul>{citations.map((citation) => <li key={citation.id} data-testid="knowledge-citation"><span>{citation.path}:{citation.start_line}-{citation.end_line}</span><code>{citation.digest.slice(0, 10)}…</code></li>)}</ul></div> : <p className="knowledge-source-card__empty">{t('employees.noCitations')}</p>}{!archived ? <div className="button-row"><button type="button" disabled={!canMutate} onClick={() => void mutate((signal) => refreshEmployeeKnowledge(employee.id, source.id, { signal }), setKnowledge, false)}>{t('employees.refresh')}</button><button type="button" disabled={!canMutate} onClick={() => confirmKnowledgeDelete(source.id)}>{t('employees.delete')}</button></div> : null}</article>
          })}{!knowledge.sources.length ? <div className="employee-empty-panel"><Database size={22} aria-hidden="true" /><p>{t('employees.noKnowledge')}</p></div> : null}</div></section><aside className="panel employee-citation-inspector"><div className="panel-head"><div><h2>{tabCopy.knowledge.inspector}</h2><p>{tabCopy.knowledge.inspectorHint}</p></div></div>{firstKnowledgeCitation ? <dl className="employee-definition-list"><dt>Path</dt><dd className="mono">{firstKnowledgeCitation.path}</dd><dt>Lines</dt><dd>{firstKnowledgeCitation.start_line}–{firstKnowledgeCitation.end_line}</dd><dt>Digest</dt><dd className="mono">{firstKnowledgeCitation.digest}</dd><dt>Source</dt><dd>{firstKnowledgeSource?.id ?? firstKnowledgeCitation.source_id}</dd></dl> : <div className="employee-empty-panel"><FileText size={22} aria-hidden="true" /><p>{t('employees.noCitations')}</p></div>}<div className="notice info">{tabCopy.knowledge.citationNote}</div></aside></div> : <p role="status">{t('common.loading')}</p>}
          {!archived && knowledgeComposerOpen ? <section className="panel knowledge-add-panel"><div className="employee-section-heading"><div><span className="section-kicker">ADD SOURCE</span><h3>{t('employees.addKnowledge')}</h3></div></div><div className="form-grid"><label>{t('employees.knowledgeKind')}<select value={sourceDraft.kind} onChange={(event) => setSourceDraft({ ...sourceDraft, kind: event.target.value as typeof sourceDraft.kind })}><option value="manual_text">{t('knowledgeKind.manual_text')}</option><option value="file">{t('knowledgeKind.file')}</option><option value="project_docs">{t('knowledgeKind.project_docs')}</option></select></label><label>{t('employees.sourceId')}<input value={sourceDraft.id} onChange={(event) => setSourceDraft({ ...sourceDraft, id: event.target.value })} /></label><label>{t('employees.sourceTitle')}<input value={sourceDraft.title} onChange={(event) => setSourceDraft({ ...sourceDraft, title: event.target.value })} /></label><label className="wide">{sourceDraft.kind === 'manual_text' ? t('knowledgeKind.manual_text') : t('employees.relativePath')}<textarea value={sourceDraft.content} onChange={(event) => setSourceDraft({ ...sourceDraft, content: event.target.value })} /></label><button className="button button--primary" type="button" disabled={!canMutate} onClick={() => void mutate((signal) => addEmployeeKnowledge(employee.id, { id: sourceDraft.id, kind: sourceDraft.kind, title: sourceDraft.title, ...(sourceDraft.kind === 'manual_text' ? { manual_text: sourceDraft.content } : { relative_path: sourceDraft.content }) }, { signal }), setKnowledge, false)}>{t('employees.addKnowledge')}</button></div></section> : null}
        </section>
      ) : null}

      {tab === 'memory' ? (
        <section className="employee-tab-page employee-memory-tab">
          <header className="page-header employee-tab-page-head"><div><span className="section-kicker">{tabCopy.memory.kicker}</span><h1>{tabCopy.memory.title}</h1><p>{tabCopy.memory.description}</p></div></header>
          <div className="split-7-5 employee-memory-workspace"><section className="stack">
          <section className="employee-subsection"><div className="employee-section-heading"><div><span className="section-kicker">REVIEW QUEUE</span><h3>{t('employees.pendingCandidates')}</h3></div></div><div className="memory-card-grid">{memory?.candidates.map((candidate) => <article className="memory-candidate-card" key={candidate.id} data-testid="memory-candidate"><div className="memory-card__topline"><span className="status-badge status-badge--warning">{t('employees.pending')}</span><code>{candidate.category}</code></div><p>{candidate.value}</p><small>{candidate.provenance.map((item) => `${item.source_type}:${item.source_id}`).join(' · ')}</small>{!archived ? <div className="button-row"><button type="button" className="button button--primary" disabled={!canMutate} onClick={() => void mutate(async (signal) => { await acceptEmployeeMemoryCandidate(employee.id, candidate.id, { signal }); const [value, pending] = await Promise.all([getEmployeeMemory(employee.id, { signal }), getEmployeeMemoryCandidates(employee.id, { signal })]); return { facts: value.facts, candidates: pending.candidates } }, setMemory, false)}>{t('employees.accept')}</button><button type="button" disabled={!canMutate} onClick={() => confirmMemoryReject(candidate.id)}>{t('employees.reject')}</button></div> : null}</article>)}{memory && !memory.candidates.length ? <div className="employee-empty-panel"><Check size={22} aria-hidden="true" /><p>{t('employees.noPendingMemory')}</p></div> : null}</div></section>
          <section className="employee-subsection"><div className="employee-section-heading"><div><span className="section-kicker">ACCEPTED FACTS</span><h3>{t('employees.acceptedMemory')}</h3></div></div><div className="memory-fact-grid">{memory?.facts.map((fact) => <article className="memory-fact-card" key={fact.id}><div className="memory-card__topline"><span className="status-badge status-badge--success">{fact.category}</span><code>{fact.digest.slice(0, 12)}…</code></div><label>{t('employees.factValue')}<input disabled={archived} value={factDraft[fact.id] ?? fact.value} onChange={(event) => setFactDraft((current) => ({ ...current, [fact.id]: event.target.value }))} /></label><small>{fact.owner_edited ? t('employees.editedByOwner') : t('employees.generatedFact')}</small>{!archived ? <div className="button-row"><button type="button" disabled={!canMutate} onClick={() => void mutate(async (signal) => { await editEmployeeMemory(employee.id, fact.id, factDraft[fact.id] ?? fact.value, { signal }); const [facts, candidates] = await Promise.all([getEmployeeMemory(employee.id, { signal }), getEmployeeMemoryCandidates(employee.id, { signal })]); return { facts: facts.facts, candidates: candidates.candidates } }, setMemory, false)}>{t('employees.edit')}</button><button type="button" disabled={!canMutate} onClick={() => confirmMemoryForget(fact.id)}>{t('employees.forget')}</button></div> : null}</article>)}{memory && !memory.facts.length ? <div className="employee-empty-panel"><Database size={22} aria-hidden="true" /><p>{t('employees.noAcceptedMemory')}</p></div> : null}</div></section>
          </section><aside className="panel employee-memory-boundary"><h2>{tabCopy.memory.boundary}</h2><dl className="employee-definition-list"><dt>{tabCopy.memory.candidateGeneration}</dt><dd>{employee.memory_policy.candidate_generation ? tabCopy.memory.enabled : t('common.no')}</dd><dt>{t('employees.promotion')}</dt><dd>{employee.memory_policy.promotion}</dd><dt>{tabCopy.memory.contextLimit}</dt><dd>{employee.memory_policy.max_context_facts} facts</dd><dt>{tabCopy.memory.byteLimit}</dt><dd>{employee.memory_policy.max_context_bytes}</dd></dl><div className="notice">{tabCopy.memory.warning}</div></aside></div>
        </section>
      ) : null}

      {tab === 'projects' ? (
        <section className="employee-tab-page employee-projects-tab">
          <header className="page-header employee-tab-page-head"><div><span className="section-kicker">{tabCopy.projects.kicker}</span><h1>{tabCopy.projects.title}</h1><p>{tabCopy.projects.description}</p></div>{!archived ? <button className="button button--primary" type="button" disabled={!canMutate} onClick={() => void saveEmployee()}>{t('employees.save')}</button> : null}</header>
          <div className="project-binding-grid">{record.project_bindings.map((binding, index) => <fieldset className="project-binding-card" key={binding.id}><legend><FolderOpen size={16} aria-hidden="true" />{binding.label}</legend><div className="project-binding-card__path"><span>{t('employees.workspace')}</span><code>{binding.workspace_fingerprint}</code></div><div className="project-permission-list"><label><input type="checkbox" aria-label={t('employees.readAllowed')} disabled={archived} checked={binding.read_allowed} onChange={(event) => setRecord({ ...record, project_bindings: record.project_bindings.map((item, itemIndex) => itemIndex === index ? { ...item, read_allowed: event.target.checked } : item) })} /><span><strong>{t('employees.readAllowed')}</strong><small>{t('employees.readAllowedHint')}</small></span></label><label><input type="checkbox" aria-label={t('employees.mutationAllowed')} disabled={archived} checked={binding.mutation_allowed} onChange={(event) => setRecord({ ...record, project_bindings: record.project_bindings.map((item, itemIndex) => itemIndex === index ? { ...item, mutation_allowed: event.target.checked } : item) })} /><span><strong>{t('employees.mutationAllowed')}</strong><small>{t('employees.mutationAllowedHint')}</small></span></label><label><input type="checkbox" aria-label={t('employees.networkAllowed')} disabled={archived} checked={binding.network_allowed} onChange={(event) => setRecord({ ...record, project_bindings: record.project_bindings.map((item, itemIndex) => itemIndex === index ? { ...item, network_allowed: event.target.checked } : item) })} /><span><strong>{t('employees.networkAllowed')}</strong><small>{t('employees.networkAllowedHint')}</small></span></label></div><div className="project-binding-card__footer"><span>{t('employees.capabilities')}: {binding.allowed_tool_capabilities.join(', ') || t('common.none')}</span><span>{t('employees.budgetOverride')}: {binding.budget_override ? `${binding.budget_override.max_model_calls}/${binding.budget_override.max_tokens}/${binding.budget_override.timeout_seconds}s` : t('employees.employeeDefault')}</span></div></fieldset>)}{!record.project_bindings.length ? <div className="employee-empty-panel"><FolderKanban size={22} aria-hidden="true" /><p>{t('employees.noProjects')}</p></div> : null}</div>
        </section>
      ) : null}

      {tab === 'loops' ? (
        <section className="employee-tab-page employee-loops-tab">
          <header className="page-header employee-tab-page-head"><div><span className="section-kicker">{tabCopy.loops.kicker}</span><h1>{tabCopy.loops.title}</h1><p>{tabCopy.loops.description}</p></div>{!archived ? <Link className="button button--primary" to="/loops">{tabCopy.loops.create}</Link> : null}</header>
          <div className="grid-2 employee-loop-workspace"><section>{loops === null ? <p role="status">{t('common.loading')}</p> : loops.length ? <div className="loop-card-grid">{loops.map((item) => <article className="loop-card" key={item.id}><div className="loop-card__topline"><span className="loop-pill">{item.schedule.kind === 'daily' ? t('loops.daily') : t('loops.manual')}</span><span className={`status-badge status-badge--${item.enabled ? 'success' : 'muted'}`}>{item.enabled ? t('loops.enabled') : t('loops.disabled')}</span></div><h2><Link to={`/loops/${encodeURIComponent(item.id)}`}>{item.name}</Link></h2><p>{item.contract.goal}</p><dl className="loop-card__facts"><div><dt>{tabCopy.loops.schedule}</dt><dd>{item.schedule.kind === 'daily' ? `${item.schedule.local_time} · ${item.schedule.timezone}` : t('loops.manual')}</dd></div><div><dt>{t('loops.does')}</dt><dd>{item.contract.sop[0]}</dd></div><div><dt>{t('loops.youGet')}</dt><dd>{item.contract.definition_of_done[0] ?? t('loops.verifiedReport')}</dd></div></dl><Link className="loop-card__action" to={`/loops/${encodeURIComponent(item.id)}`}>{tabCopy.loops.open}</Link></article>)}</div> : <div className="employee-empty-panel"><Workflow size={22} aria-hidden="true" /><p>{t('loops.noRuns')}</p></div>}</section><aside className="panel contract-card employee-loop-create-card"><h2>{tabCopy.loops.nextTitle}</h2><p className="muted">{tabCopy.loops.nextHint}</p><div className="notice info">{tabCopy.loops.invocationNote}</div><Link className="button button--secondary" to="/loops">{tabCopy.loops.goTo}</Link></aside></div>
        </section>
      ) : null}

      {tab === 'tasks' ? (
        <section className="employee-tab-page employee-tasks-tab">
          <header className="page-header employee-tab-page-head">
            <div>
              <span className="section-kicker">{tabCopy.tasks.kicker}</span>
              <h1>{tabCopy.tasks.title}</h1>
              <p>{tabCopy.tasks.description}</p>
            </div>
            {!archived ? <Link className="button button--primary" to="/tasks"><ListChecks size={16} aria-hidden="true" />{tabCopy.tasks.create}</Link> : null}
          </header>
          {tasks === null ? <p role="status">{t('common.loading')}</p> : tasks.length ? (
            <div className="table-wrap employee-task-table">
              <table>
                <thead><tr><th>{tabCopy.tasks.task}</th><th>{tabCopy.tasks.employee}</th><th>{tabCopy.tasks.status}</th><th>{tabCopy.tasks.sessionRun}</th><th>{tabCopy.tasks.updated}</th></tr></thead>
                <tbody>
                  {tasks.map((task) => (
                    <tr key={task.id}>
                      <td data-label={tabCopy.tasks.task}>
                        <Link className="row-title employee-task-title" title={task.prompt} to={`/tasks/${encodeURIComponent(task.id)}`}>{task.prompt}</Link>
                        <span className="row-sub mono">{task.id} · {tabCopy.tasks.snapshot} {task.snapshot_digest.slice(0, 15)}…</span>
                      </td>
                      <td data-label={tabCopy.tasks.employee}><span className="employee-task-cell-value">{task.employee_id}</span><span className="row-sub">rev {task.employee_revision}</span></td>
                      <td data-label={tabCopy.tasks.status}><span className={`status-badge status-badge--${task.state === 'completed' ? 'success' : task.state === 'failed' || task.state === 'cancelled' || task.state === 'interrupted' ? 'warning' : 'muted'}`}>{translatedEnum(t, 'taskStatus', task.state)}</span></td>
                      <td data-label={tabCopy.tasks.sessionRun}><span className="mono employee-task-cell-value">{task.session_id || '—'}</span><span className="row-sub mono employee-task-cell-value">{task.run_id || '—'}</span></td>
                      <td data-label={tabCopy.tasks.updated}><time>{task.updated_at.slice(0, 19).replace('T', ' ')}</time></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : <div className="employee-empty-panel"><ListChecks size={22} aria-hidden="true" /><p>{t('employees.noTasks')}</p></div>}
        </section>
      ) : null}
      {tab === 'activity' ? (
        <section className="employee-tab-page employee-activity-tab">
          <header className="page-header employee-tab-page-head">
            <div>
              <span className="section-kicker">{tabCopy.activity.kicker}</span>
              <h1>{tabCopy.activity.title}</h1>
              <p>{tabCopy.activity.description}</p>
            </div>
          </header>
          {activity === null ? <p role="status">{t('common.loading')}</p> : activity.events.length ? (
            <section className="panel employee-audit-panel">
              <ol className="employee-audit-timeline">
                {activity.events.map((event) => {
                  const translatedType = translatedEnum(t, 'employeeActivityType', event.type)
                  return (
                    <li key={event.id}>
                      <time>{event.time}</time>
                      <span className="employee-audit-timeline__dot" aria-hidden="true" />
                      <div>
                        <strong>{translatedType === t('status.unknown') ? event.type : translatedType}</strong>
                        <span className="row-sub">rev {event.employee_revision ?? '—'}{event.subject_id ? ` · ${event.subject_id}` : ''}{event.task_id ? ` · ${event.task_id}` : ''}{event.session_id ? ` · ${event.session_id}` : ''}{event.run_id ? ` · ${event.run_id}` : ''}</span>
                      </div>
                      {event.task_id ? <Link className="button button--secondary" to={`/tasks/${encodeURIComponent(event.task_id)}`}>{tabCopy.activity.open}</Link> : null}
                    </li>
                  )
                })}
              </ol>
            </section>
          ) : <div className="employee-empty-panel"><History size={22} aria-hidden="true" /><p>{t('employees.noActivity')}</p></div>}
          {activity?.next_cursor ? (
            <div className="employee-sticky-actions">
              <span>{t('employees.activityLoadHint')}</span>
              <button type="button" onClick={() => {
                const cursor = activity.next_cursor
                if (!cursor) return
                const owner = employeeId
                const epoch = requestEpoch.current
                const controller = new AbortController()
                void getEmployeeActivity(employee.id, { limit: 100, cursor }, { signal: controller.signal }).then((page) => {
                  if (epoch !== requestEpoch.current || owner !== employeeId) return
                  setActivity({ events: [...activity.events, ...page.events], ...(page.next_cursor ? { next_cursor: page.next_cursor } : {}) })
                }).catch(() => undefined)
              }}>{t('employees.loadMore')}</button>
            </div>
          ) : null}
        </section>
      ) : null}
    </article>
  )
}
