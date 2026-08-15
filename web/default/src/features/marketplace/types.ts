export type MarketplaceStatus =
  | 'draft'
  | 'verifying'
  | 'pending_review'
  | 'active'
  | 'degraded'
  | 'suspended'
  | 'disabled'

export type ModelConsistencyStatus = '' | 'passed' | 'failed' | 'questionable'

export interface ModelVerificationResult {
  model: string
  status: 'passed' | 'failed'
  listed: boolean
  latency_ms: number
  error?: string
  tested_at: string
}

export interface MarketplaceGroup {
  id: string
  channel_id: string
  public_slug: string
  system_display_name: string
  source_type: 'marketplace_user' | 'official'
  source_label: string
  provider_type: string
  credit_pool_policy: string
  lifecycle_status: MarketplaceStatus
  verification_status: string
  verification_stage: string
  verification_summary: string
  verification_detector_version: string
  verification_started_at?: string | null
  verification_completed_at?: string | null
  verification_due_at?: string | null
  multiplier: number
  models: string[]
  model_verification_results: ModelVerificationResult[]
  model_consistency_status: ModelConsistencyStatus
  rank: number
  score: number
  success_rate: number
  wilson_success_rate: number
  avg_ttft_ms: number
  avg_latency_ms: number
  avg_tps: number
  request_count: number
  independent_consumers: number
  observing: boolean
  updated_at: string
}

export interface MarketplaceGroupList {
  items: MarketplaceGroup[]
  total: number
  page: number
  page_size: number
  ranked_count: number
  window_hours: number
}

export interface MarketplaceChannel {
  id: string
  group_id: string
  public_slug: string
  system_display_name: string
  provider_type: string
  submitted_source_label: string
  approved_source_label: string
  source_label_status: 'pending' | 'approved' | 'rejected'
  source_label_review_reason: string
  credential_tail: string
  credential_version: number
  declared_models: string[]
  multiplier: number
  lifecycle_status: MarketplaceStatus
  verification_status: string
  verification_stage: string
  verification_summary: string
  verification_detector_version: string
  verification_started_at?: string | null
  verification_completed_at?: string | null
  model_verification_results: ModelVerificationResult[]
  model_consistency_status: ModelConsistencyStatus
  visibility: string
  max_concurrency: number
  qps: number
  maintenance_window: string
  internal_channel_id?: number | null
  last_review_reason: string
  verification_due_at?: string | null
  request_count: number
  total_income: number
  pending_income: number
  released_income: number
  created_at: string
  updated_at: string
}

export interface ChannelFormValues {
  provider_type: string
  source_label: string
  base_url: string
  api_key: string
  declared_models: string[]
  multiplier: number
  visibility: string
  max_concurrency: number
  qps: number
  maintenance_window: string
}

export interface ChannelUpdateValues {
  provider_type: string
  source_label: string
  declared_models: string[]
  multiplier: number
  visibility: string
  max_concurrency: number
  qps: number
  maintenance_window: string
  base_url?: string
  api_key?: string
  model_consistency_status?: ModelConsistencyStatus
}

export interface GroupFilters {
  search: string
  model: string
  source: string
  provider: string
  status: string
  verification: string
  sort: string
  direction: string
  window_hours: number
  page: number
  page_size: number
}

export interface TokenOption {
  id: number
  name: string
  group?: string | null
}

export interface MarketplaceOwnerUsageLog {
  id: number
  channel_id: string
  channel_name: string
  group_id: string
  user_id: string
  created_at: number
  status: 'success' | 'failed'
  model_name: string
  prompt_tokens: number
  completion_tokens: number
  use_time: number
  is_stream: boolean
  request_id: string
  consumer_amount: number
  owner_income: number
  platform_commission: number
  multiplier: number
  income_status: 'pending' | 'released' | 'none'
  available_at?: string | null
  released_at?: string | null
}

export interface MarketplaceOwnerUsageLogResult {
  items: MarketplaceOwnerUsageLog[]
  summary: {
    request_count: number
    success_count: number
    failed_count: number
    consumer_amount: number
    owner_income: number
  }
  total: number
  page: number
  page_size: number
}

export interface MarketplaceAutoRoutePoolItem {
  group_id: string
  public_slug: string
  system_display_name: string
  source_label: string
  lifecycle_status: MarketplaceStatus
  multiplier: number
  availability: number
  route_score: number
  observing: boolean
  request_count: number
  models: string[]
  selected: boolean
  priority: number
}

export interface MarketplaceAutoRoutePool {
  token_group: 'market:auto'
  selected_count: number
  items: MarketplaceAutoRoutePoolItem[]
}
