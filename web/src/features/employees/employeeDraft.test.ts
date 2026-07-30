import { describe, expect, it } from 'vitest'

import { ensureEmployeeId, generateEmployeeDraft, isValidEmployeeId } from './employeeDraft'

describe('guided Employee draft generation', () => {
  it('creates a safe, complete Chinese developer draft from one short brief', () => {
    const draft = generateEmployeeDraft({
      preset: 'developer',
      displayName: '小元',
      brief: '维护 GoHermit，修复问题并运行验证',
      locale: 'zh-CN',
      uniqueSuffix: 'a1b2c3',
    })

    expect(draft).toMatchObject({
      id: 'developer-a1b2c3',
      name: '小元',
      job_title: '开发工程师',
      permission_policy: {
        network_allowed: false,
      },
    })
    expect(draft.charter).toContain('维护 GoHermit')
    expect(draft.responsibilities.length).toBeGreaterThanOrEqual(3)
    expect(draft.behavior_boundaries).toContain('修改后必须运行与变更相匹配的验证')
    expect(draft.permission_policy.allowed_capabilities).toContain('filesystem.write')
  })

  it('uses an ASCII identifier even when the name and brief are not ASCII', () => {
    const draft = generateEmployeeDraft({
      preset: 'researcher',
      displayName: '研究员',
      brief: '整理竞争对手资料',
      locale: 'zh-CN',
      uniqueSuffix: 'XYZ 123',
    })

    expect(draft.id).toBe('researcher-xyz-123')
    expect(draft.id).toMatch(/^[a-z0-9._-]+$/u)
  })

  it('generates English content for an English workbench', () => {
    const draft = generateEmployeeDraft({
      preset: 'operations',
      displayName: '',
      brief: '',
      locale: 'en-US',
      uniqueSuffix: 'ops001',
    })

    expect(draft.name).toBe('Operations Assistant')
    expect(draft.job_title).toBe('Operations Engineer')
    expect(draft.charter).toContain('reliable')
  })

  it('repairs a Chinese display value into a bounded path-safe Employee ID', () => {
    const id = ensureEmployeeId('档案管理员', '档案管理员', 'a1b2c3')

    expect(id).toBe('employee-a1b2c3')
    expect(isValidEmployeeId(id)).toBe(true)
  })

  it('preserves an explicit valid Employee ID and normalizes surrounding whitespace', () => {
    expect(ensureEmployeeId('  archive.manager_01  ', '档案管理员', 'ignored'))
      .toBe('archive.manager_01')
  })

  it('rejects traversal, separators, spaces, Unicode, and oversized IDs', () => {
    expect(isValidEmployeeId('.')).toBe(false)
    expect(isValidEmployeeId('..')).toBe(false)
    expect(isValidEmployeeId('../employee')).toBe(false)
    expect(isValidEmployeeId('employee/name')).toBe(false)
    expect(isValidEmployeeId('employee name')).toBe(false)
    expect(isValidEmployeeId('档案管理员')).toBe(false)
    expect(isValidEmployeeId('a'.repeat(121))).toBe(false)
  })
})
