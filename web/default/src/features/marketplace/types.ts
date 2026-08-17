export type MarketplaceStatus =
  | 'draft'
  | 'verifying'
  | 'pending_review'
  | 'active'
  | 'degraded'
  | 'suspended'
  | 'disabled'

export type ModelConsistencyStatus = '' | 'passed' | 'failed' | 'questionable'

export type GPT56MappingStatus =
  | ''
  | 'running'
  | 'matched'
  | 'mismatch'
  | 'insufficient_evidence'

export type ConnectivityTestStatus =
  | ''
  | 'queued'
  | 'running'
  | 'passed'
  | 'failed'

export interface ModelVerificationResult {
  model: string
  status: 'passed' | 'failed'
  listed: boolean
  latency_ms: number
  error?: string
  tested_at: string
}

export interface GPT56MappingResult {
  requested_model: string
  reported_model?: string
  status: Exclude<GPT56MappingStatus, '' | 'running'>
  latency_ms: number
  sample_count: number
  matched_samples: number
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
  connectivity_test_status: ConnectivityTestStatus
  connectivity_test_checked_at?: string | null
  model_consistency_status: ModelConsistencyStatus
  auto_probe_enabled: boolean
  auto_probe_interval_minutes: number
  auto_probe_model: string
  channel_feedback: ChannelFeedbackSummary
  can_submit_channel_feedback: boolean
  channel_feedback_permission: 'allowed' | 'owner' | 'login_required'
  rank: number
  score: number
  success_rate: number
  wilson_success_rate: number
  avg_ttft_ms: number
  avg_latency_ms: number
  avg_tps: number
  cache_hit_rate: number
  latest_request_status: 'healthy' | 'unstable' | 'failed' | 'unknown'
  recent_request_series: Array<{
    ts: number
    success_rate: number
    request_count: number
  }>
  recent_request_bucket_seconds: number
  request_count: number
  max_concurrency: number
  current_concurrency: number
  observing: boolean
  updated_at: string
}

export interface ChannelFeedbackSummary {
  passed: number
  failed: number
  questionable: number
  total: number
  viewer_status: ModelConsistencyStatus
}

export interface MarketplaceGroupList {
  items: MarketplaceGroup[]
  highlights: MarketplaceGroupHighlights
  total: number
  page: number
  page_size: number
  ranked_count: number
  window_hours: number
}

export interface MarketplaceGroupHighlight {
  group_id: string
  system_display_name: string
  score: number
  multiplier: number
  avg_ttft_ms: number
}

export interface MarketplaceGroupHighlights {
  best?: MarketplaceGroupHighlight | null
  cheapest?: MarketplaceGroupHighlight | null
  fastest?: MarketplaceGroupHighlight | null
}

export interface MarketplaceChannel {
  id: string
  owner_user_id: number
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
  model_prices: Record<string, ChannelModelPrice>
  multiplier: number
  lifecycle_status: MarketplaceStatus
  verification_status: string
  verification_stage: string
  verification_summary: string
  verification_detector_version: string
  verification_started_at?: string | null
  verification_completed_at?: string | null
  model_verification_results: ModelVerificationResult[]
  connectivity_test_status: ConnectivityTestStatus
  connectivity_test_checked_at?: string | null
  model_consistency_status: ModelConsistencyStatus
  gpt56_mapping_results: GPT56MappingResult[]
  gpt56_mapping_status: GPT56MappingStatus
  gpt56_mapping_checked_at?: string | null
  auto_probe_enabled: boolean
  auto_probe_interval_minutes: number
  auto_probe_model: string
  auto_probe_last_status: ConnectivityTestStatus
  auto_probe_last_at?: string | null
  visibility: string
  max_concurrency: number
  qps: number
  maintenance_window: string
  sensitive_word_interception_enabled: boolean
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

export interface AdminMarketplaceChannelFilters {
  status?: string
  startTimestamp?: number
  endTimestamp?: number
}

export interface AdminOwnerIncomeItem {
  owner_user_id: number
  request_count: number
  total_income: number
  pending_income: number
  released_income: number
}

export interface AdminOwnerIncomeResult {
  items: AdminOwnerIncomeItem[]
  owner_count: number
  request_count: number
  total_income: number
  pending_income: number
  released_income: number
}

export interface ChannelFormValues {
  provider_type: string
  source_label: string
  base_url: string
  api_key: string
  declared_models: string[]
  model_prices: Record<string, ChannelModelPrice>
  multiplier: number
  visibility: string
  max_concurrency: number
  qps: number
  maintenance_window: string
  sensitive_word_interception_enabled: boolean
}

export interface ChannelUpdateValues {
  provider_type: string
  source_label: string
  declared_models: string[]
  model_prices: Record<string, ChannelModelPrice>
  multiplier: number
  visibility: string
  max_concurrency: number
  qps: number
  maintenance_window: string
  sensitive_word_interception_enabled: boolean
  base_url?: string
  api_key?: string
  model_consistency_status?: ModelConsistencyStatus
  auto_probe_enabled: boolean
  auto_probe_interval_minutes: number
  auto_probe_model: string
}

export interface ChannelModelPrice {
  input_price_per_million: number
  output_price_per_million: number
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

export interface MarketplaceOwnerUsageLogFilters {
  channelId?: string
  startTimestamp?: number
  endTimestamp?: number
  page: number
  pageSize: number
}

export interface MarketplaceAutoRoutePoolItem {
  group_id: string
  source_type: 'official' | 'marketplace_user'
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
  token_group: 'auto'
  selected_count: number
  items: MarketplaceAutoRoutePoolItem[]
}
