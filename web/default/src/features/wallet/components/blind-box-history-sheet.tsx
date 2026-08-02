import { useEffect, useMemo, useState } from 'react'
import {
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  Gift,
  Loader2,
  PackageCheck,
  Sparkles,
  TicketPercent,
} from 'lucide-react'
import { formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { getBlindBoxHistory, isApiSuccess } from '../api'
import type { BlindBoxHistoryPage, BlindBoxRecord } from '../types'
import { formatBlindBoxTimestamp, resolveRewardTone } from './blind-box-dialogs'

const PAGE_SIZE = 20

const PROP_TITLES: Record<string, string> = {
  topup_discount_90: '充值九折卡',
  subscription_discount_90: '套餐九折卡',
  consume_discount_95: '0.95 倍率卡',
  consume_discount_90: '0.9 倍率卡',
}

export function BlindBoxHistorySheet(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const [page, setPage] = useState(1)
  const [data, setData] = useState<BlindBoxHistoryPage | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!props.open) return
    let active = true
    setLoading(true)
    setError('')
    void getBlindBoxHistory(page, PAGE_SIZE)
      .then((response) => {
        if (!active) return
        if (!isApiSuccess(response) || !response.data) {
          throw new Error(response.message || '加载开奖历史失败')
        }
        setData(response.data)
      })
      .catch((reason: unknown) => {
        if (!active) return
        setError(reason instanceof Error ? reason.message : '加载开奖历史失败')
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [page, props.open])

  useEffect(() => {
    if (props.open) setPage(1)
  }, [props.open])

  const totalPages = useMemo(
    () => Math.max(1, Math.ceil((data?.total || 0) / PAGE_SIZE)),
    [data?.total]
  )

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className='w-[calc(100vw-1rem)] sm:max-w-xl'>
        <SheetHeader className='border-b px-5 py-4 pr-14'>
          <SheetTitle className='flex items-center gap-2'>
            <CalendarDays className='text-primary size-5' />
            开奖历史
          </SheetTitle>
          <SheetDescription>
            展示最近 {data?.retention_days || 30} 天的抽取结果和具体奖励
          </SheetDescription>
        </SheetHeader>

        <div className='min-h-0 flex-1 overflow-y-auto px-5 py-4'>
          {loading ? (
            <div className='text-muted-foreground flex min-h-48 items-center justify-center gap-2 text-sm'>
              <Loader2 className='size-4 animate-spin' />
              正在加载开奖记录
            </div>
          ) : error ? (
            <div className='border-destructive/30 bg-destructive/5 text-destructive rounded-xl border px-4 py-6 text-center text-sm'>
              {error}
            </div>
          ) : !data?.records.length ? (
            <div className='border-border text-muted-foreground rounded-xl border border-dashed px-4 py-10 text-center text-sm'>
              最近 30 天还没有抽取记录
            </div>
          ) : (
            <div className='space-y-2.5'>
              {data.records.map((record) => (
                <HistoryRecord key={record.id} record={record} />
              ))}
            </div>
          )}
        </div>

        <div className='border-t px-5 py-3'>
          <div className='flex items-center justify-between gap-3'>
            <div className='text-muted-foreground text-xs tabular-nums'>
              共 {data?.total || 0} 条 · 第 {page}/{totalPages} 页
            </div>
            <div className='flex gap-2'>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={page <= 1 || loading}
                onClick={() => setPage((current) => Math.max(1, current - 1))}
              >
                <ChevronLeft className='size-4' />
                上一页
              </Button>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={page >= totalPages || loading}
                onClick={() =>
                  setPage((current) => Math.min(totalPages, current + 1))
                }
              >
                下一页
                <ChevronRight className='size-4' />
              </Button>
            </div>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  )
}

function HistoryRecord(props: { record: BlindBoxRecord }) {
  const record = props.record
  const detail = rewardDetail(record)
  const Icon = rewardIcon(record)

  return (
    <div className='border-border/70 bg-background/50 rounded-xl border p-3.5'>
      <div className='flex items-start gap-3'>
        <div
          className={cn(
            'flex size-9 shrink-0 items-center justify-center rounded-lg border',
            resolveRewardTone(record)
          )}
        >
          <Icon className='size-4' />
        </div>
        <div className='min-w-0 flex-1'>
          <div className='flex flex-wrap items-start justify-between gap-x-3 gap-y-1'>
            <div className='text-foreground font-medium break-words'>
              {detail.title}
            </div>
            <div className='text-muted-foreground shrink-0 text-xs tabular-nums'>
              {formatBlindBoxTimestamp(record.create_time)}
            </div>
          </div>
          <div className='text-muted-foreground mt-1 text-xs leading-5'>
            {detail.description}
          </div>
          <div className='mt-2 flex flex-wrap gap-1.5'>
            <HistoryTag>{detail.type}</HistoryTag>
            {record.is_pity ? <HistoryTag>保底奖励</HistoryTag> : null}
            {record.reward_type === 'prop' && record.prop_status ? (
              <HistoryTag>{propStatusLabel(record.prop_status)}</HistoryTag>
            ) : null}
          </div>
        </div>
      </div>
    </div>
  )
}

function HistoryTag(props: { children: React.ReactNode }) {
  return (
    <span className='border-border bg-muted/50 text-muted-foreground rounded-full border px-2 py-0.5 text-[11px] font-medium'>
      {props.children}
    </span>
  )
}

function rewardDetail(record: BlindBoxRecord) {
  if (record.reward_type === 'prop') {
    const title = PROP_TITLES[record.prop_type || ''] || record.reward_title
    return {
      title: title || '实用道具奖励',
      description: propDescription(record.prop_type || ''),
      type: '道具',
    }
  }
  if (record.reward_type === 'subscription') {
    return {
      title: record.reward_title || '套餐奖励',
      description: '套餐已自动发放到当前账户。',
      type: '套餐',
    }
  }
  if (record.reward_type === 'claude_quota') {
    return {
      title: record.reward_title || `$${record.reward_usd} Claude 额度`,
      description: `${formatQuota(record.credit_amount || 0)} 已进入 Claude 钱包，永久有效。`,
      type: 'Claude 额度',
    }
  }
  return {
    title: record.reward_title || `$${record.reward_usd} 普通额度`,
    description: `${formatQuota(record.credit_amount || 0)} 已进入普通钱包，永久有效。`,
    type: '普通额度',
  }
}

function rewardIcon(record: BlindBoxRecord) {
  if (record.reward_type === 'prop') return TicketPercent
  if (record.reward_type === 'subscription') return PackageCheck
  if (record.reward_type === 'claude_quota') return Sparkles
  return Gift
}

function propDescription(propType: string) {
  if (propType === 'topup_discount_90')
    return '下次钱包充值自动享受九折，仅使用一次。'
  if (propType === 'subscription_discount_90')
    return '下次购买套餐自动享受九折，仅使用一次。'
  if (propType === 'consume_discount_95')
    return '手动启用后，额度消耗倍率按 0.95 计算，持续 24 小时。'
  if (propType === 'consume_discount_90')
    return '手动启用后，额度消耗倍率按 0.9 计算，持续 24 小时。'
  return '道具已进入“我的道具”，可按页面规则使用。'
}

function propStatusLabel(status: string) {
  if (status === 'available') return '可使用'
  if (status === 'active') return '生效中'
  if (status === 'reserved') return '已预留'
  if (status === 'used') return '已使用'
  if (status === 'expired') return '已过期'
  return status
}
