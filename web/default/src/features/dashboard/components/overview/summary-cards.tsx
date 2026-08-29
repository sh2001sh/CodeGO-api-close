import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useAuthStore } from '@/stores/auth-store'
import { getCurrencyLabel } from '@/lib/currency'
import { formatNumber, formatQuota } from '@/lib/format'
import { getUserQuotaDates } from '@/features/dashboard/api'
import {
  aggregateHourlyUsage,
  getRolling24HourRange,
} from '@/features/dashboard/lib/overview-usage'
import type { QuotaDataItem } from '@/features/dashboard/types'
import {
  getOrderedSubscriptions,
  getSubscriptionPlanSubtitle,
  isMonthlyCardPlan,
} from '@/features/subscriptions/lib'
import type { PlanRecord } from '@/features/subscriptions/types'
import { getUserLogStats } from '@/features/usage-logs/api'
import { UsageChart } from './summary-card-parts'
import {
  BalanceWorkspace,
  PackageStatusCard,
  type BalanceSegment,
  type MetricDef,
} from './summary-sections'
import { useOverviewSubscriptionData } from './use-overview-subscription-data'

function formatDateTime(timestamp?: number): string {
  if (!timestamp) return '--'
  return new Date(timestamp * 1000).toLocaleString()
}

function getRemainingDays(timestamp?: number): number {
  if (!timestamp) return 0
  const now = Date.now() / 1000
  return Math.max(0, Math.ceil((timestamp - now) / 86400))
}

function formatUsageHourLabel(timestamp?: number) {
  if (!timestamp) return '--'
  const date = new Date(timestamp * 1000)
  return `${String(date.getHours()).padStart(2, '0')}:00`
}

export function SummaryCards() {
  const user = useAuthStore((state) => state.auth.user)
  const { subscriptionData, plans } = useOverviewSubscriptionData()
  const [clock, setClock] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setClock(Date.now()), 60_000)
    return () => window.clearInterval(timer)
  }, [])
  const summaryTimeRange = useMemo(
    () => getRolling24HourRange(Math.floor(clock / 1000)),
    [clock]
  )
  const { start_timestamp: usageStart, end_timestamp: usageEnd } =
    summaryTimeRange
  const remainQuota = Number(user?.quota ?? 0)
  const usedQuota = Number(user?.used_quota ?? 0)
  const requestCount = Number(user?.request_count ?? 0)

  const usageTrendQuery = useQuery({
    queryKey: [
      'dashboard',
      'overview',
      'single-usage-chart',
      summaryTimeRange.start_timestamp,
      summaryTimeRange.end_timestamp,
    ],
    queryFn: async () =>
      getUserQuotaDates({
        start_timestamp: summaryTimeRange.start_timestamp,
        end_timestamp: summaryTimeRange.end_timestamp,
        default_time: 'hour',
      }),
    staleTime: 60 * 1000,
  })

  const usageTotalQuery = useQuery({
    queryKey: [
      'dashboard',
      'overview',
      'rolling-24-hour-total',
      usageStart,
      usageEnd,
    ],
    queryFn: () =>
      getUserLogStats({
        start_timestamp: usageStart,
        end_timestamp: usageEnd,
      }),
    staleTime: 60 * 1000,
  })

  const usageRows = usageTrendQuery.data?.data ?? []
  const chartPoints = aggregateHourlyUsage(
    usageRows.map((item: QuotaDataItem) => ({
      created_at: Number(item.created_at),
      quota: Number(item.quota),
    }))
  ).map((item) => ({
    label: formatUsageHourLabel(item.created_at),
    value: Number(item.quota) || 0,
  }))
  const recentUsage = usageTotalQuery.data?.success
    ? Number(usageTotalQuery.data.data?.quota ?? 0)
    : undefined
  const currencyLabel = getCurrencyLabel()

  const balanceSegments: BalanceSegment[] = [
    {
      label: '通用额度',
      display: formatQuota(remainQuota),
      value: remainQuota,
      dot: 'bg-primary',
      bar: 'bg-primary',
    },
  ]

  const planMetaMap = useMemo(() => {
    const map = new Map<
      number,
      { title: string; subtitle: string; plan: PlanRecord['plan'] }
    >()

    for (const item of plans) {
      if (!item?.plan?.id) continue
      map.set(item.plan.id, {
        title: item.plan.title || '',
        subtitle: getSubscriptionPlanSubtitle(item.plan),
        plan: item.plan,
      })
    }

    return map
  }, [plans])

  const orderedSubscriptions = useMemo(() => {
    const subscriptions = subscriptionData.subscriptions ?? []
    const fallbackIds = subscriptions.map((item) => item.subscription.id)
    const orderIds = subscriptionData.subscription_order_ids?.length
      ? subscriptionData.subscription_order_ids
      : fallbackIds
    return getOrderedSubscriptions(subscriptions, orderIds)
  }, [subscriptionData])

  const primarySubscription = orderedSubscriptions[0]
  const primaryPlanMeta = primarySubscription
    ? planMetaMap.get(primarySubscription.subscription.plan_id)
    : undefined
  const subscription = primarySubscription?.subscription
  const totalAmount = Number(subscription?.amount_total || 0)
  const totalUsed = Number(subscription?.amount_used || 0)
  const periodAmount = Number(subscription?.period_amount || 0)
  const periodUsed = Number(subscription?.period_used || 0)
  const isMonthlyPlan = isMonthlyCardPlan(primaryPlanMeta?.plan)
  const showPeriodQuota = !isMonthlyPlan && periodAmount > 0
  const hasSubscription = Boolean(subscription)
  const heroMetrics: MetricDef[] = [
    {
      label: '24H 消耗',
      value: recentUsage == null ? '--' : formatQuota(recentUsage),
      numeric: recentUsage,
      format: formatQuota,
    },
    {
      label: '账本累计',
      value: formatQuota(usedQuota),
      numeric: usedQuota,
      format: formatQuota,
    },
    {
      label: '请求次数',
      value: formatNumber(requestCount),
      numeric: requestCount,
      format: formatNumber,
    },
  ]

  return (
    <div className='flex flex-col gap-4'>
      <div className='grid gap-4 xl:grid-cols-[minmax(0,1.12fr)_minmax(320px,0.88fr)]'>
        <BalanceWorkspace
          available={formatQuota(remainQuota)}
          availableValue={remainQuota}
          currencyLabel={currencyLabel}
          segments={balanceSegments}
          metrics={heroMetrics}
        />

        <PackageStatusCard
          hasSubscription={hasSubscription}
          title={
            hasSubscription
              ? primaryPlanMeta?.title || `套餐 #${subscription?.id}`
              : ''
          }
          subtitle=''
          remainingDays={getRemainingDays(subscription?.end_time)}
          totalUsed={totalUsed}
          totalAmount={totalAmount}
          totalHint={`到期 ${formatDateTime(subscription?.end_time)}`}
          periodUsed={showPeriodQuota ? periodUsed : undefined}
          periodAmount={showPeriodQuota ? periodAmount : undefined}
          periodHint={
            showPeriodQuota
              ? `重置 ${formatDateTime(subscription?.next_reset_time)}`
              : undefined
          }
        />
      </div>

      <UsageChart points={chartPoints} />
    </div>
  )
}
