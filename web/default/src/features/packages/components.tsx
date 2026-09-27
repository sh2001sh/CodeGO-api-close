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
import type { ReactNode } from 'react'
import { Crown, Fuel } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import { StaggerContainer, StaggerItem } from '@/components/page-transition'
import { SubscriptionLuckySummary } from '@/features/daily-lucky-number/components/subscription-lucky-summary'
import { useDailyLuckyNumberSelf } from '@/features/daily-lucky-number/hooks/use-daily-lucky-number'
import type { GroupBuyItem } from '@/features/group-buy/types'
import {
  getSubscriptionDisabledReasonText,
  formatSubscriptionQuotaAmount,
  isMonthlyCardPlan,
} from '@/features/subscriptions/lib'
import type {
  PlanRecord,
  SubscriptionPurchaseType,
  UserSubscriptionRecord,
} from '@/features/subscriptions/types'
import {
  getSubscriptionUsageStatus,
  formatWalletDateTime,
} from '@/features/wallet/components/wallet-panel-utils'
import { translatePlanTitle } from './lib/display'
import { PackagePlanCard } from './package-plan-card'

type FuelConfig = { minimumQuota: number; quotaStep: number }

export function PlanZone(props: {
  title: string
  description: string
  plans: PlanRecord[]
  loading: boolean
  collectiveRooms?: GroupBuyItem[]
  purchaseCountMap: Map<number, number>
  onPurchase: (
    record: PlanRecord,
    purchaseType?: SubscriptionPurchaseType,
    groupBuyId?: number
  ) => void
  subscriptions: UserSubscriptionRecord[]
  onFuel?: (
    subscription: UserSubscriptionRecord,
    title: string,
    config: FuelConfig
  ) => void
}) {
  const { t } = useTranslation()

  return (
    <section className='space-y-3'>
      <div className='flex items-center gap-2.5'>
        <span aria-hidden className='bg-primary block h-3 w-[3px]' />
        <h3 className='text-foreground text-[13px] font-semibold'>
          {props.title}
        </h3>
        <span className='codego-stat-label sr-only'>{props.description}</span>
      </div>
      {props.loading ? (
        <div className='grid gap-4 sm:grid-cols-2 2xl:grid-cols-3'>
          {Array.from({ length: 2 }).map((_, index) => (
            <Skeleton key={index} className='h-[420px]' />
          ))}
        </div>
      ) : props.plans.length > 0 ? (
        <StaggerContainer className='grid gap-4 sm:grid-cols-2 2xl:grid-cols-3'>
          {props.plans.map((record) => (
            <StaggerItem key={record.plan.id}>
              <PackagePlanCard
                record={record}
                collectiveRoom={props.collectiveRooms?.find(
                  (room) => room.plan_id === record.plan.id
                )}
                purchaseCount={props.purchaseCountMap.get(record.plan.id) || 0}
                onPurchase={(purchaseType, groupBuyId) =>
                  props.onPurchase(record, purchaseType, groupBuyId)
                }
                currentSubscription={props.subscriptions.find(
                  (item) => item.subscription.plan_id === record.plan.id
                )}
                onFuel={props.onFuel}
              />
            </StaggerItem>
          ))}
        </StaggerContainer>
      ) : (
        <p className='text-muted-foreground border-border border-t pt-3 text-sm'>
          {t('No plans are currently available in this section.')}
        </p>
      )}
    </section>
  )
}

export function CurrentPackagePanel(props: {
  onRenew: (plan: PlanRecord) => void
  subscriptions: UserSubscriptionRecord[]
  plans: PlanRecord[]
  loading: boolean
  onFuel: (
    subscription: UserSubscriptionRecord,
    title: string,
    config: FuelConfig
  ) => void
}) {
  const { t } = useTranslation()
  const dailyLuckyQuery = useDailyLuckyNumberSelf(
    props.subscriptions.some((record) =>
      Boolean(record.subscription.lucky_number)
    )
  )
  if (props.loading) return <Skeleton className='h-24 w-full' />
  if (props.subscriptions.length === 0) return null

  return (
    <section aria-label={t('我的订阅')} className='grid gap-3 xl:grid-cols-2'>
      {props.subscriptions.map((record) => (
        <CurrentPackageCard
          key={record.subscription.id}
          record={record}
          plan={props.plans.find(
            (item) => item.plan.id === record.subscription.plan_id
          )}
          onFuel={props.onFuel}
          onRenew={props.onRenew}
          luckySummary={
            record.subscription.lucky_number ? (
              <SubscriptionLuckySummary
                record={record}
                plan={props.plans.find(
                  (item) => item.plan.id === record.subscription.plan_id
                )}
                draw={dailyLuckyQuery.data?.today_draw}
                rewards={dailyLuckyQuery.data?.recent_rewards ?? []}
                showLink
              />
            ) : null
          }
        />
      ))}
    </section>
  )
}

