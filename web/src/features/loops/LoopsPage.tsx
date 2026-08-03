import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import {
  Alert,
  Badge,
  Button,
  Card,
  Checkbox,
  Col,
  Collapse,
  Descriptions,
  Drawer,
  Empty,
  Flex,
  Form,
  Grid,
  Input,
  InputNumber,
  List,
  Progress,
  Row,
  Select,
  Skeleton,
  Space,
  Statistic,
  Steps,
  Switch,
  Table,
  Tabs,
  Tag,
  Timeline,
  Typography,
  type TableColumnsType,
} from 'antd'
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
  getLoopRuntime,
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
  Handoff,
  Info,
  LoopDefinition,
  LoopInvocation,
  LoopRuntimeState,
  Mission,
  RuntimeSelection,
  SessionDetailResponse,
  TeamRoleSelection,
  TeamTemplate,
  WorkItem,
} from '../../api/types'
import { useConnectivity } from '../../components/ConnectivityProvider'
import { ErrorState } from '../../components/ErrorState'
import { PageHeader } from '../../components/PageHeader'
import { useSessionEvents } from '../../hooks/useSessionEvents'
import { translatedEnum } from '../../i18n/enumLabel'
import { formatDateTime } from '../../i18n/dateTime'
import { useUI } from '../../state/UIContext'

const TEAM_ROLES = ['lead', 'explorer', 'builder', 'reviewer', 'verifier'] as const
const TERMINAL_INVOCATIONS = new Set(['completed', 'skipped', 'blocked', 'failed', 'cancelled'])
const { Paragraph, Text, Title } = Typography

function mutationKey(error: unknown) {
  if (error instanceof ApiError && error.code === 'network_error') return 'mutation.offline'
  if (error instanceof ApiError && error.status === 409) return 'mutation.conflict'
  return 'mutation.failed'
}

function statusColor(status: string) {
  if (['completed', 'approved', 'ready', 'passed'].includes(status)) return 'success'
  if (['failed', 'denied', 'blocked'].includes(status)) return 'error'
  if (['cancelled', 'skipped', 'interrupted'].includes(status)) return 'warning'
  if (['running', 'attached', 'dispatched', 'prepared', 'pending'].includes(status)) return 'processing'
  return 'default'
}

function splitLines(value: string) {
  return value.split(/\r?\n/u).map((item) => item.trim()).filter(Boolean)
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
      boundaries: [],
      sop: [],
      definition_of_done: [],
      stop_conditions: [],
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
    const page = await listEmployees(cursor
      ? { state: 'active', cursor, limit: 100 }
      : { state: 'active', limit: 100 }, { signal })
    employees.push(...page.employees)
    cursor = page.next_cursor ?? ''
  } while (cursor)
  return employees
}

function RuntimeSelectionFields({ info, value, onChange, prefix }: {
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
    <Row gutter={[16, 0]}>
      <Col xs={24} md={8}><Form.Item label={t('loops.company')} required><Select aria-label={`${prefix} ${t('loops.company')}`} value={value.company || undefined} placeholder={t('common.select')} options={companies.map((item) => ({ value: item.id, label: item.label }))} onChange={(companyID) => onChange({ company: companyID ?? '', access: '', model: '' })} /></Form.Item></Col>
      <Col xs={24} md={8}><Form.Item label={t('loops.access')} required><Select aria-label={`${prefix} ${t('loops.access')}`} value={value.access || undefined} placeholder={t('common.select')} options={company?.access.map((item) => ({ value: item.id, label: item.label, disabled: !item.supported })) ?? []} onChange={(accessID) => onChange({ ...value, access: accessID ?? '', model: '' })} /></Form.Item></Col>
      <Col xs={24} md={8}><Form.Item label={t('loops.model')} required><Select aria-label={`${prefix} ${t('loops.model')}`} value={value.model || undefined} placeholder={t('common.select')} options={access?.models.map((item) => ({ value: item.id, label: item.label })) ?? []} onChange={(modelID) => onChange({ ...value, model: modelID ?? '' })} /></Form.Item></Col>
    </Row>
  )
}

