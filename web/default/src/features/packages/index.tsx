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
import { Crown, RefreshCw } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { TitledCard } from '@/components/ui/titled-card'
import {
  CardStaggerContainer,
  CardStaggerItem,
} from '@/components/page-transition'
import { SubscriptionFuelDialog } from '@/features/subscriptions/components/dialogs/subscription-fuel-dialog'
import { SubscriptionPurchaseDialog } from '@/features/subscriptions/components/dialogs/subscription-purchase-dialog'
import type {
  PlanRecord,
  SubscriptionPurchaseType,
  UserSubscriptionRecord,
} from '@/features/subscriptions/types'
import { getEpayMethods } from '@/features/wallet/components/subscription-plans-card'
import { WalletStatsCard } from '@/features/wallet/components/wallet-stats-card'
import { WalletWorkspaceShell } from '@/features/wallet/components/wallet-workspace-shell'
import { useWalletWorkspace } from '@/features/wallet/hooks/use-wallet-workspace'
import { CurrentPackagePanel, PlanZone } from './components'

type ZoneId = 'starter' | 'monthly' | 'shortterm'

const PLAN_ORDER = [
  '新人体验卡',
  'Standard月卡',
  'Lite月卡',
  'Pro月卡',
  'Ultra月卡',
  '标准周卡',
  '50刀日卡',
  '100刀日卡',
] as const

function planRank(record: PlanRecord) {
  const title = record.plan?.title || ''
  const index = PLAN_ORDER.findIndex((item) => title.includes(item))
  return index >= 0 ? index : 999 - Number(record.plan?.sort_order || 0)
}

function getPlanZone(record: PlanRecord): ZoneId {
  const planType = record.plan?.plan_type
  if (planType === 'starter') return 'starter'
  if (planType === 'monthly') return 'monthly'
  return 'shortterm'
}

function useGroupedPlans(plans: PlanRecord[]) {
  return useMemo(() => {
    const grouped: Record<ZoneId, PlanRecord[]> = {
      starter: [],
      monthly: [],
      shortterm: [],
    }
    for (const record of plans) {
      if (!record.plan) continue
      grouped[getPlanZone(record)].push(record)
    }
    for (const value of Object.values(grouped)) {
      value.sort((a, b) => planRank(a) - planRank(b))
    }
    return grouped
  }, [plans])
}

