import { useCallback, useEffect, useRef, useState } from 'react'
import {
  Alert,
  Badge,
  Button,
  Card,
  Checkbox,
  Col,
  Descriptions,
  Drawer,
  Dropdown,
  Empty,
  Flex,
  Form,
  Grid,
  Input,
  InputNumber,
  List,
  Modal,
  Progress,
  Row,
  Select,
  Skeleton,
  Space,
  Statistic,
  Switch,
  Table,
  Tabs,
  Tag,
  Timeline,
  Tooltip,
  Typography,
} from 'antd'
import type { MenuProps, TableColumnsType } from 'antd'
import {
  Check,
  Database,
  FileText,
  MoreHorizontal,
  Pencil,
  RefreshCw,
  ShieldCheck,
} from 'lucide-react'
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
  getInfo,
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
  EmployeeSkillStatus,
  EmployeeTask,
  Info,
  KnowledgeCitation,
  KnowledgeSource,
  LoopDefinition,
  MemoryCandidate,
  MemoryFact,
  ProjectBinding,
  SkillBinding,
  SkillCatalogItem,
} from '../../api/types'
import { useConnectivity } from '../../components/ConnectivityProvider'
import { ErrorState } from '../../components/ErrorState'
import { translatedEnum } from '../../i18n/enumLabel'
import { useUI } from '../../state/UIContext'

const { Paragraph, Text, Title } = Typography

type Tab = 'overview' | 'settings' | 'skills' | 'knowledge' | 'memory' | 'projects' | 'loops' | 'tasks' | 'activity'
const TABS: Tab[] = ['overview', 'settings', 'skills', 'knowledge', 'memory', 'projects', 'loops', 'tasks', 'activity']
const TERMINAL_TASKS = new Set(['completed', 'failed', 'cancelled'])

function errorKey(error: unknown) {
  if (error instanceof ApiError && error.code === 'network_error') return 'mutation.offline'
  if (error instanceof ApiError && error.status === 409) return 'mutation.conflict'
  return 'mutation.failed'
}

function lines(value: string) {
  return value.split(/\r?\n/u).map((item) => item.trim()).filter(Boolean)
}

function skillIdentity(item: Pick<SkillBinding, 'skill_id' | 'version' | 'digest'>) {
  return `${item.skill_id}:${item.version}:${item.digest}`
}

function skillVersion(item: Pick<SkillBinding, 'skill_id' | 'version'>) {
  return `${item.skill_id}:${item.version}`
}

function statusColor(value: string) {
  if (['active', 'ready', 'current', 'completed', 'enabled'].includes(value)) return 'success'
  if (['failed', 'blocked', 'digest_drift', 'interrupted', 'cancelled'].includes(value)) return 'error'
  if (['disabled', 'missing', 'archived'].includes(value)) return 'warning'
  return 'processing'
}

function skillKindLabel(t: ReturnType<typeof useTranslation>['t'], value: string | undefined) {
  if (value === 'native') return t('employees.nativeSkill')
  if (value === 'skill_md_adapter') return t('employees.adapterSkill')
  return value || t('employees.notFound')
}

function CopyValue({ value, compact = false }: { value: string; compact?: boolean }) {
  return (
    <Text
      className="antd-copy-value"
      code={!compact}
      copyable={{ text: value, tooltips: false }}
      ellipsis={{ tooltip: value }}
    >
      {value || '—'}
    </Text>
  )
}

interface SkillRow {
  key: string
  catalog: SkillCatalogItem | undefined
  binding: SkillBinding | undefined
  status: EmployeeSkillStatus | undefined
}

function buildSkillRows(
  catalog: SkillCatalogItem[],
  draft: SkillBinding[],
  statuses: EmployeeSkillStatus[],
) {
  const rows = new Map<string, SkillRow>()
  for (const item of catalog) {
    const key = skillIdentity(item)
    rows.set(key, {
      key,
      catalog: item,
      binding: draft.find((binding) => skillIdentity(binding) === key),
      status: statuses.find((status) => skillIdentity(status.binding) === key),
    })
  }
  for (const binding of draft) {
    const key = skillIdentity(binding)
    const current = rows.get(key)
    rows.set(key, {
      key,
      catalog: current?.catalog,
      binding,
      status: statuses.find((status) => skillIdentity(status.binding) === key),
    })
  }
  return [...rows.values()].sort((left, right) => {
    const a = left.catalog?.title ?? left.binding?.skill_id ?? ''
    const b = right.catalog?.title ?? right.binding?.skill_id ?? ''
    return a.localeCompare(b)
  })
}