function DefinitionForm({ value, onChange, info, includeId }: {
  value: LoopDefinition
  onChange: (next: LoopDefinition) => void
  info: Info | null
  includeId: boolean
}) {
  const { t } = useTranslation()
  const checks = value.verification_recipe.checks
  const setCheck = (index: number, patch: Partial<(typeof checks)[number]>) => onChange({
    ...value,
    verification_recipe: {
      ...value.verification_recipe,
      checks: checks.map((check, itemIndex) => itemIndex === index ? { ...check, ...patch } : check),
    },
  })
  return (
    <Form layout="vertical" className="loop-definition-form">
      <Space direction="vertical" size={16} className="antd-page-stack">
        <Card title={t('loops.contract')}>
          <Row gutter={[16, 0]}>
            {includeId ? <Col xs={24} md={12}><Form.Item label={t('loops.id')} required><Input aria-label={t('loops.id')} value={value.id} onChange={(event) => onChange({ ...value, id: event.target.value })} /></Form.Item></Col> : null}
            <Col xs={24} md={12}><Form.Item label={t('loops.name')} required><Input aria-label={t('loops.name')} value={value.name} onChange={(event) => onChange({ ...value, name: event.target.value })} /></Form.Item></Col>
            <Col xs={24}><Form.Item label={t('loops.descriptionLabel')}><Input.TextArea aria-label={t('loops.descriptionLabel')} autoSize={{ minRows: 2, maxRows: 6 }} value={value.description} onChange={(event) => onChange({ ...value, description: event.target.value })} /></Form.Item></Col>
            <Col xs={24}><Form.Item label={t('loops.workspace')} required><Input aria-label={t('loops.workspace')} value={value.workspace_identity} onChange={(event) => onChange({ ...value, workspace_identity: event.target.value })} /></Form.Item></Col>
            <Col xs={24}><Form.Item label={t('loops.mission')} required><Input.TextArea aria-label={t('loops.mission')} autoSize={{ minRows: 3, maxRows: 10 }} value={value.task_source.prompt} onChange={(event) => onChange({ ...value, task_source: { type: 'fixed_prompt', prompt: event.target.value } })} /></Form.Item></Col>
            <Col xs={24} md={8}><Form.Item label={t('loops.agent')} required><Select aria-label={t('loops.agent')} value={value.agent_selection.agent} options={info?.agents.map((agent) => ({ value: agent.id, label: agent.label })) ?? []} onChange={(agent) => onChange({ ...value, agent_selection: { ...value.agent_selection, agent } })} /></Form.Item></Col>
            <Col xs={24} md={8}><Form.Item label={t('loops.planMode')} required><Select aria-label={t('loops.planMode')} value={value.plan_mode} options={[{ value: 'review', label: t('agent.planReview') }, { value: 'auto', label: t('agent.planAuto') }]} onChange={(mode) => onChange({ ...value, plan_mode: mode === 'auto' ? 'auto' : 'review' })} /></Form.Item></Col>
            <Col xs={24} md={8}><Form.Item label={t('loops.teamReference')} required><Input aria-label={t('loops.teamReference')} value={value.team_template_ref} onChange={(event) => onChange({ ...value, team_template_ref: event.target.value })} /></Form.Item></Col>
          </Row>
          <RuntimeSelectionFields info={info} value={value.agent_selection} prefix={t('loops.definitionModel')} onChange={(selection) => onChange({ ...value, agent_selection: { ...value.agent_selection, ...selection } })} />
        </Card>
        <Card title={t('loops.policies')}>
          <Row gutter={[16, 16]}>
            {[
              { label: t('loops.enabled'), checked: value.enabled, change: (checked: boolean) => onChange({ ...value, enabled: checked }) },
              { label: t('loops.readOnly'), checked: value.workspace_policy.read_only, change: (checked: boolean) => onChange({ ...value, workspace_policy: { ...value.workspace_policy, read_only: checked } }) },
              { label: t('loops.cleanGit'), checked: value.workspace_policy.require_clean_git, change: (checked: boolean) => onChange({ ...value, workspace_policy: { ...value.workspace_policy, require_clean_git: checked } }) },
              { label: t('loops.requireApproval'), checked: value.approval_policy.require_for_mutation, change: (checked: boolean) => onChange({ ...value, approval_policy: { require_for_mutation: checked } }) },
              { label: t('loops.includeDiff'), checked: value.output_policy.include_diff, change: (checked: boolean) => onChange({ ...value, output_policy: { ...value.output_policy, include_diff: checked } }) },
            ].map((item) => <Col xs={24} sm={12} lg={8} key={item.label}><Flex align="center" gap={12}><Switch aria-label={item.label} checked={item.checked} onChange={item.change} /><Text>{item.label}</Text></Flex></Col>)}
          </Row>
        </Card>
        <Card title={t('loops.budget')}>
          <Row gutter={[16, 0]}>
            <Col xs={24} sm={12} xl={6}><Form.Item label={t('loops.maxCalls')} required><InputNumber aria-label={t('loops.maxCalls')} min={0} inputMode="numeric" value={value.budget.max_model_calls} onChange={(next) => onChange({ ...value, budget: { ...value.budget, max_model_calls: next ?? value.budget.max_model_calls } })} /></Form.Item></Col>
            <Col xs={24} sm={12} xl={6}><Form.Item label={t('loops.maxTokens')} required><InputNumber aria-label={t('loops.maxTokens')} min={0} inputMode="numeric" value={value.budget.max_tokens} onChange={(next) => onChange({ ...value, budget: { ...value.budget, max_tokens: next ?? value.budget.max_tokens } })} /></Form.Item></Col>
            <Col xs={24} sm={12} xl={6}><Form.Item label={t('loops.timeout')} required><InputNumber aria-label={t('loops.timeout')} min={0} inputMode="numeric" value={value.budget.timeout_seconds} onChange={(next) => onChange({ ...value, budget: { ...value.budget, timeout_seconds: next ?? value.budget.timeout_seconds } })} /></Form.Item></Col>
            <Col xs={24} sm={12} xl={6}><Form.Item label={t('loops.maxReport')} required><InputNumber aria-label={t('loops.maxReport')} min={0} inputMode="numeric" value={value.output_policy.max_report_bytes} onChange={(next) => onChange({ ...value, output_policy: { ...value.output_policy, max_report_bytes: next ?? value.output_policy.max_report_bytes } })} /></Form.Item></Col>
          </Row>
        </Card>
        <Card title={<Title level={2}>{t('loops.verificationChecks')}</Title>}>
          <Row gutter={[16, 0]}>
            <Col xs={24} md={12}><Form.Item label={t('loops.independentVerifier')}><Switch aria-label={t('loops.independentVerifier')} checked={value.verification_recipe.independent_verifier} onChange={(checked) => onChange({ ...value, verification_recipe: { ...value.verification_recipe, independent_verifier: checked } })} /></Form.Item></Col>
            <Col xs={24} md={12}><Form.Item label={t('loops.maxRepair')}><InputNumber aria-label={t('loops.maxRepair')} min={0} inputMode="numeric" value={value.verification_recipe.max_repair_attempts} onChange={(next) => onChange({ ...value, verification_recipe: { ...value.verification_recipe, max_repair_attempts: next ?? value.verification_recipe.max_repair_attempts } })} /></Form.Item></Col>
          </Row>
          <Space direction="vertical" size={16} className="antd-page-stack">
            {checks.map((check, index) => <Card key={`${check.id}-${index}`} type="inner" title={t('loops.checkNumber', { number: index + 1 })} extra={<Button danger onClick={() => onChange({ ...value, verification_recipe: { ...value.verification_recipe, checks: checks.filter((_, itemIndex) => itemIndex !== index) } })}>{t('common.remove')}</Button>}>
              <Form.Item label={t('loops.checkId')} required><Input aria-label={t('loops.checkId')} value={check.id} onChange={(event) => setCheck(index, { id: event.target.value })} /></Form.Item>
              <Title level={5}>{t('loops.commandArguments')}</Title>
              <Space direction="vertical" size={8} className="antd-page-stack loop-argv-editor">
                {check.command.map((argument, argumentIndex) => <Flex key={argumentIndex} gap={8} align="end" wrap="wrap"><Form.Item className="loop-argv-field" label={t('loops.commandArgument', { number: argumentIndex + 1 })}><Input aria-label={t('loops.commandArgument', { number: argumentIndex + 1 })} value={argument} onChange={(event) => setCheck(index, { command: check.command.map((current, currentIndex) => currentIndex === argumentIndex ? event.target.value : current) })} /></Form.Item><Button danger onClick={() => setCheck(index, { command: check.command.filter((_, currentIndex) => currentIndex !== argumentIndex) })}>{t('common.remove')}</Button></Flex>)}
                <Button disabled={check.command.length >= 32} onClick={() => setCheck(index, { command: [...check.command, ''] })}>{t('loops.addArgument')}</Button>
              </Space>
              <Row gutter={[16, 0]}>
                <Col xs={24} md={12}><Form.Item label={t('loops.checkTimeout')}><InputNumber aria-label={t('loops.checkTimeout')} min={0} inputMode="numeric" value={check.timeout_seconds} onChange={(next) => setCheck(index, { timeout_seconds: next ?? check.timeout_seconds })} /></Form.Item></Col>
                <Col xs={24} md={12}><Form.Item label={t('loops.required')}><Switch aria-label={t('loops.required')} checked={check.required} onChange={(required) => setCheck(index, { required })} /></Form.Item></Col>
              </Row>
            </Card>)}
            <Button type="dashed" block disabled={checks.length >= 16} onClick={() => onChange({ ...value, verification_recipe: { ...value.verification_recipe, checks: [...checks, { id: '', command: [], required: true, timeout_seconds: 300 }] } })}>{t('loops.addCheck')}</Button>
          </Space>
        </Card>
      </Space>
    </Form>
  )
}

function LoopList({ loops }: { loops: LoopDefinition[] }) {
  const { t } = useTranslation()
  const screens = Grid.useBreakpoint()
  const columns: TableColumnsType<LoopDefinition> = [
    { title: t('loops.name'), key: 'name', width: '24%', render: (_, item) => <Link to={`/loops/${encodeURIComponent(item.id)}`}><Text strong>{item.name}</Text></Link> },
    { title: t('loops.state'), key: 'state', width: 116, render: (_, item) => <Tag color={item.enabled ? 'success' : 'default'}>{item.enabled ? t('loops.enabled') : t('loops.disabled')}</Tag> },
    { title: t('loops.teamReference'), dataIndex: 'team_template_ref', key: 'team', width: 150 },
    { title: t('loops.mission'), key: 'mission', width: '34%', render: (_, item) => <Paragraph ellipsis={{ rows: 2, tooltip: item.contract.goal }}>{item.contract.goal || item.task_source.prompt}</Paragraph> },
    { title: t('loops.when'), key: 'when', width: 150, render: (_, item) => item.schedule.kind === 'daily' ? `${item.schedule.local_time} · ${item.schedule.timezone}` : t('loops.manual') },
    { title: t('actions.actions'), key: 'actions', width: 112, render: (_, item) => <Button><Link to={`/loops/${encodeURIComponent(item.id)}`}>{t('loops.openLoop')}</Link></Button> },
  ]
  if (screens.md) return <Table className="loop-list-table" rowKey="id" columns={columns} dataSource={loops} tableLayout="fixed" pagination={false} />
  return <List dataSource={loops} locale={{ emptyText: <Empty description={t('loops.noRuns')} /> }} renderItem={(definition) => <List.Item><Card className="mobile-resource-card" title={<Link to={`/loops/${encodeURIComponent(definition.id)}`}>{definition.name}</Link>} extra={<Badge status={definition.enabled ? 'success' : 'default'} text={definition.enabled ? t('loops.enabled') : t('loops.disabled')} />}><Descriptions column={1} size="small"><Descriptions.Item label={t('loops.employee')}>{definition.employee_id || t('loops.legacy')}</Descriptions.Item><Descriptions.Item label={t('loops.teamReference')}>{definition.team_template_ref}</Descriptions.Item><Descriptions.Item label={t('loops.when')}>{definition.schedule.kind === 'daily' ? `${definition.schedule.local_time} · ${definition.schedule.timezone}` : t('loops.manual')}</Descriptions.Item><Descriptions.Item label={t('loops.mission')}><Paragraph ellipsis={{ rows: 4, expandable: true }}>{definition.contract.goal || definition.task_source.prompt}</Paragraph></Descriptions.Item></Descriptions><Button block type="primary"><Link to={`/loops/${encodeURIComponent(definition.id)}`}>{t('loops.openLoop')}</Link></Button></Card></List.Item>} />
}

