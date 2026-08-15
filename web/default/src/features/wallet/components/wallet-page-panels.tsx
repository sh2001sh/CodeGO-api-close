import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  consumeSubscriptionResetOpportunity,
  updateBillingPreference,
} from '@/features/subscriptions/api'
import {
  getBillingPreferenceFromFundingSourceOrder,
  normalizeFundingSourceOrder,
} from '@/features/subscriptions/billing'
import { getSubscriptionPlanSubtitle } from '@/features/subscriptions/lib'
import type {
  FundingSource,
  PlanRecord,
  SelfSubscriptionData,
} from '@/features/subscriptions/types'
import { RedemptionCodePanel } from './redemption-code-panel'
import { WalletBillingOrderPanel } from './wallet-billing-order-panel'
import {
  getOrderedSubscriptions,
  type WalletPlanMeta,
} from './wallet-panel-utils'
import { WalletPeerTransferCard } from './wallet-peer-transfer-card'
import { WalletResetOpportunityPanel } from './wallet-reset-opportunity-panel'

const ALL_FUNDING_SOURCES: FundingSource[] = ['subscription', 'wallet']

interface WalletPagePanelsProps {
  plans: PlanRecord[]
  plansLoading?: boolean
  loading?: boolean
  topupLink?: string
  redemptionCode: string
  onRedemptionCodeChange: (code: string) => void
  onRedeem: () => void
  redeeming: boolean
  subscriptionData?: SelfSubscriptionData | null
  subscriptionLoading?: boolean
  onSubscriptionRefresh?: () => Promise<void>
  onUserRefresh?: () => Promise<void>
  section: 'funding' | 'conversion' | 'transfer' | 'billing'
  onOpenConversionHistory?: () => void
  onOpenTransferHistory?: () => void
}

