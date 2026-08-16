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
import { api } from '@/lib/api'
import type {
  RedemptionRequest,
  PaymentRequest,
  AmountRequest,
  ApiResponse,
  TopupInfoResponse,
  RedemptionResponse,
  AmountResponse,
  PaymentResponse,
  StripePaymentResponse,
  AffiliateCodeResponse,
  AffiliateRewardsOverviewResponse,
  BillingHistoryResponse,
  CompleteOrderRequest,
  CreemPaymentRequest,
  CreemPaymentResponse,
  WaffoPaymentRequest,
  WaffoPaymentResponse,
  WaffoPancakePaymentRequest,
  WaffoPancakePaymentResponse,
  BlindBoxSelfResponse,
  BlindBoxAmountRequest,
  BlindBoxPayRequest,
  BlindBoxOpenRequest,
  BlindBoxOpenResponse,
  BlindBoxOrderStatusResponse,
  BlindBoxProp,
  BlindBoxRecord,
  BlindBoxHistoryResponse,
  BalanceBlindBoxOverview,
  BalanceBlindBoxGift,
  BalanceBlindBoxPurchase,
  ConfigureWalletTransferPasswordRequest,
  CreateWalletTransferRequest,
  WalletTransferOverviewResponse,
  WalletTransferRecipientResponse,
  WalletTransferResponse,
  WalletTransferEmailCodeResponse,
  UnifiedCreditMigrationDetailResponse,
  BalanceBlindBoxSimulationResult,
} from './types'

// ============================================================================
// Wallet API Functions
// ============================================================================

/**
 * Check if API response is successful
 */
export function isApiSuccess(response: ApiResponse): boolean {
  return response.success === true || response.message === 'success'
}

export async function getUnifiedCreditMigrationDetail(): Promise<UnifiedCreditMigrationDetailResponse> {
  const res = await api.get('/api/wallet/unified-credit-migration')
  return res.data
}

/**
 * Get topup configuration info
 */
export async function getTopupInfo(): Promise<TopupInfoResponse> {
  const res = await api.get('/api/user/topup/info')
  return res.data
}

export async function getWalletTransfers(
  page = 1,
  pageSize = 10
): Promise<WalletTransferOverviewResponse> {
  const params = new URLSearchParams({
    p: String(page),
    page_size: String(pageSize),
  })
  const res = await api.get(`/api/wallet/transfers?${params.toString()}`)
  return res.data
}

export async function lookupWalletTransferRecipient(
  externalId: string
): Promise<WalletTransferRecipientResponse> {
  const res = await api.get(
    `/api/wallet/transfers/recipients/${encodeURIComponent(externalId)}`
  )
  return res.data
}

export async function configureWalletTransferPassword(
  request: ConfigureWalletTransferPasswordRequest
): Promise<ApiResponse<{ password_set: boolean }>> {
  const res = await api.put('/api/wallet/transfers/payment-password', request)
  return res.data
}

export async function sendWalletTransferPasswordEmailCode(): Promise<WalletTransferEmailCodeResponse> {
  const res = await api.post(
    '/api/wallet/transfers/payment-password/email-code'
  )
  return res.data
}

export async function createWalletTransfer(
  request: CreateWalletTransferRequest
): Promise<WalletTransferResponse> {
  const res = await api.post('/api/wallet/transfers', request)
  return res.data
}

/**
 * Redeem a topup code
 */
export async function redeemTopupCode(
  request: RedemptionRequest
): Promise<RedemptionResponse> {
  const res = await api.post('/api/user/topup', request)
  return res.data
}

/**
 * Calculate payment amount for regular payment
 */
