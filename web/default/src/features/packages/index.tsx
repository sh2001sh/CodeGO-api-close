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
import { Activity, BookOpenText, ChevronDown, Crown, RefreshCw, Wallet } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { TitledCard } from '@/components/ui/titled-card'
import {
  CardStaggerContainer,
  CardStaggerItem,
} from '@/components/page-transition'
import { SubscriptionFuelDialog } from '@/features/subscriptions/components/dialogs/subscription-fuel-dialog'
import { SubscriptionPurchaseDialog } from '@/features/subscriptions/components/dialogs/subscription-purchase-dialog'
import { PackageModelScopeNotice } from '@/features/subscriptions/components/package-model-scope-notice'
import type {
  PlanRecord,
  SubscriptionPurchaseType,
  UserSubscriptionRecord,
} from '@/features/subscriptions/types'
import { getEpayMethods } from '@/features/wallet/components/subscription-plans-card'
import { ResetOpportunityEntryCard } from '@/features/wallet/components/reset-opportunity-entry-card'
import { WalletWorkspaceShell } from '@/features/wallet/components/wallet-workspace-shell'
import { useWalletWorkspace } from '@/features/wallet/hooks/use-wallet-workspace'
import { CurrentPackagePanel, PlanZone } from './components'
import { MonthlyPlanRules } from './monthly-plan-rules'

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

function formatQuotaDisplay(quota: number | undefined): string {
  const usd = (quota ?? 0) / 500_000
  return `$${usd.toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
}

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
  const { t } = useTranslation()
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
  const currentSubscription = workspace.subscriptionData?.subscriptions[0]
  const shouldPrioritizeMonthlyPlans = Boolean(currentSubscription)
  const primaryPlanZones: Array<{
    id: 'starter' | 'monthly'
    title: string
    description: string
  }> = shouldPrioritizeMonthlyPlans
    ? [
        { id: 'monthly', title: t('Monthly plans'), description: '' },
        { id: 'starter', title: t('Starter plans'), description: '' },
      ]
    : [
        { id: 'starter', title: t('Starter plans'), description: '' },
        { id: 'monthly', title: t('Monthly plans'), description: '' },
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
        title={t('Plans')}
        canonicalPath='/packages'
        framedMain={false}
        kicker='C·03 · PACKAGES'
        main={
          <CardStaggerContainer className='space-y-4'>
            <CardStaggerItem>
              <div className='border-border bg-card grid grid-cols-2 gap-4 rounded-lg border px-4 py-3.5 sm:grid-cols-4 sm:px-5'>
                <div className='min-w-0'>
                  <div className='text-muted-foreground flex items-center gap-1.5 text-xs'>
                    <Wallet className='text-primary size-3.5' />
                    通用余额
                  </div>
                  <div className='text-foreground mt-1 truncate text-2xl font-bold tabular-nums'>
                    {formatQuotaDisplay(workspace.user?.quota)}
                  </div>
                </div>
                <div className='min-w-0'>
                  <div className='text-muted-foreground text-xs'>账本累计消耗</div>
                  <div className='text-foreground mt-1 truncate text-lg font-semibold tabular-nums'>
                    {formatQuotaDisplay(workspace.user?.used_quota)}
                  </div>
                </div>
                <div className='min-w-0'>
                  <div className='text-muted-foreground flex items-center gap-1.5 text-xs'>
                    <Activity className='text-primary size-3.5' />
                    API 请求
                  </div>
                  <div className='text-foreground mt-1 truncate text-lg font-semibold tabular-nums'>
                    {(workspace.user?.request_count ?? 0).toLocaleString()}
                  </div>
                </div>
                <div className='min-w-0'>
                  <div className='text-muted-foreground flex items-center gap-1.5 text-xs'>
                    <Crown className='text-primary size-3.5' />
                    生效订阅
                  </div>
                  <div className='text-foreground mt-1 truncate text-lg font-semibold tabular-nums'>
                    {workspace.subscriptionData?.subscriptions?.length ?? 0}
                  </div>
                </div>
              </div>
            </CardStaggerItem>
            <CardStaggerItem>
              <CurrentPackagePanel
                subscriptions={workspace.subscriptionData?.subscriptions || []}
                plans={workspace.publicPlans}
                loading={workspace.subscriptionLoading}
                onFuel={openFuel}
              />
            </CardStaggerItem>

            <CardStaggerItem>
              <TitledCard
                title={t('Plan purchase')}
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
                    {t('Refresh')}
                  </Button>
                }
                contentClassName='space-y-5'
              >
                <details className='codego-package-rules group rounded-lg border'>
                  <summary className='flex cursor-pointer list-none items-center justify-between gap-3 px-4 py-3'>
                    <span className='text-foreground flex items-center gap-2 text-[13px] font-semibold'>
                      <BookOpenText className='text-primary h-4 w-4' />
                      {t('规格与规则')}
                    </span>
                    <span className='text-muted-foreground text-xs transition-transform group-open:rotate-180'>
                      <ChevronDown className='h-4 w-4' />
                    </span>
                  </summary>
                  <div className='space-y-4 border-t px-4 py-4 sm:px-5'>
                    <MonthlyPlanRules />
                    <PackageModelScopeNotice />
                  </div>
                </details>

                {primaryPlanZones.map((zone) => {
                  if (
                    zone.id === 'monthly' &&
                    groupedPlans.monthly.length === 0
                  ) {
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
                      currentSubscription={currentSubscription}
                      onFuel={openFuel}
                    />
                  )
                })}
                {groupedPlans.shortterm.length > 0 && (
                  <PlanZone
                    title={t('Short-term quota packs')}
                    description=''
                    plans={groupedPlans.shortterm}
                    loading={workspace.publicPlansLoading}
                    onPurchase={openPurchase}
                    purchaseCountMap={purchaseCountMap}
                    currentSubscription={currentSubscription}
                    onFuel={openFuel}
                  />
                )}
              </TitledCard>
            </CardStaggerItem>
          </CardStaggerContainer>
        }
        sidebar={
          <aside className='space-y-4 lg:sticky lg:top-4'>
            <ResetOpportunityEntryCard
              resetOpportunity={
                workspace.subscriptionData?.reset_opportunity ?? {
                  available_count: 0,
                  earned_total: 0,
                  used_total: 0,
                  used_this_month: false,
                  current_month: '',
                  last_used_month: '',
                }
              }
              compact
              title='套餐额度刷新'
            />
          </aside>
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
