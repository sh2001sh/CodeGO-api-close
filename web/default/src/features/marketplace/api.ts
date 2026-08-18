import { api } from '@/lib/api'
import type {
  ChannelFormValues,
  ChannelUpdateValues,
  GroupFilters,
  MarketplaceChannel,
  MarketplaceAutoRoutePool,
  MarketplaceGroupList,
  MarketplaceOwnerUsageLogResult,
  MarketplaceOwnerUsageLogFilters,
  MarketplaceMultiplierTrend,
  ChannelFeedbackSummary,
  AdminMarketplaceChannelFilters,
  AdminOwnerIncomeResult,
  TokenOption,
} from './types'

interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export async function submitMarketplaceChannelFeedback(input: {
  groupId: string
  status: 'passed' | 'failed' | 'questionable'
}) {
  const response = await api.post<ApiResponse<ChannelFeedbackSummary>>(
    `/api/marketplace/groups/${input.groupId}/feedback`,
    { status: input.status }
  )
  return requireData(response.data)
}

function requireData<T>(response: ApiResponse<T>): T {
  if (!response.success || response.data == null) {
    throw new Error(response.message || '请求失败')
  }
  return response.data
}

export async function getMarketplaceGroups(filters: GroupFilters) {
  const params = new URLSearchParams()
  Object.entries(filters).forEach(([key, value]) => {
    if (value !== '') params.set(key, String(value))
  })
  const response = await api.get<ApiResponse<MarketplaceGroupList>>(
    `/api/marketplace/groups?${params.toString()}`
  )
  return requireData(response.data)
}

export async function getMarketplaceMultiplierTrends(input: {
  rangeHours: number
  model: string
}) {
  const params = new URLSearchParams({ range_hours: String(input.rangeHours) })
  if (input.model) params.set('model', input.model)
  const response = await api.get<ApiResponse<MarketplaceMultiplierTrend>>(
    `/api/marketplace/multiplier-trends?${params.toString()}`
  )
  return requireData(response.data)
}

export async function getMyMarketplaceChannels() {
  const response = await api.get<ApiResponse<MarketplaceChannel[]>>(
    '/api/marketplace/channels/mine'
  )
  return requireData(response.data)
}

export async function getMarketplaceAutoRoutePool() {
  const response = await api.get<ApiResponse<MarketplaceAutoRoutePool>>(
    '/api/marketplace/auto-route-pool'
  )
  return requireData(response.data)
}

export async function updateMarketplaceAutoRoutePool(groupIds: string[]) {
  const response = await api.put<ApiResponse<MarketplaceAutoRoutePool>>(
    '/api/marketplace/auto-route-pool',
    { group_ids: groupIds }
  )
  return requireData(response.data)
}

export async function getMyMarketplaceUsageLogs(
  params: MarketplaceOwnerUsageLogFilters
) {
  const search = new URLSearchParams({
    page: String(params.page),
    page_size: String(params.pageSize),
  })
  if (params.channelId) search.set('channel_id', params.channelId)
  if (params.startTimestamp) {
    search.set('start_timestamp', String(params.startTimestamp))
  }
  if (params.endTimestamp) {
    search.set('end_timestamp', String(params.endTimestamp))
  }
  const response = await api.get<ApiResponse<MarketplaceOwnerUsageLogResult>>(
    `/api/marketplace/channels/mine/logs?${search.toString()}`
  )
  return requireData(response.data)
}

export async function createMarketplaceChannel(values: ChannelFormValues) {
  const response = await api.post<ApiResponse<MarketplaceChannel>>(
    '/api/marketplace/channels',
    values
  )
  return requireData(response.data)
}

export async function updateMarketplaceChannel(
  channelId: string,
  values: ChannelUpdateValues,
  admin: boolean
) {
  const prefix = admin ? '/api/marketplace/admin' : '/api/marketplace'
  const response = await api.patch<ApiResponse<MarketplaceChannel>>(
    `${prefix}/channels/${channelId}`,
    values
  )
  return requireData(response.data)
}

export async function deleteMarketplaceChannel(
  channelId: string,
  admin: boolean
) {
  const prefix = admin ? '/api/marketplace/admin' : '/api/marketplace'
  const response = await api.delete<ApiResponse>(
    `${prefix}/channels/${channelId}`
  )
  if (!response.data.success) {
    throw new Error(response.data.message || '删除渠道失败')
  }
}

