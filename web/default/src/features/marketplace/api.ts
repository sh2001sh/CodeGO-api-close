import { api } from '@/lib/api'
import { MOCK_AUTO_ROUTE_POOL, MOCK_MARKETPLACE_GROUPS } from './lib/mock-data'
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
  MarketplaceBatchTest,
  MarketplaceObservability,
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
  try {
    const response = await api.get<ApiResponse<MarketplaceGroupList>>(
      `/api/marketplace/groups?${params.toString()}`
    )
    return requireData(response.data)
  } catch (error) {
    if (!import.meta.env.DEV) throw error
    const search = filters.search.trim().toLowerCase()
    const filtered = MOCK_MARKETPLACE_GROUPS.filter((item) => {
      if (
        search &&
        !`${item.system_display_name} ${item.models.join(' ')}`
          .toLowerCase()
          .includes(search)
      )
        return false
      if (filters.source && item.source_label !== filters.source) return false
      if (filters.provider && item.provider_type !== filters.provider)
        return false
      if (filters.model && !item.models.includes(filters.model)) return false
      return true
    })
    const start = (filters.page - 1) * filters.page_size
    const items = filtered.slice(start, start + filters.page_size)
    const cheapest = [...items].sort((a, b) => a.multiplier - b.multiplier)[0]
    const fastest = [...items].sort((a, b) => a.avg_ttft_ms - b.avg_ttft_ms)[0]
    const toHighlight = (item: (typeof items)[number] | undefined) =>
      item
        ? {
            group_id: item.id,
            system_display_name: item.system_display_name,
            score: item.score,
            multiplier: item.multiplier,
            avg_ttft_ms: item.avg_ttft_ms,
            attempt_ttft_p50_ms: item.attempt_ttft_p50_ms,
          }
        : undefined
    return {
      items,
      highlights: {
        best: toHighlight(items[0]),
        cheapest: toHighlight(cheapest),
        fastest: toHighlight(fastest),
      },
      total: filtered.length,
      page: filters.page,
      page_size: filters.page_size,
      ranked_count: filtered.length,
      window_hours: filters.window_hours,
    }
  }
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
  try {
    const response = await api.get<ApiResponse<MarketplaceAutoRoutePool>>(
      '/api/marketplace/auto-route-pool'
    )
    return requireData(response.data)
  } catch (error) {
    if (!import.meta.env.DEV) throw error
    return MOCK_AUTO_ROUTE_POOL
  }
}

export async function updateMarketplaceAutoRoutePool(input: {
  groupIds: string[]
  config?: MarketplaceAutoRoutePool['config']
}) {
  try {
    const response = await api.put<ApiResponse<MarketplaceAutoRoutePool>>(
      '/api/marketplace/auto-route-pool',
      { group_ids: input.groupIds, config: input.config }
    )
    return requireData(response.data)
  } catch (error) {
    if (!import.meta.env.DEV) throw error
    const items = input.groupIds.map((id, index) => {
      const source = MOCK_MARKETPLACE_GROUPS.find((group) => group.id === id)
      return {
        group_id: id,
        source_type: source?.source_type ?? 'marketplace_user',
        public_slug: source?.public_slug ?? id,
        system_display_name: source?.system_display_name ?? id,
        source_label: source?.source_label ?? '第三方市场',
        lifecycle_status: 'active' as const,
        multiplier: source?.multiplier ?? 1,
        availability: 0.98,
        success_rate: source?.success_rate ?? 0.96,
        cache_hit_rate: source?.cache_hit_rate ?? 0.3,
        avg_latency_ms: source?.avg_latency_ms ?? 800,
        latest_request_status: 'healthy',
        metrics_available: true,
        route_score: source?.score ?? 80,
        observing: false,
        request_count: source?.request_count ?? 0,
        models: source?.models ?? [],
        selected: true,
        priority: index + 1,
      }
    })
    return {
      token_group: 'auto' as const,
      selected_count: items.length,
      items,
      config: input.config ?? {
        strategy: 'priority' as const,
        max_attempts: 3,
        failure_cooldown_seconds: 30,
        max_multiplier: 0,
      },
    }
  }
}

export async function startMarketplaceBatchTest(input: {
  groupIds: string[]
  model: string
}) {
  const response = await api.post<ApiResponse<MarketplaceBatchTest>>(
    '/api/marketplace/batch-tests',
    { group_ids: input.groupIds, model: input.model }
  )
  return requireData(response.data)
}

