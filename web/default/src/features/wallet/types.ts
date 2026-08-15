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
// ============================================================================
// Wallet Type Definitions
// ============================================================================

/**
 * Generic API response
 */
export interface ApiResponse<T = unknown> {
  success?: boolean
  message?: string
  data?: T
}

export interface RedemptionResult {
  redeem_type: 'quota' | 'subscription' | 'blind_box' | string
  quota?: number
  wallet_type?: WalletType
  plan_id?: number
  plan_title?: string
  blind_box_quantity?: number
  blind_box_order_id?: number
  blind_box_open_count?: number
  blind_box_records?: BlindBoxRecord[]
  user_subscription_id?: number
}

/**
 * Standard API response types
 */
export type TopupInfoResponse = ApiResponse<TopupInfo>
export type RedemptionResponse = ApiResponse<RedemptionResult>
export type AmountResponse = ApiResponse<string>
export type PaymentResponse = ApiResponse<Record<string, unknown>> & {
  url?: string
}
export type StripePaymentResponse = ApiResponse<{ pay_link: string }>
export type AffiliateCodeResponse = ApiResponse<string>
export type AffiliateRewardsOverviewResponse =
  ApiResponse<AffiliateRewardsOverview>
export type CreemPaymentResponse = ApiResponse<{ checkout_url: string }>
export type WaffoPaymentResponse = ApiResponse<
  { payment_url?: string } | string
>
export type WaffoPancakePaymentResponse = ApiResponse<
  | {
      checkout_url?: string
      session_id?: string
      expires_at?: number | string
      order_id?: string
    }
  | string
>

/**
 * Creem product configuration
 */
export interface CreemProduct {
  /** Product display name */
  name: string
  /** Creem product ID */
  productId: string
  /** Product price */
  price: number
  /** Quota amount to credit */
  quota: number
  /** Currency (USD or EUR) */
  currency: 'USD' | 'EUR'
}

/**
 * Creem payment request
 */
export interface CreemPaymentRequest {
  /** Creem product ID */
  product_id: string
  /** Payment method identifier */
  payment_method: 'creem'
}

export type WalletType = 'default' | 'claude'

export type WalletQuotaConversionDirection =
  | 'standard_to_claude'
  | 'claude_to_standard'

export interface WalletQuotaConversion {
  id: number
  user_id: number
  request_id: string
  direction: WalletQuotaConversionDirection
  status: string
  source_quota: number
  target_quota: number
  standard_quota_before: number
  standard_quota_after: number
  claude_quota_before: number
  claude_quota_after: number
  created_at: number
}

export interface WalletQuotaConversionOverview {
  standard_per_claude: number
  quota_per_usd: number
  standard_quota: number
  claude_quota: number
  recent_conversions: WalletQuotaConversion[]
}

export interface WalletQuotaConversionRequest {
  direction: WalletQuotaConversionDirection
  source_quota: number
  request_id: string
}

export type WalletQuotaConversionOverviewResponse =
  ApiResponse<WalletQuotaConversionOverview>
export type WalletQuotaConversionResponse = ApiResponse<WalletQuotaConversion>

export interface WalletTransferSecurityOverview {
  password_set: boolean
  locked_until: number
  remaining_password_attempts: number
  requires_account_password: boolean
}

export interface WalletTransferHistoryItem {
  id: number
  request_id: string
  direction: 'incoming' | 'outgoing'
  counterparty_external_id: string
  counterparty_display_name_masked: string
  amount_quota: number
  fee_quota: number
  total_debit_quota: number
  balance_after: number
  status: string
  created_at: number
}

export interface WalletTransferHistoryPage {
  page: number
  page_size: number
  total: number
  items: WalletTransferHistoryItem[]
}

export interface WalletTransferOverview {
  quota_per_usd: number
  min_quota: number
  balance: number
  fee_bps: number
  security: WalletTransferSecurityOverview
  history: WalletTransferHistoryPage
}

