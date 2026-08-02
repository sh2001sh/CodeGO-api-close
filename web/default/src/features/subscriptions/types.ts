/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { z } from 'zod'

// ============================================================================
// Subscription Plan Schema & Types
// ============================================================================

export const subscriptionPlanSchema = z.object({
  id: z.number(),
  title: z.string(),
  subtitle: z.string().optional(),
  price_amount: z.number(),
  currency: z.string().default('USD'),
  duration_unit: z.enum(['year', 'month', 'day', 'hour', 'custom']),
  duration_value: z.number(),
  custom_seconds: z.number().optional(),
  quota_reset_period: z.enum(['never', 'daily', 'weekly', 'monthly', 'custom']),
  quota_reset_custom_seconds: z.number().optional(),
  enabled: z.boolean(),
  internal_only: z.boolean().default(false),
  sort_order: z.number(),
  max_purchase_per_user: z.number(),
  plan_type: z
    .enum(['monthly', 'weekly', 'daily', 'starter'])
    .default('monthly')
    .optional(),
  membership_tier: z
    .enum(['none', 'lite', 'standard', 'pro', 'ultra'])
    .default('none')
    .optional(),
  lucky_draw_enabled: z.boolean().default(false).optional(),
  blind_box_benefit_count: z.number().default(0).optional(),
  group_buy_enabled: z.boolean().default(false).optional(),
  group_buy_bonus_2: z.number().default(0).optional(),
  group_buy_bonus_3: z.number().default(0).optional(),
  group_buy_bonus_5: z.number().default(0).optional(),
  fuel_enabled: z.boolean().default(false).optional(),
  fuel_unit_price: z.number().default(0).optional(),
  fuel_min_quota: z.number().default(0).optional(),
  fuel_quota_step: z.number().default(0).optional(),
  total_amount: z.number(),
  period_amount: z.number().optional(),
  model_limits: z.string().optional(),
  upgrade_group: z.string().optional(),
  stripe_price_id: z.string().optional(),
  creem_product_id: z.string().optional(),
})

export type SubscriptionPlan = z.infer<typeof subscriptionPlanSchema>

export const subscriptionLuckyNumberSchema = z.object({
  id: z.number(),
  user_subscription_id: z.number(),
  user_id: z.number(),
  card_code: z.string(),
  lucky_suffix: z.string(),
  assigned_at: z.number(),
  created_at: z.number(),
  updated_at: z.number(),
})

export type SubscriptionLuckyNumber = z.infer<
  typeof subscriptionLuckyNumberSchema
>

export interface PlanRecord {
  plan: SubscriptionPlan
  action?: 'subscribe' | 'renew' | 'upgrade' | 'disabled'
  base_amount_due?: number
  amount_due?: number
  disabled_reason?: string
  first_purchase_discount_applied?: boolean
  first_purchase_discount_multiplier?: number
}

// ============================================================================
// User Subscription Schema & Types
// ============================================================================

export const userSubscriptionSchema = z.object({
  id: z.number(),
  user_id: z.number(),
  plan_id: z.number(),
  status: z.string(),
  source: z.string().optional(),
  start_time: z.number(),
  end_time: z.number(),
  amount_total: z.number(),
  amount_used: z.number(),
  period_amount: z.number().optional(),
  period_used: z.number().optional(),
  model_limits: z.string().optional(),
  model_usage: z.string().optional(),
  next_reset_time: z.number().optional(),
  conversion_preview: z
    .object({
      eligible: z.boolean(),
      max_source_quota: z.number(),
      preview_claude_quota: z.number(),
    })
    .optional(),
  lucky_number: subscriptionLuckyNumberSchema.optional(),
  membership_tier: z
    .enum(['none', 'lite', 'standard', 'pro', 'ultra'])
    .optional(),
})

export type UserSubscription = z.infer<typeof userSubscriptionSchema>

export interface UserSubscriptionRecord {
  subscription: UserSubscription
}

export interface SubscriptionResetOpportunitySummary {
  available_count: number
  earned_total: number
  used_total: number
  used_this_month: boolean
  current_month: string
  last_used_month: string
}

