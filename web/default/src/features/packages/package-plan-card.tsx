import { useState } from 'react'
import { ArrowRight, ChevronDown, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  formatDuration,
  formatSubscriptionPlanPrice,
  formatSubscriptionPlanTitle,
  formatSubscriptionQuotaAmount,
  getSubscriptionDisabledReasonText,
  getSubscriptionPlanActionLabel,
  getSubscriptionPlanSubtitle,
  parseSubscriptionQuotaUSDToUnits,
} from '@/features/subscriptions/lib'
import type {
  PlanRecord,
  SubscriptionPurchaseType,
  UserSubscriptionRecord,
} from '@/features/subscriptions/types'

export function PackagePlanCard(props: {
  record: PlanRecord
  purchaseCount: number
  onPurchase: (purchaseType?: SubscriptionPurchaseType) => void
  currentSubscriptions?: UserSubscriptionRecord[]
  onFuel?: (
    subscription: UserSubscriptionRecord,
    title: string,
    config: { minimumQuota: number; quotaStep: number }
  ) => void
}) {
  const { t } = useTranslation()
  const [showDetails, setShowDetails] = useState(false)
  const plan = props.record.plan
  const title = formatSubscriptionPlanTitle(plan.title) || '套餐'
  const isRecommended = title.includes('Standard')
  const groupBuyEnabled = plan.group_buy_enabled === true
  const limit = Number(plan.max_purchase_per_user || 0)
  const limitReached = limit > 0 && props.purchaseCount >= limit
  const actionLabel = getSubscriptionPlanActionLabel(props.record.action, t)
  const effectiveAmount = props.record.amount_due ?? plan.price_amount
  const baseQuota = Number(plan.total_amount || 0)
  const blockedReason =
    getSubscriptionDisabledReasonText(props.record.disabled_reason) ||
    '当前还有更高档且未用完的生效套餐，暂不支持直接降级。'
  const tierRows = buildPackageQuotaTiers(plan)
  const currentSubscription = props.currentSubscriptions?.find(
    (item) =>
      item.subscription.plan_id === plan.id &&
      item.subscription.status === 'active'
  )
  const isCurrentPlan = Boolean(currentSubscription)
  const canFuel =
    isCurrentPlan &&
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
              最受欢迎
            </span>
          )}
          <div className='text-muted-foreground text-xs font-medium'>
            {getSubscriptionPlanSubtitle(plan)}
          </div>
          <h4 className='text-foreground mt-1 text-lg font-bold'>{title}</h4>
          {isCurrentPlan && (
            <span className='border-primary/25 bg-primary/10 text-primary mt-2 inline-flex rounded-md border px-2 py-1 text-xs font-semibold'>
              当前使用中
            </span>
          )}
        </div>

        <div className='text-center'>
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
            <span className='text-muted-foreground'>基础额度</span>
            <span className='text-foreground font-semibold'>
              {formatSubscriptionQuotaAmount(baseQuota)}
            </span>
          </div>
          <div className='flex items-center justify-between text-sm'>
            <span className='text-muted-foreground'>有效期</span>
            <span className='text-foreground font-semibold'>
              {formatDuration(plan, t)}
            </span>
          </div>
          {groupBuyEnabled && (
            <div className='bg-muted/40 -mx-4 mt-2 space-y-1.5 px-4 py-2'>
              <div className='flex items-center justify-between text-sm'>
                <span className='text-muted-foreground'>2人团</span>
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
                <span className='text-muted-foreground'>3人团</span>
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
                <span className='text-muted-foreground'>5人团</span>
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
                ? '拼团后先支付基础价，满5人或48小时后按实际成团人数补发赠额。'
                : '该套餐不参与拼团，支付后立即完成结算。'}
            </div>
          </div>
        )}

        <button
          onClick={() => setShowDetails(!showDetails)}
          className='text-primary hover:text-primary/80 -mx-4 flex items-center justify-center gap-1 py-1 text-xs font-medium transition-colors'
        >
          {showDetails ? '收起' : '查看'}详情
          <ChevronDown
            className={cn(
              'h-3.5 w-3.5 transition-transform',
              showDetails && 'rotate-180'
            )}
          />
        </button>

        <div className='mt-auto space-y-2'>
          {canFuel && currentSubscription && props.onFuel && (
            <Button
              className='w-full'
              onClick={() =>
                props.onFuel?.(currentSubscription, title, {
                  minimumQuota: Number(plan.fuel_min_quota || 0),
                  quotaStep: Number(plan.fuel_quota_step || 0),
                })
              }
            >
              为当前月卡加油
            </Button>
          )}
          <Button
            className='w-full'
            disabled={limitReached || props.record.action === 'disabled'}
            onClick={() => props.onPurchase('normal')}
          >
            {limitReached ? '已达购买上限' : actionLabel}
            {!limitReached && <ArrowRight className='ml-1 h-4 w-4' />}
          </Button>
          {groupBuyEnabled && (
            <Button
              variant='outline'
              className='w-full'
              disabled={limitReached || props.record.action === 'disabled'}
              onClick={() => props.onPurchase('group_buy')}
            >
              进入拼团
            </Button>
          )}
          {limitReached && (
            <div className='text-muted-foreground text-center text-xs'>
              已达上限 ({props.purchaseCount}/{limit})
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

function buildPackageQuotaTiers(
  plan: PlanRecord['plan']
): Array<{ label: string; detail: string; value: string }> {
  const baseQuota = Number(plan.total_amount || 0)
  const tiers = [
    {
      label: '单独购买',
      detail: '无需等待，支付后立即生效',
      value: formatSubscriptionQuotaAmount(baseQuota),
    },
  ]

  if (plan.group_buy_enabled) {
    tiers.push(
      {
        label: '2 人成团',
        detail: `额外 +$${Number(plan.group_buy_bonus_2 || 0)}`,
        value: formatSubscriptionQuotaAmount(
          baseQuota +
            parseSubscriptionQuotaUSDToUnits(plan.group_buy_bonus_2 || 0)
        ),
      },
      {
        label: '3 人成团',
        detail: `额外 +$${Number(plan.group_buy_bonus_3 || 0)}`,
        value: formatSubscriptionQuotaAmount(
          baseQuota +
            parseSubscriptionQuotaUSDToUnits(plan.group_buy_bonus_3 || 0)
        ),
      },
      {
        label: '5 人成团',
        detail: `额外 +$${Number(plan.group_buy_bonus_5 || 0)}`,
        value: formatSubscriptionQuotaAmount(
          baseQuota +
            parseSubscriptionQuotaUSDToUnits(plan.group_buy_bonus_5 || 0)
        ),
      }
    )
  }

  return tiers
}