export function LoopsPage() {
  const { t } = useTranslation()
  const { actions } = useUI()
  const navigate = useNavigate()
  const connectivity = useConnectivity()
  const screens = Grid.useBreakpoint()
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
    void Promise.all([listLoops({ signal: controller.signal }), getInfo({ signal: controller.signal }), loadAllActiveEmployees(controller.signal)])
      .then(([page, catalog, activeEmployees]) => {
        if (epoch !== pageEpoch.current) return
        setLoops(page.loops)
        setEmployees(activeEmployees)
        setInfo(catalog)
        setDraft((current) => current.agent_selection.company ? current : { ...current, workspace_identity: catalog.workspace, agent_selection: { ...catalog.selection, agent: 'team' } })
      })
      .catch(() => { if (!controller.signal.aborted) actions.showToast({ messageKey: 'mutation.failed', tone: 'error' }) })
    return () => { pageEpoch.current += 1; controller.abort() }
  }, [actions, connectivity.generation])

  async function saveNew() {
    if (!connectivity.canMutate) return
    const epoch = pageEpoch.current
    setCreating(true)
    try {
      const saved = await createLoop(normalizedDefinition(draft))
      if (epoch === pageEpoch.current) void navigate(`/loops/${encodeURIComponent(saved.id)}`)
    } catch (error) {
      if (epoch === pageEpoch.current) actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
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
      if (epoch === pageEpoch.current) void navigate(`/loops/${encodeURIComponent(saved.id)}`)
    } catch (error) {
      if (epoch === pageEpoch.current) setStrictError(error instanceof ApiError && error.status === 409 ? t('mutation.conflict') : t('loops.rejectedImport'))
    }
  }

  return <article className="feature-page loop-workbench antd-deep-page">
    <PageHeader title={t('loops.heroTitle')} description={t('loops.heroDescription')} />
    <Card title={t('loops.activeLoops')}><LoopList loops={loops} /></Card>
    <Card className="loop-quick-create" title={<Title level={2}>{t('loops.describeJob')}</Title>} extra={<Tag>{t('loops.newLoop')}</Tag>}>
      <Paragraph type="secondary">{t('loops.quickCreateDescription')}</Paragraph>
      <Form layout="vertical">
        <Row gutter={[16, 0]}>
          <Col xs={24} md={12}><Form.Item label={t('loops.employee')} required><Select aria-label={t('loops.employee')} value={draft.employee_id || undefined} placeholder={t('loops.selectEmployee')} options={employees.map((employee) => ({ value: employee.id, label: `${employee.name} · ${employee.job_title}` }))} onChange={(employeeID) => setDraft({ ...draft, employee_id: employeeID })} /></Form.Item></Col>
          <Col xs={24} md={12}><Form.Item label={t('loops.name')} required><Input aria-label={t('loops.name')} value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value, id: draft.id || `loop-${Date.now().toString(36)}` })} /></Form.Item></Col>
          <Col xs={24}><Form.Item label={t('loops.goal')} required><Input.TextArea aria-label={t('loops.goal')} autoSize={{ minRows: 3, maxRows: 8 }} value={draft.contract.goal} onChange={(event) => setDraft({ ...draft, contract: { ...draft.contract, goal: event.target.value }, task_source: { type: 'fixed_prompt', prompt: event.target.value } })} /></Form.Item></Col>
          <Col xs={24} md={12}><Form.Item label={t('loops.scheduleKind')}><Select aria-label={t('loops.scheduleKind')} value={draft.schedule.kind || 'manual'} options={[{ value: 'manual', label: t('loops.manual') }, { value: 'daily', label: t('loops.daily') }]} onChange={(kind) => setDraft({ ...draft, schedule: kind === 'daily' ? { kind: 'daily', local_time: draft.schedule.local_time || '02:00', timezone: draft.schedule.timezone || 'Asia/Shanghai' } : { kind: 'manual', local_time: '', timezone: '' } })} /></Form.Item></Col>
          {draft.schedule.kind === 'daily' ? <Col xs={24} md={12}><Form.Item label={t('loops.runTime')}><Input aria-label={t('loops.runTime')} type="time" value={draft.schedule.local_time} onChange={(event) => setDraft({ ...draft, schedule: { ...draft.schedule, local_time: event.target.value } })} /></Form.Item></Col> : null}
        </Row>
        <div className="loop-create-actions"><Button block={!screens.md} type="primary" loading={creating} disabled={!connectivity.canMutate || !draft.employee_id || !draft.name.trim() || !draft.contract.goal.trim()} onClick={() => void saveNew()}>{t('loops.createAndConfigure')}</Button></div>
      </Form>
      <Collapse className="loop-advanced-collapse" items={[{ key: 'advanced', label: t('loops.advanced'), children: <Space direction="vertical" size={16} className="antd-page-stack"><DefinitionForm value={draft} onChange={setDraft} info={info} includeId /><Card title={t('loops.import')}><Form layout="vertical"><Form.Item label={t('loops.import')} {...(strictError ? { validateStatus: 'error' as const, help: strictError } : {})}><Input.TextArea aria-label={t('loops.import')} autoSize={{ minRows: 8, maxRows: 20 }} value={importText} onChange={(event) => setImportText(event.target.value)} /></Form.Item><Button disabled={!connectivity.canMutate} onClick={() => void importStrict()}>{t('loops.import')}</Button></Form></Card></Space> }]} />
    </Card>
  </article>
}

