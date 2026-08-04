import type {
  ApiResponse,
  PlanRecord,
  SubscriptionLuckyNumber,
  SubscriptionPlan,
  UserSubscription,
} from '@/features/subscriptions/types'

export type { ApiResponse, SubscriptionLuckyNumber }

export type MembershipTier = 'none' | 'lite' | 'standard' | 'pro' | 'ultra'

export interface LuckyNumberSubscription {
  subscription: UserSubscription
  plan: SubscriptionPlan
  number?: SubscriptionLuckyNumber
}

export interface LuckyNumberRules {
  base_reward_1_usd: number
  base_reward_2_usd: number
  base_reward_3_usd: number
  base_reward_4_usd: number
  multiplier_lite: number
  multiplier_standard: number
  multiplier_pro: number
  multiplier_ultra: number
  jackpot_initial_usd: number
  jackpot_increment_usd: number
  jackpot_cap_usd: number
}

export interface LuckyDrawView {
  id: number
  draw_date: string
  winning_number: string
  jackpot_before: number
  jackpot_after: number
  full_match_count: number
  status: string
  drawn_at: number
  completed_at: number
}

export interface LuckyRewardRecord {
  id: number
  draw_id: number
  user_subscription_id: number
  lucky_number: string
  membership_tier: MembershipTier | string
  matched_digits: number
  base_reward_usd: number
  tier_multiplier: number
  jackpot_reward_usd: number
  final_reward_quota: number
  credit_status: string
  credited_at: number
}

export interface LuckyRewardView {
  reward: LuckyRewardRecord
  draw_date: string
  winning_number: string
  reward_usd: number
}

export interface LuckyPublicWin {
  draw_date: string
  winning_number: string
  membership_tier: MembershipTier | string
  lucky_suffix: string
  matched_digits: number
  reward_usd: number
}

export interface LuckyNumberSelfPayload {
  enabled: boolean
  timezone: string
  draw_hour: number
  draw_minute: number
  next_draw_at: number
  today_draw?: LuckyDrawView
  previous_draw?: LuckyDrawView
  jackpot_usd: number
  jackpot_cap_usd: number
  rules?: LuckyNumberRules
  subscriptions: LuckyNumberSubscription[]
  recent_rewards: LuckyRewardView[]
}

export interface LuckyRewardPage {
  page: number
  page_size: number
  total: number
  records: LuckyRewardView[]
}

export interface LuckyRewardNotification {
  id: number
  reward: LuckyRewardView
  read_at: number
  created_at: number
}

export interface LuckyRewardNotificationPage {
  unread_count: number
  items: LuckyRewardNotification[]
}

export interface LuckyPublicWinPage {
  page: number
  page_size: number
  total: number
  records: LuckyPublicWin[]
}

export interface DailyLuckyConfig {
  enabled: boolean
  timezone: string
  draw_hour: number
  draw_minute: number
  base_reward_1_usd: number
  base_reward_2_usd: number
  base_reward_3_usd: number
  base_reward_4_usd: number
  multiplier_lite: number
  multiplier_standard: number
  multiplier_pro: number
  multiplier_ultra: number
  jackpot_initial_usd: number
  jackpot_increment_usd: number
  jackpot_cap_usd: number
  cost_per_usd: number
  monthly_budget_usd: number
}

export interface LuckyDrawSnapshot {
  id: number
  draw_date: string
  winning_number: string
  jackpot_before: number
  jackpot_after: number
  full_match_count: number
  status: string
  error_message?: string
  timezone: string
  draw_hour: number
  draw_minute: number
  base_reward_1_usd: number
  base_reward_2_usd: number
  base_reward_3_usd: number
  base_reward_4_usd: number
  multiplier_lite: number
  multiplier_standard: number
  multiplier_pro: number
  multiplier_ultra: number
  jackpot_initial_usd: number
  jackpot_increment_usd: number
  jackpot_cap_usd: number
  cost_per_usd: number
  monthly_budget_usd: number
  drawn_at: number
  completed_at: number
  created_at: number
  updated_at: number
}

export interface LuckyDrawAdminView {
  draw: LuckyDrawSnapshot
  participant_count: number
  reward_count: number
  credited_count: number
  nominal_reward_usd: number
  actual_cost_cny: number
}

export interface LuckyDrawAdminPayload {
  config: DailyLuckyConfig
  draws: LuckyDrawAdminView[]
  page: number
  page_size: number
  total: number
  monthly_nominal_reward_usd: number
  monthly_actual_cost_cny: number
  monthly_budget_usd: number
  monthly_budget_usage_percent: number
}

export interface LuckyBackfillResult {
  scanned: number
  already_exists: number
  created: number
  failed: number
  failed_ids: number[]
}

export type DailyLuckySelfResponse = ApiResponse<LuckyNumberSelfPayload>
export type DailyLuckyHistoryResponse = ApiResponse<LuckyRewardPage>
export type DailyLuckyRewardNotificationResponse =
  ApiResponse<LuckyRewardNotificationPage>
export type DailyLuckyPublicWinsResponse = ApiResponse<LuckyPublicWinPage>
export type DailyLuckyAdminResponse = ApiResponse<LuckyDrawAdminPayload>
export type DailyLuckyConfigResponse = ApiResponse<DailyLuckyConfig>
export type DailyLuckyBackfillResponse = ApiResponse<LuckyBackfillResult>

export type DailyLuckyPlanRecord = PlanRecord