export async function fetchMarketplaceModels(
  values: Pick<ChannelFormValues, 'provider_type' | 'base_url' | 'api_key'>
) {
  const response = await api.post<ApiResponse<string[]>>(
    '/api/marketplace/channels/fetch-models',
    values
  )
  return requireData(response.data)
}

export async function queueMarketplaceVerification(
  channelId: string,
  admin = false
) {
  const prefix = admin ? '/api/marketplace/admin' : '/api/marketplace'
  const response = await api.post<ApiResponse<{ queued: boolean }>>(
    `${prefix}/channels/${channelId}/verify`
  )
  return requireData(response.data)
}

export async function queueMarketplaceDetection(
  channelId: string,
  admin = false
) {
  return queueMarketplaceChannelAction(channelId, 'detect', admin)
}

export async function queueMarketplaceConnectivityTest(
  channelId: string,
  admin = false
) {
  return queueMarketplaceChannelAction(channelId, 'test', admin)
}

async function queueMarketplaceChannelAction(
  channelId: string,
  action: 'detect' | 'test',
  admin: boolean
) {
  const prefix = admin ? '/api/marketplace/admin' : '/api/marketplace'
  const response = await api.post<ApiResponse<{ queued: boolean }>>(
    `${prefix}/channels/${channelId}/${action}`
  )
  return requireData(response.data)
}

export async function setMarketplaceChannelPaused(
  channelId: string,
  paused: boolean
) {
  const action = paused ? 'pause' : 'resume'
  const response = await api.post<ApiResponse>(
    `/api/marketplace/channels/${channelId}/${action}`
  )
  if (!response.data.success)
    throw new Error(response.data.message || '请求失败')
}

export async function bindMarketplaceToken(groupId: string, tokenId: number) {
  const response = await api.post<ApiResponse>(
    `/api/marketplace/groups/${groupId}/bind-token`,
    { token_id: tokenId }
  )
  if (!response.data.success)
    throw new Error(response.data.message || '绑定失败')
}

export async function getTokenOptions(): Promise<TokenOption[]> {
  const response = await api.get<ApiResponse<{ items: TokenOption[] }>>(
    '/api/token/?p=1&size=50'
  )
  return requireData(response.data).items
}

export async function getAdminMarketplaceChannels(
  filters: AdminMarketplaceChannelFilters
) {
  const search = new URLSearchParams()
  if (filters.search) search.set('search', filters.search)
  if (filters.status) search.set('status', filters.status)
  if (filters.source) search.set('source', filters.source)
  if (filters.provider) search.set('provider', filters.provider)
  if (filters.verification) search.set('verification', filters.verification)
  if (filters.mappingStatus) search.set('mapping_status', filters.mappingStatus)
  if (filters.ownerSearch) search.set('owner_search', filters.ownerSearch)
  if (filters.startTimestamp) {
    search.set('start_timestamp', String(filters.startTimestamp))
  }
  if (filters.endTimestamp) {
    search.set('end_timestamp', String(filters.endTimestamp))
  }
  const response = await api.get<ApiResponse<MarketplaceChannel[]>>(
    `/api/marketplace/admin/channels?${search.toString()}`
  )
  return requireData(response.data)
}

export async function getAdminOwnerIncome(
  filters: Pick<
    AdminMarketplaceChannelFilters,
    'ownerSearch' | 'startTimestamp' | 'endTimestamp'
  >
) {
  const search = new URLSearchParams()
  if (filters.ownerSearch) search.set('owner_search', filters.ownerSearch)
  if (filters.startTimestamp) {
    search.set('start_timestamp', String(filters.startTimestamp))
  }
  if (filters.endTimestamp) {
    search.set('end_timestamp', String(filters.endTimestamp))
  }
  const response = await api.get<ApiResponse<AdminOwnerIncomeResult>>(
    `/api/marketplace/admin/owner-income?${search.toString()}`
  )
  return requireData(response.data)
}

export async function reviewMarketplaceChannel(
  channelId: string,
  approved: boolean,
  reason: string
) {
  const response = await api.post<ApiResponse<MarketplaceChannel>>(
    `/api/marketplace/admin/channels/${channelId}/review`,
    { approved, reason }
  )
  return requireData(response.data)
}