function TeamEditor({ team, setTeam, employees, readiness, info, onSave, busy }: {
  team: TeamTemplate
  setTeam: (next: TeamTemplate) => void
  employees: EmployeeSummary[]
  readiness: Record<string, EmployeeDryRun>
  info: Info | null
  onSave: () => void
  busy: boolean
}) {
  const { t } = useTranslation()
  const screens = Grid.useBreakpoint()
  const setDefault = (next: Pick<RuntimeSelection, 'company' | 'access' | 'model'>) => setTeam({ ...team, default: { ...team.default, ...next } })
  const setRole = (role: string, next: TeamRoleSelection) => setTeam({ ...team, roles: { ...team.roles, [role]: next } })
  return <Card title={<Title level={2}>{t('loops.teamRoles')}</Title>}>
    <Form layout="vertical">
      <Form.Item label={t('loops.teamName')} required><Input aria-label={t('loops.teamName')} value={team.name} onChange={(event) => setTeam({ ...team, name: event.target.value })} /></Form.Item>
      <Title level={3}>{t('loops.defaultSelection')}</Title>
      <RuntimeSelectionFields info={info} value={team.default} onChange={setDefault} prefix={t('loops.defaultSelection')} />
      <Alert type="info" showIcon message={t('loops.modelOverrideRule')} />
      <Form.List name="roles" initialValue={TEAM_ROLES.map((role) => ({ role }))}>
        {(fields) => <Space direction="vertical" size={16} className="antd-page-stack">{fields.map((field, index) => {
          const role = TEAM_ROLES[index]
          if (!role) return null
          const selection = team.roles[role] ?? { ...team.default }
          const employee = employees.find((item) => item.id === selection.employee_id)
          const employeeReady = selection.employee_id ? readiness[selection.employee_id] : undefined
          const override = Boolean(selection.employee_id && selection.company && selection.access && selection.model)
          return <Card key={field.key} className="team-role-card" type="inner" title={<Space><Tag>{translatedEnum(t, 'teamRole', role)}</Tag>{employee ? <Text>{employee.name} · r{employee.revision}</Text> : null}</Space>}>
            <Row gutter={[16, 0]}>
              <Col xs={24} md={12}><Form.Item label={t('loops.employee')}><Select data-testid={`team-role-${role}`} aria-label={`${translatedEnum(t, 'teamRole', role)} ${t('loops.employee')}`} value={selection.employee_id || undefined} allowClear placeholder={t('loops.noEmployee')} options={employees.map((item) => ({ value: item.id, label: `${item.name} · r${item.revision}` }))} onChange={(employeeID) => setRole(role, employeeID ? { company: '', access: '', model: '', employee_id: employeeID } : { ...team.default })} /></Form.Item></Col>
              <Col xs={24} md={6}><Form.Item label={t('loops.maxCalls')}><InputNumber<number> aria-label={`${role} ${t('loops.maxCalls')}`} min={0} inputMode="numeric" value={selection.max_model_calls ?? 0} onChange={(next) => setRole(role, { ...selection, max_model_calls: next ?? 0 })} /></Form.Item></Col>
              <Col xs={24} md={6}><Form.Item label={t('loops.maxTokens')}><InputNumber<number> aria-label={`${role} ${t('loops.maxTokens')}`} min={0} inputMode="numeric" value={selection.max_tokens ?? 0} onChange={(next) => setRole(role, { ...selection, max_tokens: next ?? 0 })} /></Form.Item></Col>
            </Row>
            {selection.employee_id ? <Space direction="vertical" size={12} className="antd-page-stack">
              <Alert type={employee && employeeReady?.ready ? 'success' : 'error'} showIcon message={employee ? `${employee.name} · r${employee.revision} · ${employeeReady?.ready ? t('loops.ready') : t('loops.notReady')}` : `${selection.employee_id} · ${t('loops.unavailable')}`} description={employeeReady?.checks.map((check) => `${check.name}: ${check.ready ? t('loops.ready') : t('loops.notReady')} · ${check.detail}`).join('\n')} />
              <Checkbox aria-label={t('loops.useMissionOverride', { role })} checked={override} onChange={(event) => setRole(role, event.target.checked ? { ...team.default, employee_id: selection.employee_id } : { company: '', access: '', model: '', employee_id: selection.employee_id })}>{t('loops.missionOverride')}</Checkbox>
              {override ? <RuntimeSelectionFields info={info} value={selection} onChange={(next) => setRole(role, { ...selection, ...next })} prefix={role} /> : <Alert type="info" message={t('loops.employeeDefaultPath')} />}
            </Space> : <Alert type="info" message={t('loops.teamDefaultPath')} />}
          </Card>
        })}</Space>}
      </Form.List>
      <div className="antd-mobile-action-bar"><Button block={!screens.md} type="primary" loading={busy} disabled={busy} onClick={onSave}>{t('loops.saveTeam')}</Button></div>
    </Form>
  </Card>
}

function DryRunProjection({ report }: { report: DryRunReport }) {
  const { t } = useTranslation()
  return <Alert type={report.ready ? 'success' : 'error'} showIcon message={`${t('loops.dryRunResult')} · ${report.ready ? t('loops.ready') : t('loops.notReady')}`} description={<Space direction="vertical"><Descriptions column={{ xs: 1, sm: 2, lg: 4 }} size="small"><Descriptions.Item label={t('loops.definitionValid')}>{report.definition_valid ? t('common.yes') : t('common.no')}</Descriptions.Item><Descriptions.Item label={t('loops.workspaceMatch')}>{report.workspace_matches ? t('common.yes') : t('common.no')}</Descriptions.Item><Descriptions.Item label={t('loops.gitClean')}>{report.git_clean ? t('common.yes') : t('common.no')}</Descriptions.Item><Descriptions.Item label={t('loops.writeScope')}>{report.write_scope}</Descriptions.Item></Descriptions>{report.reasons.map((reason) => <Text key={reason}>{reason}</Text>)}</Space>} />
}

type FlowStatus = 'idle' | 'active' | 'success' | 'blocked' | 'failed'

function orchestrationStatus(invocation: LoopInvocation | undefined): FlowStatus {
  if (!invocation) return 'idle'
  if (invocation.status === 'completed') return 'success'
  if (invocation.status === 'failed') return 'failed'
  if (['blocked', 'skipped', 'cancelled'].includes(invocation.status)) return 'blocked'
  return 'active'
}

function LoopOrchestrationBoard({ definition, runtime, history, current, evidence }: {
  definition: LoopDefinition
  runtime?: LoopRuntimeState | null
  history: LoopInvocation[]
  current?: LoopInvocation | null
  evidence?: { plan: number; tools: number; checks: number; artifacts: number }
}) {
  const { t } = useTranslation()
  const latest = current ?? history.at(-1)
  const flow = orchestrationStatus(latest)
  const currentStep = flow === 'idle' ? 0 : flow === 'active' ? 1 : flow === 'success' ? 5 : 2
  return <Card className="loop-orchestration" data-testid="loop-orchestration" title={<Space wrap><Title level={2}>{t('loops.orchestrationTitle')}</Title><Tag color={statusColor(flow)}>{t(`loops.flowStatus.${flow}`)}</Tag></Space>}>
    <Paragraph type="secondary">{t('loops.orchestrationDescription')}</Paragraph>
    <Steps responsive current={currentStep} status={flow === 'failed' ? 'error' : flow === 'blocked' ? 'error' : 'process'} items={[
      { title: t('loops.triggerNode'), description: latest?.trigger ?? t('loops.manual') },
      { title: t('loops.orchestratorNode'), description: t('loops.dispatchDetail') },
      { title: t('loops.executorNode'), description: latest?.session_id ? t('loops.sessionBound') : t('loops.waitingForRun') },
      { title: t('loops.verifierNode'), description: t('loops.checkCount', { count: definition.verification_recipe.checks.length }) },
      { title: t('loops.evidenceNode'), description: evidence ? t('loops.evidenceSummary', evidence) : t('loops.evidencePending') },
      { title: t('loops.evolveNode'), description: t('loops.evolveDetail') },
    ]} />
    <Row gutter={[16, 16]} className="loop-flow-stats">
      <Col xs={12} md={6}><Statistic title={t('loops.boundariesRedrawn')} value={definition.contract.boundaries.length} /></Col>
      <Col xs={12} md={6}><Statistic title={t('loops.repeatedSteps')} value={definition.contract.sop.length} /></Col>
      <Col xs={12} md={6}><Statistic title={t('loops.totalRuns')} value={runtime?.total_runs ?? history.length} /></Col>
      <Col xs={12} md={6}><Statistic title={t('loops.lastStatus')} value={runtime?.last_status ? translatedEnum(t, 'invocationStatus', runtime.last_status) : t('loops.neverRun')} /></Col>
    </Row>
  </Card>
}

