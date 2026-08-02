import { useState } from 'react'
import { ArrowRight, ChevronDown, Layers3, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  formatDuration,
  formatSubscriptionPlanPrice,
  formatSubscriptionQuotaAmount,
  parseSubscriptionQuotaUSDToUnits,
} from '@/features/subscriptions/lib'
import type {
  PlanRecord,
  SubscriptionPurchaseType,
  UserSubscriptionRecord,
} from '@/features/subscriptions/types'
import { buildPackageQuotaTiers } from './lib/collective-benefit'
import {
  translateDisabledReason,
  translateCollectiveTierLabel,
  translatePlanAction,
  translatePlanSubtitle,
  translatePlanTitle,
} from './lib/display'

export function PackagePlanCard(props: {
  record: PlanRecord
  purchaseCount: number
  onPurchase: (purchaseType?: SubscriptionPurchaseType) => void
  currentSubscription?: UserSubscriptionRecord
  onFuel?: (
    subscription: UserSubscriptionRecord,
    title: string,
    config: { minimumQuota: number; quotaStep: number }
  ) => void
}) {
  const { t } = useTranslation()
  const [showDetails, setShowDetails] = useState(false)
  const plan = props.record.plan
  const title = translatePlanTitle(plan.title, t)
  const isRecommended = title.includes('Standard')
  const groupBuyEnabled =
    plan.group_buy_enabled === true && plan.plan_type !== 'daily'
  const limit = Number(plan.max_purchase_per_user || 0)
  const limitReached = limit > 0 && props.purchaseCount >= limit
  const actionLabel = translatePlanAction(props.record.action, t)
  const effectiveAmount =
    props.record.action === 'disabled'
      ? plan.price_amount
      : (props.record.amount_due ?? plan.price_amount)
  const firstPurchaseDiscountApplied =
    props.record.first_purchase_discount_applied === true
  const firstPurchaseDiscount = Number(
    (props.record.first_purchase_discount_multiplier || 0) * 10
  )
  const baseQuota = Number(plan.total_amount || 0)
  const blockedReason =
    translateDisabledReason(props.record.disabled_reason, t) ||
    t('A higher active plan with remaining quota prevents downgrading.')
  const tierRows = buildPackageQuotaTiers(plan, t)
  const isCurrentPlan =
    props.currentSubscription?.subscription.plan_id === plan.id
  const canFuel =
    isCurrentPlan &&
    props.currentSubscription?.subscription.status === 'active' &&
    plan.fuel_enabled === true &&
    Number(plan.fuel_min_quota || 0) > 0 &&
    Number(plan.fuel_quota_step || 0) > 0

  return (
    <Card
      className={cn(
        'border-border bg-card relative h-full overflow-hidden shadow-sm transition-all hover:shadow-md',
        isRecommended && 'border-primary bg-primary/[0.035] border-2'
      )}
    >
      <CardContent className='flex h-full flex-col gap-3 p-4'>
        <div className='text-center'>
          {isRecommended && (
            <span className='text-primary mb-2 inline-flex items-center gap-1 text-xs font-semibold'>
              <Sparkles className='h-3.5 w-3.5' />
              {t('Most popular')}
            </span>
          )}
          <div className='text-muted-foreground text-xs font-medium'>
            {translatePlanSubtitle(plan, t)}
          </div>
          <h4 className='text-foreground mt-1 text-lg font-bold'>{title}</h4>
          {isCurrentPlan && (
            <span className='border-primary/25 bg-primary/10 text-primary mt-2 inline-flex rounded-md border px-2 py-1 text-xs font-semibold'>
              {t('Currently active')}
            </span>
          )}
          {firstPurchaseDiscountApplied ? (
            <span className='border-warning/25 bg-warning/10 text-warning mt-2 ml-1 inline-flex rounded-md border px-2 py-1 text-xs font-semibold'>
              套餐首购 {Number(firstPurchaseDiscount.toFixed(1))} 折
            </span>
          ) : null}
        </div>

        <div className='text-center'>
          <div className='text-muted-foreground text-xs font-medium'>
            {t('Payment price')}
          </div>
          <div className='text-primary flex items-baseline justify-center gap-1 text-3xl font-bold'>
            {formatSubscriptionPlanPrice(effectiveAmount, plan.currency)}
          </div>
          {effectiveAmount !== plan.price_amount && (
            <div className='text-muted-foreground mt-1 text-xs line-through'>
              {formatSubscriptionPlanPrice(plan.price_amount, plan.currency)}
            </div>
          )}
        </div>

        <div className='space-y-2'>
          <div className='flex items-center justify-between text-sm'>
            <span className='text-muted-foreground'>
              {t('Base quota (USD)')}
            </span>
            <span className='text-foreground font-semibold'>
              {formatSubscriptionQuotaAmount(baseQuota)}
            </span>
          </div>
          <div className='flex items-center justify-between text-sm'>
            <span className='text-muted-foreground'>{t('Validity')}</span>
            <span className='text-foreground font-semibold'>
              {formatDuration(plan, t)}
            </span>
          </div>
          {groupBuyEnabled && (
            <div className='bg-muted/40 -mx-4 mt-2 space-y-1.5 px-4 py-2'>
              <div className='flex items-center justify-between gap-2'>
                <span className='text-foreground flex items-center gap-1.5 text-xs font-semibold'>
                  <Layers3 className='text-primary h-3.5 w-3.5' />
                  {t('Collective benefit plan')}
                </span>
                <span className='text-muted-foreground text-[11px]'>
                  {t('Final quota by tier')}
                </span>
              </div>
              <div className='flex items-center justify-between text-sm'>
                <span className='text-muted-foreground'>
                  {translateCollectiveTierLabel(2, t)}
                </span>
                <span className='text-primary font-semibold'>
                  {formatSubscriptionQuotaAmount(
                    baseQuota +
                      parseSubscriptionQuotaUSDToUnits(
                        plan.group_buy_bonus_2 || 0
                      )
                  )}
                </span>
              </div>
              <div className='flex items-center justify-between text-sm'>
                <span className='text-muted-foreground'>
                  {translateCollectiveTierLabel(3, t)}
                </span>
                <span className='text-primary font-semibold'>
                  {formatSubscriptionQuotaAmount(
                    baseQuota +
                      parseSubscriptionQuotaUSDToUnits(
                        plan.group_buy_bonus_3 || 0
                      )
                  )}
                </span>
              </div>
              <div className='flex items-center justify-between text-sm'>
                <span className='text-muted-foreground'>
                  {translateCollectiveTierLabel(5, t)}
                </span>
                <span className='text-primary font-semibold'>
                  {formatSubscriptionQuotaAmount(
                    baseQuota +
                      parseSubscriptionQuotaUSDToUnits(
                        plan.group_buy_bonus_5 || 0
                      )
                  )}
                </span>
              </div>
            </div>
          )}
        </div>

        {showDetails && (
          <div className='border-border space-y-2 rounded-lg border p-3'>
            <div className='space-y-1.5 text-xs'>
              {tierRows.map((tier) => (
                <div key={tier.label} className='flex justify-between'>
                  <span className='text-muted-foreground'>
                    {tier.label}: {tier.detail}
                  </span>
                  <span className='text-foreground font-medium'>
                    {tier.value}
                  </span>
                </div>
              ))}
            </div>
            <div className='text-muted-foreground text-xs leading-relaxed'>
              {groupBuyEnabled
                ? t(
                    'The base quota is available immediately. The collective bonus is settled by the final participation tier after five participants join or 48 hours pass.'
                  )
                : t(
                    'This plan is not included in the Collective Benefit Program and is settled immediately after payment.'
                  )}
            </div>
          </div>
        )}

        <button
          onClick={() => setShowDetails(!showDetails)}
          className='text-primary hover:text-primary/80 -mx-4 flex items-center justify-center gap-1 py-1 text-xs font-medium transition-colors'
        >
          {t(showDetails ? 'Hide details' : 'View details')}
          <ChevronDown
            className={cn(
              'h-3.5 w-3.5 transition-transform',
              showDetails && 'rotate-180'
            )}
          />
        </button>

        <div className='mt-auto space-y-2'>
          {canFuel && props.currentSubscription && props.onFuel && (
            <Button
              className='w-full'
              onClick={() =>
                props.onFuel?.(props.currentSubscription!, title, {
                  minimumQuota: Number(plan.fuel_min_quota || 0),
                  quotaStep: Number(plan.fuel_quota_step || 0),
                })
              }
            >
              {t('Add quota to current plan')}
            </Button>
          )}
          <Button
            className='w-full'
            disabled={limitReached || props.record.action === 'disabled'}
            onClick={() => props.onPurchase('normal')}
          >
            {limitReached ? t('Purchase limit reached') : actionLabel}
            {!limitReached && <ArrowRight className='ml-1 h-4 w-4' />}
          </Button>
          {groupBuyEnabled && (
            <Button
              variant='outline'
              className='w-full'
              disabled={limitReached || props.record.action === 'disabled'}
              onClick={() => props.onPurchase('group_buy')}
            >
              <Layers3 className='mr-1 h-4 w-4' />
              {t('Participate in collective benefit')}
            </Button>
          )}
          {limitReached && (
            <div className='text-muted-foreground text-center text-xs'>
              {t('Limit reached ({{current}}/{{limit}})', {
                current: props.purchaseCount,
                limit,
              })}
            </div>
          )}
          {props.record.action === 'disabled' && (
            <div className='text-muted-foreground text-center text-xs leading-relaxed'>
              {blockedReason}
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
