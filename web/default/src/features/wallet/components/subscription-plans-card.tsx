import { type ReactNode, useEffect, useMemo, useState } from 'react'
import { Crown, RefreshCw } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'
import {
  CardStaggerContainer,
  CardStaggerItem,
} from '@/components/page-transition'
import { PackagePlanCard } from '@/features/packages/package-plan-card'
import { SubscriptionPurchaseDialog } from '@/features/subscriptions/components/dialogs/subscription-purchase-dialog'
import {
  formatSubscriptionQuotaAmount,
  isDayPassPlan,
  isMonthlyCardPlan,
  normalizeSubscriptionText,
} from '@/features/subscriptions/lib'
import type {
  PlanRecord,
  SelfSubscriptionData,
  SubscriptionPurchaseType,
  UserSubscriptionRecord,
} from '@/features/subscriptions/types'
import type { PaymentMethod, TopupInfo } from '../types'

interface SubscriptionPlansCardProps {
  topupInfo: TopupInfo | null
  plans: PlanRecord[]
  plansLoading?: boolean
  subscriptionData?: SelfSubscriptionData | null
  subscriptionLoading?: boolean
  onAvailabilityChange?: (available: boolean) => void
  onSubscriptionRefresh?: () => Promise<void>
  onPlansRefresh?: () => Promise<void>
}

export function getEpayMethods(
  payMethods: PaymentMethod[] = []
): PaymentMethod[] {
  return payMethods
    .filter(
      (method) =>
        method?.type && method.type !== 'stripe' && method.type !== 'creem'
    )
    .map((method) =>
      method.type === 'xunhu' || method.type === 'wxpay'
        ? { ...method, name: '微信支付' }
        : method
    )
}

function getRemainingDays(sub: UserSubscriptionRecord) {
  const endTime = sub?.subscription?.end_time || 0
  if (!endTime) return 0
  const now = Date.now() / 1000
  return Math.max(0, Math.ceil((endTime - now) / 86400))
}

function getUsagePercent(used: number, total: number) {
  if (total <= 0) return 0
  return Math.round((used / total) * 100)
}

