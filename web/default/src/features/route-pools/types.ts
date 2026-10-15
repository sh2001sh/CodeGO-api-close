export type RoutePoolMember = {
  channel_id: number
  cost_multiplier: number
  model_cost_overrides: string
  fault_domain: string
  enabled: boolean
}

export type RoutePool = {
  id: number
  name: string
  group: string
  enabled: boolean
  auto_discover: boolean
}

export type RoutePoolGroup = {
  group: string
  pool_id: number
  enabled: boolean
  algorithm_active: boolean
  auto_discover: boolean
  channels: Array<{
    channel_id: number
    channel_name: string
    channel_status: number
    models: string
    enabled: boolean
    cost_multiplier: number
    model_cost_overrides: string
    fault_domain: string
  }>
}

export type RoutePoolDetail = {
  pool: RoutePool
  members: RoutePoolMember[]
}

export type FundingPolicy = {
  source: 'topup' | 'blind_box' | 'subscription'
  revenue_multiplier: number
}

export type RoutePoolMetrics = {
  members: Array<{
    channel_id: number
    channel_name: string
    eligible: boolean
    score: number
    health: {
      state: string
      success_rate_5m: number
      ttft_p50_ms: number
      ttft_p95_ms: number
      cooling_until?: string
    }
  }>
}

export type RoutePoolDraft = RoutePool & { members: RoutePoolMember[] }

export const createBlankRoutePoolDraft = (): RoutePoolDraft => ({
  id: 0,
  name: '',
  group: '',
  enabled: true,
  auto_discover: false,
  members: [],
})
