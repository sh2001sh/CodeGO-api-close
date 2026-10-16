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
import { getIntlLocale } from '@/i18n/format'
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
import {
  CardStaggerContainer,
  CardStaggerItem,
} from '@/components/page-transition'
import { SubscriptionPurchaseDialog } from '@/features/subscriptions/components/dialogs/subscription-purchase-dialog'
import type { PlanRecord } from '@/features/subscriptions/types'
import { ResetOpportunityEntryCard } from '@/features/wallet/components/reset-opportunity-entry-card'
import { getEpayMethods } from '@/features/wallet/components/subscription-plans-card'
import { WalletWorkspaceShell } from '@/features/wallet/components/wallet-workspace-shell'
import { useWalletWorkspace } from '@/features/wallet/hooks/use-wallet-workspace'
import { CurrentPackagePanel, PlanZone, PackageHistory } from './components'
import { CreditPackRules } from './credit-pack-rules'
import './packages.css'

function formatQuotaDisplay(quota: number | undefined): string {
  if (quota === undefined) return '--'
  const usd = quota / 500_000
  return `${usd.toLocaleString(getIntlLocale(), { minimumFractionDigits: 2, maximumFractionDigits: 2 })} credits`
}

export function PackagesPage() {
  const { t } = useTranslation()
  const workspace = useWalletWorkspace()
  const [selectedPlan, setSelectedPlan] = useState<PlanRecord | null>(null)
  const [purchaseOpen, setPurchaseOpen] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const creditPackPlans = useMemo(
    () =>
      workspace.publicPlans
        .filter((record) => record.plan?.plan_type === 'credit_pack')
        .sort(
          (a, b) =>
            b.plan.sort_order - a.plan.sort_order || a.plan.id - b.plan.id
        ),
    [workspace.publicPlans]
  )
  const resetOpportunity = workspace.subscriptionData?.reset_opportunity
  const hasLegacyReset =
    !workspace.subscriptionError &&
    Boolean(
      resetOpportunity &&
      (resetOpportunity.available_count > 0 || resetOpportunity.used_total > 0)
    )
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
  const handleRefresh = async () => {
    setRefreshing(true)
    try {
      await Promise.all([
        workspace.fetchUser(),
        workspace.fetchPublicPlans(),
        workspace.fetchSubscriptionData(),
      ])
    } finally {
      setRefreshing(false)
    }
  }

  const openPurchase = (record: PlanRecord) => {
    setSelectedPlan(record)
    setPurchaseOpen(true)
  }

  return (
    <>
      <WalletWorkspaceShell
        title={t('Plans')}
        canonicalPath='/packages'
        framedMain={false}
        main={
          <CardStaggerContainer className='package-workspace'>
            {workspace.userError ||
            workspace.subscriptionError ||
            workspace.publicPlansError ? (
              <div role='alert' className='commerce-error'>
                <span>{t('加载失败')}</span>
                <Button
                  variant='outline'
                  onClick={() => void handleRefresh()}
                  disabled={refreshing}
                >
                  {t('重试')}
                </Button>
              </div>
            ) : null}
            <CardStaggerItem>
              <div className='commerce-package-metrics'>
                <div className='min-w-0'>
                  <div className='text-muted-foreground flex items-center gap-1.5 text-xs'>
                    <Wallet className='text-primary size-3.5' />
                    <Trans i18nKey={'通用余额'} />
                  </div>
                  <div className='text-foreground mt-1 truncate text-2xl font-bold tabular-nums'>
                    {formatQuotaDisplay(
                      workspace.userError ? undefined : workspace.user?.quota
                    )}
                  </div>
                </div>
                <div className='min-w-0'>
                  <div className='text-muted-foreground text-xs'>
                    <Trans i18nKey={'账本累计消耗'} />
                  </div>
                  <div className='text-foreground mt-1 truncate text-lg font-semibold tabular-nums'>
                    {formatQuotaDisplay(
                      workspace.userError
                        ? undefined
                        : workspace.user?.used_quota
                    )}
                  </div>
                </div>
                <div className='min-w-0'>
                  <div className='text-muted-foreground flex items-center gap-1.5 text-xs'>
                    <Activity className='text-primary size-3.5' />
                    <Trans i18nKey={'API 请求'} />
                  </div>
                  <div className='text-foreground mt-1 truncate text-lg font-semibold tabular-nums'>
                    {workspace.userError
                      ? '--'
                      : (workspace.user?.request_count?.toLocaleString(
                          getIntlLocale()
                        ) ?? '--')}
                  </div>
                </div>
                <div className='min-w-0'>
                  <div className='text-muted-foreground flex items-center gap-1.5 text-xs'>
                    <Crown className='text-primary size-3.5' />
                    <Trans i18nKey={'生效订阅'} />
                  </div>
                  <div className='text-foreground mt-1 truncate text-lg font-semibold tabular-nums'>
                    {workspace.subscriptionError
                      ? '--'
                      : (workspace.subscriptionData?.subscriptions?.length ??
                        '--')}
                  </div>
                </div>
              </div>
            </CardStaggerItem>
            {workspace.subscriptionLoading ||
            workspace.subscriptionData?.subscriptions.length ? (
              <CardStaggerItem>
                <CurrentPackagePanel
                  subscriptions={
                    workspace.subscriptionData?.subscriptions || []
                  }
                  plans={workspace.publicPlans}
                  loading={workspace.subscriptionLoading}
                />
              </CardStaggerItem>
            ) : null}

            <CardStaggerItem>
              <section
                className='package-catalog'
                aria-labelledby='package-catalog-title'
              >
                <div className='commerce-section-heading'>
                  <h2 id='package-catalog-title'>{t('Plan purchase')}</h2>
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
                </div>
                <details className='codego-package-rules package-disclosure group'>
                  <summary className='flex cursor-pointer list-none items-center justify-between gap-3 px-4 py-3'>
                    <span className='text-foreground flex items-center gap-2 text-[13px] font-semibold'>
                      <BookOpenText className='text-primary h-4 w-4' />
                      {t('规格与规则')}
                    </span>
                    <span className='text-muted-foreground text-xs transition-transform group-open:rotate-180'>
                      <ChevronDown className='h-4 w-4' />
                    </span>
                  </summary>
                  <div className='package-disclosure-content'>
                    <CreditPackRules />
                  </div>
                </details>

                {!workspace.publicPlansError && (
                  <PlanZone
                    title={t('永久额度包')}
                    plans={creditPackPlans}
                    loading={workspace.publicPlansLoading}
                    onPurchase={openPurchase}
                    purchaseCountMap={purchaseCountMap}
                    subscriptions={
                      workspace.subscriptionData?.subscriptions ?? []
                    }
                  />
                )}
              </section>
            </CardStaggerItem>
            {hasLegacyReset && (
              <CardStaggerItem>
                <details className='package-disclosure group'>
                  <summary className='flex cursor-pointer list-none items-center justify-between gap-3 py-4'>
                    <span className='font-medium'>{t('历史刷新权益')}</span>
                    <span className='text-muted-foreground flex items-center gap-3 text-sm'>
                      {workspace.subscriptionData &&
                      !workspace.subscriptionError
                        ? t('{{count}} available', {
                            count:
                              workspace.subscriptionData.reset_opportunity
                                ?.available_count ?? 0,
                          })
                        : '--'}
                      <ChevronDown className='size-4 transition-transform group-open:rotate-180' />
                    </span>
                  </summary>
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
                    title={t('历史刷新权益')}
                    className='package-reset-detail'
                  />
                </details>
              </CardStaggerItem>
            )}
            <CardStaggerItem>
              <PackageHistory
                subscriptions={
                  workspace.subscriptionData?.all_subscriptions || []
                }
                plans={workspace.publicPlans}
                loading={workspace.subscriptionLoading}
              />
            </CardStaggerItem>
          </CardStaggerContainer>
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
        purchaseType='normal'
        purchaseCount={
          selectedPlan?.plan?.id
            ? purchaseCountMap.get(selectedPlan.plan.id)
            : undefined
        }
      />
    </>
  )
}
