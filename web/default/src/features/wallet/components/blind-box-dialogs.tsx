import { Trophy } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import type { BlindBoxRecord } from '../types'
import { PrizeRevealHeader, PrizeRevealList } from './blind-box-prize-reveal'

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

export function formatBlindBoxTimestamp(timestamp?: number) {
  if (!timestamp) return '--'
  return new Date(timestamp * 1000).toLocaleString()
}

export function getBlindBoxMethodLabel(method?: {
  type?: string
  name?: string
} | null) {
  if (!method) return '未选择'
  if (method.type === 'xunhu') return '微信支付'
  return method.name || method.type || '在线支付'
}

export function summarizeOpenResult(records: BlindBoxRecord[]) {
  const subscriptionHits = records.filter(
    (record) => record.reward_type === 'subscription'
  ).length
  const propHits = records.filter((record) => record.reward_type === 'prop').length
  const quotaHits = records.filter((record) => record.reward_type === 'quota').length
  const claudeHits = records.filter(
    (record) => record.reward_type === 'claude_quota'
  ).length
  const quotaTotal = records
    .filter((record) => record.reward_type === 'quota')
    .reduce((sum, record) => sum + (record.reward_usd || 0), 0)
  const claudeQuotaTotal = records
    .filter((record) => record.reward_type === 'claude_quota')
    .reduce((sum, record) => sum + (record.reward_usd || 0), 0)

  const parts: string[] = []
  if (subscriptionHits > 0) parts.push(`${subscriptionHits} 个套餐`)
  if (quotaHits > 0) parts.push(`$${quotaTotal.toFixed(0)} 额度`)
  if (claudeHits > 0) parts.push(`$${claudeQuotaTotal.toFixed(0)} Claude 额度`)
  if (propHits > 0) parts.push(`${propHits} 个道具`)
  if (parts.length === 0) {
    return `获得 ${records.length} 项奖励`
  }
  return `获得 ${parts.join('、')}`
}

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

export function BlindBoxPrizeDialog(props: {
  state: PrizeDialogState
  onOpenChange: (open: boolean) => void
  onUseReward?: (record: BlindBoxRecord) => void
}) {
  return (
    <Dialog open={props.state.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='w-[calc(100vw-1rem)] max-w-2xl overflow-hidden p-0'>
        <DialogHeader className='border-b px-5 py-4'>
          <DialogTitle className='flex items-center gap-2 text-base'>
            <Trophy className='size-5 text-primary' />
            抽奖结果
          </DialogTitle>
        </DialogHeader>

        <div className='space-y-4 px-5 py-5'>
          <PrizeRevealHeader
            summary={summarizeOpenResult(props.state.records)}
            openCount={props.state.openCount}
            records={props.state.records}
          />

          <PrizeRevealList
            records={props.state.records}
            onUseReward={props.onUseReward}
            formatTimestamp={formatBlindBoxTimestamp}
          />

          <Button onClick={() => props.onOpenChange(false)}>确定</Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