export interface WalletTransferRecipient {
  external_id: string
  display_name_masked: string
}

export interface ConfigureWalletTransferPasswordRequest {
  current_password?: string
  old_payment_password?: string
  new_payment_password: string
  confirm_password: string
}

export interface CreateWalletTransferRequest {
  recipient_external_id: string
  amount_quota: number
  payment_password: string
  request_id: string
}

export type WalletTransferOverviewResponse = ApiResponse<WalletTransferOverview>
export type WalletTransferRecipientResponse =
  ApiResponse<WalletTransferRecipient>
export type WalletTransferResponse = ApiResponse<WalletTransferHistoryItem>

/**
 * Payment method configuration
 */
export interface PaymentMethod {
  /** Display name of payment method */
  name: string
  /** Payment method type identifier */
  type: string
  /** Optional color for UI display */
  color?: string
  /** Minimum topup amount for this payment method */
  min_topup?: number
  /** Optional icon URL provided by backend (preferred over built-in icons) */
  icon?: string
}

/**
 * Waffo payment method configuration
 */
export interface WaffoPayMethod {
  /** Display name of payment method */
  name: string
  /** Optional icon path */
  icon?: string
  /** Waffo pay method type */
  payMethodType?: string
  /** Waffo pay method name */
  payMethodName?: string
}

/**
 * Topup configuration information
 */
export interface TopupInfo {
  /** Whether online topup is enabled */
  enable_online_topup: boolean
  /** Whether Stripe topup is enabled */
  enable_stripe_topup: boolean
  /** Available payment methods */
  pay_methods: PaymentMethod[]
  /** Minimum topup amount for online topup */
  min_topup: number
  /** Minimum topup amount for Stripe */
  stripe_min_topup: number
  /** Preset amount options */
  amount_options: number[]
  /** Discount rates by amount */
  discount: Record<number, number>
  /** Optional topup link for purchasing codes */
  topup_link?: string
  /** Whether Creem topup is enabled */
  enable_creem_topup?: boolean
  /** Available Creem products */
  creem_products?: CreemProduct[]
  /** Whether Waffo topup is enabled */
  enable_waffo_topup?: boolean
  /** Available Waffo payment methods */
  waffo_pay_methods?: WaffoPayMethod[]
  /** Minimum topup amount for Waffo */
  waffo_min_topup?: number
  /** Whether Waffo Pancake topup is enabled */
  enable_waffo_pancake_topup?: boolean
  /** Minimum topup amount for Waffo Pancake */
  waffo_pancake_min_topup?: number
  /** Whether redemption code usage is enabled */
  enable_redemption?: boolean
  /** Whether compliance confirmation has been completed */
  payment_compliance_confirmed?: boolean
  /** Current compliance terms version */
  payment_compliance_terms_version?: string
}

/**
 * Preset amount option with optional discount
 */
export interface PresetAmount {
  /** Preset amount value */
  value: number
  /** Optional discount rate (0-1) */
  discount?: number
}

/**
 * Redemption code request
 */
export interface RedemptionRequest {
  /** Redemption code key */
  key: string
}

/**
 * Payment request parameters
 */
export interface PaymentRequest {
  /** Topup amount */
  amount: number
  /** Payment method identifier */
  payment_method: string
  /** Target wallet balance pool */
  wallet_type?: WalletType
}

/**
 * Waffo payment request parameters
 */
export interface WaffoPaymentRequest {
  /** Topup amount */
  amount: number
  /** Target wallet balance pool */
  wallet_type?: WalletType
  /** Optional server-side Waffo payment method index */
  pay_method_index?: number
}

/**
 * Waffo Pancake payment request parameters
 */
export interface WaffoPancakePaymentRequest {
  /** Topup amount */
  amount: number
  /** Target wallet balance pool */
  wallet_type?: WalletType
}

/**
 * Amount calculation request
 */