export interface SubscriptionResetOpportunityUseResult {
  reset_opportunity: SubscriptionResetOpportunitySummary
  subscription_id: number
  amount_used_before: number
  amount_used_after: number
  period_used_before: number
  period_used_after: number
  cleared_used_amount: number
}

export interface SubscriptionClaudeConversionConfig {
  enabled: boolean
  ratio_numerator: number
  ratio_denominator: number
  exclude_day_pass: boolean
}

export interface SubscriptionClaudeConversionRecord {
  id: number
  user_id: number
  user_subscription_id: number
  request_id: string
  status: string
  source_quota: number
  target_claude_quota: number
  ratio_numerator: number
  ratio_denominator: number
  created_at: number
  updated_at: number
}

export interface SubscriptionClaudeConversionResult {
  subscription_id: number
  source_quota: number
  target_claude_quota: number
  claude_quota_after: number
  amount_used_after: number
  period_used_after: number
  conversion: SubscriptionClaudeConversionRecord
  config: SubscriptionClaudeConversionConfig
}

// ============================================================================
// API Request/Response Types
// ============================================================================

export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export interface PlanPayload {
  plan: Partial<SubscriptionPlan>
}

export interface SubscriptionPayRequest {
  plan_id: number
  payment_method?: string
  purchase_type?: SubscriptionPurchaseType
  group_buy_id?: number
}

export type SubscriptionPurchaseType = 'normal' | 'group_buy' | 'join_group'

export interface SubscriptionPayResponse {
  success: boolean
  message?: string
  data?: {
    pay_link?: string
    checkout_url?: string
    pay_url?: string
    qrcode_url?: string
    order_id?: string
    amount_due?: number
    action?: 'subscribe' | 'renew' | 'upgrade' | 'disabled'
    form?: Record<string, unknown>
  }
  url?: string
}

export interface SubscriptionFuelQuote {
  subscription_id: number
  plan_id: number
  plan_title: string
  quota: number
  unit_price: number
  amount_due: number
  expires_at: number
  min_quota: number
  quota_step: number
  quota_per_unit: number
}

export interface SubscriptionOrderStatus {
  trade_no: string
  status: 'pending' | 'success' | 'expired' | string
  plan_id: number
  plan_title?: string
  money: number
  original_money?: number
  first_purchase_discount_applied?: boolean
  first_purchase_discount_multiplier?: number
  payment_method?: string
  payment_provider?: string
  create_time?: number
  complete_time?: number
  fulfillment_status?: 'pending' | 'completed' | string
  target_subscription_id?: number
  lucky_number?: SubscriptionLuckyNumber & {
    membership_tier?: string
  }
  blind_box_benefit?: {
    expected_count: number
    granted_count: number
    membership_tier: string
    starts_at: number
    ends_at: number
    status: string
  }
}

export interface CreateUserSubscriptionRequest {
  plan_id: number
}

export interface UpdateUserSubscriptionRequest {
  start_time: number
  end_time: number
  status: string
  amount_total: number
  amount_used: number
  period_amount: number
  period_used: number
  model_limits: string
}

export interface ResetUserSubscriptionQuotaRequest {
  advance_reset_time?: boolean
}

// ============================================================================
// Self Subscription Data (user-facing)
// ============================================================================

export interface SelfSubscriptionData {
  billing_preference: string
  funding_source_order: FundingSource[]
  subscription_order_ids: number[]
  subscriptions: UserSubscriptionRecord[]
  all_subscriptions: UserSubscriptionRecord[]
  reset_opportunity: SubscriptionResetOpportunitySummary
  claude_quota: number
  conversion_config: SubscriptionClaudeConversionConfig
  recent_conversions: SubscriptionClaudeConversionRecord[]
}

export type FundingSource = 'subscription' | 'wallet'

// ============================================================================
// Dialog Types
// ============================================================================

export type SubscriptionsDialogType =
  | 'create'
  | 'update'
  | 'toggle-status'
  | 'delete'
