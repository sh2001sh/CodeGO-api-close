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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import i18n from '@/i18n/config'
import {
  Activity,
  BookOpenText,
  ChevronDown,
  Crown,
  RefreshCw,
  Wallet,
} from 'lucide-react'
import { Trans, useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { TitledCard } from '@/components/ui/titled-card'
import {
  CardStaggerContainer,
  CardStaggerItem,
} from '@/components/page-transition'
import { GroupBuyRecords } from '@/features/group-buy'
import { getGroupBuyList } from '@/features/group-buy/api'
import { SubscriptionFuelDialog } from '@/features/subscriptions/components/dialogs/subscription-fuel-dialog'
import { SubscriptionPurchaseDialog } from '@/features/subscriptions/components/dialogs/subscription-purchase-dialog'
import { PackageModelScopeNotice } from '@/features/subscriptions/components/package-model-scope-notice'
import type {
  PlanRecord,
  SubscriptionPurchaseType,
  UserSubscriptionRecord,
} from '@/features/subscriptions/types'
import { ResetOpportunityEntryCard } from '@/features/wallet/components/reset-opportunity-entry-card'
import { getEpayMethods } from '@/features/wallet/components/subscription-plans-card'
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
  const queryClient = useQueryClient()
  const collectiveQuery = useQuery({
    queryKey: ['group-buy', 'list'],
    refetchInterval: 30_000,
    queryFn: async () => {
      const response = await getGroupBuyList()
      if (!response.success || !response.data)
        throw new Error(
          response.message || t('Unable to load collective plans.')
        )
      return response
    },
  })
  const collectiveRooms = collectiveQuery.data?.data?.data ?? []
  const [selectedGroupBuyId, setSelectedGroupBuyId] = useState(0)
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
        collectiveQuery.refetch(),
      ])
    } finally {
      setRefreshing(false)
    }
  }

  const openPurchase = (
    record: PlanRecord,
    purchaseType: SubscriptionPurchaseType = 'normal',
    groupBuyId = 0
  ) => {
    setSelectedGroupBuyId(groupBuyId)
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
                    <Trans i18nKey={'通用余额'} />
                  </div>
                  <div className='text-foreground mt-1 truncate text-2xl font-bold tabular-nums'>
                    {formatQuotaDisplay(workspace.user?.quota)}
                  </div>
                </div>
                <div className='min-w-0'>
                  <div className='text-muted-foreground text-xs'>
                    <Trans i18nKey={'账本累计消耗'} />
                  </div>
                  <div className='text-foreground mt-1 truncate text-lg font-semibold tabular-nums'>
                    {formatQuotaDisplay(workspace.user?.used_quota)}
                  </div>
                </div>
                <div className='min-w-0'>
                  <div className='text-muted-foreground flex items-center gap-1.5 text-xs'>
                    <Activity className='text-primary size-3.5' />
                    <Trans i18nKey={'API 请求'} />
                  </div>
                  <div className='text-foreground mt-1 truncate text-lg font-semibold tabular-nums'>
                    {(workspace.user?.request_count ?? 0).toLocaleString()}
                  </div>
                </div>
                <div className='min-w-0'>
                  <div className='text-muted-foreground flex items-center gap-1.5 text-xs'>
                    <Crown className='text-primary size-3.5' />
                    <Trans i18nKey={'生效订阅'} />
                  </div>
                  <div className='text-foreground mt-1 truncate text-lg font-semibold tabular-nums'>
                    {workspace.subscriptionData?.subscriptions?.length ?? 0}
                  </div>
                </div>
              </div>
            </CardStaggerItem>
            <CardStaggerItem>
              <CurrentPackagePanel
                onRenew={openPurchase}
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
                {collectiveQuery.isError && (
                  <div role='alert' className='text-destructive text-sm'>
                    {t('Unable to load collective plans.')}{' '}
                    <Button
                      variant='link'
                      onClick={() => void collectiveQuery.refetch()}
                    >
                      {t('Try again')}
                    </Button>
                  </div>
                )}
                <div className='border-primary/25 bg-primary/5 rounded-lg border px-4 py-3'>
                  <p className='text-foreground text-sm font-semibold'>
                    {t('Package multiplier is 10× the wallet multiplier.')}
                  </p>
                  <p className='text-muted-foreground mt-1 text-xs leading-relaxed'>
                    {t(
                      'For the same model and group, package quota is deducted at 10× the wallet rate. Package quota and wallet balance are not equivalent.'
                    )}
                  </p>
                </div>
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
                      collectiveRooms={collectiveRooms}
                      purchaseCountMap={purchaseCountMap}
                      subscriptions={
                        workspace.subscriptionData?.subscriptions ?? []
                      }
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
                    collectiveRooms={collectiveRooms}
                    purchaseCountMap={purchaseCountMap}
                    subscriptions={
                      workspace.subscriptionData?.subscriptions ?? []
                    }
                    onFuel={openFuel}
                  />
                )}
              </TitledCard>
            </CardStaggerItem>
            <CardStaggerItem>
              <GroupBuyRecords />
            </CardStaggerItem>
          </CardStaggerContainer>
        }
        sidebar={
          <div className='space-y-4'>
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
              title={i18n.t('套餐额度刷新')}
            />
          </div>
        }
      />

      <SubscriptionPurchaseDialog
        open={purchaseOpen}
        onOpenChange={(open) => {
          setPurchaseOpen(open)
          if (!open) {
            void workspace.fetchPublicPlans()
            void workspace.fetchSubscriptionData()
            void queryClient.invalidateQueries({ queryKey: ['group-buy'] })
          }
        }}
        plan={selectedPlan}
        enableStripe={!!topupInfo?.enable_stripe_topup}
        enableCreem={!!topupInfo?.enable_creem_topup}
        enableOnlineTopUp={!!topupInfo?.enable_online_topup}
        epayMethods={epayMethods}
        purchaseLimit={selectedPlan?.plan?.max_purchase_per_user || undefined}
        purchaseType={selectedPurchaseType}
        groupBuyId={selectedGroupBuyId}
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