export interface AmountRequest {
  /** Topup amount to calculate */
  amount: number
  /** Target wallet balance pool */
  wallet_type?: WalletType
}

export interface SubscriptionResetOpportunitySummary {
  available_count: number
  earned_total: number
  used_total: number
  used_this_month: boolean
  current_month: string
  last_used_month: string
}

export interface AffiliateInviteeRewardStatus {
  invitee_id: number
  invitee_external_id: string
  invitee_username: string
  invitee_display_name?: string
  created_at: number
  month_card_purchased: boolean
  reset_opportunity_earned: boolean
  reset_opportunity_earned_at: number
}

export interface AffiliateRewardsOverview {
  affiliate_code: string
  invited_count: number
  successful_purchase_invites: number
  reset_opportunity: SubscriptionResetOpportunitySummary
  invitees: AffiliateInviteeRewardStatus[]
}

/**
 * User wallet data
 */
export interface UserWalletData {
  /** User ID */
  id: number
  /** Username */
  username: string
  /** Current quota balance */
  quota: number
  /** Claude-only quota balance */
  claude_quota?: number
  /** Total used quota */
  used_quota: number
  /** Total request count */
  request_count: number
  /** Affiliate quota (pending rewards) */
  aff_quota: number
  /** Total affiliate quota earned (historical) */
  aff_history_quota: number
  /** Number of successful affiliate invites */
  aff_count: number
  /** User group */
  group: string
}

/**
 * Topup record status
 */
export type TopupStatus = 'success' | 'pending' | 'expired'

/**
 * Topup billing record
 */
export interface TopupRecord {
  /** Record ID */
  id: number
  /** User ID */
  user_id: number
  /** Topup amount (quota) */
  amount: number
  /** Payment amount (actual money paid) */
  money: number
  /** Trade/order number */
  trade_no: string
  /** Payment method type */
  payment_method: string
  /** Target wallet balance pool */
  wallet_type?: WalletType
  /** Creation timestamp */
  create_time: number
  /** Completion timestamp */
  complete_time?: number
  /** Payment status */
  status: TopupStatus
}

/**
 * Billing history response
 */
export interface BillingHistoryResponse {
  items: TopupRecord[]
  total: number
}

/**
 * Complete order request (admin only)
 */
export interface CompleteOrderRequest {
  trade_no: string
}

export interface BlindBoxTier {
  name: string
  min_usd: number
  max_usd: number
  probability: number
  reward_type?: 'quota' | 'claude_quota' | 'prop' | 'subscription' | string
  wallet_type?: 'default' | 'claude' | string
}

export interface BlindBoxProp {
  id: number
  user_id: number
  open_record_id: number
  prop_type: string
  title: string
  status:
    | 'available'
    | 'active'
    | 'paused'
    | 'reserved'
    | 'used'
    | 'expired'
    | string
  discount_rate: number
  multiplier: number
  duration_seconds: number
  remaining_seconds?: number
  activated_at?: number
  expires_at?: number
  reserved_at?: number
  used_at?: number
  reserved_order_type?: string
  reserved_order_trade_no?: string
  benefit_reference?: string
  created_at: number
  updated_at: number
}

export interface BlindBoxRecord {
  id: number
  reward_type: 'quota' | 'claude_quota' | 'subscription' | string
  reward_wallet_type?: 'default' | 'claude' | string
  reward_usd: number
  credit_amount: number
  reward_title: string
  reward_tier: string
  pool_type?: 'standard' | 'balance_15' | string
  user_subscription_id?: number
  is_pity?: boolean
  create_time: number
  prop_id?: number
  prop_type?: string
  prop_status?: string
  prop_expires_at?: number
  /** One-day lucky number issued with this opened box. */
  lucky_number?: string
  lucky_draw_date?: string
  lucky_expires_at?: number
}

