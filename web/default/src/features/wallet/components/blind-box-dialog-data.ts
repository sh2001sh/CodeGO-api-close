import interfaceI18n from '@/i18n/config'
import { getIntlLocale } from '@/i18n/format'
import { formatQuota } from '@/lib/format'
import type { BlindBoxRecord, BlindBoxSelfData, PaymentMethod } from '../types'

export function canQuoteBlindBoxAmount(
  data: Pick<BlindBoxSelfData, 'enabled' | 'inventory' | 'pay_methods'> | null,
  quantity: number,
  method: PaymentMethod | null,
  ready: boolean
): boolean {
  return Boolean(
    ready &&
    data?.enabled &&
    data.inventory?.enabled &&
    method &&
    data.pay_methods.some((available) => available.type === method.type) &&
    Number.isInteger(quantity) &&
    quantity > 0 &&
    quantity <= data.inventory.remaining_purchase_limit
  )
}

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
  return new Date(timestamp * 1000).toLocaleString(getIntlLocale())
}

/** Resolve the payment method name shown in blind-box purchase flows. */
export function getBlindBoxMethodLabel(
  method?: {
    type?: string
    name?: string
  } | null
) {
  if (!method) return interfaceI18n.t('未选择')
  if (method.type === 'xunhu') return interfaceI18n.t('微信支付（XunhuPay）')
  return method.name || method.type || interfaceI18n.t('在线支付')
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
  if (subscriptionHits > 0)
    parts.push(
      interfaceI18n.t('{{param0}} 个套餐', { param0: subscriptionHits })
    )
  if (creditRecords.length > 0) {
    parts.push(
      interfaceI18n.t('{{param0}} 通用额度', {
        param0: formatQuota(creditTotal),
      })
    )
  }
  if (propHits > 0)
    parts.push(interfaceI18n.t('{{param0}} 个道具', { param0: propHits }))
  return parts.length > 0
    ? interfaceI18n.t('获得 {{param0}}', { param0: parts.join('、') })
    : interfaceI18n.t('获得 {{param0}} 项奖励', { param0: records.length })
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