export async function calculateAmount(
  request: AmountRequest
): Promise<AmountResponse> {
  const res = await api.post('/api/user/amount', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Calculate payment amount for Stripe payment
 */
export async function calculateStripeAmount(
  request: AmountRequest
): Promise<AmountResponse> {
  const res = await api.post('/api/user/stripe/amount', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Request regular payment
 */
export async function requestPayment(
  request: PaymentRequest
): Promise<PaymentResponse> {
  const res = await api.post('/api/user/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return {
    ...res.data,
    url: res.data.url || (res as unknown as { url?: string }).url,
  }
}

/**
 * Request Stripe payment
 */
export async function requestStripePayment(
  request: PaymentRequest
): Promise<StripePaymentResponse> {
  const res = await api.post('/api/user/stripe/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Request Creem payment
 */
export async function requestCreemPayment(
  request: CreemPaymentRequest
): Promise<CreemPaymentResponse> {
  const res = await api.post('/api/user/creem/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Request Waffo payment
 */
export async function requestWaffoPayment(
  request: WaffoPaymentRequest
): Promise<WaffoPaymentResponse> {
  const res = await api.post('/api/user/waffo/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Calculate payment amount for Waffo Pancake payment
 */
export async function calculateWaffoPancakeAmount(
  request: AmountRequest
): Promise<AmountResponse> {
  const res = await api.post('/api/user/waffo-pancake/amount', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Request Waffo Pancake payment
 */
export async function requestWaffoPancakePayment(
  request: WaffoPancakePaymentRequest
): Promise<WaffoPancakePaymentResponse> {
  const res = await api.post('/api/user/waffo-pancake/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Get affiliate code
 */
export async function getAffiliateCode(): Promise<AffiliateCodeResponse> {
  const res = await api.get('/api/user/aff')
  return res.data
}

export async function getAffiliateRewardsOverview(): Promise<AffiliateRewardsOverviewResponse> {
  const res = await api.get('/api/user/aff/overview')
  return res.data
}

/**
 * Get billing history for current user
 */
export async function getUserBillingHistory(
  page: number,
  pageSize: number,
  keyword?: string
): Promise<ApiResponse<BillingHistoryResponse>> {
  const params = new URLSearchParams({
    p: page.toString(),
    page_size: pageSize.toString(),
  })
  if (keyword) {
    params.append('keyword', keyword)
  }
  const res = await api.get(`/api/user/topup/self?${params.toString()}`)
  return res.data
}

/**
 * Get billing history for all users (admin only)
 */
export async function getAllBillingHistory(
  page: number,
  pageSize: number,
  keyword?: string
): Promise<ApiResponse<BillingHistoryResponse>> {
  const params = new URLSearchParams({
    p: page.toString(),
    page_size: pageSize.toString(),
  })
  if (keyword) {
    params.append('keyword', keyword)
  }
  const res = await api.get(`/api/user/topup?${params.toString()}`)
  return res.data
}

/**
 * Complete a pending order (admin only)
 */
export async function completeOrder(
  request: CompleteOrderRequest
): Promise<ApiResponse> {
  const res = await api.post('/api/user/topup/complete', request)
  return res.data
}

export async function getBlindBoxSelf(): Promise<BlindBoxSelfResponse> {
  const res = await api.get('/api/blind-box/self')
  return res.data
}

export async function getBlindBoxHistory(
  page: number,
  pageSize: number
): Promise<BlindBoxHistoryResponse> {
  const params = new URLSearchParams({
    p: String(page),
    page_size: String(pageSize),
  })
  const res = await api.get(`/api/blind-box/history?${params.toString()}`)
  return res.data
}

export async function calculateBlindBoxAmount(
  request: BlindBoxAmountRequest
): Promise<AmountResponse> {
  const res = await api.post('/api/blind-box/amount', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

export async function requestBlindBoxPayment(
  request: BlindBoxPayRequest
): Promise<PaymentResponse> {
  const res = await api.post('/api/blind-box/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return {
    ...res.data,
    url: res.data.url || (res as unknown as { url?: string }).url,
  }
}

export async function getBlindBoxOrderStatus(
  tradeNo: string
): Promise<BlindBoxOrderStatusResponse> {
  const res = await api.get(`/api/blind-box/orders/${tradeNo}`)
  return res.data
}

export async function openBlindBoxes(
  request: BlindBoxOpenRequest
): Promise<BlindBoxOpenResponse> {
  const res = await api.post('/api/blind-box/open', request)
  return res.data
}

export async function activateBlindBoxProp(
  propId: number
): Promise<ApiResponse<{ prop: BlindBoxProp }>> {
  const res = await api.post(`/api/blind-box/props/${propId}/use`)
  return res.data
}

export async function pauseBlindBoxProp(
  propId: number
): Promise<ApiResponse<{ prop: BlindBoxProp }>> {
  const res = await api.post(`/api/blind-box/props/${propId}/pause`)
  return res.data
}

export async function convertBlindBoxProp(
  propId: number,
  targetType: 'topup_discount_90' | 'subscription_discount_90'
): Promise<ApiResponse<{ prop: BlindBoxProp }>> {
  const res = await api.post(`/api/blind-box/props/${propId}/convert`, {
    target_type: targetType,
  })
  return res.data
}

export async function openBalanceBlindBox(
  requestId: string,
  count = 1
): Promise<
  ApiResponse<{
    record: BlindBoxRecord
    records: BlindBoxRecord[]
    balance_usd: number
    overview: BalanceBlindBoxOverview
  }>
> {
  const res = await api.post('/api/blind-box/inventory/open', {
    request_id: requestId,
    count,
  })
  return res.data
}

export async function purchaseBalanceBlindBoxes(
  requestId: string,
  count: number
): Promise<
  ApiResponse<{
    purchase: BalanceBlindBoxPurchase
    overview: BalanceBlindBoxOverview
  }>
> {
  const res = await api.post('/api/blind-box/inventory/purchase', {
    request_id: requestId,
    count,
  })
  return res.data
}

export async function giftBalanceBlindBoxes(
  requestId: string,
  recipientExternalId: string,
  count: number
): Promise<
  ApiResponse<{
    gift: BalanceBlindBoxGift
    overview: BalanceBlindBoxOverview
    recipient: {
      external_id: string
      display_name_masked: string
    }
  }>
> {
  const res = await api.post('/api/blind-box/inventory/gift', {
    request_id: requestId,
    recipient_external_id: recipientExternalId,
    count,
  })
  return res.data
}

export async function simulateBalanceBlindBoxes(
  balanceQuota: number,
  count: number
): Promise<ApiResponse<BalanceBlindBoxSimulationResult>> {
  const res = await api.post('/api/blind-box/simulation/draw', {
    balance_quota: balanceQuota,
    count,
  })
  return res.data
}
