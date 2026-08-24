import { useCallback, useEffect, useState } from 'react'
import { AlertTriangle, CheckCircle2, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'
import { getUnifiedCreditMigrationDetail, isApiSuccess } from '../api'
import type { UnifiedCreditMigrationDetail } from '../types'

const TIER_LABELS: Record<string, string> = {
  lite: 'Lite',
  standard: 'Standard',
  pro: 'Pro',
  ultra: 'Ultra',
}

export function SubscriptionSettlementHistory() {
  const { t } = useTranslation()
  const [data, setData] = useState<UnifiedCreditMigrationDetail | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const response = await getUnifiedCreditMigrationDetail()
      setData(isApiSuccess(response) && response.data ? response.data : null)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  return (
    <TitledCard
      title={t('Historical monthly card settlement')}
      icon={<CheckCircle2 className='size-4' />}
      action={
        <Button
          type='button'
          size='icon'
          variant='outline'
          onClick={() => void load()}
          disabled={loading}
          aria-label={t('Refresh settlement records')}
        >
          <RefreshCw className={loading ? 'size-4 animate-spin' : 'size-4'} />
        </Button>
      }
      contentClassName='space-y-5'
    >
      <p className='text-muted-foreground max-w-3xl text-sm leading-6'>
        {t(
          'Monthly cards are no longer sold. Each valid card is settled by its tier price and unused quota percentage, without checking how the card was issued. All credited unified balance is permanent.'
        )}
      </p>

      {loading ? (
        <SettlementSkeleton />
      ) : data?.migration || data?.settlements.length ? (
        <>
          <SettlementSummary data={data} />
          <div className='divide-border divide-y border-y'>
            {data.settlements.map((item) => {
              const unusedPercent =
                item.amount_total > 0
                  ? (item.unused_amount / item.amount_total) * 100
                  : 0
              const needsReview = item.status === 'review_required'
              return (
                <div
                  key={item.id}
                  className='grid gap-3 py-4 md:grid-cols-[minmax(0,1fr)_repeat(3,minmax(110px,auto))] md:items-center'
                >
                  <div className='min-w-0'>
                    <div className='flex flex-wrap items-center gap-2'>
                      <span className='text-foreground font-semibold'>
                        {TIER_LABELS[item.membership_tier] ||
                          item.membership_tier}
                      </span>
                      <span className='text-muted-foreground text-xs'>
                        #{item.user_subscription_id}
                      </span>
                      {needsReview ? (
                        <span className='text-destructive flex items-center gap-1 text-xs font-medium'>
                          <AlertTriangle className='size-3.5' />
                          {t('Review required')}
                        </span>
                      ) : null}
                    </div>
                    <p className='text-muted-foreground mt-1 text-xs leading-5'>
                      {needsReview
                        ? item.review_reason
                        : t('Settled with rule {{version}}', {
                            version: item.rule_version,
                          })}
                    </p>
                  </div>
                  <SettlementValue
                    label={t('Tier base price')}
                    value={formatAmount(item.base_price_cents / 100)}
                  />
                  <SettlementValue
                    label={t('Unused percentage')}
                    value={`${unusedPercent.toFixed(2)}%`}
                  />
                  <SettlementValue
                    label={t('Unified credit settled')}
                    value={formatQuota(item.settlement_quota)}
                    strong
                  />
                </div>
              )
            })}
          </div>
        </>
      ) : (
        <div className='border-border text-muted-foreground rounded-lg border border-dashed px-4 py-8 text-sm'>
          {t(
            'No monthly card settlement record is associated with this account.'
          )}
        </div>
      )}
    </TitledCard>
  )
}

function SettlementSummary(props: { data: UnifiedCreditMigrationDetail }) {
  const migration = props.data.migration
  const values = [
    ['原 GPT 额度折算', formatQuota(migration?.converted_unified_quota || 0)],
    ['月卡清算到账', formatQuota(migration?.subscription_unified_quota || 0)],
    ['清算记录', props.data.settlements.length],
  ] as const
  return (
    <div className='border-border grid gap-0 overflow-hidden rounded-lg border sm:grid-cols-3'>
      {values.map(([label, value], index) => (
        <div
          key={label}
          className={
            index > 0
              ? 'border-border border-t p-4 sm:border-t-0 sm:border-l'
              : 'p-4'
          }
        >
          <div className='text-muted-foreground text-xs'>{label}</div>
          <div className='text-foreground mt-1 text-lg font-semibold tabular-nums'>
            {label === '清算记录' ? `${value} 条` : value}
          </div>
        </div>
      ))}
    </div>
  )
}

function SettlementValue(props: {
  label: string
  value: string
  strong?: boolean
}) {
  return (
    <div className='md:text-right'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div
        className={
          props.strong
            ? 'text-primary mt-1 font-semibold tabular-nums'
            : 'text-foreground mt-1 text-sm font-medium tabular-nums'
        }
      >
        {props.value}
      </div>
    </div>
  )
}

function SettlementSkeleton() {
  return (
    <div className='space-y-3'>
      <Skeleton className='h-20 w-full rounded-lg' />
      <Skeleton className='h-24 w-full rounded-lg' />
      <Skeleton className='h-24 w-full rounded-lg' />
    </div>
  )
}

function formatAmount(value: number) {
  return Number.isFinite(value) ? value.toFixed(2) : '0.00'
}
