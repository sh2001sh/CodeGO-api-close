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
import { useMemo, useState } from 'react'
import { ChevronDown, Crown, Fuel } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import {
  formatSubscriptionPlanTitle,
  subscriptionQuotaUnitsToUSD,
} from '@/features/subscriptions/lib'
import type {
  PlanRecord,
  SubscriptionPurchaseType,
  UserSubscriptionRecord,
} from '@/features/subscriptions/types'
import { PackagePlanCard } from './package-plan-card'

type FuelConfig = { minimumQuota: number; quotaStep: number }

export function PlanZone(props: {
  title: string
  description: string
  plans: PlanRecord[]
  loading: boolean
  purchaseCountMap: Map<number, number>
  onPurchase: (
    record: PlanRecord,
    purchaseType?: SubscriptionPurchaseType
  ) => void
  currentSubscriptions: UserSubscriptionRecord[]
  onFuel?: (
    subscription: UserSubscriptionRecord,
    title: string,
    config: FuelConfig
  ) => void
}) {
  return (
    <section className='space-y-3'>
      <div>
        <h3 className='text-foreground text-base font-semibold'>
          {props.title}
        </h3>
        <p className='text-muted-foreground mt-1 text-sm leading-6'>
          {props.description}
        </p>
      </div>
      {props.loading ? (
        <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4'>
          {Array.from({ length: 4 }).map((_, index) => (
            <Skeleton key={index} className='h-[420px] rounded-xl' />
          ))}
        </div>
      ) : props.plans.length > 0 ? (
        <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4'>
          {props.plans.map((record) => (
            <PackagePlanCard
              key={record.plan.id}
              record={record}
              purchaseCount={props.purchaseCountMap.get(record.plan.id) || 0}
              onPurchase={(purchaseType) =>
                props.onPurchase(record, purchaseType)
              }
              currentSubscriptions={props.currentSubscriptions}
              onFuel={props.onFuel}
            />
          ))}
        </div>
      ) : (
        <p className='text-muted-foreground border-border border-t pt-3 text-sm'>
          当前分区暂无可购买套餐。
        </p>
      )}
    </section>
  )
}

export function CurrentPackagePanel(props: {
  subscriptions: UserSubscriptionRecord[]
  plans: PlanRecord[]
  loading: boolean
  onFuel: (
    subscription: UserSubscriptionRecord,
    title: string,
    config: FuelConfig
  ) => void
}) {
  const [showAllSubscriptions, setShowAllSubscriptions] = useState(false)
  const planMap = useMemo(() => {
    const map = new Map<number, PlanRecord['plan']>()
    for (const item of props.plans) map.set(item.plan.id, item.plan)
    return map
  }, [props.plans])
  const currentSubscriptions = useMemo(() => {
    return props.subscriptions
      .filter((item) => item.subscription.status === 'active')
      .sort((left, right) => {
        const leftPlan = planMap.get(left.subscription.plan_id)
        const rightPlan = planMap.get(right.subscription.plan_id)
        const priceDifference =
          Number(rightPlan?.price_amount || 0) -
          Number(leftPlan?.price_amount || 0)
        if (priceDifference !== 0) return priceDifference
        return right.subscription.end_time - left.subscription.end_time
      })
  }, [planMap, props.subscriptions])
  if (!props.loading && currentSubscriptions.length === 0) {
    return null
  }
  const visibleSubscriptions = showAllSubscriptions
    ? currentSubscriptions
    : currentSubscriptions.slice(0, 1)

  return (
    <section className='border-border bg-card rounded-lg border px-4 py-3'>
      {props.loading ? (
        <Skeleton className='h-8 w-full sm:w-96' />
      ) : (
        <>
          <div className='mb-3 flex items-center gap-2'>
            <Crown className='text-primary size-4 shrink-0' />
            <span className='text-foreground text-sm font-semibold'>
              当前生效套餐
            </span>
            <span className='text-muted-foreground text-xs'>
              {currentSubscriptions.length} 个
            </span>
          </div>
          <div
            className={
              showAllSubscriptions && currentSubscriptions.length > 1
                ? 'grid gap-2.5 lg:grid-cols-2'
                : 'grid gap-2.5'
            }
          >
            {visibleSubscriptions.map((current) => {
              const currentPlan = planMap.get(current.subscription.plan_id)
              const currentTitle =
                formatSubscriptionPlanTitle(currentPlan?.title) ||
                `套餐 #${current.subscription.plan_id}`
              const minimumQuota = currentPlan?.fuel_min_quota || 0
              const quotaStep = currentPlan?.fuel_quota_step || 0
              const canFuel =
                currentPlan?.fuel_enabled === true &&
                minimumQuota > 0 &&
                quotaStep > 0
              const remaining = Math.max(
                0,
                current.subscription.amount_total - current.subscription.amount_used
              )

              return (
                <div
                  key={current.subscription.id}
                  className='border-border/70 bg-background/60 rounded-md border px-3 py-2.5'
                >
                  <div className='flex flex-wrap items-center justify-between gap-2'>
                    <div className='text-foreground text-sm font-semibold'>
                      {currentTitle}
                    </div>
                    {canFuel ? (
                      <Button
                        size='sm'
                        onClick={() =>
                          props.onFuel(current, currentTitle, {
                            minimumQuota,
                            quotaStep,
                          })
                        }
                      >
                        <Fuel className='mr-1 size-4' />
                        加油包
                      </Button>
                    ) : null}
                  </div>
                  <div className='text-muted-foreground mt-1.5 flex flex-wrap gap-x-3 gap-y-1 text-xs tabular-nums'>
                    <span>
                      剩余 ${subscriptionQuotaUnitsToUSD(remaining).toFixed(2)}
                      /${subscriptionQuotaUnitsToUSD(current.subscription.amount_total).toFixed(2)}
                    </span>
                    <span>
                      到期 {new Date(current.subscription.end_time * 1000).toLocaleString()}
                    </span>
                  </div>
                  <Progress
                    className='mt-2 h-1.5'
                    value={
                      current.subscription.amount_total > 0
                        ? Math.round(
                            (current.subscription.amount_used /
                              current.subscription.amount_total) *
                              100
                          )
                        : 0
                    }
                  />
                </div>
              )
            })}
          </div>
          {currentSubscriptions.length > 1 ? (
            <Button
              size='sm'
              variant='ghost'
              className='mt-2.5 w-full'
              onClick={() => setShowAllSubscriptions((current) => !current)}
            >
              {showAllSubscriptions
                ? '收起其他套餐'
                : `查看其他 ${currentSubscriptions.length - 1} 个套餐`}
              <ChevronDown
                className={
                  showAllSubscriptions
                    ? 'ml-1 size-4 rotate-180 transition-transform'
                    : 'ml-1 size-4 transition-transform'
                }
              />
            </Button>
          ) : null}
        </>
      )}
    </section>
  )
}