export function PackagesPage() {
  const workspace = useWalletWorkspace()
  const [selectedPlan, setSelectedPlan] = useState<PlanRecord | null>(null)
  const [selectedPurchaseType, setSelectedPurchaseType] =
    useState<SubscriptionPurchaseType>('normal')
  const [purchaseOpen, setPurchaseOpen] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [fuelSubscription, setFuelSubscription] =
    useState<UserSubscriptionRecord | null>(null)
  const [fuelTitle, setFuelTitle] = useState('')
  const [fuelConfig, setFuelConfig] = useState({
    minimumQuota: 500_000,
    quotaStep: 500_000,
  })
  const groupedPlans = useGroupedPlans(workspace.publicPlans)
  const topupInfo = workspace.topupInfo
  const epayMethods = useMemo(
    () => getEpayMethods(topupInfo?.pay_methods),
    [topupInfo?.pay_methods]
  )

  const purchaseCountMap = useMemo(() => {
    const map = new Map<number, number>()
    for (const item of workspace.subscriptionData?.all_subscriptions ?? []) {
      const planId = item.subscription?.plan_id
      if (planId) map.set(planId, (map.get(planId) || 0) + 1)
    }
    return map
  }, [workspace.subscriptionData?.all_subscriptions])
  const activeSubscriptions = useMemo(
    () => workspace.subscriptionData?.subscriptions || [],
    [workspace.subscriptionData?.subscriptions]
  )
  const activeMonthlyPlanIDs = useMemo(
    () =>
      new Set(
        activeSubscriptions
          .filter((item) => {
            const plan = workspace.publicPlans.find(
              (record) => record.plan.id === item.subscription.plan_id
            )?.plan
            return plan?.plan_type === 'monthly'
          })
          .map((item) => item.subscription.plan_id)
      ),
    [activeSubscriptions, workspace.publicPlans]
  )
  const shouldPrioritizeMonthlyPlans = activeMonthlyPlanIDs.size > 0
  const primaryPlanZones: Array<{
    id: 'starter' | 'monthly'
    title: string
    description: string
  }> = shouldPrioritizeMonthlyPlans
    ? [
        {
          id: 'monthly',
          title: '月卡专区',
          description: '适合持续开发与团队日常调用。连续续费可按阶梯获得额外额度。',
        },
        {
          id: 'starter',
          title: '新人专区',
          description: '低门槛体验，限购 1 次。购买后 72 小时内升级月卡可获得额外额度奖励。',
        },
      ]
    : [
        {
          id: 'starter',
          title: '新人专区',
          description: '低门槛体验，限购 1 次。购买后 72 小时内升级月卡可获得额外额度奖励。',
        },
        {
          id: 'monthly',
          title: '月卡专区',
          description: '适合持续开发与团队日常调用。连续续费可按阶梯获得额外额度。',
        },
      ]

  const openFuel = (
    subscription: UserSubscriptionRecord,
    title: string,
    config: { minimumQuota: number; quotaStep: number }
  ) => {
    setFuelSubscription(subscription)
    setFuelTitle(title)
    setFuelConfig(config)
  }

  const handleRefresh = async () => {
    setRefreshing(true)
    try {
      await Promise.all([
        workspace.fetchPublicPlans(),
        workspace.fetchSubscriptionData(),
      ])
    } finally {
      setRefreshing(false)
    }
  }

  const openPurchase = (
    record: PlanRecord,
    purchaseType: SubscriptionPurchaseType = 'normal'
  ) => {
    setSelectedPlan(record)
    setSelectedPurchaseType(purchaseType)
    setPurchaseOpen(true)
  }

  return (
    <>
      <WalletWorkspaceShell
        title='套餐'
        description='按使用节奏选择新人体验卡、月卡或短期补量卡。支持拼团的套餐会直接展示 2/3/5 人成团后的最终额度。'
        canonicalPath='/packages'
        framedMain={false}
        main={
          <CardStaggerContainer className='space-y-4'>
            <CardStaggerItem>
              <CurrentPackagePanel
                subscriptions={activeSubscriptions}
                plans={workspace.publicPlans}
                loading={workspace.subscriptionLoading}
                onFuel={openFuel}
              />
            </CardStaggerItem>

            <CardStaggerItem>
              <TitledCard
                title='套餐购买'
                description='先看基础额度和有效期，再看单买、2 人团、3 人团、5 人团各自能拿到的最终额度。'
                icon={<Crown className='h-4 w-4' />}
                action={
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={() => void handleRefresh()}
                    disabled={refreshing}
                  >
                    <RefreshCw
                      className={cn(
                        'mr-1 h-4 w-4',
                        refreshing && 'animate-spin'
                      )}
                    />
                    刷新
                  </Button>
                }
                contentClassName='space-y-5'
              >
                {primaryPlanZones.map((zone) => {
                  if (zone.id === 'monthly' && groupedPlans.monthly.length === 0) {
                    return null
                  }
                  return (
                    <PlanZone
                      key={zone.id}
                      title={zone.title}
                      description={zone.description}
                      plans={groupedPlans[zone.id]}
                      loading={workspace.publicPlansLoading}
                      onPurchase={openPurchase}
                      purchaseCountMap={purchaseCountMap}
                      currentSubscriptions={activeSubscriptions}
                      onFuel={openFuel}
                    />
                  )
                })}
                {groupedPlans.shortterm.length > 0 && (
                  <PlanZone
                    title='短期补量专区'
                    description='适合当天或本周高峰任务；支持拼团的套餐会在满员或 48 小时后统一补发赠额。'
                    plans={groupedPlans.shortterm}
                    loading={workspace.publicPlansLoading}
                    onPurchase={openPurchase}
                    purchaseCountMap={purchaseCountMap}
                    currentSubscriptions={activeSubscriptions}
                    onFuel={openFuel}
                  />
                )}
              </TitledCard>
            </CardStaggerItem>

          </CardStaggerContainer>
        }
        sidebar={
          <WalletStatsCard
            user={workspace.user}
            plans={workspace.publicPlans}
            loading={workspace.userLoading}
            subscriptionData={workspace.subscriptionData}
            subscriptionLoading={workspace.subscriptionLoading}
            onSubscriptionRefresh={workspace.fetchSubscriptionData}
          />
        }
      />

      <SubscriptionPurchaseDialog
        open={purchaseOpen}
        onOpenChange={(open) => {
          setPurchaseOpen(open)
          if (!open) {
            void workspace.fetchPublicPlans()
            void workspace.fetchSubscriptionData()
          }
        }}
        plan={selectedPlan}
        enableStripe={!!topupInfo?.enable_stripe_topup}
        enableCreem={!!topupInfo?.enable_creem_topup}
        enableOnlineTopUp={!!topupInfo?.enable_online_topup}
        epayMethods={epayMethods}
        purchaseLimit={selectedPlan?.plan?.max_purchase_per_user || undefined}
        purchaseType={selectedPurchaseType}
        purchaseCount={
          selectedPlan?.plan?.id
            ? purchaseCountMap.get(selectedPlan.plan.id)
            : undefined
        }
      />
      {fuelSubscription ? (
        <SubscriptionFuelDialog
          open
          onOpenChange={(open) => {
            if (!open) setFuelSubscription(null)
          }}
          subscription={fuelSubscription.subscription}
          title={fuelTitle}
          minimumQuota={fuelConfig.minimumQuota}
          quotaStep={fuelConfig.quotaStep}
          paymentMethods={epayMethods}
          enableStripe={!!topupInfo?.enable_stripe_topup}
          onCompleted={workspace.fetchSubscriptionData}
        />
      ) : null}
    </>
  )
}