export function WalletPagePanels(props: WalletPagePanelsProps) {
  const { t } = useTranslation()
  const [draftFundingSourceOrder, setDraftFundingSourceOrder] = useState<
    FundingSource[]
  >(['subscription', 'wallet'])
  const [draftOrderIds, setDraftOrderIds] = useState<number[]>([])
  const [saving, setSaving] = useState(false)
  const [usingResetOpportunity, setUsingResetOpportunity] = useState(false)

  const activeSubscriptions = useMemo(
    () => props.subscriptionData?.subscriptions ?? [],
    [props.subscriptionData?.subscriptions]
  )
  const hasActiveSubscriptions = activeSubscriptions.length > 0

  useEffect(() => {
    if (!props.subscriptionData) return
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setDraftFundingSourceOrder(
      normalizeFundingSourceOrder(
        props.subscriptionData.funding_source_order,
        props.subscriptionData.billing_preference
      )
    )
    const fallbackIds = activeSubscriptions.map((item) => item.subscription.id)
    setDraftOrderIds(
      props.subscriptionData.subscription_order_ids?.length
        ? props.subscriptionData.subscription_order_ids
        : fallbackIds
    )
  }, [activeSubscriptions, props.subscriptionData])

  const planMetaMap = useMemo(() => {
    const map = new Map<number, WalletPlanMeta>()
    for (const item of props.plans) {
      if (!item?.plan?.id) continue
      map.set(item.plan.id, {
        title: item.plan.title || '',
        subtitle: getSubscriptionPlanSubtitle(item.plan),
        plan: item.plan,
      })
    }
    return map
  }, [props.plans])

  const orderedSubscriptions = useMemo(
    () => getOrderedSubscriptions(activeSubscriptions, draftOrderIds),
    [activeSubscriptions, draftOrderIds]
  )

  const currentSubscription = orderedSubscriptions[0]?.subscription
  const currentSubscriptionPlanMeta = currentSubscription
    ? planMetaMap.get(currentSubscription.plan_id)
    : undefined
  const resetOpportunity = props.subscriptionData?.reset_opportunity ?? {
    available_count: 0,
    earned_total: 0,
    used_total: 0,
    used_this_month: false,
    current_month: '',
    last_used_month: '',
  }

  const disabledFundingSources = ALL_FUNDING_SOURCES.filter(
    (source) => !draftFundingSourceOrder.includes(source)
  )
  const subscriptionModeEnabled =
    draftFundingSourceOrder.includes('subscription')
  const isLoadingPanels = Boolean(
    props.loading || props.subscriptionLoading || props.plansLoading
  )
  const canUseResetOpportunity =
    resetOpportunity.available_count > 0 &&
    !resetOpportunity.used_this_month &&
    !!currentSubscription

  const moveFundingSource = (source: FundingSource, direction: -1 | 1) => {
    setDraftFundingSourceOrder((current) => {
      const next = [...current]
      const index = next.indexOf(source)
      if (index < 0) return current
      const targetIndex = index + direction
      if (targetIndex < 0 || targetIndex >= next.length) return current
      ;[next[index], next[targetIndex]] = [next[targetIndex], next[index]]
      return next
    })
  }

  const toggleFundingSource = (source: FundingSource) => {
    setDraftFundingSourceOrder((current) => {
      if (current.includes(source)) {
        const next = current.filter((item) => item !== source)
        const hasPrimarySource = next.some(
          (item) => item === 'subscription' || item === 'wallet'
        )
        if (!hasPrimarySource) {
          toast.error(t('Keep at least one primary billing source enabled.'))
          return current
        }
        return next
      }
      return [...current, source]
    })
  }

  const moveSubscription = (id: number, direction: -1 | 1) => {
    setDraftOrderIds((current) => {
      const next = [...current]
      const index = next.indexOf(id)
      if (index < 0) return current
      const targetIndex = index + direction
      if (targetIndex < 0 || targetIndex >= next.length) return current
      ;[next[index], next[targetIndex]] = [next[targetIndex], next[index]]
      return next
    })
  }

  const resetFundingSourceOrder = () => {
    setDraftFundingSourceOrder(['subscription', 'wallet'])
  }

  const resetSubscriptionOrder = () => {
    setDraftOrderIds(activeSubscriptions.map((item) => item.subscription.id))
  }

  const handleSave = async () => {
    setSaving(true)
    try {
      const fundingSourceOrder = normalizeFundingSourceOrder(
        draftFundingSourceOrder,
        getBillingPreferenceFromFundingSourceOrder(draftFundingSourceOrder)
      )
      const response = await updateBillingPreference({
        billingPreference:
          getBillingPreferenceFromFundingSourceOrder(fundingSourceOrder),
        fundingSourceOrder,
        subscriptionOrderIds: hasActiveSubscriptions ? draftOrderIds : [],
      })
      if (!response.success) {
        toast.error(response.message || t('Failed to save billing priority.'))
        return
      }
      toast.success(t('Billing priority updated.'))
      await props.onSubscriptionRefresh?.()
    } catch {
      toast.error(t('Failed to save billing priority.'))
    } finally {
      setSaving(false)
    }
  }

  const handleUseResetOpportunity = async () => {
    if (!canUseResetOpportunity || usingResetOpportunity) return
    setUsingResetOpportunity(true)
    try {
      const response = await consumeSubscriptionResetOpportunity()
      if (!response.success || !response.data) {
        toast.error(
          response.message || t('Failed to use the quota reset opportunity.')
        )
        return
      }
      toast.success(
        t('Cleared used quota for {{plan}}.', {
          plan: currentSubscriptionPlanMeta?.title || t('Current subscription'),
        })
      )
      await props.onSubscriptionRefresh?.()
      if (typeof window !== 'undefined') {
        window.dispatchEvent(new Event('subscription:changed'))
      }
    } catch {
      toast.error(t('Failed to use the quota reset opportunity.'))
    } finally {
      setUsingResetOpportunity(false)
    }
  }

  if (props.loading && props.section !== 'funding') {
    return (
      <div className='grid gap-4 lg:grid-cols-2'>
        {Array.from({ length: 4 }).map((_, index) => (
          <div key={index} className='app-page-shell p-4'>
            <Skeleton className='h-5 w-28' />
            <Skeleton className='mt-3 h-10 w-full' />
            <Skeleton className='mt-3 h-10 w-full' />
          </div>
        ))}
      </div>
    )
  }

  if (props.section === 'funding') {
    return (
      <RedemptionCodePanel
        title={t('Redemption code')}
        description={t(
          '兑换码可发放官方 GPT 专属额度、通用额度、GPT 套餐或活动权益。'
        )}
        topupLink={props.topupLink}
        redemptionCode={props.redemptionCode}
        onRedemptionCodeChange={props.onRedemptionCodeChange}
        onRedeem={props.onRedeem}
        redeeming={props.redeeming}
      />
    )
  }

  if (props.section === 'conversion') {
    return (
      <section className='app-page-shell p-5'>
        <h3 className='font-semibold'>{t('额度转换已关闭')}</h3>
        <p className='text-muted-foreground mt-2 max-w-2xl text-sm leading-6'>
          {t(
            'GPT 套餐、官方 GPT 专属额度和通用额度用途固定，不能在额度池之间转换。历史转换记录仍可查询。'
          )}
        </p>
        {props.onOpenConversionHistory ? (
          <Button
            className='mt-4'
            variant='outline'
            onClick={props.onOpenConversionHistory}
          >
            {t('查看历史转换记录')}
          </Button>
        ) : null}
      </section>
    )
  }

  if (props.section === 'transfer') {
    return (
      <WalletPeerTransferCard
        onUserRefresh={props.onUserRefresh}
        onOpenHistory={props.onOpenTransferHistory}
      />
    )
  }

  return (
    <div className='space-y-4'>
      <WalletResetOpportunityPanel
        resetOpportunity={resetOpportunity}
        currentSubscriptionTitle={currentSubscriptionPlanMeta?.title}
        canUseResetOpportunity={canUseResetOpportunity}
        usingResetOpportunity={usingResetOpportunity}
        onUseResetOpportunity={() => void handleUseResetOpportunity()}
      />

      <WalletBillingOrderPanel
        draftFundingSourceOrder={draftFundingSourceOrder}
        disabledFundingSources={disabledFundingSources}
        subscriptionModeEnabled={subscriptionModeEnabled}
        hasActiveSubscriptions={hasActiveSubscriptions}
        orderedSubscriptions={orderedSubscriptions}
        planMetaMap={planMetaMap}
        saving={saving}
        isLoading={isLoadingPanels}
        subscriptionLoading={props.subscriptionLoading ?? false}
        onRefresh={() => void props.onSubscriptionRefresh?.()}
        onSave={() => void handleSave()}
        onResetFundingSourceOrder={resetFundingSourceOrder}
        onResetSubscriptionOrder={resetSubscriptionOrder}
        onToggleFundingSource={toggleFundingSource}
        onMoveFundingSource={moveFundingSource}
        onMoveSubscription={moveSubscription}
      />
    </div>
  )
}
