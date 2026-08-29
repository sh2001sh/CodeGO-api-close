import { formatQuota } from '@/lib/format'
import type { BlindBoxRecord } from '../types'

export type PaymentStage = 'idle' | 'pending' | 'success' | 'failed'

export interface BlindBoxPaymentState {
  open: boolean
  stage: PaymentStage
  orderId: string
  amountDue: number
  methodLabel: string
  payUrl: string
  qrCodeUrl: string
  formUrl: string
  formFields: Record<string, unknown> | null
  quantity: number
  message: string
  pollingStartTime?: number
  retryPayload?: {
    quantity: number
    paymentMethod: string
  }
}

export interface PrizeDialogState {
  open: boolean
  records: BlindBoxRecord[]
  openCount: number
}

export const EMPTY_PAYMENT_STATE: BlindBoxPaymentState = {
  open: false,
  stage: 'idle',
  orderId: '',
  amountDue: 0,
  methodLabel: '',
  payUrl: '',
  qrCodeUrl: '',
  formUrl: '',
  formFields: null,
  quantity: 0,
  message: '',
}

export const EMPTY_PRIZE_STATE: PrizeDialogState = {
  open: false,
  records: [],
  openCount: 0,
}

/** Format a blind-box event timestamp for the current locale. */
export function formatBlindBoxTimestamp(timestamp?: number) {
  if (!timestamp) return '--'
  return new Date(timestamp * 1000).toLocaleString()
}

/** Resolve the payment method name shown in blind-box purchase flows. */
export function getBlindBoxMethodLabel(
  method?: {
    type?: string
    name?: string
  } | null
) {
  if (!method) return '未选择'
  if (method.type === 'xunhu') return '微信支付'
  return method.name || method.type || '在线支付'
}

/** Summarize all rewards returned by one blind-box opening. */
export function summarizeOpenResult(records: BlindBoxRecord[]) {
  const subscriptionHits = records.filter(
    (record) => record.reward_type === 'subscription'
  ).length
  const propHits = records.filter(
    (record) => record.reward_type === 'prop'
  ).length
  const creditRecords = records.filter((record) =>
    ['quota', 'claude_quota'].includes(record.reward_type)
  )
  const creditTotal = creditRecords.reduce(
    (sum, record) => sum + (record.credit_amount || 0),
    0
  )

  const parts: string[] = []
  if (subscriptionHits > 0) parts.push(`${subscriptionHits} 个套餐`)
  if (creditRecords.length > 0) {
    parts.push(`${formatQuota(creditTotal)} 通用额度`)
  }
  if (propHits > 0) parts.push(`${propHits} 个道具`)
  return parts.length > 0
    ? `获得 ${parts.join('、')}`
    : `获得 ${records.length} 项奖励`
}

/** Resolve the visual tone for a blind-box reward. */
export function resolveRewardTone(record: BlindBoxRecord) {
  if (record.reward_type === 'subscription' || record.is_pity) {
    return 'border-primary/30 bg-primary/10 text-primary'
  }
  if (record.reward_type === 'claude_quota') {
    return 'border-violet-500/30 bg-violet-500/10 text-violet-700 dark:text-violet-300'
  }
  if (record.reward_type === 'prop') {
    return 'border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300'
  }
  return 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
}