function CurrentPackageCard(props: {
  record: UserSubscriptionRecord
  plan?: PlanRecord
  onFuel: (
    record: UserSubscriptionRecord,
    title: string,
    config: FuelConfig
  ) => void
  luckySummary: ReactNode
  onRenew: (plan: PlanRecord) => void
}) {
  const { t } = useTranslation()
  const current = props.record
  const currentPlanRecord = props.plan
  const currentPlan = currentPlanRecord?.plan
  const usage = getSubscriptionUsageStatus(current, currentPlan, t)
  const hasTotalLimit = current.subscription.amount_total > 0
  const hasPeriodLimit =
    !isMonthlyCardPlan(currentPlan) &&
    (current.subscription.period_amount ?? 0) > 0
  const currentTitle =
    translatePlanTitle(currentPlan?.title, t) ||
    t('Plan #{{id}}', { id: current.subscription.plan_id })
  const canFuel =
    current.subscription.status === 'active' &&
    currentPlan?.fuel_enabled === true &&
    (currentPlan?.fuel_min_quota || 0) > 0 &&
    (currentPlan?.fuel_quota_step || 0) > 0
  const renewalBlocked = currentPlanRecord?.action === 'disabled'
  const renewalBlockedReason = getSubscriptionDisabledReasonText(
    currentPlanRecord?.disabled_reason
  )

  return (
    <article
      aria-label={currentTitle}
      className='border-border bg-card flex min-w-0 flex-wrap items-center gap-x-4 gap-y-3 rounded-lg border px-4 py-3'
    >
      <div className='flex min-w-0 items-center gap-2.5'>
        <Crown className='text-primary size-4 shrink-0' />
        <span className='text-foreground truncate text-sm font-semibold'>
          {currentTitle}
        </span>
      </div>
      <span className='text-muted-foreground text-xs'>{usage.label}</span>
      <div className='text-muted-foreground basis-full text-sm tabular-nums'>
        {t('Remaining')}{' '}
        {hasTotalLimit
          ? formatSubscriptionQuotaAmount(
              Math.max(
                0,
                current.subscription.amount_total -
                  current.subscription.amount_used
              )
            )
          : t('不限')}
        {hasTotalLimit && (
          <>
            {' '}
            / {formatSubscriptionQuotaAmount(current.subscription.amount_total)}
          </>
        )}
      </div>
      {hasTotalLimit && (
        <Progress
          aria-label={t('已用额度比例')}
          className='w-full'
          value={
            current.subscription.amount_total > 0
              ? Math.min(
                  100,
                  Math.max(
                    0,
                    Math.round(
                      (current.subscription.amount_used /
                        current.subscription.amount_total) *
                        100
                    )
                  )
                )
              : 0
          }
        />
      )}
      {hasPeriodLimit && (
        <p className='text-muted-foreground basis-full text-xs'>
          {t('周期剩余额度')}{' '}
          {formatSubscriptionQuotaAmount(
            Math.max(
              0,
              (current.subscription.period_amount ?? 0) -
                (current.subscription.period_used ?? 0)
            )
          )}
          {' / '}
          {formatSubscriptionQuotaAmount(current.subscription.period_amount)}
        </p>
      )}
      <p className='text-muted-foreground basis-full text-xs'>
        {t('到期时间')} {formatWalletDateTime(current.subscription.end_time)}
      </p>
      {props.luckySummary && (
        <div className='min-w-0 basis-full border-t pt-3'>
          {props.luckySummary}
        </div>
      )}
      <div className='ml-auto flex gap-2'>
        {canFuel ? (
          <Button
            size='sm'
            onClick={() =>
              props.onFuel(current, currentTitle, {
                minimumQuota: currentPlan?.fuel_min_quota || 0,
                quotaStep: currentPlan?.fuel_quota_step || 0,
              })
            }
          >
            <Fuel className='mr-1 size-4' />
            {t('Add quota')}
          </Button>
        ) : null}
        {renewalBlocked ? (
          <div className='flex flex-col items-end gap-1'>
            <Button size='sm' variant='outline' disabled>
              {t('Renewal unavailable')}
            </Button>
            <span className='text-muted-foreground text-xs'>
              {renewalBlockedReason}
            </span>
          </div>
        ) : (
          <Button
            size='sm'
            variant='outline'
            disabled={!currentPlanRecord}
            onClick={() =>
              currentPlanRecord && props.onRenew(currentPlanRecord)
            }
          >
            {t('Renew')}
          </Button>
        )}
      </div>
    </article>
  )
}
