import { useEffect, useMemo, useState } from 'react'
import interfaceI18n from '@/i18n/config'
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
import { useTranslation, Trans } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { getBlindBoxHistory, isApiSuccess } from '../api'
import { blindBoxGuaranteeLabel } from '../lib/blind-box-guarantee'
import type { BlindBoxHistoryPage, BlindBoxRecord } from '../types'
import {
  formatBlindBoxTimestamp,
  resolveRewardTone,
} from './blind-box-dialog-data'

const PAGE_SIZE = 20

const PROP_TITLES: Record<string, string> = {
  topup_discount_90: '九折充值卡',
  subscription_discount_90: '历史套餐折扣卡',
  consume_discount_95: '0.95 倍率卡',
  consume_discount_90: '0.9 倍率卡',
  consume_discount_10: '15 分钟 0.1 倍率卡',
  extra_draw: '再来一抽',
  monthly_pass_multiplier: '套餐 0.1 倍率卡',
  zero_hour_multiplier: '历史 0 倍率道具',
}

export function BlindBoxHistorySheet(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  useTranslation()

  const [page, setPage] = useState(1)
  const [data, setData] = useState<BlindBoxHistoryPage | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!props.open) return
    let active = true
    queueMicrotask(() => {
      if (!active) return
      setLoading(true)
      setError('')
    })
    void getBlindBoxHistory(page, PAGE_SIZE)
      .then((response) => {
        if (!active) return
        if (!isApiSuccess(response) || !response.data) {
          throw new Error(
            response.message || interfaceI18n.t('加载开奖历史失败')
          )
        }
        setData(response.data)
      })
      .catch((reason: unknown) => {
        if (!active) return
        setError(
          reason instanceof Error
            ? reason.message
            : interfaceI18n.t('加载开奖历史失败')
        )
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [page, props.open])

  useEffect(() => {
    if (props.open) queueMicrotask(() => setPage(1))
  }, [props.open])

  const totalPages = useMemo(
    () => Math.max(1, Math.ceil((data?.total || 0) / PAGE_SIZE)),
    [data?.total]
  )

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className='commerce-dialog w-[calc(100vw-1rem)] sm:max-w-xl'>
        <SheetHeader className='px-5 py-4 pr-14'>
          <SheetTitle className='flex items-center gap-2'>
            <CalendarDays className='text-primary size-5' />
            <Trans i18nKey={'开奖历史'} />
          </SheetTitle>
          <span className='text-muted-foreground text-sm tabular-nums'>
            {data?.retention_days || 30} <Trans i18nKey={'天'} />
          </span>
        </SheetHeader>

        <div className='min-h-0 flex-1 overflow-y-auto px-5 py-4'>
          {loading ? (
            <div className='text-muted-foreground flex min-h-48 items-center justify-center gap-2 text-sm'>
              <Loader2 className='size-4 animate-spin' />
              <Trans i18nKey={'正在加载开奖记录'} />
            </div>
          ) : error ? (
            <div className='border-destructive/30 bg-destructive/5 text-destructive rounded-xl border px-4 py-6 text-center text-sm'>
              {error}
            </div>
          ) : !data?.records.length ? (
            <div className='border-border text-muted-foreground rounded-xl border border-dashed px-4 py-10 text-center text-sm'>
              <Trans i18nKey={'最近 30 天还没有抽取记录'} />
            </div>
          ) : (
            <div className='space-y-2.5'>
              {data.records.map((record) => (
                <HistoryRecord key={record.id} record={record} />
              ))}
            </div>
          )}
        </div>

        <div className='bg-muted/40 px-5 py-3'>
          <div className='flex items-center justify-between gap-3'>
            <div className='text-muted-foreground text-xs tabular-nums'>
              <Trans i18nKey={'共'} /> {data?.total || 0}{' '}
              <Trans i18nKey={'条 · 第'} /> {page}/{totalPages}{' '}
              <Trans i18nKey={'页'} />
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
                <Trans i18nKey={'上一页'} />
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
                <Trans i18nKey={'下一页'} />
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

  return (
    <div className='bg-muted/45 rounded-xl p-3.5'>
      <div className='flex items-start gap-3'>
        <div
          className={cn(
            'flex size-9 shrink-0 items-center justify-center rounded-lg border',
            resolveRewardTone(record)
          )}
        >
          <RewardIcon record={record} />
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

          <RewardFacts record={record} />
          <div className='mt-2 flex flex-wrap gap-1.5'>
            <HistoryTag>
              <Trans i18nKey={'统一盲盒'} />
            </HistoryTag>
            <HistoryTag>{detail.type}</HistoryTag>
            {record.is_pity ? (
              <HistoryTag>{blindBoxGuaranteeLabel(record)}</HistoryTag>
            ) : null}
            {record.reward_type === 'prop' && record.prop_status ? (
              <HistoryTag>{propStatusLabel(record.prop_status)}</HistoryTag>
            ) : null}
          </div>
        </div>
      </div>
    </div>
  )
}