function InvocationHistory({ definition, history }: { definition: LoopDefinition; history: LoopInvocation[] }) {
  const { t, i18n } = useTranslation()
  const screens = Grid.useBreakpoint()
  const columns: TableColumnsType<LoopInvocation> = [
    { title: t('loops.invocation'), dataIndex: 'id', key: 'id', width: '28%', render: (id: string) => <Link className="safe-wrap" to={`/loops/${encodeURIComponent(definition.id)}/invocations/${encodeURIComponent(id)}`}>{id}</Link> },
    { title: t('loops.state'), dataIndex: 'status', key: 'status', width: 120, render: (status: string) => <Tag color={statusColor(status)}>{translatedEnum(t, 'invocationStatus', status)}</Tag> },
    { title: t('loops.trigger'), dataIndex: 'trigger', key: 'trigger', width: 160, render: (trigger: string) => <Text className="safe-wrap">{trigger}</Text> },
    { title: t('loops.created'), dataIndex: 'created_at', key: 'created', width: 178, render: (value: string) => <span className="task-updated-cell">{formatDateTime(value, i18n.language)}</span> },
  ]
  if (screens.md) return <Table className="loop-invocation-table" rowKey="id" dataSource={history} columns={columns} tableLayout="fixed" pagination={false} />
  return <List dataSource={history} locale={{ emptyText: <Empty description={t('loops.noRuns')} /> }} renderItem={(invocation) => <List.Item><Card className="mobile-resource-card" title={<Link className="safe-wrap" to={`/loops/${encodeURIComponent(definition.id)}/invocations/${encodeURIComponent(invocation.id)}`}>{invocation.id}</Link>} extra={<Tag color={statusColor(invocation.status)}>{translatedEnum(t, 'invocationStatus', invocation.status)}</Tag>}><Descriptions column={1} size="small"><Descriptions.Item label={t('loops.trigger')}>{invocation.trigger}</Descriptions.Item><Descriptions.Item label={t('loops.created')}><span className="task-updated-cell">{formatDateTime(invocation.created_at, i18n.language)}</span></Descriptions.Item><Descriptions.Item label={t('loops.failure')}>{invocation.failure_summary || '—'}</Descriptions.Item></Descriptions></Card></List.Item>} />
}

export function LoopDetailPage() {
  const { loopId } = useParams()
  const { t } = useTranslation()
  const { actions } = useUI()
  const connectivity = useConnectivity()
  const screens = Grid.useBreakpoint()
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
        try { return await dryRunEmployee(employee.id, { signal: controller.signal }) } catch { return { employee_id: employee.id, revision: employee.revision, ready: false, checks: [] } }
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
      if (epoch === requestEpoch.current) setNotFound(error instanceof ApiError && error.status === 404)
    }
  }, [loopId])

  useEffect(() => { setBusy(false); void refresh(); return () => { requestEpoch.current += 1; requestController.current?.abort() } }, [connectivity.generation, refresh])

  async function mutate(action: 'save' | 'start' | 'dry-run') {
    if (!loopId || !definition || !connectivity.canMutate) return
    const owner = loopId
    const epoch = requestEpoch.current
    setBusy(true)
    try {
      if (action === 'save') {
        const updated = await updateLoop(loopId, normalizedDefinition(definition))
        if (epoch === requestEpoch.current && owner === loopId) { setDefinition(updated); setReport(null) }
      } else if (action === 'start') {
        await startLoopInvocation(loopId)
        if (epoch === requestEpoch.current && owner === loopId) await refresh()
      } else {
        const nextReport = await dryRunLoop(loopId)
        if (epoch === requestEpoch.current && owner === loopId) setReport(nextReport)
      }
    } catch (error) {
      if (epoch === requestEpoch.current && owner === loopId) {
        actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
        if (error instanceof ApiError && error.status === 409) await refresh()
      }
    } finally {
      if (owner === loopId) setBusy(false)
    }
  }

  async function saveTeam() {
    if (!team || !connectivity.canMutate) return
    const owner = loopId
    const epoch = requestEpoch.current
    const incomplete = Object.values(team.roles).some((selection) => selection.employee_id && [selection.company, selection.access, selection.model].some(Boolean) && ![selection.company, selection.access, selection.model].every(Boolean))
    if (incomplete) { actions.showToast({ messageKey: 'loops.incompleteOverride', tone: 'error' }); return }
    setBusy(true)
    try {
      await importTeamTemplate(team)
      if (epoch === requestEpoch.current && owner === loopId) actions.showToast({ messageKey: 'toast.saved', tone: 'success' })
    } catch (error) {
      if (epoch === requestEpoch.current && owner === loopId) actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
    } finally {
      if (owner === loopId) setBusy(false)
    }
  }

  if (notFound) return <ErrorState title={t('loops.notFound')} description={t('loops.notFoundDescription')} />
  if (!definition || !team || !runtime) return <Skeleton active paragraph={{ rows: 10 }} />
  const contractForm = <Form layout="vertical"><Form.Item label={t('loops.goal')}><Input.TextArea aria-label={t('loops.goal')} autoSize={{ minRows: 3, maxRows: 10 }} value={definition.contract.goal} onChange={(event) => setDefinition({ ...definition, contract: { ...definition.contract, goal: event.target.value } })} /></Form.Item>{[
    { key: 'boundaries', label: t('loops.boundaries'), value: definition.contract.boundaries },
    { key: 'sop', label: t('loops.sop'), value: definition.contract.sop },
    { key: 'definition_of_done', label: t('loops.definitionOfDone'), value: definition.contract.definition_of_done },
    { key: 'stop_conditions', label: t('loops.stopConditions'), value: definition.contract.stop_conditions },
  ].map((field) => <Form.Item key={field.key} label={field.label}><Input.TextArea aria-label={field.label} autoSize={{ minRows: 3, maxRows: 8 }} value={field.value.join('\n')} onChange={(event) => setDefinition({ ...definition, contract: { ...definition.contract, [field.key]: splitLines(event.target.value) } })} /></Form.Item>)}<Row gutter={[16, 0]}><Col xs={24} md={12}><Form.Item label={t('loops.scheduleKind')}><Select aria-label={t('loops.scheduleKind')} value={definition.schedule.kind || 'manual'} options={[{ value: 'manual', label: t('loops.manual') }, { value: 'daily', label: t('loops.daily') }]} onChange={(kind) => setDefinition({ ...definition, schedule: kind === 'daily' ? { kind: 'daily', local_time: definition.schedule.local_time || '02:00', timezone: definition.schedule.timezone || 'Asia/Shanghai' } : { kind: 'manual', local_time: '', timezone: '' } })} /></Form.Item></Col>{definition.schedule.kind === 'daily' ? <Col xs={24} md={12}><Form.Item label={t('loops.runTime')}><Input type="time" aria-label={t('loops.runTime')} value={definition.schedule.local_time} onChange={(event) => setDefinition({ ...definition, schedule: { ...definition.schedule, local_time: event.target.value } })} /></Form.Item></Col> : null}</Row><Space direction={screens.md ? 'horizontal' : 'vertical'}><Button block={!screens.md} type="primary" loading={busy} disabled={!connectivity.canMutate} onClick={() => void mutate('save')}>{t('loops.saveContract')}</Button><Button block={!screens.md} href={`/api/loops/${encodeURIComponent(definition.id)}/contract.md`} target="_blank">{t('loops.openMarkdown')}</Button></Space></Form>
  return <article className="feature-page loop-workbench antd-deep-page">
    <PageHeader title={definition.name} description={`${definition.employee_id ?? t('loops.legacy')} · ${definition.id} · ${t('loops.revision')}: ${definition.revision}`} />
    <LoopOrchestrationBoard definition={definition} runtime={runtime} history={history} />
    <Row gutter={[16, 16]}>
      <Col xs={12} lg={6}><Card><Statistic title={t('loops.nextRun')} value={runtime.next_run_at ? new Date(runtime.next_run_at).toLocaleString() : t('loops.manual')} /></Card></Col>
      <Col xs={12} lg={6}><Card><Statistic title={t('loops.lastStatus')} value={runtime.last_status ? translatedEnum(t, 'invocationStatus', runtime.last_status) : t('loops.neverRun')} /></Card></Col>
      <Col xs={12} lg={6}><Card><Statistic title={t('loops.totalRuns')} value={runtime.total_runs} /></Card></Col>
      <Col xs={12} lg={6}><Card><Statistic title={t('loops.successRate')} value={runtime.total_runs ? Math.round(runtime.successful_runs / runtime.total_runs * 100) : 0} suffix="%" /></Card></Col>
    </Row>
    <Card><Space direction={screens.md ? 'horizontal' : 'vertical'} className="loop-primary-actions"><Button block={!screens.md} loading={busy} disabled={!connectivity.canMutate} onClick={() => void mutate('dry-run')}>{t('loops.dryRun')}</Button><Button block={!screens.md} type="primary" loading={busy} disabled={!connectivity.canMutate || !report?.ready} onClick={() => void mutate('start')}>{t('loops.runNow')}</Button></Space>{report ? <DryRunProjection report={report} /> : <Alert type="info" showIcon message={t('loops.runDryFirst')} />}</Card>
    <Tabs items={[
      { key: 'contract', label: t('loops.contract'), children: <Card>{contractForm}</Card> },
      { key: 'definition', label: t('loops.advanced'), children: <Space direction="vertical" size={16} className="antd-page-stack"><DefinitionForm value={definition} onChange={(next) => { setDefinition(next); setReport(null) }} info={info} includeId={false} /><Button type="primary" loading={busy} disabled={!connectivity.canMutate} onClick={() => void mutate('save')}>{t('loops.saveContract')}</Button>{!definition.employee_id ? <TeamEditor team={team} setTeam={setTeam} employees={employees} readiness={readiness} info={info} onSave={() => void saveTeam()} busy={busy || !connectivity.canMutate} /> : null}</Space> },
      { key: 'history', label: t('loops.logs'), children: <Card><InvocationHistory definition={definition} history={history} /></Card> },
    ]} />
  </article>
}

