import { api } from '@/lib/api'
import type {
  ChannelFormValues,
  ChannelUpdateValues,
  GroupFilters,
  MarketplaceChannel,
  MarketplaceAutoRoutePool,
  MarketplaceGroupList,
  MarketplaceOwnerUsageLogResult,
  TokenOption,
} from './types'

interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
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

export async function getMyMarketplaceUsageLogs(params: {
  channelId?: string
  page: number
  pageSize: number
}) {
  const search = new URLSearchParams({
    page: String(params.page),
    page_size: String(params.pageSize),
  })
  if (params.channelId) search.set('channel_id', params.channelId)
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

export async function fetchMarketplaceModels(
  values: Pick<ChannelFormValues, 'provider_type' | 'base_url' | 'api_key'>
) {
  const response = await api.post<ApiResponse<string[]>>(
    '/api/marketplace/channels/fetch-models',
    values
  )
  return requireData(response.data)
}

export async function queueMarketplaceVerification(channelId: string) {
  const response = await api.post<ApiResponse<{ queued: boolean }>>(
    `/api/marketplace/channels/${channelId}/verify`
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

export async function getAdminMarketplaceChannels(status = '') {
  const response = await api.get<ApiResponse<MarketplaceChannel[]>>(
    `/api/marketplace/admin/channels?status=${encodeURIComponent(status)}`
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