export interface BlindBoxHistoryPage {
  page: number
  page_size: number
  total: number
  retention_days: number
  cutoff_time: number
  records: BlindBoxRecord[]
}

export interface BlindBoxGrant {
  id: number
  user_id: number
  admin_user_id: number
  blind_box_order_id: number
  quantity: number
  reason: string
  trade_no: string
  created_at: number
}

export interface BlindBoxOverview {
  available_boxes: number
  pending_boxes: number
  // Mirrors the user's main wallet quota. Blind-box rewards are credited
  // into the normal wallet immediately, so this is not a separate pool.
  remaining_quota: number
  claude_quota: number
  pity_progress: number
  pity_threshold: number
  effective_pity_threshold: number
  purchased_today: number
  purchased_this_month: number
  recent_records: BlindBoxRecord[]
}

export interface BlindBoxZeroHourOverview {
  current_probability: number
  max_probability: number
  points: number
  point_cap: number
  active: boolean
  active_until?: number
}

export interface BlindBoxRewardStatistics {
  reward_type: string
  opened_count: number
  reward_usd: number
  credit_amount: number
}

export interface BlindBoxStatistics {
  total_opened: number
  pity_wins: number
  rewards: BlindBoxRewardStatistics[]
}

export interface BlindBoxSelfData {
  enabled: boolean
  unit_price: number
  daily_limit: number
  monthly_limit: number
  daily_open_limit: number
  first_purchase_guarantee_usd: number
  first_purchase_guarantee_eligible: boolean
  count_options: number[]
  tiers: BlindBoxTier[]
  subscription_prize_probability: number
  subscription_plan_title: string
  pity_threshold: number
  pity_guarantee_usd: number
  low_reward_threshold_usd: number
  pay_methods: PaymentMethod[]
  overview: BlindBoxOverview
  props: BlindBoxProp[]
  zero_hour?: BlindBoxZeroHourOverview
  statistics?: BlindBoxStatistics
  grants?: BlindBoxGrant[]
  balance_blind_box?: BalanceBlindBoxOverview
}

export interface BalanceBlindBoxOverview {
  enabled: boolean
  price_usd: number
  balance_usd: number
  tiers: BlindBoxTier[]
  inventory_count: number
  purchased_today: number
  daily_purchase_limit: number
  remaining_purchase_limit: number
  pity_progress: number
  pity_threshold: number
  pity_guarantee_usd: number
  small_pity_progress: number
  small_pity_threshold: number
  small_pity_guarantee_usd: number
  first_draw_guarantee_usd: number
  first_draw_eligible: boolean
}

export interface BalanceBlindBoxPurchase {
  id: number
  request_id: string
  quantity: number
  unit_price_usd: number
  total_quota: number
  purchase_date: string
  status: string
  created_at: number
}

export interface BalanceBlindBoxGift {
  id: number
  request_id: string
  sender_external_id: string
  recipient_external_id: string
  sender_display_name_masked: string
  recipient_display_name_masked: string
  quantity: number
  status: string
  created_at: number
}

export interface BlindBoxOrderStatus {
  trade_no: string
  status: 'pending' | 'success' | 'expired' | string
  quantity: number
  opened_count: number
  money: number
  payment_method?: string
  payment_provider?: string
  create_time?: number
  complete_time?: number
}

export type BlindBoxSelfResponse = ApiResponse<BlindBoxSelfData>
export type BlindBoxOpenResponse = ApiResponse<{
  records: BlindBoxRecord[]
  overview: BlindBoxOverview
  open_count: number
}>
export type BlindBoxOrderStatusResponse = ApiResponse<BlindBoxOrderStatus>
export type BlindBoxHistoryResponse = ApiResponse<BlindBoxHistoryPage>

export interface BlindBoxAmountRequest {
  quantity: number
}

export interface BlindBoxPayRequest {
  quantity: number
  payment_method: string
}

export interface BlindBoxOpenRequest {
  count: number
}