function ProjectionList({ title, empty, children }: { title: string; empty: string; children: ReactNode }) {
  return <Card title={<Title level={2}>{title}</Title>}>{children ?? <Empty description={empty} />}</Card>
}

function MissionPanel({ mission, approvals, onDecision }: { mission: Mission; approvals: ApprovalRequest[]; onDecision: (request: ApprovalRequest, decision: 'approve' | 'deny') => void }) {
  const { t, i18n } = useTranslation()
  const screens = Grid.useBreakpoint()
  const [selected, setSelected] = useState<WorkItem | null>(null)
  const [selectedHandoff, setSelectedHandoff] = useState<Handoff | null>(null)
  const selectedAssignment = selected ? mission.employee_assignments[selected.id] : undefined
  const percent = mission.budget.max_tokens > 0 ? Math.min(100, Math.round(mission.usage.tokens / mission.budget.max_tokens * 100)) : 0
  const workColumns: TableColumnsType<WorkItem> = [
    { title: t('tasks.title'), dataIndex: 'title', key: 'title', width: '30%', render: (title: string) => <Text className="safe-wrap">{title}</Text> },
    { title: t('loops.role'), dataIndex: 'role', key: 'role', width: 130, render: (role: string) => translatedEnum(t, 'teamRole', role) },
    { title: t('tasks.state'), dataIndex: 'status', key: 'status', width: 120, render: (status: string) => <Tag color={statusColor(status)}>{translatedEnum(t, 'taskStatus', status)}</Tag> },
    { title: t('loops.budget'), key: 'budget', width: 150, render: (_, item) => `${mission.budget.role_limits[item.role]?.model_calls ?? 0} / ${mission.budget.role_limits[item.role]?.tokens ?? 0}` },
    { title: t('tasks.updated'), dataIndex: 'updated_at', key: 'updated', width: 178, render: (value: string) => <span className="task-updated-cell">{formatDateTime(value, i18n.language)}</span> },
    { title: t('actions.actions'), key: 'action', width: 112, render: (_, item) => <Button onClick={() => setSelected(item)}>{t('actions.open')}</Button> },
  ]
  const workItems = screens.md ? <Table className="mission-work-item-table" rowKey="id" columns={workColumns} dataSource={mission.work_items} tableLayout="fixed" pagination={false} /> : <List dataSource={mission.work_items} renderItem={(item) => {
    const assignment = mission.employee_assignments[item.id]
    return <List.Item><Card className="mobile-resource-card" title={<Text className="safe-wrap">{item.title}</Text>} extra={<Tag color={statusColor(item.status)}>{translatedEnum(t, 'taskStatus', item.status)}</Tag>}><Descriptions column={1} size="small"><Descriptions.Item label={t('loops.role')}>{translatedEnum(t, 'teamRole', item.role)}</Descriptions.Item><Descriptions.Item label={t('loops.employee')}>{assignment ? <><Text copyable>{assignment.employee_id}</Text> · {t('employees.revisionShort')} {assignment.employee_revision}</> : t('loops.assignmentSnapshot')}</Descriptions.Item><Descriptions.Item label={t('loops.budget')}>{mission.budget.role_limits[item.role]?.model_calls ?? 0} / {mission.budget.role_limits[item.role]?.tokens ?? 0}</Descriptions.Item><Descriptions.Item label={t('tasks.updated')}><span className="task-updated-cell">{formatDateTime(item.updated_at, i18n.language)}</span></Descriptions.Item><Descriptions.Item label={t('session.verification')}>{item.error || item.handoff_id || '—'}</Descriptions.Item></Descriptions><Button block onClick={() => setSelected(item)}>{t('actions.open')}</Button></Card></List.Item>
  }} />
  return <Space direction="vertical" size={16} className="antd-page-stack mission-panel">
    <Card title={<Space><Title level={2}>{t('loops.mission')}</Title><Tag color={statusColor(mission.status)}>{translatedEnum(t, 'taskStatus', mission.status)}</Tag></Space>}><Paragraph>{mission.goal}</Paragraph><Progress percent={percent} status={mission.status === 'failed' ? 'exception' : 'active'} /><Descriptions column={{ xs: 1, sm: 2, lg: 4 }}><Descriptions.Item label={t('loops.maxCalls')}>{mission.usage.model_calls} / {mission.budget.max_model_calls}</Descriptions.Item><Descriptions.Item label={t('loops.maxTokens')}>{mission.usage.tokens} / {mission.budget.max_tokens}</Descriptions.Item><Descriptions.Item label={t('loops.teamReference')}>{mission.template}</Descriptions.Item><Descriptions.Item label={t('loops.state')}>{translatedEnum(t, 'taskStatus', mission.status)}</Descriptions.Item></Descriptions></Card>
    <Card title={t('loops.workItems')}>{workItems}</Card>
    <Tabs items={[
      { key: 'approvals', label: t('session.approvals'), children: <List dataSource={approvals} locale={{ emptyText: <Empty /> }} renderItem={(approval) => <List.Item><Card className="mobile-resource-card" title={approval.tool} extra={<Tag color={statusColor(approval.status)}>{translatedEnum(t, 'approvalStatus', approval.status)}</Tag>}><Paragraph>{approval.args_summary}</Paragraph><Space direction={screens.md ? 'horizontal' : 'vertical'}><Button block={!screens.md} type="primary" disabled={approval.status !== 'pending'} onClick={() => onDecision(approval, 'approve')}>{t('approval.approve')}</Button><Button block={!screens.md} danger disabled={approval.status !== 'pending'} onClick={() => onDecision(approval, 'deny')}>{t('approval.deny')}</Button></Space></Card></List.Item>} /> },
      { key: 'handoffs', label: t('loops.handoffs'), children: <List dataSource={mission.handoffs} locale={{ emptyText: <Empty /> }} renderItem={(handoff) => <List.Item><Card className="mobile-resource-card" title={handoff.summary} extra={<Tag>{translatedEnum(t, 'teamRole', handoff.role)}</Tag>}><Paragraph ellipsis={{ rows: 3, expandable: true }}>{handoff.evidence.join('\n')}</Paragraph><Button onClick={() => setSelectedHandoff(handoff)}>{t('actions.open')}</Button></Card></List.Item>} /> },
      { key: 'timeline', label: t('loops.timeline'), children: <Timeline items={mission.work_items.map((item) => ({ color: statusColor(item.status), children: <Space direction="vertical" size={0}><Text strong className="safe-wrap">{item.title}</Text><Text type="secondary" className="task-updated-cell">{formatDateTime(item.updated_at, i18n.language)} · {translatedEnum(t, 'taskStatus', item.status)}</Text></Space> }))} /> },
    ]} />
    <Drawer title={selected?.title} open={selected !== null} width={screens.md ? 560 : '100%'} onClose={() => setSelected(null)}><Alert type="info" showIcon message={t('loops.hiddenWorkerProtected')} /><Descriptions column={1} bordered size="small"><Descriptions.Item label={t('loops.workItemId')}><Text copyable>{selected?.id}</Text></Descriptions.Item><Descriptions.Item label={t('loops.role')}>{selected ? translatedEnum(t, 'teamRole', selected.role) : ''}</Descriptions.Item><Descriptions.Item label={t('tasks.state')}>{selected ? translatedEnum(t, 'taskStatus', selected.status) : ''}</Descriptions.Item><Descriptions.Item label={t('loops.goal')}>{selected?.goal}</Descriptions.Item>{selectedAssignment ? <><Descriptions.Item label={t('loops.employee')}><Text copyable>{selectedAssignment.employee_id}</Text> · {t('employees.revisionShort')} {selectedAssignment.employee_revision}</Descriptions.Item><Descriptions.Item label={t('loops.assignmentSnapshot')}><Text copyable ellipsis={{ tooltip: selectedAssignment.employee_snapshot_digest }}>{selectedAssignment.employee_snapshot_digest}</Text></Descriptions.Item><Descriptions.Item label={t('loops.model')}>{selectedAssignment.company} / {selectedAssignment.access} / {selectedAssignment.model}</Descriptions.Item><Descriptions.Item label={t('loops.policy')}><Text copyable ellipsis={{ tooltip: selectedAssignment.effective_policy_digest }}>{selectedAssignment.effective_policy_digest}</Text></Descriptions.Item><Descriptions.Item label={t('loops.assignmentDigest')}><Text copyable ellipsis={{ tooltip: selectedAssignment.digest }}>{selectedAssignment.digest}</Text></Descriptions.Item></> : null}<Descriptions.Item label={t('loops.budget')}>{selected ? `${mission.budget.role_limits[selected.role]?.model_calls ?? 0} / ${mission.budget.role_limits[selected.role]?.tokens ?? 0}` : ''}</Descriptions.Item><Descriptions.Item label={t('session.verification')}>{selected?.error || selected?.handoff_id || '—'}</Descriptions.Item></Descriptions></Drawer>
    <Drawer title={t('loops.handoffs')} open={selectedHandoff !== null} width={screens.md ? 640 : '100%'} onClose={() => setSelectedHandoff(null)}><Descriptions column={1} bordered><Descriptions.Item label={t('loops.role')}>{selectedHandoff ? translatedEnum(t, 'teamRole', selectedHandoff.role) : ''}</Descriptions.Item><Descriptions.Item label={t('loops.summary')}>{selectedHandoff?.summary}</Descriptions.Item><Descriptions.Item label={t('session.verification')}>{selectedHandoff?.checks.map((check) => `${check.command}: ${check.passed ? t('common.passed') : t('common.failed')} · ${check.summary}`).join('\n')}</Descriptions.Item><Descriptions.Item label={t('loops.failure')}>{selectedHandoff?.issues.join('\n') || '—'}</Descriptions.Item></Descriptions></Drawer>
  </Space>
}

