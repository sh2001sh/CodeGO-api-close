import { api } from '@/lib/api'
import { bountyUsdToQuota } from './lib/bounty-format'
import type {
  BountyBalance,
  BountyDetail,
  BountyListResponse,
  BountyNotificationResponse,
  BountyReport,
  BountySearch,
  BountyDraftPayload,
  CreateBountyPayload,
} from './types'

interface BountyApiEnvelope<T> {
  success?: boolean
  message?: string
  data?: T
}

export function unwrap<T>(response: { data?: BountyApiEnvelope<T> }) {
  const body = response.data
  if (body?.success === false) {
    throw new Error(body.message || 'Request failed')
  }
  return body?.data as T
}

export async function getBounties(search: BountySearch) {
  const isMineScope =
    search.scope === 'mine_published' ||
    search.scope === 'mine_assigned' ||
    search.scope === 'mine_disputes'
  const response = await api.get(
    isMineScope ? '/api/bounties/mine' : '/api/bounties',
    {
      params: {
        scope: search.scope === 'all' ? undefined : search.scope,
        keyword: search.keyword || undefined,
        wallet_type:
          search.wallet_type === 'all' ? undefined : search.wallet_type,
        status: search.status === 'all' ? undefined : search.status,
        sort: search.sort === 'latest' ? undefined : search.sort,
        tag: search.tag || undefined,
        min_reward: search.min_reward
          ? bountyUsdToQuota(search.min_reward)
          : undefined,
        max_reward: search.max_reward
          ? bountyUsdToQuota(search.max_reward)
          : undefined,
        page: search.page || 1,
        page_size: 20,
      },
    }
  )
  return unwrap<BountyListResponse>(response)
}

export async function getBountyDetail(taskId: string) {
  const response = await api.get(`/api/bounties/${taskId}`)
  return unwrap<BountyDetail>(response)
}

export async function getBountyBalances() {
  const response = await api.get('/api/bounties/balances')
  return unwrap<BountyBalance[]>(response)
}

export async function getBountyNotifications() {
  const response = await api.get('/api/bounties/notifications')
  return unwrap<BountyNotificationResponse>(response)
}

export async function getAdminBountyReports() {
  const response = await api.get('/api/admin/bounties/reports')
  return unwrap<BountyReport[]>(response)
}

export async function createBounty(payload: CreateBountyPayload) {
  const response = await api.post('/api/bounties', payload, {
    headers: payload.idempotency_key
      ? { 'Idempotency-Key': payload.idempotency_key }
      : undefined,
  })
  return unwrap<BountyDetail>(response)
}

export async function saveBountyDraft(payload: BountyDraftPayload) {
  const response = await api.post('/api/bounties/drafts', payload, {
    headers: payload.idempotency_key
      ? { 'Idempotency-Key': payload.idempotency_key }
      : undefined,
  })
  return unwrap<BountyDetail>(response)
}

export async function updateBountyDraft(
  taskId: string,
  payload: BountyDraftPayload
) {
  const response = await api.put(`/api/bounties/${taskId}/draft`, payload)
  return unwrap<BountyDetail>(response)
}

export async function publishBountyDraft(taskId: string) {
  const response = await api.post(`/api/bounties/${taskId}/draft/publish`)
  return unwrap<BountyDetail>(response)
}

export async function postBountyAction<T>(
  taskId: string,
  action: string,
  payload?: T
) {
  const response = await api.post(`/api/bounties/${taskId}/${action}`, payload)
  return unwrap<BountyDetail>(response)
}

export async function postAdminBountyAction<T>(
  taskId: string,
  action: string,
  payload?: T
) {
  const response = await api.post(
    `/api/admin/bounties/${taskId}/${action}`,
    payload
  )
  return unwrap<BountyDetail>(response)
}

export async function resolveAdminBountyDispute(
  taskId: string,
  payload: {
    dispute_id: string
    resolution_type:
      | 'pay_full'
      | 'pay_partial'
      | 'release'
      | 'changes_requested'
    amount?: number
    note?: string
  }
) {
  const response = await api.post(
    `/api/admin/bounties/${taskId}/resolve`,
    payload
  )
  return unwrap<BountyDetail>(response)
}

export async function assignBounty(taskId: string, applicationId: string) {
  return postBountyAction(taskId, 'assignment', {
    application_id: applicationId,
  })
}

export async function postMaterialReply(
  taskId: string,
  requestId: string,
  payload: { content: string; source_type?: string; source_url?: string }
) {
  const response = await api.post(
    `/api/bounties/${taskId}/material-requests/${requestId}/replies`,
    payload
  )
  return unwrap<BountyDetail>(response)
}

export async function handleMaterialTimeout(
  taskId: string,
  requestId: string,
  payload: { action: 'extend' | 'cancel'; extension_hours?: number }
) {
  const response = await api.post(
    `/api/bounties/${taskId}/material-requests/${requestId}/timeout`,
    payload
  )
  return unwrap<BountyDetail>(response)
}

export async function reportBounty(
  taskId: string,
  payload: { reason: string; details?: string }
) {
  const response = await api.post(`/api/bounties/${taskId}/reports`, payload)
  return unwrap<BountyDetail>(response)
}

export async function resolveMaterialRequest(
  taskId: string,
  requestId: string
) {
  const response = await api.post(
    `/api/bounties/${taskId}/material-requests/${requestId}/resolve`
  )
  return unwrap<BountyDetail>(response)
}

export async function markBountyNotificationRead(notificationId: string) {
  await api.post(`/api/bounties/notifications/${notificationId}/read`)
}

export async function markAllBountyNotificationsRead() {
  await api.post('/api/bounties/notifications/read-all')
}