export function EmployeeDetailPage() {
  const { employeeId } = useParams()
  const { t, i18n } = useTranslation()
  const { actions } = useUI()
  const connectivity = useConnectivity()
  const [searchParams, setSearchParams] = useSearchParams()
  const screens = Grid.useBreakpoint()
  const isMobile = !screens.md
  const requestedTab = searchParams.get('tab')
  const tab: Tab = TABS.includes(requestedTab as Tab) ? requestedTab as Tab : 'overview'
  const detailEpoch = useRef(0)
  const tabEpoch = useRef(0)
  const [record, setRecord] = useState<EmployeeRecord | null>(null)
  const [info, setInfo] = useState<Info | null>(null)
  const [notFound, setNotFound] = useState(false)
  const [busy, setBusy] = useState(false)
  const [dirty, setDirty] = useState(false)
  const [conflict, setConflict] = useState(false)
  const [skills, setSkills] = useState<Awaited<ReturnType<typeof getEmployeeSkills>> | null>(null)
  const [catalog, setCatalog] = useState<SkillCatalogItem[]>([])
  const [skillDraft, setSkillDraft] = useState<SkillBinding[]>([])
  const [skillConfiguration, setSkillConfiguration] = useState<Record<string, string>>({})
  const [skillErrors, setSkillErrors] = useState<Record<string, string>>({})
  const [skillQuery, setSkillQuery] = useState('')
  const [skillKindFilter, setSkillKindFilter] = useState<'all' | 'native' | 'skill_md_adapter'>('all')
  const [selectedSkillKey, setSelectedSkillKey] = useState<string | null>(null)
  const [knowledge, setKnowledge] = useState<EmployeeKnowledge | null>(null)
  const [knowledgeQuery, setKnowledgeQuery] = useState('')
  const [selectedCitation, setSelectedCitation] = useState<KnowledgeCitation | null>(null)
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
  const [projectDraft, setProjectDraft] = useState<ProjectBinding[]>([])
  const [projectEditMode, setProjectEditMode] = useState(false)
  const loadedEmployeeId = record?.employee.id
  const zh = i18n.resolvedLanguage?.startsWith('zh') ?? true

  const copy = zh ? {
    settings: '设置',
    archived: '该员工已归档。历史配置仍可审阅，所有修改操作均已禁用。',
    overview: '员工概览',
    overviewHint: '所有状态均来自当前服务端投影；未执行的 Dry Run 不会被推断为 Ready。',
    reload: '重新加载服务端版本',
    conflict: '保存时检测到 revision 冲突。当前输入仍保留，请先核对后重新加载。',
    unsaved: '存在未保存的 Employee 设置。离开后这些输入将丢失。',
    definition: '员工定义',
    policy: '权限、预算与并发',
    danger: '影响性操作',
    dangerHint: '生命周期操作与普通保存分离，并需要明确确认。',
    exactIdentity: 'Skill 身份由 skill_id + version + digest 共同确定。',
    capabilities: '能力交集',
    capabilityHint: 'Skill 只能收窄 Employee 权限，不能扩大权限。',
    inspect: '检查配置',
    addSource: '添加知识来源',
    citation: 'Citation 证据',
    provenance: '来源链',
    workspaceOnly: '仅当前 Service Workspace 可用；不会扫描 Home 或其他 Workspace。',
    openTask: '打开任务',
    noDryRun: '尚未执行 Dry Run',
    lastVerification: '最近 Verification',
    notProjected: '当前 Employee DTO 不提供持久化的最近 Verification 投影',
  } : {
    settings: 'Settings',
    archived: 'This Employee is archived. Historical configuration remains readable and every mutation is disabled.',
    overview: 'Employee overview',
    overviewHint: 'Every status comes from the current server projection; an unexecuted Dry Run is never inferred as Ready.',
    reload: 'Reload server revision',
    conflict: 'The save detected a revision conflict. Your input is preserved; review it before reloading.',
    unsaved: 'Employee settings contain unsaved input. Leaving will discard it.',
    definition: 'Employee definition',
    policy: 'Permissions, budget, and concurrency',
    danger: 'Impactful actions',
    dangerHint: 'Lifecycle actions are separated from ordinary save and always require confirmation.',
    exactIdentity: 'Skill identity is the exact skill_id + version + digest tuple.',
    capabilities: 'Capability intersection',
    capabilityHint: 'A Skill can only narrow Employee permissions; it can never expand them.',
    inspect: 'Inspect configuration',
    addSource: 'Add Knowledge source',
    citation: 'Citation evidence',
    provenance: 'Provenance',
    workspaceOnly: 'Only the current Service Workspace is available; Home and other workspaces are never scanned.',
    openTask: 'Open task',
    noDryRun: 'Dry Run has not been executed',
    lastVerification: 'Latest Verification',
    notProjected: 'The current Employee DTO does not expose a persisted latest Verification projection',
  }

  const refresh = useCallback(async (signal?: AbortSignal) => {
    if (!employeeId) return
    const epoch = ++detailEpoch.current
    const options = signal ? { signal } : {}
    setNotFound(false)
    void getInfo(options).then((value) => {
      if (epoch === detailEpoch.current) setInfo(value)
    }).catch(() => undefined)
    try {
      const value = await getEmployee(employeeId, options)
      if (epoch !== detailEpoch.current) return
      setRecord(value)
      setProjectDraft(value.project_bindings)
      setSkillDraft(value.employee.skill_bindings)
      setSkillConfiguration(Object.fromEntries(value.employee.skill_bindings.map((binding) => [
        skillIdentity(binding), JSON.stringify(binding.configuration, null, 2),
      ])))
      setDirty(false)
      setConflict(false)
    } catch (error) {
      if (epoch !== detailEpoch.current) return
      setNotFound(error instanceof ApiError && error.status === 404)
    }
  }, [employeeId])

  useEffect(() => {
    setRecord(null)
    setSkills(null)
    setKnowledge(null)
    setMemory(null)
    setLoops(null)
    setTasks(null)
    setActivity(null)
    setDryRun(null)
    setKnowledgeComposerOpen(false)
    setKnowledgeQuery('')
    setSkillQuery('')
    setSkillKindFilter('all')
    setSelectedSkillKey(null)
    setSelectedCitation(null)
    setBusy(false)
    setDirty(false)
    setConflict(false)
    setProjectEditMode(false)
  }, [employeeId])

  useEffect(() => {
    const controller = new AbortController()
    void refresh(controller.signal)
    return () => {
      detailEpoch.current += 1
      controller.abort()
    }
  }, [refresh, connectivity.generation])

  useEffect(() => {
    if (!dirty) return
    const beforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault()
      event.returnValue = copy.unsaved
    }
    const linkGuard = (event: MouseEvent) => {
      const target = event.target instanceof Element ? event.target.closest('a[href]') : null
      if (!target || !target.getAttribute('href')?.startsWith('/')) return
      if (!window.confirm(copy.unsaved)) event.preventDefault()
    }
    window.addEventListener('beforeunload', beforeUnload)
    document.addEventListener('click', linkGuard, true)
    return () => {
      window.removeEventListener('beforeunload', beforeUnload)
      document.removeEventListener('click', linkGuard, true)
    }
  }, [copy.unsaved, dirty])

  useEffect(() => {
    if (!employeeId || loadedEmployeeId !== employeeId || tab === 'settings' || tab === 'projects') return
    const controller = new AbortController()
    const epoch = ++tabEpoch.current
    const current = () => epoch === tabEpoch.current
    const options = { signal: controller.signal }
    if (tab === 'overview') {
      void getEmployeeSkills(employeeId, options).then((value) => { if (current()) setSkills(value) }).catch(() => undefined)
      void getEmployeeKnowledge(employeeId, options).then((value) => { if (current()) setKnowledge(value) }).catch(() => undefined)
      void listEmployeeTasks(employeeId, { limit: 100 }, options).then((value) => { if (current()) setTasks(value.tasks) }).catch(() => undefined)
    } else if (tab === 'skills') {
      void Promise.all([getEmployeeSkills(employeeId, options), listSkills(options)]).then(([projection, skillCatalog]) => {
        if (!current()) return
        setSkills(projection)
        setCatalog(skillCatalog.skills)
      }).catch(() => undefined)
    } else if (tab === 'knowledge') {
      void getEmployeeKnowledge(employeeId, options).then((value) => { if (current()) setKnowledge(value) }).catch(() => undefined)
    } else if (tab === 'memory') {
      void Promise.all([getEmployeeMemory(employeeId, options), getEmployeeMemoryCandidates(employeeId, options)])
        .then(([facts, candidates]) => {
          if (current()) setMemory({ facts: facts.facts, candidates: candidates.candidates })
        }).catch(() => undefined)
    } else if (tab === 'loops') {
      void listLoops(options).then((value) => {
        if (current()) setLoops(value.loops.filter((item) => item.employee_id === employeeId))
      }).catch(() => undefined)
    } else if (tab === 'tasks') {
      void listEmployeeTasks(employeeId, { limit: 100 }, options).then((value) => {
        if (current()) setTasks(value.tasks)
      }).catch(() => undefined)
    } else if (tab === 'activity') {
      void getEmployeeActivity(employeeId, { limit: 100 }, options).then((value) => {
        if (current()) setActivity(value)
      }).catch(() => undefined)
    }
    return () => {
      tabEpoch.current += 1
      controller.abort()
    }
  }, [employeeId, loadedEmployeeId, tab])

  async function mutate<T>(
    action: (signal: AbortSignal) => Promise<T>,
    onSuccess?: (value: T) => void,
    refreshAfter = true,
  ) {
    if (!connectivity.canMutate) return
    const epoch = detailEpoch.current
    const owner = employeeId
    const controller = new AbortController()
    setBusy(true)
    try {
      const value = await action(controller.signal)
      if (epoch !== detailEpoch.current || owner !== employeeId) return
      onSuccess?.(value)
      actions.showToast({ messageKey: 'toast.saved', tone: 'success' })
      if (refreshAfter) await refresh()
    } catch (error) {
      if (epoch !== detailEpoch.current || owner !== employeeId) return
      actions.showToast({ messageKey: errorKey(error), tone: 'error' })
    } finally {
      if (owner === employeeId) setBusy(false)
    }
  }

  if (notFound) return <ErrorState title={t('employees.notFound')} description={t('employees.notFoundDescription')} />
  if (!record) return <Skeleton active paragraph={{ rows: 8 }} />

  const employeeRecord = record
  const employee = employeeRecord.employee
  const archived = employee.state === 'archived'
  const active = employee.state === 'active'
  const canMutate = connectivity.canMutate && !archived && !busy
  const companies = info?.available_companies ?? []
  const company = companies.find((item) => item.id === employee.default_selection.company)
  const access = company?.access.find((item) => item.id === employee.default_selection.access)
  const model = access?.models.find((item) => item.id === employee.default_selection.model)
  const skillRows = buildSkillRows(catalog, skillDraft, skills?.bindings ?? [])
  const filteredSkills = skillRows.filter((row) => {
    const item = row.catalog
    if (skillKindFilter !== 'all' && item?.kind !== skillKindFilter) return false
    const query = skillQuery.trim().toLocaleLowerCase()
    if (!query) return true
    return `${item?.title ?? ''} ${row.binding?.skill_id ?? item?.skill_id ?? ''} ${item?.description ?? ''} ${row.binding?.version ?? item?.version ?? ''}`
      .toLocaleLowerCase().includes(query)
  })
  const selectedSkillRow = skillRows.find((row) => row.key === selectedSkillKey) ?? null
  const currentTasks = tasks ?? []
  const activeTask = currentTasks.find((item) => !TERMINAL_TASKS.has(item.state))
  const skillReady = skills !== null && skills.bindings.every((item) => item.status === 'current')
  const knowledgeReady = knowledge !== null && knowledge.sources.every((item) => item.status === 'ready')
  const readinessChecks = [
    Boolean(model),
    employeeRecord.project_bindings.length > 0,
    skillReady,
    knowledgeReady,
    dryRun?.ready === true,
  ]
  const readinessPercent = Math.round((readinessChecks.filter(Boolean).length / readinessChecks.length) * 100)
  const citations = knowledge?.indexes.flatMap((index) => index.documents.flatMap((document) => document.citations)) ?? []
  const filteredKnowledge = knowledge?.sources.filter((source) => {
    const query = knowledgeQuery.trim().toLocaleLowerCase()
    return !query || `${source.title} ${source.id} ${source.relative_path ?? ''}`.toLocaleLowerCase().includes(query)
  }) ?? []

  function patchEmployee(value: Partial<typeof employee>) {
    setRecord({ ...employeeRecord, employee: { ...employee, ...value } })
    setDirty(true)
    setConflict(false)
  }

  async function saveEmployee() {
    if (!canMutate) return
    const owner = employee.id
    const epoch = detailEpoch.current
    setBusy(true)
    setConflict(false)
    try {
      const updated = await updateEmployee(employee.id, {
        expected_revision: employee.revision,
        employee,
        project_bindings: employeeRecord.project_bindings,
      })
      if (owner !== employeeId || epoch !== detailEpoch.current) return
      setRecord(updated)
      setProjectDraft(updated.project_bindings)
      setDirty(false)
      actions.showToast({ messageKey: 'toast.saved', tone: 'success' })
    } catch (error) {
      if (owner !== employeeId || epoch !== detailEpoch.current) return
      setConflict(error instanceof ApiError && error.status === 409)
      actions.showToast({ messageKey: errorKey(error), tone: 'error' })
    } finally {
      if (owner === employeeId) setBusy(false)
    }
  }

  function lifecycle(action: 'disable' | 'enable' | 'archive') {
    actions.openDialog({
      titleKey: `employees.${action}Title`,
      descriptionKey: `employees.${action}Description`,
      confirmKey: `employees.${action}`,
      tone: action === 'archive' ? 'warning' : 'info',
      onConfirm: () => void mutate(
        (signal) => mutateEmployeeLifecycle(employee.id, action, employee.revision, { signal }),
        (value) => {
          setRecord(value)
          setDirty(false)
        },
        false,
      ),
    })
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

  function reloadMemory(signal: AbortSignal) {
    return Promise.all([getEmployeeMemory(employee.id, { signal }), getEmployeeMemoryCandidates(employee.id, { signal })])
      .then(([facts, candidates]) => ({ facts: facts.facts, candidates: candidates.candidates }))
  }

  function confirmMemory(action: 'reject' | 'forget', id: string) {
    actions.openDialog({
      titleKey: action === 'reject' ? 'employees.rejectMemoryTitle' : 'employees.forgetMemoryTitle',
      descriptionKey: action === 'reject' ? 'employees.rejectMemoryDescription' : 'employees.forgetMemoryDescription',
      confirmKey: action === 'reject' ? 'employees.reject' : 'employees.forget',
      tone: 'warning',
      onConfirm: () => void mutate(async (signal) => {
        if (action === 'reject') await rejectEmployeeMemoryCandidate(employee.id, id, { signal })
        else await forgetEmployeeMemory(employee.id, id, { signal })
        return reloadMemory(signal)
      }, setMemory, false),
    })
  }

  function selectTab(next: string) {
    const value = TABS.includes(next as Tab) ? next as Tab : 'overview'
    setSearchParams(value === 'overview' ? {} : { tab: value })
  }

  const tabItems = TABS.map((value) => ({
    key: value,
    label: value === 'settings' ? copy.settings : t(`employees.tabs.${value}`),
  }))

  const skillColumns: TableColumnsType<SkillRow> = [
    {
      title: 'Skill',
      key: 'skill',
      render: (_, row) => (
        <Space direction="vertical" size={0}>
          <Text strong>{row.catalog?.title ?? row.binding?.skill_id}</Text>
          <Text type="secondary">{row.binding?.skill_id ?? row.catalog?.skill_id}@{row.binding?.version ?? row.catalog?.version}</Text>
        </Space>
      ),
    },
    { title: t('employees.skillKind'), key: 'kind', render: (_, row) => <Tag>{skillKindLabel(t, row.catalog?.kind ?? row.status?.kind)}</Tag> },
    { title: 'Digest', key: 'digest', render: (_, row) => <CopyValue compact value={row.binding?.digest ?? row.catalog?.digest ?? ''} /> },
    { title: t('employees.bindingStatus'), key: 'status', render: (_, row) => { const value = row.status?.status ?? (row.binding ? 'current' : 'missing'); return <Tag color={statusColor(value)}>{row.binding ? translatedEnum(t, 'skillStatus', value) : t('employees.notBound')}</Tag> } },
    {
      title: t('actions.actions'),
      key: 'actions',
      render: (_, row) => (
        <Space wrap>
          <Button onClick={() => setSelectedSkillKey(row.key)}>{copy.inspect}</Button>
          {row.binding ? (
            <Button aria-label={`Remove Skill ${row.binding.skill_id}@${row.binding.version}`} disabled={!canMutate} onClick={() => setSkillDraft((value) => value.filter((item) => skillIdentity(item) !== row.key))}>{t('employees.removeSkill')}</Button>
          ) : row.catalog ? (
            <Button disabled={!canMutate} onClick={() => {
              setSkillDraft((value) => [...value, { skill_id: row.catalog!.skill_id, version: row.catalog!.version, digest: row.catalog!.digest, configuration: {}, enabled: true }])
              setSkillConfiguration((value) => ({ ...value, [row.key]: '{}' }))
              setSelectedSkillKey(row.key)
            }}>{t('employees.bindSkill')}</Button>
          ) : null}
        </Space>
      ),
    },
  ]

  const taskColumns: TableColumnsType<EmployeeTask> = [
    { title: t('tasks.prompt'), dataIndex: 'prompt', key: 'prompt', render: (value: string, task) => <Link className="task-prompt-link" to={`/tasks/${encodeURIComponent(task.id)}`}><Typography.Paragraph className="task-prompt-text" ellipsis={{ rows: 3, expandable: true }}>{value}</Typography.Paragraph></Link> },
    { title: t('tasks.state'), dataIndex: 'state', key: 'state', render: (value: string) => <Tag color={statusColor(value)}>{translatedEnum(t, 'taskStatus', value)}</Tag> },
    { title: t('tasks.project'), key: 'project', render: (_, task) => <Text>{task.project_binding.label}</Text> },
    { title: 'Session / Run', key: 'session', render: (_, task) => <Space direction="vertical" size={0}><CopyValue compact value={task.session_id ?? ''} /><CopyValue compact value={task.run_id ?? ''} /></Space> },
    { title: t('tasks.updated'), dataIndex: 'updated_at', key: 'updated' },
    { title: t('actions.actions'), key: 'action', render: (_, task) => <Button><Link to={`/tasks/${encodeURIComponent(task.id)}`}>{copy.openTask}</Link></Button> },
  ]

  const lifecycleMenu: MenuProps['items'] = [
    employee.state === 'active' ? { key: 'disable', label: t('employees.disable') } : { key: 'enable', label: t('employees.enable') },
    { type: 'divider' },
    { key: 'archive', danger: true, label: t('employees.archive') },
  ]

  return (
    <article className="feature-page employee-detail-page antd-deep-page">
      <Card className="employee-identity-card" variant="borderless">
        <Flex justify="space-between" align="flex-start" gap={16} wrap>
          <Space align="start" size={16}>
            <Badge status={active ? 'success' : archived ? 'default' : 'warning'}>
              <div className="employee-avatar" aria-hidden="true">{employee.avatar.value.trim() || employee.name.trim().slice(0, 2).toUpperCase()}</div>
            </Badge>
            <div className="employee-identity-copy">
              <Title level={2}>{employee.name}</Title>
              <Paragraph type="secondary">{employee.job_title}</Paragraph>
              <Space wrap>
                <CopyValue compact value={employee.id} />
                <Tag>rev {employee.revision}</Tag>
                <Tag color={statusColor(employee.state)} data-testid="employee-status">{translatedEnum(t, 'employeeStatus', employee.state)}</Tag>
              </Space>
            </div>
          </Space>
          {!archived ? (
            isMobile ? (
              <Dropdown
                trigger={['click']}
                menu={{ items: lifecycleMenu, onClick: ({ key }) => lifecycle(key as 'disable' | 'enable' | 'archive') }}
              >
                <Button aria-label={t('actions.actions')} icon={<MoreHorizontal size={18} aria-hidden="true" />} />
              </Dropdown>
            ) : (
              <Space wrap>
                <Button onClick={() => selectTab('settings')} icon={<Pencil size={16} aria-hidden="true" />}>{copy.settings}</Button>
                <Button loading={busy} onClick={() => void mutate((signal) => dryRunEmployee(employee.id, { signal }), setDryRun, false)}>{t('employees.dryRun')}</Button>
              </Space>
            )
          ) : null}
        </Flex>
      </Card>

      {archived ? <Alert type="warning" showIcon message={t('employees.archivedReadOnly')} description={copy.archived} /> : null}
      {!connectivity.canMutate ? <Alert type="warning" showIcon message={t('mutation.offline')} /> : null}

      {!screens.lg ? <Select
        className="employee-mobile-tab-select"
        aria-label={t('employees.tabNavigation')}
        value={tab}
        options={tabItems.map((item) => ({ value: item.key, label: item.label }))}
        onChange={selectTab}
      /> : null}
      <Tabs
        className="employee-antd-tabs"
        activeKey={tab}
        items={tabItems}
        onChange={selectTab}
        more={{ icon: <MoreHorizontal aria-label={t('actions.more')} size={18} /> }}
      />

      {tab === 'overview' ? (
        <Space direction="vertical" size={16} className="antd-page-stack">
          <Flex justify="space-between" align="flex-start" wrap gap={16}>
            <div><Title level={3}>{copy.overview}</Title><Paragraph type="secondary">{copy.overviewHint}</Paragraph></div>
            {!archived ? <Button type="primary"><Link to="/tasks">{t('tasks.create')}</Link></Button> : null}
          </Flex>
          <Row gutter={[16, 16]}>
            <Col xs={24} sm={12} lg={6}><Card><Statistic title={t('employees.revision')} value={employee.revision} prefix="r" /></Card></Col>
            <Col xs={24} sm={12} lg={6}><Card><Statistic title={t('employees.maxRunningTasks')} value={employee.concurrency_policy.max_running_tasks} /></Card></Col>
            <Col xs={24} sm={12} lg={6}><Card><Statistic title={t('employees.tabs.projects')} value={employeeRecord.project_bindings.length} /></Card></Col>
            <Col xs={24} sm={12} lg={6}><Card><Statistic title={t('employees.tabs.skills')} value={employee.skill_bindings.length} /></Card></Col>
          </Row>
          <Row gutter={[16, 16]}>
            <Col xs={24} lg={16}>
              <Card title={t('employees.charter')}>
                <Paragraph>{employee.charter || '—'}</Paragraph>
                <Row gutter={[16, 16]}>
                  <Col xs={24} md={12}><Title level={5}>{t('employees.responsibilities')}</Title><List size="small" dataSource={employee.responsibilities} locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} /> }} renderItem={(item) => <List.Item>{item}</List.Item>} /></Col>
                  <Col xs={24} md={12}><Title level={5}>{t('employees.behaviorBoundaries')}</Title><List size="small" dataSource={employee.behavior_boundaries} locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} /> }} renderItem={(item) => <List.Item>{item}</List.Item>} /></Col>
                </Row>
              </Card>
            </Col>
            <Col xs={24} lg={8}>
              <Card title={t('employees.readiness')}>
                <Progress percent={readinessPercent} status={dryRun?.ready === false ? 'exception' : 'active'} />
                <List
                  size="small"
                  dataSource={[
                    { label: t('employees.defaultModel'), ready: Boolean(model), detail: model?.label ?? employee.default_selection.model },
                    { label: t('employees.tabs.projects'), ready: employeeRecord.project_bindings.length > 0, detail: `${employeeRecord.project_bindings.length}` },
                    { label: t('employees.tabs.skills'), ready: skillReady, detail: skills ? `${skills.bindings.filter((item) => item.status === 'current').length}/${skills.bindings.length}` : t('common.loading') },
                    { label: t('employees.tabs.knowledge'), ready: knowledgeReady, detail: knowledge ? `${knowledge.sources.filter((item) => item.status === 'ready').length}/${knowledge.sources.length}` : t('common.loading') },
                    { label: 'Dry Run', ready: dryRun?.ready === true, detail: dryRun ? `${dryRun.checks.filter((item) => item.ready).length}/${dryRun.checks.length}` : copy.noDryRun },
                  ]}
                  renderItem={(item) => <List.Item><List.Item.Meta avatar={item.ready ? <Check color="var(--color-success)" size={18} /> : <ShieldCheck color="var(--color-warning)" size={18} />} title={item.label} description={item.detail} /></List.Item>}
                />
                {!archived ? <Button block type="primary" loading={busy} onClick={() => void mutate((signal) => dryRunEmployee(employee.id, { signal }), setDryRun, false)}>{t('employees.dryRun')}</Button> : null}
              </Card>
            </Col>
          </Row>
          <Row gutter={[16, 16]}>
            <Col xs={24} md={12}>
              <Card title={t('employees.defaultModel')}>
                <Descriptions column={1} size="small">
                  <Descriptions.Item label={t('employees.company')}>{company?.label ?? employee.default_selection.company}</Descriptions.Item>
                  <Descriptions.Item label={t('employees.access')}>{access?.label ?? employee.default_selection.access}</Descriptions.Item>
                  <Descriptions.Item label={t('employees.model')}>{model?.label ?? employee.default_selection.model}</Descriptions.Item>
                  <Descriptions.Item label={t('employees.agent')}>{employee.agent_profile}</Descriptions.Item>
                </Descriptions>
              </Card>
            </Col>
            <Col xs={24} md={12}>
              <Card title={t('tasks.activity')}>
                {activeTask ? <Descriptions column={1} size="small"><Descriptions.Item label={t('tasks.state')}><Tag color={statusColor(activeTask.state)}>{translatedEnum(t, 'taskStatus', activeTask.state)}</Tag></Descriptions.Item><Descriptions.Item label="Task"><Link to={`/tasks/${encodeURIComponent(activeTask.id)}`}>{activeTask.prompt}</Link></Descriptions.Item></Descriptions> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('employees.noTasks')} />}
                <Alert className="employee-projection-note" type="info" showIcon message={copy.lastVerification} description={copy.notProjected} />
              </Card>
            </Col>
          </Row>
        </Space>
      ) : null}

      {tab === 'settings' ? (
        <Space direction="vertical" size={16} className="antd-page-stack employee-settings-page">
          {dirty ? <Alert type="warning" showIcon message={copy.unsaved} /> : null}
          {conflict ? <Alert type="error" showIcon message={copy.conflict} action={<Button onClick={() => void refresh()}>{copy.reload}</Button>} /> : null}
          <Form layout="vertical" requiredMark disabled={archived} onFinish={() => void saveEmployee()}>
            <Card title={copy.definition}>
              <Row gutter={[16, 0]}>
                <Col xs={24} md={12}><Form.Item label={t('employees.name')} required><Input aria-label={t('employees.name')} value={employee.name} autoComplete="name" onChange={(event) => patchEmployee({ name: event.target.value })} /></Form.Item></Col>
                <Col xs={24} md={12}><Form.Item label={t('employees.jobTitle')} required><Input aria-label={t('employees.jobTitle')} value={employee.job_title} autoComplete="organization-title" onChange={(event) => patchEmployee({ job_title: event.target.value })} /></Form.Item></Col>
                <Col xs={24}><Form.Item label={t('employees.charter')} required><Input.TextArea aria-label={t('employees.charter')} autoSize={{ minRows: 3, maxRows: 8 }} value={employee.charter} onChange={(event) => patchEmployee({ charter: event.target.value })} /></Form.Item></Col>
                <Col xs={24} md={8}><Form.Item label={t('employees.avatar')}><Select aria-label={t('employees.avatar')} value={employee.avatar.kind} options={[{ value: 'initials', label: t('employees.initials') }, { value: 'emoji', label: t('employees.emoji') }]} onChange={(value) => patchEmployee({ avatar: { kind: value, value: '' } })} /></Form.Item></Col>
                <Col xs={24} md={16}><Form.Item label={t('employees.avatarValue')}><Input aria-label={t('employees.avatarValue')} value={employee.avatar.value} onChange={(event) => patchEmployee({ avatar: { ...employee.avatar, value: event.target.value } })} /></Form.Item></Col>
                <Col xs={24} md={12}><Form.Item label={t('employees.responsibilities')}><Input.TextArea aria-label={t('employees.responsibilities')} autoSize={{ minRows: 4, maxRows: 10 }} value={employee.responsibilities.join('\n')} onChange={(event) => patchEmployee({ responsibilities: lines(event.target.value) })} /></Form.Item></Col>
                <Col xs={24} md={12}><Form.Item label={t('employees.behaviorBoundaries')}><Input.TextArea aria-label={t('employees.behaviorBoundaries')} autoSize={{ minRows: 4, maxRows: 10 }} value={employee.behavior_boundaries.join('\n')} onChange={(event) => patchEmployee({ behavior_boundaries: lines(event.target.value) })} /></Form.Item></Col>
              </Row>
            </Card>
            <Card title={t('employees.steps.modelAgent')}>
              <Row gutter={[16, 0]}>
                <Col xs={24} md={12}><Form.Item label={t('employees.company')} required><Select aria-label={t('employees.company')} value={employee.default_selection.company} options={companies.map((item) => ({ value: item.id, label: item.label }))} onChange={(value) => { const nextCompany = companies.find((item) => item.id === value); const nextAccess = nextCompany?.access.find((item) => item.supported) ?? nextCompany?.access[0]; patchEmployee({ default_selection: { company: value, access: nextAccess?.id ?? '', model: nextAccess?.models[0]?.id ?? '' } }) }} /></Form.Item></Col>
                <Col xs={24} md={12}><Form.Item label={t('employees.access')} required><Select aria-label={t('employees.access')} value={employee.default_selection.access} options={company?.access.map((item) => ({ value: item.id, label: item.label, disabled: !item.supported })) ?? []} onChange={(value) => { const next = company?.access.find((item) => item.id === value); patchEmployee({ default_selection: { ...employee.default_selection, access: value, model: next?.models[0]?.id ?? '' } }) }} /></Form.Item></Col>
                <Col xs={24} md={12}><Form.Item label={t('employees.model')} required><Select aria-label={t('employees.model')} value={employee.default_selection.model} options={access?.models.map((item) => ({ value: item.id, label: item.label })) ?? []} onChange={(value) => patchEmployee({ default_selection: { ...employee.default_selection, model: value } })} /></Form.Item></Col>
                <Col xs={24} md={12}><Form.Item label={t('employees.agent')} required><Select aria-label={t('employees.agent')} value={employee.agent_profile} options={info?.agents.map((item) => ({ value: item.id, label: item.label })) ?? []} onChange={(value) => patchEmployee({ agent_profile: value })} /></Form.Item></Col>
              </Row>
              {!model ? <Alert type="error" showIcon message={t('employees.modelUnavailable')} /> : null}
            </Card>
            <Card title={copy.policy}>
              <Row gutter={[16, 0]}>
                <Col xs={24}><Form.Item label={t('employees.capabilities')} required><Input.TextArea aria-label={t('employees.capabilities')} autoSize={{ minRows: 3, maxRows: 8 }} value={employee.permission_policy.allowed_capabilities.join('\n')} onChange={(event) => patchEmployee({ permission_policy: { ...employee.permission_policy, allowed_capabilities: lines(event.target.value) } })} /></Form.Item></Col>
                <Col xs={24}><Form.Item label={t('employees.network')}><Switch aria-label={t('employees.network')} checked={employee.permission_policy.network_allowed} onChange={(checked) => patchEmployee({ permission_policy: { ...employee.permission_policy, network_allowed: checked } })} /></Form.Item></Col>
                <Col xs={24} sm={12} lg={6}><Form.Item label={t('employees.maxCalls')} required><InputNumber aria-label={t('employees.maxCalls')} min={1} inputMode="numeric" value={employee.budget_policy.max_model_calls} onChange={(value) => patchEmployee({ budget_policy: { ...employee.budget_policy, max_model_calls: value ?? 1 } })} /></Form.Item></Col>
                <Col xs={24} sm={12} lg={6}><Form.Item label={t('employees.maxTokens')} required><InputNumber aria-label={t('employees.maxTokens')} min={1} inputMode="numeric" value={employee.budget_policy.max_tokens} onChange={(value) => patchEmployee({ budget_policy: { ...employee.budget_policy, max_tokens: value ?? 1 } })} /></Form.Item></Col>
                <Col xs={24} sm={12} lg={6}><Form.Item label={t('employees.timeoutSeconds')} required><InputNumber aria-label={t('employees.timeoutSeconds')} min={1} inputMode="numeric" value={employee.budget_policy.timeout_seconds} onChange={(value) => patchEmployee({ budget_policy: { ...employee.budget_policy, timeout_seconds: value ?? 1 } })} /></Form.Item></Col>
                <Col xs={24} sm={12} lg={6}><Form.Item label={t('employees.maxRunningTasks')} required><InputNumber aria-label={t('employees.maxRunningTasks')} min={1} inputMode="numeric" value={employee.concurrency_policy.max_running_tasks} onChange={(value) => patchEmployee({ concurrency_policy: { max_running_tasks: value ?? 1 } })} /></Form.Item></Col>
                <Col xs={24} md={8}><Form.Item label={t('employees.memoryCandidates')}><Switch aria-label={t('employees.memoryCandidates')} checked={employee.memory_policy.candidate_generation} onChange={(checked) => patchEmployee({ memory_policy: { ...employee.memory_policy, candidate_generation: checked, promotion: checked ? 'owner_confirmation' : 'disabled' } })} /></Form.Item></Col>
                <Col xs={24} md={8}><Form.Item label={t('employees.maxContextFacts')}><InputNumber aria-label={t('employees.maxContextFacts')} min={0} inputMode="numeric" value={employee.memory_policy.max_context_facts} onChange={(value) => patchEmployee({ memory_policy: { ...employee.memory_policy, max_context_facts: value ?? 0 } })} /></Form.Item></Col>
                <Col xs={24} md={8}><Form.Item label={t('employees.maxContextBytes')}><InputNumber aria-label={t('employees.maxContextBytes')} min={0} inputMode="numeric" value={employee.memory_policy.max_context_bytes} onChange={(value) => patchEmployee({ memory_policy: { ...employee.memory_policy, max_context_bytes: value ?? 0 } })} /></Form.Item></Col>
              </Row>
            </Card>
            {!archived ? <div className="antd-mobile-action-bar"><Button htmlType="submit" type="primary" block={isMobile} loading={busy} disabled={!connectivity.canMutate}>{t('employees.save')}</Button></div> : null}
          </Form>
          {!archived ? <Card title={copy.danger}><Paragraph type="secondary">{copy.dangerHint}</Paragraph><Space direction={isMobile ? 'vertical' : 'horizontal'} className="antd-impact-actions"><Button block={isMobile} disabled={!canMutate} onClick={() => lifecycle(active ? 'disable' : 'enable')}>{active ? t('employees.disable') : t('employees.enable')}</Button><Button block={isMobile} danger disabled={!canMutate} onClick={() => lifecycle('archive')}>{t('employees.archive')}</Button></Space></Card> : null}
        </Space>
      ) : null}

      {tab === 'skills' ? (
        <Space direction="vertical" size={16} className="antd-page-stack employee-skills-page">
          <Alert type="info" showIcon message={copy.exactIdentity} description={copy.capabilityHint} />
          <Flex gap={12} wrap>
            <Input.Search allowClear aria-label={t('employees.searchSkills')} placeholder={t('employees.searchSkills')} value={skillQuery} onChange={(event) => setSkillQuery(event.target.value)} />
            <Select aria-label={t('employees.skillKind')} value={skillKindFilter} onChange={setSkillKindFilter} options={[{ value: 'all', label: t('employees.all') }, { value: 'native', label: 'Native' }, { value: 'skill_md_adapter', label: 'SKILL.md Adapter' }]} />
          </Flex>
          {skills === null ? <Skeleton active /> : isMobile ? (
            <List dataSource={filteredSkills} locale={{ emptyText: <Empty /> }} renderItem={(row) => (
              <List.Item>
                <Card data-testid={`skill-card-${row.binding?.skill_id ?? row.catalog?.skill_id}`} className="mobile-resource-card" title={row.catalog?.title ?? row.binding?.skill_id} extra={(() => { const value = row.status?.status ?? (row.binding ? 'current' : 'missing'); return <Tag color={statusColor(value)}>{row.binding ? translatedEnum(t, 'skillStatus', value) : t('employees.notBound')}</Tag> })()}>
                  <Descriptions column={1} size="small"><Descriptions.Item label="ID">{row.binding?.skill_id ?? row.catalog?.skill_id}</Descriptions.Item><Descriptions.Item label={t('employees.version')}>{row.binding?.version ?? row.catalog?.version}</Descriptions.Item><Descriptions.Item label="Digest"><CopyValue value={row.binding?.digest ?? row.catalog?.digest ?? ''} /></Descriptions.Item><Descriptions.Item label={t('employees.skillKind')}>{skillKindLabel(t, row.catalog?.kind ?? row.status?.kind)}</Descriptions.Item></Descriptions>
                  <Space direction="vertical" className="mobile-card-actions"><Button block onClick={() => setSelectedSkillKey(row.key)}>{copy.inspect}</Button>{row.binding ? <Button aria-label={`Remove Skill ${row.binding.skill_id}@${row.binding.version}`} block disabled={!canMutate} onClick={() => setSkillDraft((value) => value.filter((item) => skillIdentity(item) !== row.key))}>{t('employees.removeSkill')}</Button> : row.catalog ? <Button block type="primary" disabled={!canMutate} onClick={() => { setSkillDraft((value) => [...value, { skill_id: row.catalog!.skill_id, version: row.catalog!.version, digest: row.catalog!.digest, configuration: {}, enabled: true }]); setSkillConfiguration((value) => ({ ...value, [row.key]: '{}' })); setSelectedSkillKey(row.key) }}>{t('employees.bindSkill')}</Button> : null}</Space>
                </Card>
              </List.Item>
            )} />
          ) : <Table className="skill-table" rowKey="key" columns={skillColumns} dataSource={filteredSkills} pagination={false} />}
          {skills?.bindings.filter((item) => item.status === 'digest_drift').map((item) => {
            const current = catalog.find((candidate) => skillVersion(candidate) === skillVersion(item.binding))
            return current ? <Alert key={skillIdentity(item.binding)} type="warning" showIcon message={t('employees.staleDigest')} description={<Space wrap><CopyValue value={item.binding.digest} /><Button aria-label={`Upgrade to current digest ${item.binding.skill_id}@${item.binding.version}`} disabled={!canMutate} onClick={() => setSkillDraft((value) => value.map((binding) => skillIdentity(binding) === skillIdentity(item.binding) ? { ...binding, digest: current.digest } : binding))}>{t('employees.upgradeSkill')}</Button></Space>} /> : null
          })}
          <Drawer title={copy.inspect} open={selectedSkillRow !== null} width={isMobile ? '100%' : 560} onClose={() => setSelectedSkillKey(null)} destroyOnClose={false}>
            {selectedSkillRow ? (
              <Space direction="vertical" size={16} className="antd-page-stack">
                <Descriptions column={1} bordered size="small"><Descriptions.Item label="Skill">{selectedSkillRow.catalog?.title ?? selectedSkillRow.binding?.skill_id}</Descriptions.Item><Descriptions.Item label="Identity"><CopyValue value={selectedSkillRow.key} /></Descriptions.Item><Descriptions.Item label={t('employees.bindingStatus')}>{(() => { const value = selectedSkillRow.status?.status ?? (selectedSkillRow.binding ? 'current' : 'missing'); return <Tag color={statusColor(value)}>{selectedSkillRow.binding ? translatedEnum(t, 'skillStatus', value) : t('employees.notBound')}</Tag> })()}</Descriptions.Item></Descriptions>
                {selectedSkillRow.catalog ? <Card size="small" title={copy.capabilities}><List size="small" dataSource={selectedSkillRow.catalog.requested_capabilities} renderItem={(capability) => <List.Item><Space><Checkbox checked={employee.permission_policy.allowed_capabilities.includes(capability)} disabled /><Text>{capability}</Text></Space></List.Item>} /><Paragraph type="secondary">{copy.capabilityHint}</Paragraph></Card> : null}
                {selectedSkillRow.binding ? <Switch aria-label={`Enabled ${selectedSkillRow.catalog?.title ?? selectedSkillRow.binding.skill_id}`} checked={selectedSkillRow.binding.enabled} disabled={!canMutate} checkedChildren={t('bindingStatus.enabled')} unCheckedChildren={t('bindingStatus.disabled')} onChange={(enabled) => setSkillDraft((value) => value.map((binding) => skillIdentity(binding) === selectedSkillRow.key ? { ...binding, enabled } : binding))} /> : null}
                {selectedSkillRow.binding && selectedSkillRow.catalog?.kind === 'native' ? <Form layout="vertical"><Form.Item label={t('employees.configurationJSON')} {...(skillErrors[selectedSkillRow.key] ? { validateStatus: 'error' as const, help: skillErrors[selectedSkillRow.key] } : {})}><Input.TextArea aria-label={`Configuration JSON ${selectedSkillRow.catalog.title}`} className="json-editor" autoSize={{ minRows: 10, maxRows: 24 }} value={skillConfiguration[selectedSkillRow.key] ?? JSON.stringify(selectedSkillRow.binding.configuration, null, 2)} onChange={(event) => { setSkillConfiguration((value) => ({ ...value, [selectedSkillRow.key]: event.target.value })); setSkillErrors((value) => ({ ...value, [selectedSkillRow.key]: '' })) }} /></Form.Item></Form> : null}
                {selectedSkillRow.catalog?.kind === 'skill_md_adapter' ? <Alert type="info" showIcon message={t('employees.adapterZeroCapability')} description={copy.capabilityHint} /> : null}
              </Space>
            ) : null}
          </Drawer>
          {!archived ? <div className="antd-mobile-action-bar"><Button type="primary" block={isMobile} loading={busy} disabled={!canMutate} onClick={() => void (async () => {
            const nextErrors: Record<string, string> = {}
            const parsed = skillDraft.map((binding) => {
              const row = skillRows.find((item) => item.binding && skillIdentity(item.binding) === skillIdentity(binding))
              if (row?.catalog?.kind === 'skill_md_adapter') return { ...binding, configuration: {} }
              try {
                const value = JSON.parse(skillConfiguration[skillIdentity(binding)] ?? JSON.stringify(binding.configuration)) as unknown
                if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('configuration must be a JSON object')
                return { ...binding, configuration: value as Record<string, unknown> }
              } catch {
                nextErrors[skillIdentity(binding)] = t('employees.invalidConfiguration')
                return binding
              }
            })
            setSkillErrors(nextErrors)
            if (Object.keys(nextErrors).length > 0) {
              setSelectedSkillKey(Object.keys(nextErrors)[0] ?? null)
              return
            }
            await mutate((signal) => updateEmployeeSkills(employee.id, employee.revision, parsed, { signal }), (value) => { setRecord(value); setSkillDraft(value.employee.skill_bindings) }, false)
            const projection = await getEmployeeSkills(employee.id)
            setSkills(projection)
          })()}>{t('employees.saveSkills')}</Button></div> : null}
        </Space>
      ) : null}

      {tab === 'knowledge' ? (
        <Space direction="vertical" size={16} className="antd-page-stack employee-knowledge-page">
          <Flex justify="space-between" align="center" gap={12} wrap><Input.Search allowClear aria-label={t('actions.search')} placeholder={t('actions.search')} value={knowledgeQuery} onChange={(event) => setKnowledgeQuery(event.target.value)} />{!archived ? <Button type="primary" icon={<Database size={16} />} onClick={() => setKnowledgeComposerOpen(true)}>{copy.addSource}</Button> : null}</Flex>
          {knowledge === null ? <Skeleton active /> : isMobile ? <List dataSource={filteredKnowledge} locale={{ emptyText: <Empty /> }} renderItem={(source) => <List.Item><KnowledgeCard source={source} citations={citations.filter((item) => item.source_id === source.id)} canMutate={canMutate} onCitation={setSelectedCitation} onRefresh={() => void mutate((signal) => refreshEmployeeKnowledge(employee.id, source.id, { signal }), setKnowledge, false)} onDelete={() => confirmKnowledgeDelete(source.id)} t={t} /></List.Item>} /> : <Table rowKey="id" pagination={false} dataSource={filteredKnowledge} columns={[
            { title: t('employees.sourceTitle'), dataIndex: 'title', key: 'title' },
            { title: t('employees.knowledgeKind'), dataIndex: 'kind', key: 'kind', render: (value: string) => <Tag>{translatedEnum(t, 'knowledgeKind', value)}</Tag> },
            { title: t('employees.sourceDigest'), dataIndex: 'digest', key: 'digest', render: (value: string) => <CopyValue compact value={value} /> },
            { title: t('employees.citations'), key: 'citations', render: (_: unknown, source: KnowledgeSource) => <Button onClick={() => setSelectedCitation(citations.find((item) => item.source_id === source.id) ?? null)}>{citations.filter((item) => item.source_id === source.id).length}</Button> },
            { title: t('employees.bindingStatus'), dataIndex: 'status', key: 'status', render: (value: string, source: KnowledgeSource) => <Tooltip title={source.error}><Tag color={statusColor(value)}>{translatedEnum(t, 'knowledgeStatus', value)}</Tag></Tooltip> },
            { title: t('actions.actions'), key: 'actions', render: (_: unknown, source: KnowledgeSource) => <Dropdown trigger={['click']} menu={{ items: [{ key: 'refresh', label: t('employees.refresh') }, { key: 'delete', danger: true, label: t('employees.delete') }], onClick: ({ key }) => key === 'refresh' ? void mutate((signal) => refreshEmployeeKnowledge(employee.id, source.id, { signal }), setKnowledge, false) : confirmKnowledgeDelete(source.id) }}><Button aria-label={t('actions.actions')} icon={<MoreHorizontal size={18} />} disabled={!canMutate} /></Dropdown> },
          ]} />}
          <Modal title={copy.addSource} open={knowledgeComposerOpen} onCancel={() => setKnowledgeComposerOpen(false)} footer={null} width={isMobile ? 'calc(100vw - 24px)' : 640}>
            <Form layout="vertical" onFinish={() => void mutate((signal) => addEmployeeKnowledge(employee.id, { id: sourceDraft.id, kind: sourceDraft.kind, title: sourceDraft.title, ...(sourceDraft.kind === 'manual_text' ? { manual_text: sourceDraft.content } : { relative_path: sourceDraft.content }) }, { signal }), (value) => { setKnowledge(value); setKnowledgeComposerOpen(false); setSourceDraft({ id: '', kind: 'manual_text', title: '', content: '' }) }, false)}>
              <Form.Item label={t('employees.knowledgeKind')} required><Select aria-label={t('employees.knowledgeKind')} value={sourceDraft.kind} options={[{ value: 'manual_text', label: t('knowledgeKind.manual_text') }, { value: 'file', label: t('knowledgeKind.file') }, { value: 'project_docs', label: t('knowledgeKind.project_docs') }]} onChange={(kind) => setSourceDraft((value) => ({ ...value, kind }))} /></Form.Item>
              <Form.Item label={t('employees.sourceId')} required><Input aria-label={t('employees.sourceId')} value={sourceDraft.id} onChange={(event) => setSourceDraft((value) => ({ ...value, id: event.target.value }))} /></Form.Item>
              <Form.Item label={t('employees.sourceTitle')} required><Input aria-label={t('employees.sourceTitle')} value={sourceDraft.title} onChange={(event) => setSourceDraft((value) => ({ ...value, title: event.target.value }))} /></Form.Item>
              <Form.Item label={sourceDraft.kind === 'manual_text' ? t('knowledgeKind.manual_text') : t('employees.relativePath')} required><Input.TextArea aria-label={sourceDraft.kind === 'manual_text' ? t('knowledgeKind.manual_text') : t('employees.relativePath')} autoSize={{ minRows: 8, maxRows: 20 }} value={sourceDraft.content} onChange={(event) => setSourceDraft((value) => ({ ...value, content: event.target.value }))} /></Form.Item>
              <Alert type="info" showIcon message={copy.workspaceOnly} />
              <Flex justify="end" gap={12} className="modal-form-actions"><Button onClick={() => setKnowledgeComposerOpen(false)}>{t('actions.cancel')}</Button><Button type="primary" htmlType="submit" loading={busy}>{t('employees.addKnowledge')}</Button></Flex>
            </Form>
          </Modal>
          <Drawer title={copy.citation} open={selectedCitation !== null} width={isMobile ? '100%' : 600} onClose={() => setSelectedCitation(null)}>
            {selectedCitation ? <Descriptions column={1} bordered size="small"><Descriptions.Item label="Source"><CopyValue value={selectedCitation.source_id} /></Descriptions.Item><Descriptions.Item label="Path"><CopyValue value={selectedCitation.path} /></Descriptions.Item><Descriptions.Item label="Heading"><Text className="safe-wrap">{selectedCitation.heading ?? '—'}</Text></Descriptions.Item><Descriptions.Item label="Lines">{selectedCitation.start_line}–{selectedCitation.end_line}</Descriptions.Item><Descriptions.Item label="Snippet"><Paragraph className="safe-wrap">{selectedCitation.snippet}</Paragraph></Descriptions.Item><Descriptions.Item label="Digest"><CopyValue value={selectedCitation.digest} /></Descriptions.Item></Descriptions> : null}
          </Drawer>
        </Space>
      ) : null}

      {tab === 'memory' ? (
        <Space direction="vertical" size={16} className="antd-page-stack employee-memory-page">
          <Alert type="info" showIcon message={zh ? 'Memory 边界' : 'Memory boundary'} description={zh ? '只展示 bounded Fact/Candidate 与来源引用，不显示私有推理或原始工具参数。' : 'Only bounded Facts, Candidates, and provenance references are shown. Private reasoning and raw tool arguments stay hidden.'} />
          {memory === null ? <Skeleton active /> : (
            <Row gutter={[16, 16]}>
              <Col xs={24} lg={12}><Card title={t('employees.pendingCandidates')} extra={<Badge count={memory.candidates.length} showZero />}><List dataSource={memory.candidates} locale={{ emptyText: <Empty description={t('employees.noPendingMemory')} /> }} renderItem={(candidate) => <List.Item><Card className="mobile-resource-card" data-testid="memory-candidate"><Space direction="vertical" className="antd-page-stack"><Flex justify="space-between" wrap gap={8}><Tag color="warning">{candidate.category}</Tag><CopyValue compact value={candidate.id} /></Flex><Paragraph ellipsis={{ rows: 4, expandable: true }}>{candidate.value}</Paragraph><Descriptions column={1} size="small"><Descriptions.Item label={copy.provenance}>{candidate.provenance.map((item) => <div key={`${item.source_type}:${item.source_id}`}><Text>{item.source_type}: </Text><CopyValue compact value={item.source_id} /></div>)}</Descriptions.Item></Descriptions>{!archived ? <Space direction={isMobile ? 'vertical' : 'horizontal'} className="mobile-card-actions"><Button type="primary" block={isMobile} disabled={!canMutate} onClick={() => void mutate(async (signal) => { await acceptEmployeeMemoryCandidate(employee.id, candidate.id, { signal }); return reloadMemory(signal) }, setMemory, false)}>{t('employees.accept')}</Button><Button danger block={isMobile} disabled={!canMutate} onClick={() => confirmMemory('reject', candidate.id)}>{t('employees.reject')}</Button></Space> : null}</Space></Card></List.Item>} /></Card></Col>
              <Col xs={24} lg={12}><Card title={t('employees.acceptedMemory')} extra={<Badge count={memory.facts.length} showZero />}><List dataSource={memory.facts} locale={{ emptyText: <Empty description={t('employees.noAcceptedMemory')} /> }} renderItem={(fact) => <List.Item><Card className="mobile-resource-card"><Space direction="vertical" className="antd-page-stack"><Flex justify="space-between" wrap gap={8}><Tag color="success">{fact.category}</Tag><CopyValue compact value={fact.digest} /></Flex><Input.TextArea disabled={archived} autoSize={{ minRows: 3, maxRows: 12 }} value={factDraft[fact.id] ?? fact.value} onChange={(event) => setFactDraft((value) => ({ ...value, [fact.id]: event.target.value }))} /><Descriptions column={1} size="small"><Descriptions.Item label={t('tasks.updated')}>{fact.updated_at}</Descriptions.Item><Descriptions.Item label={copy.provenance}>{fact.provenance.map((item) => <div key={`${item.source_type}:${item.source_id}`}><Text>{item.source_type}: </Text><CopyValue compact value={item.source_id} /></div>)}</Descriptions.Item></Descriptions>{!archived ? <Space direction={isMobile ? 'vertical' : 'horizontal'} className="mobile-card-actions"><Button block={isMobile} disabled={!canMutate} onClick={() => void mutate(async (signal) => { await editEmployeeMemory(employee.id, fact.id, factDraft[fact.id] ?? fact.value, { signal }); return reloadMemory(signal) }, setMemory, false)}>{t('employees.edit')}</Button><Button danger block={isMobile} disabled={!canMutate} onClick={() => confirmMemory('forget', fact.id)}>{t('employees.forget')}</Button></Space> : null}</Space></Card></List.Item>} /></Card></Col>
            </Row>
          )}
        </Space>
      ) : null}

      {tab === 'projects' ? (
        <Space direction="vertical" size={16} className="antd-page-stack employee-projects-page">
          <Alert type="info" showIcon message={copy.workspaceOnly} />
          <Flex justify="end">{!archived && !projectEditMode ? <Button icon={<Pencil size={16} />} disabled={!canMutate} onClick={() => { setProjectDraft(employeeRecord.project_bindings); setProjectEditMode(true) }}>{t('actions.edit')}</Button> : null}</Flex>
          {employeeRecord.project_bindings.length ? employeeRecord.project_bindings.map((binding, index) => {
            const draft = projectDraft[index] ?? binding
            return <Card key={binding.id} title={binding.label} extra={<Tag color="success">{t('employees.bound')}</Tag>}><Descriptions column={isMobile ? 1 : 2} bordered size="small"><Descriptions.Item label="Project ID"><CopyValue value={binding.id} /></Descriptions.Item><Descriptions.Item label={t('employees.workspace')}><CopyValue value={binding.workspace_real_path} /></Descriptions.Item><Descriptions.Item label={t('employees.fingerprint')} span={isMobile ? 1 : 2}><CopyValue value={binding.workspace_fingerprint} /></Descriptions.Item><Descriptions.Item label={t('employees.readAllowed')}>{projectEditMode ? <Switch aria-label={t('employees.readAllowed')} checked={draft.read_allowed} onChange={(checked) => setProjectDraft((value) => value.map((item, itemIndex) => itemIndex === index ? { ...item, read_allowed: checked } : item))} /> : <Tag color={draft.read_allowed ? 'success' : 'default'}>{draft.read_allowed ? t('common.yes') : t('common.no')}</Tag>}</Descriptions.Item><Descriptions.Item label={t('employees.mutationAllowed')}>{projectEditMode ? <Switch aria-label={t('employees.mutationAllowed')} checked={draft.mutation_allowed} onChange={(checked) => setProjectDraft((value) => value.map((item, itemIndex) => itemIndex === index ? { ...item, mutation_allowed: checked } : item))} /> : <Tag color={draft.mutation_allowed ? 'warning' : 'default'}>{draft.mutation_allowed ? t('common.yes') : t('common.no')}</Tag>}</Descriptions.Item><Descriptions.Item label={t('employees.networkAllowed')}>{projectEditMode ? <Switch aria-label={t('employees.networkAllowed')} checked={draft.network_allowed} onChange={(checked) => setProjectDraft((value) => value.map((item, itemIndex) => itemIndex === index ? { ...item, network_allowed: checked } : item))} /> : <Tag color={draft.network_allowed ? 'warning' : 'default'}>{draft.network_allowed ? t('common.yes') : t('common.no')}</Tag>}</Descriptions.Item><Descriptions.Item label={t('employees.capabilities')}><Space wrap>{binding.allowed_tool_capabilities.map((item) => <Tag key={item}>{item}</Tag>)}</Space></Descriptions.Item></Descriptions></Card>
          }) : <Empty description={t('employees.noProjects')} />}
          {projectEditMode ? <div className="antd-mobile-action-bar"><Space direction={isMobile ? 'vertical' : 'horizontal'}><Button block={isMobile} onClick={() => { setProjectDraft(employeeRecord.project_bindings); setProjectEditMode(false) }}>{t('actions.cancel')}</Button><Button block={isMobile} type="primary" loading={busy} disabled={!canMutate} onClick={() => void mutate((signal) => updateEmployee(employee.id, { expected_revision: employee.revision, employee, project_bindings: projectDraft }, { signal }), (value) => { setRecord(value); setProjectDraft(value.project_bindings); setProjectEditMode(false) }, false)}>{t('actions.save')}</Button></Space></div> : null}
        </Space>
      ) : null}

      {tab === 'loops' ? (
        <Space direction="vertical" size={16} className="antd-page-stack employee-loops-page"><Flex justify="space-between" wrap gap={12}><Title level={3}>{t('employees.tabs.loops')}</Title>{!archived ? <Button type="primary"><Link to="/loops">{t('loops.create')}</Link></Button> : null}</Flex>{loops === null ? <Skeleton active /> : <List grid={{ gutter: 16, xs: 1, md: 2, xl: 3 }} dataSource={loops} locale={{ emptyText: <Empty description={t('loops.noRuns')} /> }} renderItem={(loop) => <List.Item><Card title={<Link to={`/loops/${encodeURIComponent(loop.id)}`}>{loop.name}</Link>} extra={<Tag color={loop.enabled ? 'success' : 'default'}>{loop.enabled ? t('loops.enabled') : t('loops.disabled')}</Tag>}><Paragraph ellipsis={{ rows: 3, expandable: true }}>{loop.contract.goal}</Paragraph><Descriptions column={1} size="small"><Descriptions.Item label={t('loops.scheduleKind')}>{loop.schedule.kind === 'daily' ? `${loop.schedule.local_time} · ${loop.schedule.timezone}` : t('loops.manual')}</Descriptions.Item><Descriptions.Item label={t('loops.revision')}>{loop.revision}</Descriptions.Item></Descriptions><Button block={isMobile}><Link to={`/loops/${encodeURIComponent(loop.id)}`}>{t('actions.open')}</Link></Button></Card></List.Item>} />}</Space>
      ) : null}

      {tab === 'tasks' ? (
        <Space direction="vertical" size={16} className="antd-page-stack employee-tasks-page"><Flex justify="space-between" wrap gap={12}><Title level={3}>{t('employees.tabs.tasks')}</Title>{!archived ? <Button type="primary"><Link to="/tasks">{t('tasks.create')}</Link></Button> : null}</Flex>{tasks === null ? <Skeleton active /> : isMobile ? <List dataSource={tasks} locale={{ emptyText: <Empty description={t('employees.noTasks')} /> }} renderItem={(task) => <List.Item><Card className="mobile-resource-card" title={<Link className="task-prompt-link" to={`/tasks/${encodeURIComponent(task.id)}`}><Typography.Paragraph className="task-prompt-text" ellipsis={{ rows: 3, expandable: true }}>{task.prompt}</Typography.Paragraph></Link>} extra={<Tag color={statusColor(task.state)}>{translatedEnum(t, 'taskStatus', task.state)}</Tag>}><Descriptions column={1} size="small"><Descriptions.Item label={t('tasks.employee')}>{task.employee_id} · rev {task.employee_revision}</Descriptions.Item><Descriptions.Item label={t('tasks.project')}>{task.project_binding.label}</Descriptions.Item><Descriptions.Item label="Session"><CopyValue value={task.session_id ?? ''} /></Descriptions.Item><Descriptions.Item label="Run"><CopyValue value={task.run_id ?? ''} /></Descriptions.Item><Descriptions.Item label={t('tasks.updated')}>{task.updated_at}</Descriptions.Item></Descriptions><Button block><Link to={`/tasks/${encodeURIComponent(task.id)}`}>{copy.openTask}</Link></Button></Card></List.Item>} /> : <Table className="employee-task-table" rowKey="id" dataSource={tasks} columns={taskColumns} pagination={false} />}</Space>
      ) : null}

      {tab === 'activity' ? (
        <Space direction="vertical" size={16} className="antd-page-stack employee-activity-page"><Title level={3}>{t('employees.tabs.activity')}</Title>{activity === null ? <Skeleton active /> : activity.events.length ? <Card><Timeline items={activity.events.map((event) => ({ color: statusColor(event.type), label: isMobile ? undefined : event.time, children: <Space direction="vertical" size={4}><Text strong>{translatedEnum(t, 'employeeActivityType', event.type)}</Text><Text type="secondary">{event.time}</Text><Space wrap>{event.employee_revision ? <Tag>rev {event.employee_revision}</Tag> : null}{event.subject_id ? <CopyValue compact value={event.subject_id} /> : null}{event.task_id ? <Link to={`/tasks/${encodeURIComponent(event.task_id)}`}>{event.task_id}</Link> : null}{event.session_id ? <CopyValue compact value={event.session_id} /> : null}{event.run_id ? <CopyValue compact value={event.run_id} /> : null}</Space></Space> }))} /></Card> : <Empty description={t('employees.noActivity')} />}{activity?.next_cursor ? <Button loading={busy} onClick={() => { const cursor = activity.next_cursor; if (!cursor) return; const owner = employeeId; const epoch = tabEpoch.current; void getEmployeeActivity(employee.id, { limit: 100, cursor }).then((page) => { if (epoch !== tabEpoch.current || owner !== employeeId) return; setActivity({ events: [...activity.events, ...page.events], ...(page.next_cursor ? { next_cursor: page.next_cursor } : {}) }) }).catch(() => undefined) }}>{t('employees.loadMore')}</Button> : null}</Space>
      ) : null}
    </article>
  )
}

