import { apiRequest, type ApiRequestOptions } from './client'
import {
  decodeApprovals,
  decodeCancellation,
  decodeCodexLogin,
  decodeConfigured,
  decodeDecision,
  decodeHealth,
  decodeInfo,
  decodeInvocations,
  decodeLoops,
  decodeOwnerProfile,
  decodeRunReference,
  decodeSessionCreated,
  decodeSessionDetail,
  decodeSessionList,
} from './decoders'
import type { OwnerProfile, PlanMode } from './types'

type ReadOptions = Pick<ApiRequestOptions, 'signal'>

function segment(value: string): string {
  return encodeURIComponent(value)
}

export const getHealth = (options: ReadOptions = {}) =>
  apiRequest('/api/health', decodeHealth, options)
export const getInfo = (options: ReadOptions = {}) =>
  apiRequest('/api/info', decodeInfo, options)
export const listLoops = (options: ReadOptions = {}) =>
  apiRequest('/api/loops', decodeLoops, options)
export const listLoopInvocations = (loopId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/loops/${segment(loopId)}/invocations?limit=50`, decodeInvocations, options)

export const getOwner = (options: ReadOptions = {}) =>
  apiRequest('/api/owner', decodeOwnerProfile, options)
export const saveOwner = (profile: OwnerProfile, options: ReadOptions = {}) =>
  apiRequest('/api/owner', decodeOwnerProfile, { ...options, method: 'PUT', body: profile })
export const saveOwnerFact = (
  factId: string,
  fact: { category: string; value: string; source: string; confirmed: boolean },
  options: ReadOptions = {},
) => apiRequest(`/api/owner/facts/${segment(factId)}`, decodeOwnerProfile, {
  ...options,
  method: 'PUT',
  body: fact,
})
export const forgetOwnerFact = (factId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/owner/facts/${segment(factId)}`, decodeOwnerProfile, {
    ...options,
    method: 'DELETE',
  })

export const saveProviderAPIKey = (
  provider: string,
  apiKey: string,
  options: ReadOptions = {},
) => apiRequest(`/api/settings/providers/${segment(provider)}/api-key`, decodeConfigured, {
  ...options,
  method: 'PUT',
  body: { api_key: apiKey },
})
export const deleteProviderCredentials = (provider: string, options: ReadOptions = {}) =>
  apiRequest(
    `/api/settings/providers/${segment(provider)}/credentials`,
    decodeConfigured,
    { ...options, method: 'DELETE' },
  )
export const startCodexLogin = (options: ReadOptions = {}) =>
  apiRequest('/api/settings/providers/openai-codex/login', decodeCodexLogin, {
    ...options,
    method: 'POST',
  })
export const getCodexLogin = (loginId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/settings/logins/${segment(loginId)}`, decodeCodexLogin, options)

export const listSessions = (options: ReadOptions = {}) =>
  apiRequest('/api/sessions?limit=100', decodeSessionList, options)
export const getSession = (sessionId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/sessions/${segment(sessionId)}`, decodeSessionDetail, options)
export const createSession = (
  input: {
    title: string
    company: string
    access: string
    model: string
    agent: string
    plan_mode: PlanMode
  },
  options: ReadOptions = {},
) => apiRequest('/api/sessions', decodeSessionCreated, {
  ...options,
  method: 'POST',
  body: input,
})
export const startRun = (sessionId: string, message: string, options: ReadOptions = {}) =>
  apiRequest(`/api/sessions/${segment(sessionId)}/runs`, decodeRunReference, {
    ...options,
    method: 'POST',
    body: { message },
  })
export const cancelRun = (sessionId: string, runId: string, options: ReadOptions = {}) =>
  apiRequest(
    `/api/sessions/${segment(sessionId)}/runs/${segment(runId)}/cancel`,
    decodeCancellation,
    { ...options, method: 'POST' },
  )
export const resumeRun = (sessionId: string, runId: string, options: ReadOptions = {}) =>
  apiRequest(
    `/api/sessions/${segment(sessionId)}/runs/${segment(runId)}/resume`,
    decodeRunReference,
    { ...options, method: 'POST' },
  )
export const approvePlan = (sessionId: string, runId: string, options: ReadOptions = {}) =>
  apiRequest(
    `/api/sessions/${segment(sessionId)}/runs/${segment(runId)}/approve`,
    decodeRunReference,
    { ...options, method: 'POST' },
  )
export const listApprovals = (sessionId: string, options: ReadOptions = {}) =>
  apiRequest(`/api/sessions/${segment(sessionId)}/approvals?status=pending`, decodeApprovals, options)
export const decideApproval = (
  sessionId: string,
  requestId: string,
  decision: 'approve' | 'deny',
  options: ReadOptions = {},
) => apiRequest(
  `/api/sessions/${segment(sessionId)}/approvals/${segment(requestId)}/decide`,
  decodeDecision,
  { ...options, method: 'POST', body: { decision } },
)