export async function getMarketplaceBatchTest(id: string) {
  const response = await api.get<ApiResponse<MarketplaceBatchTest>>(
    `/api/marketplace/batch-tests/${encodeURIComponent(id)}`
  )
  return requireData(response.data)
}

export async function getMarketplaceObservability(input?: {
  startTimestamp?: number
  endTimestamp?: number
}) {
  const params = new URLSearchParams()
  if (input?.startTimestamp)
    params.set('start_timestamp', String(input.startTimestamp))
  if (input?.endTimestamp)
    params.set('end_timestamp', String(input.endTimestamp))
  const response = await api.get<ApiResponse<MarketplaceObservability>>(
    `/api/marketplace/channels/mine/observability?${params.toString()}`
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
  if (params.status) search.set('status', params.status)
  if (params.modelName) search.set('model_name', params.modelName)
  if (params.requestId) search.set('request_id', params.requestId)
  if (params.upstreamRequestId) {
    search.set('upstream_request_id', params.upstreamRequestId)
  }
  if (params.externalUserId) {
    search.set('external_user_id', params.externalUserId)
  }
  if (params.search) search.set('search', params.search)
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

export async function retryMarketplaceFailedConnectivity(
  channelId: string,
  admin = false
) {
  return queueMarketplaceChannelAction(channelId, 'test/failed', admin)
}

export async function removeMarketplaceFailedModel(input: {
  channelId: string
  model: string
  admin?: boolean
}) {
  const prefix = input.admin ? '/api/marketplace/admin' : '/api/marketplace'
  const response = await api.post<ApiResponse<MarketplaceChannel>>(
    `${prefix}/channels/${input.channelId}/models/remove-failed`,
    { model: input.model }
  )
  return requireData(response.data)
}

export async function pauseMarketplaceVerification(
  channelId: string,
  admin = false
) {
  const prefix = admin ? '/api/marketplace/admin' : '/api/marketplace'
  const response = await api.post<ApiResponse<{ paused: boolean }>>(
    `${prefix}/channels/${channelId}/verification/pause`
  )
  return requireData(response.data)
}

async function queueMarketplaceChannelAction(
  channelId: string,
  action: 'detect' | 'test' | 'test/failed',
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

export async function setMarketplaceChannelUserBlock(input: {
  channelId: string
  userId: number
  blocked: boolean
}) {
  const response = await api.post<ApiResponse>(
    `/api/marketplace/channels/${input.channelId}/user-block`,
    { user_id: input.userId, blocked: input.blocked }
  )
  if (!response.data.success)
    throw new Error(response.data.message || '操作失败')
}

export async function setAdminMarketplaceChannelPaused(
  channelId: string,
  paused: boolean
) {
  const response = await api.post<ApiResponse>(
    `/api/marketplace/admin/channels/${channelId}/${paused ? 'pause' : 'resume'}`
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

export async function createMarketplaceGroupInvite(groupId: string) {
  const response = await api.post<
    ApiResponse<{
      token: string
      group_id: string
      group_name: string
      expires_at?: string
    }>
  >(`/api/marketplace/groups/${groupId}/invite`)
  return requireData(response.data)
}

export async function acceptMarketplaceGroupInvite(token: string) {
  const response = await api.post<
    ApiResponse<{
      group_id: string
      group_name: string
      expires_at?: string
    }>
  >('/api/marketplace/invites/accept', { token })
  return requireData(response.data)
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

export async function releaseAdminOwnerIncome(
  filters: Pick<
    AdminMarketplaceChannelFilters,
    'ownerSearch' | 'ownerUserIds' | 'startTimestamp' | 'endTimestamp'
  >
) {
  const search = new URLSearchParams()
  if (filters.ownerSearch) search.set('owner_search', filters.ownerSearch)
  if (filters.ownerUserIds?.length)
    search.set('owner_user_ids', filters.ownerUserIds.join(','))
  if (filters.startTimestamp)
    search.set('start_timestamp', String(filters.startTimestamp))
  if (filters.endTimestamp)
    search.set('end_timestamp', String(filters.endTimestamp))
  const response = await api.post<
    ApiResponse<{ reclaimed_count: number; reclaimed_amount: number }>
  >(`/api/marketplace/admin/owner-income/release?${search.toString()}`)
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