function RewardFacts(props: { record: BlindBoxRecord }) {
  const { t } = useTranslation()
  const record = props.record
  const facts: Array<{ label: string; value: string }> = []
  if (record.reward_type === 'quota' || record.reward_type === 'claude_quota') {
    facts.push({
      label: t('额度'),
      value:
        record.credit_amount > 0
          ? formatQuota(record.credit_amount)
          : `${record.reward_usd} credits`,
    })
    facts.push({ label: t('有效期'), value: t('永久有效') })
  }
  if (
    record.prop_type === 'consume_discount_95' ||
    record.prop_type === 'consume_discount_90'
  ) {
    facts.push({
      label: t('倍率'),
      value: record.prop_type === 'consume_discount_95' ? '0.95' : '0.90',
    })
    facts.push({
      label: t('生效时长'),
      value: t('{{param0}} 小时', { param0: 24 }),
    })
    facts.push({ label: t('适用范围'), value: t('仅官方渠道') })
  }
  if (record.prop_type === 'consume_discount_10') {
    facts.push({
      label: t('生效时长'),
      value: t('{{param0}} 分钟', { param0: 15 }),
    })
    facts.push({ label: t('适用范围'), value: t('全部现有官方分组') })
  }
  if (record.prop_type === 'monthly_pass_multiplier') {
    facts.push({
      label: t('适用范围'),
      value: `${t('官方分组')} · ${t('月卡额度')}`,
    })
  }
  if (record.prop_type === 'topup_discount_90') {
    facts.push({ label: t('次数'), value: '1' })
    facts.push({
      label: t('适用范围'),
      value: `${t('套餐购买')} / ${t('余额充值')}`,
    })
  }
  if (record.prop_type === 'extra_draw' || record.reward_title === '再来一抽') {
    facts.push({ label: t('盲盒数量'), value: '1' })
  }
  if (!facts.length) return null
  return (
    <dl className='mt-2 flex flex-wrap gap-x-4 gap-y-1 text-sm'>
      {facts.map((fact) => (
        <div key={fact.label} className='flex flex-wrap gap-x-2'>
          <dt className='text-muted-foreground'>{fact.label}</dt>
          <dd>{fact.value}</dd>
        </div>
      ))}
    </dl>
  )
}

function RewardIcon(props: { record: BlindBoxRecord }) {
  const className = 'size-4'
  if (props.record.reward_type === 'prop') {
    return <TicketPercent className={className} />
  }
  if (props.record.reward_type === 'subscription') {
    return <PackageCheck className={className} />
  }
  if (props.record.reward_type === 'claude_quota') {
    return <Sparkles className={className} />
  }
  return <Gift className={className} />
}

function HistoryTag(props: { children: React.ReactNode }) {
  return (
    <span className='border-border bg-muted/50 text-muted-foreground rounded-full border px-2 py-0.5 text-xs font-medium'>
      {props.children}
    </span>
  )
}

function rewardDetail(record: BlindBoxRecord) {
  if (record.reward_type === 'prop') {
    if (record.reward_title === '再来一抽') {
      return {
        title: interfaceI18n.t('再来一抽'),
        type: '额外机会',
      }
    }
    const title = PROP_TITLES[record.prop_type || ''] || record.reward_title
    return {
      title: title || interfaceI18n.t('实用道具奖励'),
      type: '道具',
    }
  }
  if (record.reward_type === 'subscription') {
    return {
      title: record.reward_title || interfaceI18n.t('套餐奖励'),
      type: '套餐',
    }
  }
  if (record.reward_type === 'claude_quota' || record.reward_type === 'quota') {
    return {
      title:
        record.reward_title ||
        (record.credit_amount > 0
          ? interfaceI18n.t('{{param0}} 通用额度', {
              param0: formatQuota(record.credit_amount),
            })
          : interfaceI18n.t('{{param0}} credits', {
              param0: record.reward_usd,
            })),
      type: '通用额度',
    }
  }
  return { title: record.reward_title, type: '奖励' }
}

function propStatusLabel(status: string) {
  if (status === 'available') return interfaceI18n.t('可使用')
  if (status === 'active') return interfaceI18n.t('生效中')
  if (status === 'reserved') return interfaceI18n.t('已预留')
  if (status === 'used') return interfaceI18n.t('已使用')
  if (status === 'expired') return interfaceI18n.t('已过期')
  return status
}