export function SubscriptionPlansCard({
  topupInfo,
  plans,
  plansLoading = false,
  subscriptionData,
  subscriptionLoading = false,
  onAvailabilityChange,
  onSubscriptionRefresh,
  onPlansRefresh,
}: SubscriptionPlansCardProps) {
  const [now] = useState(() => Date.now() / 1000)
  const [refreshing, setRefreshing] = useState(false)
  const [purchaseOpen, setPurchaseOpen] = useState(false)
  const [selectedPlan, setSelectedPlan] = useState<PlanRecord | null>(null)
  const [selectedPurchaseType, setSelectedPurchaseType] =
    useState<SubscriptionPurchaseType>('normal')

  const enableStripe = !!topupInfo?.enable_stripe_topup
  const enableCreem = !!topupInfo?.enable_creem_topup
  const enableOnlineTopUp = !!topupInfo?.enable_online_topup
  const epayMethods = useMemo(
    () => getEpayMethods(topupInfo?.pay_methods),
    [topupInfo?.pay_methods]
  )
  const allSubscriptions = useMemo(
    () => subscriptionData?.all_subscriptions ?? [],
    [subscriptionData?.all_subscriptions]
  )

  const handleRefresh = async () => {
    setRefreshing(true)
    try {
      await Promise.all([onPlansRefresh?.(), onSubscriptionRefresh?.()])
    } finally {
      setRefreshing(false)
    }
  }

  const isAvailable =
    plansLoading ||
    subscriptionLoading ||
    plans.length > 0 ||
    allSubscriptions.length > 0

  useEffect(() => {
    onAvailabilityChange?.(isAvailable)
  }, [isAvailable, onAvailabilityChange])

  const groupedPlans = useMemo(() => {
    const monthPlans: PlanRecord[] = []
    const dayPlans: PlanRecord[] = []
    for (const item of plans) {
      if (!item.plan) continue
      if (isDayPassPlan(item.plan)) {
        dayPlans.push(item)
      } else {
        monthPlans.push(item)
      }
    }
    return { monthPlans, dayPlans }
  }, [plans])

  const planPurchaseCountMap = useMemo(() => {
    const map = new Map<number, number>()
    for (const item of allSubscriptions) {
      const planId = item?.subscription?.plan_id
      if (!planId) continue
      map.set(planId, (map.get(planId) || 0) + 1)
    }
    return map
  }, [allSubscriptions])

  const planTitleMap = useMemo(() => {
    const map = new Map<number, string>()
    for (const item of plans) {
      if (!item?.plan?.id) continue
      map.set(item.plan.id, normalizeSubscriptionText(item.plan.title))
    }
    return map
  }, [plans])

  const planRecordMap = useMemo(() => {
    const map = new Map<number, PlanRecord>()
    for (const item of plans) {
      if (!item?.plan?.id) continue
      map.set(item.plan.id, item)
    }
    return map
  }, [plans])

  const renderPlanCard = (record: PlanRecord) => {
    const plan = record.plan
    if (!plan) return null
    const count = planPurchaseCountMap.get(plan.id) || 0

    return (
      <CardStaggerItem key={plan.id} className='h-full'>
        <PackagePlanCard
          record={record}
          purchaseCount={count}
          onPurchase={(purchaseType) => {
            setSelectedPlan(record)
            setSelectedPurchaseType(purchaseType || 'normal')
            setPurchaseOpen(true)
          }}
        />
      </CardStaggerItem>
    )
  }

  if (!isAvailable) {
    return null
  }

  return (
    <>
      <div id='wallet-subscriptions' className='scroll-mt-4'>
        <TitledCard
          title='套餐购买'
          icon={<Crown className='h-4 w-4' />}
          action={
            <Button
              variant='outline'
              size='icon'
              className='h-9 w-9 shrink-0'
              onClick={() => void handleRefresh()}
              disabled={refreshing}
            >
              <RefreshCw
                className={cn('h-4 w-4', refreshing && 'animate-spin')}
              />
            </Button>
          }
          contentClassName='space-y-4'
        >
          <div className='border-border bg-muted/40 text-muted-foreground rounded-2xl border px-4 py-3 text-sm leading-6'>
            GPT 套餐额度仅用于官方 GPT 分组；通用额度用于
            Claude、其他模型和第三方市场分组。两类额度用途固定，不能相互转换。
          </div>

          <PlanSection
            title='月卡套餐'
            description='适合长期使用 GPT 系列模型。月卡有效期 1 个月，购买的总额度就是本月可用额度，一个月内可自由使用。'
            loading={plansLoading}
            emptyText='当前没有可购买的月卡套餐。'
          >
            {groupedPlans.monthPlans.map((record) => renderPlanCard(record))}
          </PlanSection>

          <PlanSection
            title='日卡套餐'
            description='适合临时补量。日卡额度独立结算，不并入月卡总额度，扣费时默认优先于月卡。'
            loading={plansLoading}
            emptyText='当前没有可购买的日卡套餐。'
          >
            {groupedPlans.dayPlans.map((record) => renderPlanCard(record))}
          </PlanSection>

          {allSubscriptions.length > 0 ? (
            <section className='app-subtle-panel space-y-4 p-4 shadow-none'>
              <div>
                <div className='text-foreground text-base font-semibold tracking-tight'>
                  我的订阅
                </div>
                <p className='text-muted-foreground mt-1 text-sm leading-6'>
                  先确认当前生效的订阅与额度，再决定是否继续加购。月卡只展示本月可用额度，不展示周期重置。
                </p>
              </div>

              <CardStaggerContainer className='grid gap-3 xl:grid-cols-2'>
                {allSubscriptions.map((record) => {
                  const subscription = record.subscription
                  const title =
                    planTitleMap.get(subscription.plan_id) ||
                    `订阅 #${subscription.id}`
                  const relatedPlanRecord = planRecordMap.get(
                    subscription.plan_id
                  )
                  const relatedPlan = relatedPlanRecord?.plan
                  const totalAmount = Number(subscription.amount_total || 0)
                  const usedAmount = Number(subscription.amount_used || 0)
                  const totalRemain =
                    totalAmount > 0 ? Math.max(0, totalAmount - usedAmount) : 0
                  const periodAmount = Number(subscription.period_amount || 0)
                  const periodUsed = Number(subscription.period_used || 0)
                  const periodRemain =
                    periodAmount > 0
                      ? Math.max(0, periodAmount - periodUsed)
                      : 0
                  const isMonthlyPlan = isMonthlyCardPlan(relatedPlan)
                  const isDayPass = isDayPassPlan(relatedPlan)
                  const totalPercent = getUsagePercent(usedAmount, totalAmount)
                  const periodPercent = getUsagePercent(
                    periodUsed,
                    periodAmount
                  )
                  const remainDays = getRemainingDays(record)
                  const active =
                    subscription.status === 'active' &&
                    subscription.end_time > now
                  const totalExhausted = totalAmount > 0 && totalRemain <= 0
                  const periodExhausted =
                    !isMonthlyPlan &&
                    periodAmount > 0 &&
                    periodRemain <= 0 &&
                    totalRemain > 0
                  const statusLabel = active
                    ? totalExhausted
                      ? '额度已用完'
                      : periodExhausted
                        ? '等待下次重置'
                        : '生效中'
                    : subscription.status === 'cancelled'
                      ? '已取消'
                      : '已过期'
                  const scopedUsageLabel = isMonthlyPlan
                    ? '本月可用额度'
                    : '周期额度'
                  const totalUsageLabel = isMonthlyPlan
                    ? '本月可用额度'
                    : isDayPass
                      ? '日卡额度'
                      : '总额度'

                  return (
                    <CardStaggerItem key={subscription.id} className='h-full'>
                      <Card className='border-border bg-card h-full shadow-none'>
                        <CardContent className='space-y-3 p-4'>
                          <div className='flex flex-wrap items-start justify-between gap-2'>
                            <div>
                              <div className='flex flex-wrap items-center gap-2'>
                                <div className='text-foreground font-semibold'>
                                  {title}
                                </div>
                                <span className='border-border bg-card text-muted-foreground rounded-full border px-2 py-0.5 text-[11px]'>
                                  {statusLabel}
                                </span>
                              </div>
                              <p className='text-muted-foreground mt-1 text-xs'>
                                {active
                                  ? `剩余 ${remainDays} 天`
                                  : '该订阅已结束'}{' '}
                                · 到期时间{' '}
                                {new Date(
                                  subscription.end_time * 1000
                                ).toLocaleString()}
                              </p>
                            </div>
                          </div>

                          {!isMonthlyPlan && periodAmount > 0 ? (
                            <UsageBlock
                              label={scopedUsageLabel}
                              used={periodUsed}
                              total={periodAmount}
                              remain={periodRemain}
                              percent={periodPercent}
                              toneClass='[&_[data-slot=progress-indicator]]:bg-chart-4'
                            />
                          ) : null}

                          <UsageBlock
                            label={totalUsageLabel}
                            used={usedAmount}
                            total={totalAmount}
                            remain={totalRemain}
                            percent={totalPercent}
                            toneClass='[&_[data-slot=progress-indicator]]:bg-primary'
                          />

                          <div className='grid gap-2 sm:grid-cols-2'>
                            {!isMonthlyPlan ? (
                              <InfoItem
                                label='下一次重置'
                                value={
                                  subscription.next_reset_time
                                    ? new Date(
                                        subscription.next_reset_time * 1000
                                      ).toLocaleString()
                                    : '--'
                                }
                              />
                            ) : null}
                            <InfoItem
                              label='订阅状态'
                              value={
                                active
                                  ? '生效中'
                                  : subscription.status === 'cancelled'
                                    ? '已取消'
                                    : '已过期'
                              }
                            />
                          </div>
                          {active && isMonthlyPlan ? (
                            <div className='flex flex-wrap gap-2 border-t pt-3'>
                              <Button
                                size='sm'
                                variant='outline'
                                onClick={() => {
                                  const planRecord =
                                    plans.find(
                                      (item) =>
                                        item.plan?.id === subscription.plan_id
                                    ) ?? null
                                  setSelectedPlan(planRecord)
                                  setSelectedPurchaseType('normal')
                                  setPurchaseOpen(true)
                                }}
                              >
                                提前续费
                              </Button>
                            </div>
                          ) : null}
                        </CardContent>
                      </Card>
                    </CardStaggerItem>
                  )
                })}
              </CardStaggerContainer>
            </section>
          ) : null}
        </TitledCard>
      </div>

      <SubscriptionPurchaseDialog
        open={purchaseOpen}
        onOpenChange={(open) => {
          setPurchaseOpen(open)
          if (!open) {
            void onPlansRefresh?.()
            void onSubscriptionRefresh?.()
          }
        }}
        plan={selectedPlan}
        enableStripe={enableStripe}
        enableCreem={enableCreem}
        enableOnlineTopUp={enableOnlineTopUp}
        epayMethods={epayMethods}
        purchaseLimit={
          selectedPlan?.plan?.max_purchase_per_user
            ? Number(selectedPlan.plan.max_purchase_per_user)
            : undefined
        }
        purchaseCount={
          selectedPlan?.plan?.id
            ? planPurchaseCountMap.get(selectedPlan.plan.id)
            : undefined
        }
        purchaseType={selectedPurchaseType}
      />
    </>
  )
}