function KnowledgeCard({
  source,
  citations,
  canMutate,
  onCitation,
  onRefresh,
  onDelete,
  t,
}: {
  source: KnowledgeSource
  citations: KnowledgeCitation[]
  canMutate: boolean
  onCitation: (citation: KnowledgeCitation | null) => void
  onRefresh: () => void
  onDelete: () => void
  t: ReturnType<typeof useTranslation>['t']
}) {
  return (
    <Card className="mobile-resource-card" data-testid="knowledge-source" title={source.title} extra={<Tag color={statusColor(source.status)}>{translatedEnum(t, 'knowledgeStatus', source.status)}</Tag>}>
      <Descriptions column={1} size="small">
        <Descriptions.Item label="ID"><CopyValue value={source.id} /></Descriptions.Item>
        <Descriptions.Item label={t('employees.knowledgeKind')}>{translatedEnum(t, 'knowledgeKind', source.kind)}</Descriptions.Item>
        <Descriptions.Item label={t('employees.sourceDigest')}><CopyValue value={source.digest} /></Descriptions.Item>
        {source.relative_path ? <Descriptions.Item label={t('employees.relativePath')}><CopyValue value={source.relative_path} /></Descriptions.Item> : null}
        {source.error ? <Descriptions.Item label={t('common.error')}><Text type="danger" className="safe-wrap">{source.error}</Text></Descriptions.Item> : null}
      </Descriptions>
      <Space direction="vertical" className="mobile-card-actions">
        <Button block icon={<FileText size={16} />} disabled={citations.length === 0} onClick={() => onCitation(citations[0] ?? null)}>{t('employees.citations')} ({citations.length})</Button>
        <Dropdown trigger={['click']} menu={{ items: [{ key: 'refresh', label: t('employees.refresh'), icon: <RefreshCw size={15} /> }, { key: 'delete', danger: true, label: t('employees.delete') }], onClick: ({ key }) => key === 'refresh' ? onRefresh() : onDelete() }}><Button block disabled={!canMutate} icon={<MoreHorizontal size={18} />}>{t('actions.actions')}</Button></Dropdown>
      </Space>
    </Card>
  )
}
