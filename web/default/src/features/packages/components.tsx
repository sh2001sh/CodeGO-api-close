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
import { Crown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { StaggerContainer, StaggerItem } from '@/components/page-transition'
import type { GroupBuyItem } from '@/features/group-buy/types'
import {
  formatSubscriptionQuotaAmount,
  isMonthlyCardPlan,
  isActiveUserSubscription,
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

export function PlanZone(props: {
  title: string
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
}) {
  const { t } = useTranslation()

  return (
    <section className='commerce-package-zone'>
      <div>
        <h3 className='text-foreground'>{props.title}</h3>
      </div>
      {props.loading ? (
        <div className='grid gap-4 sm:grid-cols-2 2xl:grid-cols-3'>
          {Array.from({ length: 2 }).map((_, index) => (
            <Skeleton key={index} className='h-[420px]' />
          ))}
        </div>
      ) : props.plans.length > 0 ? (
        <StaggerContainer className='commerce-plan-grid'>
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
              />
            </StaggerItem>
          ))}
        </StaggerContainer>
      ) : (
        <p className='commerce-empty'>
          {t('No plans are currently available in this section.')}
        </p>
      )}
    </section>
  )
}

export function CurrentPackagePanel(props: {
  subscriptions: UserSubscriptionRecord[]
  plans: PlanRecord[]
  loading: boolean
}) {
  const { t } = useTranslation()
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
        />
      ))}
    </section>
  )
}

function CurrentPackageCard(props: {
  record: UserSubscriptionRecord
  plan?: PlanRecord
}) {
  const { t } = useTranslation()
  const current = props.record
  const currentPlan = props.record.plan ?? props.plan?.plan
  const usage = getSubscriptionUsageStatus(current, currentPlan, t)
  const hasTotalLimit = current.subscription.amount_total > 0
  const hasPeriodLimit =
    !isMonthlyCardPlan(currentPlan) &&
    (current.subscription.period_amount ?? 0) > 0
  const currentTitle =
    translatePlanTitle(currentPlan?.title, t) ||
    t('Plan #{{id}}', { id: current.subscription.plan_id })

  return (
    <article
      aria-label={currentTitle}
      className='commerce-entitlement border-border bg-card flex min-w-0 flex-wrap items-center gap-x-4 gap-y-3 border'
    >
      <div className='flex min-w-0 items-center gap-2.5'>
        <Crown className='text-primary size-4 shrink-0' />
        <span className='text-foreground text-lg font-semibold'>
          {currentTitle}
        </span>
        <code className='text-muted-foreground text-sm'>
          #{current.subscription.id}
        </code>
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
        {t('到期时间')}{' '}
        {current.subscription.end_time === 0
          ? t('永久有效')
          : formatWalletDateTime(current.subscription.end_time)}
      </p>
      {currentPlan?.upgrade_group ? (
        <p className='text-muted-foreground basis-full text-base'>
          {t('适用分组')} · {currentPlan.upgrade_group}
        </p>
      ) : null}
      {current.subscription.credit_pack_paid_quota !== undefined ? (
        <p className='text-muted-foreground basis-full text-base'>
          {t('实付额度')} ·{' '}
          {formatSubscriptionQuotaAmount(
            current.subscription.credit_pack_paid_quota
          )}
        </p>
      ) : null}
      {current.subscription.credit_pack_granted_quota !== undefined &&
      current.subscription.credit_pack_paid_quota !== undefined ? (
        <p className='text-muted-foreground basis-full text-base'>
          {t('赠额')} ·{' '}
          {formatSubscriptionQuotaAmount(
            Math.max(
              0,
              current.subscription.credit_pack_granted_quota -
                (current.subscription.credit_pack_paid_quota ?? 0)
            )
          )}
        </p>
      ) : null}
      {current.subscription.source ? (
        <p className='text-muted-foreground basis-full text-base'>
          {t('Source')} · {current.subscription.source}
        </p>
      ) : null}
    </article>
  )
}

export function PackageHistory(props: {
  subscriptions: UserSubscriptionRecord[]
  plans: PlanRecord[]
  loading: boolean
}) {
  const { t } = useTranslation()
  const records = props.subscriptions.filter(
    (record) => !isActiveUserSubscription(record.subscription)
  )
  if (props.loading)
    return <Skeleton className='h-48 w-full' aria-label={t('Loading')} />
  return (
    <section
      aria-labelledby='package-history-title'
      className='package-history space-y-6'
    >
      <h2 id='package-history-title' className='text-2xl font-semibold'>
        {t('历史权益')}
      </h2>
      {records.length === 0 ? (
        <div className='commerce-empty'>{t('暂无记录')}</div>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Subscription')}</TableHead>
              <TableHead>{t('Source')}</TableHead>
              <TableHead>{t('Remaining')}</TableHead>
              <TableHead>{t('到期时间')}</TableHead>
              <TableHead>{t('Status')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {records.map((record) => {
              const plan =
                record.plan ||
                props.plans.find(
                  (item) => item.plan.id === record.subscription.plan_id
                )?.plan
              const subscription = record.subscription
              return (
                <TableRow key={subscription.id}>
                  <TableCell>
                    <span className='block font-medium'>
                      {translatePlanTitle(plan?.title, t) ||
                        t('Plan #{{id}}', { id: subscription.plan_id })}
                    </span>
                    <code className='text-muted-foreground'>
                      #{subscription.id}
                    </code>
                  </TableCell>
                  <TableCell>{subscription.source || '--'}</TableCell>
                  <TableCell className='tabular-nums'>
                    {formatSubscriptionQuotaAmount(
                      Math.max(
                        0,
                        subscription.amount_total - subscription.amount_used
                      )
                    )}{' '}
                    / {formatSubscriptionQuotaAmount(subscription.amount_total)}
                  </TableCell>
                  <TableCell>
                    {subscription.end_time === 0
                      ? t('永久有效')
                      : formatWalletDateTime(subscription.end_time)}
                  </TableCell>
                  <TableCell>
                    <span className='commerce-status'>
                      {getSubscriptionUsageStatus(record, plan, t).label}
                    </span>
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      )}
    </section>
  )
}
