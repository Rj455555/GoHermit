import type { Employee } from '../../api/types'

export type EmployeePreset = 'developer' | 'researcher' | 'operations' | 'writer'

interface DraftInput {
  preset: EmployeePreset
  displayName: string
  brief: string
  locale: string
  uniqueSuffix: string
}

type GeneratedEmployeeFields = Pick<
  Employee,
  | 'id'
  | 'name'
  | 'job_title'
  | 'charter'
  | 'responsibilities'
  | 'behavior_boundaries'
  | 'permission_policy'
>

const capabilities: Record<EmployeePreset, string[]> = {
  developer: [
    'filesystem.read',
    'filesystem.list',
    'filesystem.search',
    'filesystem.write',
    'patch.apply',
    'shell.execute',
    'git.status',
    'git.diff',
    'test.run',
  ],
  researcher: ['filesystem.read', 'filesystem.list', 'filesystem.search'],
  operations: [
    'filesystem.read',
    'filesystem.list',
    'filesystem.search',
    'shell.execute',
    'git.status',
    'git.diff',
    'test.run',
  ],
  writer: ['filesystem.read', 'filesystem.list', 'filesystem.search', 'filesystem.write'],
}

const chinesePresets: Record<EmployeePreset, {
  name: string
  title: string
  purpose: string
  responsibilities: string[]
  boundaries: string[]
}> = {
  developer: {
    name: '开发助手',
    title: '开发工程师',
    purpose: '交付经过验证的软件变更',
    responsibilities: ['理解需求与现有架构', '实现范围明确的代码修改', '运行测试并报告验证结果'],
    boundaries: ['修改后必须运行与变更相匹配的验证', '不提交密钥、凭据或运行数据', '遇到高风险或范围外变更时停止并汇报'],
  },
  researcher: {
    name: '研究助手',
    title: '研究分析师',
    purpose: '收集、验证并整理可追溯的信息',
    responsibilities: ['明确研究问题和判断标准', '交叉核对权威来源', '输出带来源的结论与不确定项'],
    boundaries: ['事实和推断必须明确区分', '不把未经验证的信息写入长期记忆', '不泄露私有资料'],
  },
  operations: {
    name: '运维助手',
    title: '运维工程师',
    purpose: '保持本地服务可靠、可恢复并经过验证',
    responsibilities: ['诊断服务与构建状态', '执行可回滚的维护操作', '验证健康状态与数据完整性'],
    boundaries: ['破坏性操作前必须获得明确授权', '不清理其他项目的容器、镜像或缓存', '变更后必须验证服务和持久数据'],
  },
  writer: {
    name: '内容助手',
    title: '内容编辑',
    purpose: '产出清晰、一致且可复用的内容',
    responsibilities: ['理解读者、目标与语气', '组织内容结构并完成初稿', '校对事实、术语和表达一致性'],
    boundaries: ['不编造来源、数据或用户观点', '保留敏感信息边界', '发布前必须由 Owner 复核'],
  },
}

const englishPresets: typeof chinesePresets = {
  developer: {
    name: 'Development Assistant',
    title: 'Software Engineer',
    purpose: 'deliver verified software changes',
    responsibilities: ['Understand the goal and current architecture', 'Implement bounded code changes', 'Run tests and report verification evidence'],
    boundaries: ['Run verification that matches every change', 'Never persist secrets, credentials, or runtime data', 'Stop and report high-risk or out-of-scope changes'],
  },
  researcher: {
    name: 'Research Assistant',
    title: 'Research Analyst',
    purpose: 'collect, verify, and organize traceable information',
    responsibilities: ['Define the research question and criteria', 'Cross-check authoritative sources', 'Report conclusions, sources, and uncertainty'],
    boundaries: ['Separate facts from inference', 'Do not promote unverified claims into memory', 'Protect private source material'],
  },
  operations: {
    name: 'Operations Assistant',
    title: 'Operations Engineer',
    purpose: 'keep local services reliable, recoverable, and verified',
    responsibilities: ['Diagnose service and build state', 'Perform reversible maintenance', 'Verify health and data integrity'],
    boundaries: ['Require explicit approval for destructive actions', 'Do not clean resources owned by other projects', 'Verify services and persisted data after changes'],
  },
  writer: {
    name: 'Writing Assistant',
    title: 'Content Editor',
    purpose: 'produce clear, consistent, and reusable content',
    responsibilities: ['Understand the audience, goal, and voice', 'Structure and draft the content', 'Check facts, terminology, and consistency'],
    boundaries: ['Never invent sources, data, or user opinions', 'Respect sensitive-information boundaries', 'Require Owner review before publishing'],
  },
}

// The wizard derives "project-" + Employee ID; both Store IDs are capped at 128 bytes.
const EMPLOYEE_ID_MAX_BYTES = 120
const EMPLOYEE_ID_PATTERN = /^[A-Za-z0-9_.-]+$/u

export function isValidEmployeeId(value: string): boolean {
  const trimmed = value.trim()
  return trimmed !== ''
    && trimmed.length <= EMPLOYEE_ID_MAX_BYTES
    && trimmed !== '.'
    && trimmed !== '..'
    && EMPLOYEE_ID_PATTERN.test(trimmed)
}

export function ensureEmployeeId(value: string, name: string, uniqueSuffix: string): string {
  const trimmed = value.trim()
  if (isValidEmployeeId(trimmed)) return trimmed

  const stem = name
    .normalize('NFKD')
    .replace(/[\u0300-\u036f]/gu, '')
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/gu, '-')
    .replace(/^[._-]+|[._-]+$/gu, '') || 'employee'
  const suffix = safeSuffix(uniqueSuffix)
  const maximumStemLength = Math.max(1, EMPLOYEE_ID_MAX_BYTES - suffix.length - 1)
  return `${stem.slice(0, maximumStemLength)}-${suffix}`
}

function safeSuffix(value: string): string {
  const normalized = value.toLowerCase().replace(/[^a-z0-9._-]+/gu, '-').replace(/^-+|-+$/gu, '')
  return normalized || 'draft'
}

export function generateEmployeeDraft(input: DraftInput): GeneratedEmployeeFields {
  const chinese = input.locale.toLowerCase().startsWith('zh')
  const preset = (chinese ? chinesePresets : englishPresets)[input.preset]
  const brief = input.brief.trim()
  const purpose = brief || preset.purpose
  return {
    id: `${input.preset}-${safeSuffix(input.uniqueSuffix)}`,
    name: input.displayName.trim() || preset.name,
    job_title: preset.title,
    charter: chinese
      ? `专门负责：${purpose}。以可验证的结果为完成标准，并在信息不足时主动说明。`
      : `Own this responsibility: ${purpose}. Finish with verifiable evidence and surface missing context early.`,
    responsibilities: [...preset.responsibilities],
    behavior_boundaries: [...preset.boundaries],
    permission_policy: {
      allowed_capabilities: [...capabilities[input.preset]],
      network_allowed: false,
    },
  }
}