export function LoopInvocationPage() {
  const { invocationId } = useParams()
  const { t, i18n } = useTranslation()
  const { actions } = useUI()
  const connectivity = useConnectivity()
  const screens = Grid.useBreakpoint()
  const [invocation, setInvocation] = useState<LoopInvocation | null>(null)
  const [session, setSession] = useState<SessionDetailResponse | null>(null)
  const [approvals, setApprovals] = useState<ApprovalRequest[]>([])
  const [decisionBusy, setDecisionBusy] = useState('')
  const [invocationTab, setInvocationTab] = useState('snapshot')
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
      const [nextSession, pending] = next.session_id ? await Promise.all([getSession(next.session_id, { signal: controller.signal }), listApprovals(next.session_id, { signal: controller.signal })]) : [null, { approvals: [] }]
      if (epoch !== requestEpoch.current) return
      setInvocation(next)
      setSession(nextSession)
      setApprovals(pending.approvals)
      setNotFound(false)
    } catch (error) {
      if (epoch === requestEpoch.current) setNotFound(error instanceof ApiError && error.status === 404)
    }
  }, [invocationId])
  useEffect(() => { void refresh(); return () => { requestEpoch.current += 1; requestController.current?.abort() } }, [connectivity.generation, refresh])
  const events = useSessionEvents({ sessionId: invocation?.session_id, frontier: session?.session.next_event_sequence ?? 0, runId: invocation?.run_id, onRefresh: () => { void refresh() } })
  const boundRun = useMemo(() => session?.session.runs.find((run) => run.id === invocation?.run_id), [invocation?.run_id, session])
  const tools = session?.session.tool_calls.filter((tool) => !invocation?.run_id || tool.run_id === invocation.run_id) ?? []
  const verification = session?.session.test_results.filter((test) => !invocation?.run_id || test.run_id === invocation.run_id) ?? []

  function cancel() {
    if (!invocation || !connectivity.canMutate) return
    actions.openDialog({ titleKey: 'loops.cancelTitle', descriptionKey: 'loops.cancelDescription', confirmKey: 'loops.cancel', tone: 'warning', onConfirm: () => {
      const owner = invocation.id
      const epoch = requestEpoch.current
      void (async () => {
        try {
          const updated = await cancelLoopInvocation(invocation.id)
          if (epoch !== requestEpoch.current || owner !== invocationId) return
          setInvocation(updated)
          await refresh()
        } catch (error) {
          if (epoch === requestEpoch.current && owner === invocationId) actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
        }
      })()
    } })
  }

  async function decide(requestId: string, decision: 'approve' | 'deny') {
    if (!invocation?.session_id || !connectivity.canMutate || decisionBusy) return
    const owner = invocation.id
    const epoch = requestEpoch.current
    setDecisionBusy(requestId)
    try {
      await decideApproval(invocation.session_id, requestId, decision)
      if (epoch === requestEpoch.current && owner === invocationId) await refresh()
    } catch (error) {
      if (epoch === requestEpoch.current && owner === invocationId) actions.showToast({ messageKey: mutationKey(error), tone: 'error' })
    } finally {
      if (owner === invocationId) setDecisionBusy('')
    }
  }

  function confirmDecision(request: ApprovalRequest, decision: 'approve' | 'deny') {
    actions.openDialog({ titleKey: decision === 'approve' ? 'approval.approve' : 'approval.deny', descriptionKey: 'loops.requireApproval', confirmKey: decision === 'approve' ? 'approval.approve' : 'approval.deny', tone: decision === 'approve' ? 'info' : 'error', onConfirm: () => { void decide(request.request_id, decision) } })
  }

  if (notFound) return <ErrorState title={t('loops.invocationNotFound')} description={t('loops.notFoundDescription')} />
  if (!invocation) return <Skeleton active paragraph={{ rows: 10 }} />
  const active = !TERMINAL_INVOCATIONS.has(invocation.status)
  const evidence = { plan: boundRun?.plan?.steps.length ?? 0, tools: tools.length, checks: verification.length, artifacts: 0 }
  return <article className="feature-page antd-deep-page loop-invocation-page">
    <PageHeader title={invocation.id} description={`${invocation.loop_id} · ${translatedEnum(t, 'invocationStatus', invocation.status)}`} />
    <LoopOrchestrationBoard definition={invocation.definition_snapshot} history={[invocation]} current={invocation} evidence={evidence} />
    <Card><Descriptions column={{ xs: 1, sm: 2, lg: 3 }} bordered size="small"><Descriptions.Item label={t('loops.definitionRevision')}>{invocation.definition_revision}</Descriptions.Item><Descriptions.Item label={t('loops.trigger')}>{invocation.trigger}</Descriptions.Item><Descriptions.Item label={t('loops.created')}><span className="task-updated-cell">{formatDateTime(invocation.created_at, i18n.language)}</span></Descriptions.Item><Descriptions.Item label={t('loops.started')}><span className="task-updated-cell">{invocation.started_at ? formatDateTime(invocation.started_at, i18n.language) : '—'}</span></Descriptions.Item><Descriptions.Item label={t('loops.finished')}><span className="task-updated-cell">{invocation.finished_at ? formatDateTime(invocation.finished_at, i18n.language) : '—'}</span></Descriptions.Item><Descriptions.Item label={t('loops.failure')}>{invocation.failure_code ? <Text type="danger">{`${invocation.failure_code} · ${invocation.failure_summary}`}</Text> : <Text>—</Text>}</Descriptions.Item></Descriptions>{active ? <div className="antd-mobile-action-bar"><Button block={!screens.md} danger disabled={!connectivity.canMutate} onClick={cancel}>{t('loops.cancel')}</Button></div> : null}</Card>
    {session?.session.mission ? <MissionPanel mission={session.session.mission} approvals={approvals} onDecision={confirmDecision} /> : null}
    {!screens.md ? <Select
      className="loop-mobile-tab-select"
      aria-label={t('loops.invocationSection')}
      value={invocationTab}
      options={[
        { value: 'snapshot', label: t('loops.definitionSnapshot') },
        { value: 'plan', label: t('session.plan') },
        { value: 'tools', label: t('loops.tools') },
        { value: 'verification', label: t('session.verification') },
        { value: 'approvals', label: t('session.approvals') },
        { value: 'activity', label: t('loops.timeline') },
      ]}
      onChange={setInvocationTab}
    /> : null}
    <Tabs className="loop-invocation-tabs" activeKey={invocationTab} onChange={setInvocationTab} items={[
      { key: 'snapshot', label: t('loops.definitionSnapshot'), forceRender: true, children: <ProjectionList title={t('loops.definitionSnapshot')} empty={t('common.empty')}><Descriptions column={{ xs: 1, md: 2 }}><Descriptions.Item label={t('loops.name')}>{invocation.definition_snapshot.name}</Descriptions.Item><Descriptions.Item label={t('loops.workspace')}><Text copyable>{invocation.definition_snapshot.workspace_identity}</Text></Descriptions.Item><Descriptions.Item label={t('loops.mission')}>{invocation.task_snapshot}</Descriptions.Item><Descriptions.Item label={t('loops.model')}>{invocation.definition_snapshot.agent_selection.model}</Descriptions.Item></Descriptions></ProjectionList> },
      { key: 'plan', label: t('session.plan'), forceRender: true, children: <ProjectionList title={t('session.plan')} empty={t('common.empty')}>{boundRun?.plan ? <List dataSource={boundRun.plan.steps} renderItem={(step) => <List.Item><Space><Checkbox checked={step.status === 'completed'} disabled /><Text>{step.title}</Text><Tag color={statusColor(step.status)}>{translatedEnum(t, 'planStatus', step.status)}</Tag></Space></List.Item>} /> : null}</ProjectionList> },
      { key: 'tools', label: t('loops.tools'), forceRender: true, children: <ProjectionList title={t('loops.tools')} empty={t('common.empty')}>{tools.length ? <List dataSource={tools} renderItem={(tool) => <List.Item><Card className="mobile-resource-card" title={tool.name} extra={<Tag color={statusColor(tool.status || (tool.is_error ? 'error' : 'completed'))}>{translatedEnum(t, 'toolStatus', tool.status || (tool.is_error ? 'error' : 'completed'))}</Tag>}><Paragraph className="safe-wrap">{tool.summary}</Paragraph></Card></List.Item>} /> : null}</ProjectionList> },
      { key: 'verification', label: t('session.verification'), forceRender: true, children: <ProjectionList title={t('session.verification')} empty={t('common.empty')}>{verification.length ? <List dataSource={verification} renderItem={(test) => <List.Item><Alert type={test.passed ? 'success' : 'error'} showIcon message={test.command} description={test.summary} /></List.Item>} /> : null}</ProjectionList> },
      { key: 'approvals', label: t('session.approvals'), forceRender: true, children: <ProjectionList title={t('session.approvals')} empty={t('common.empty')}>{approvals.length ? <List dataSource={approvals} renderItem={(approval) => <List.Item><Card className="mobile-resource-card" title={approval.tool} extra={<Tag color={statusColor(approval.status)}>{translatedEnum(t, 'approvalStatus', approval.status)}</Tag>}><Paragraph>{approval.args_summary}</Paragraph><Space direction={screens.md ? 'horizontal' : 'vertical'}><Button block={!screens.md} type="primary" loading={decisionBusy === approval.request_id} disabled={!connectivity.canMutate || approval.status !== 'pending'} onClick={() => confirmDecision(approval, 'approve')}>{t('approval.approve')}</Button><Button block={!screens.md} danger loading={decisionBusy === approval.request_id} disabled={!connectivity.canMutate || approval.status !== 'pending'} onClick={() => confirmDecision(approval, 'deny')}>{t('approval.deny')}</Button></Space></Card></List.Item>} /> : null}</ProjectionList> },
      { key: 'activity', label: t('loops.timeline'), forceRender: true, children: <Card data-testid="loop-timeline" title={<Title level={2}>{t('loops.timeline')}</Title>}><Text>{invocation.id}</Text>{events.fatal ? <Button onClick={events.reconnect}>{t('session.reconnectEvents')}</Button> : null}{events.status === 'reconnecting' ? <Alert type="warning" showIcon message={t('session.reconnecting')} /> : null}{events.truncated ? <Alert type="warning" showIcon message={t('session.streamingTruncated')} /> : null}<Timeline items={events.events.map((event) => ({ children: <Space><Text>{event.sequence}</Text><Tag>{translatedEnum(t, 'runtimeEventType', event.type)}</Tag></Space> }))} /></Card> },
    ]} />
  </article>
}