function PlanSection(props: {
  title: string
  description: string
  loading: boolean
  emptyText: string
  children: ReactNode
}) {
  const childArray = Array.isArray(props.children)
    ? props.children
    : [props.children].filter(Boolean)

  return (
    <section className='app-subtle-panel p-4 shadow-none'>
      <div className='mb-4'>
        <h3 className='text-foreground text-base font-semibold tracking-tight'>
          {props.title}
        </h3>
        <p className='text-muted-foreground mt-1.5 text-sm leading-6'>
          {props.description}
        </p>
      </div>

      {props.loading ? (
        <div className='grid grid-cols-1 gap-3 md:grid-cols-2 2xl:grid-cols-3'>
          {Array.from({ length: 4 }).map((_, index) => (
            <Skeleton key={index} className='h-[280px] w-full rounded-2xl' />
          ))}
        </div>
      ) : childArray.length > 0 ? (
        <CardStaggerContainer className='grid grid-cols-1 gap-3 md:grid-cols-2 2xl:grid-cols-3'>
          {childArray}
        </CardStaggerContainer>
      ) : (
        <div className='border-border text-muted-foreground rounded-2xl border border-dashed px-4 py-8 text-sm'>
          {props.emptyText}
        </div>
      )}
    </section>
  )
}

function UsageBlock(props: {
  label: string
  used: number
  total: number
  remain: number
  percent: number
  toneClass?: string
}) {
  if (props.total <= 0) {
    return (
      <div className='app-subtle-panel text-muted-foreground p-3 text-sm'>
        {props.label}：不限
      </div>
    )
  }
  return (
    <div className='space-y-2'>
      <div className='flex flex-wrap items-center justify-between gap-2 text-sm'>
        <span className='text-foreground font-medium'>{props.label}</span>
        <span className='text-muted-foreground text-xs'>
          {formatSubscriptionQuotaAmount(props.used)}/
          {formatSubscriptionQuotaAmount(props.total)} · 剩余{' '}
          {formatSubscriptionQuotaAmount(props.remain)}
        </span>
      </div>
      <Progress className={props.toneClass} value={props.percent} />
    </div>
  )
}

function InfoItem(props: { label: string; value: string }) {
  return (
    <div className='border-border bg-muted/50 rounded-xl border px-3 py-2.5'>
      <div className='text-muted-foreground text-[11px] font-medium'>
        {props.label}
      </div>
      <div className='text-foreground mt-1 text-xs font-medium'>
        {props.value}
      </div>
    </div>
  )
}
