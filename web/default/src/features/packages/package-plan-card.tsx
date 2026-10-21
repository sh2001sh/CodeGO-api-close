import { useEffect, useState } from 'react'
import { toIntlLocale } from '@/i18n/languages'
import { ArrowRight, ChevronDown, Clock3, Gift, Layers3 } from 'lucide-react'
import { Trans, useTranslation } from 'react-i18next'
import { getCurrencyLabel } from '@/lib/currency'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import type { GroupBuyItem } from '@/features/group-buy/types'
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

const monthlyPassBenefitKey = {
  lite: '15-minute 0.1x multiplier card',
  standard: '30-minute 0.1x multiplier card',
  pro: '45-minute 0.1x multiplier card',
  ultra: '1-hour 0.1x multiplier card',
} as const

export function PackagePlanCard(props: {
  formatPrice?: (amount: number, currency: string) => string
  collectiveRoom?: GroupBuyItem
  record: PlanRecord
  purchaseCount: number
  onPurchase: (
    purchaseType?: SubscriptionPurchaseType,
    groupBuyId?: number
  ) => void
  currentSubscription?: UserSubscriptionRecord
}) {
  const { t, i18n } = useTranslation()
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [])
  const [showDetails, setShowDetails] = useState(false)
  const plan = props.record.plan
  const title = translatePlanTitle(plan.title, t)
  const formatPrice = props.formatPrice || formatSubscriptionPlanPrice
  const isCreditPack = plan.plan_type === 'credit_pack'
  const groupBuyEnabled =
    plan.group_buy_enabled === true &&
    plan.plan_type !== 'daily' &&
    !isCreditPack
  const limit = Number(plan.max_purchase_per_user || 0)
  const limitReached = limit > 0 && props.purchaseCount >= limit
  const actionLabel = isCreditPack
    ? t('购买')
    : translatePlanAction(props.record.action, t)
  const effectiveAmount =
    props.record.action === 'disabled'
      ? plan.price_amount
      : (props.record.amount_due ?? plan.price_amount)
  const firstPurchaseDiscountApplied =
    !isCreditPack && props.record.first_purchase_discount_applied === true
  const firstPurchaseDiscount = Number(
    (props.record.first_purchase_discount_multiplier || 0) * 10
  )
  const baseQuota = Number(plan.total_amount || 0)
  const blockedReason =
    translateDisabledReason(props.record.disabled_reason, t) ||
    t('A higher active plan with remaining quota prevents downgrading.')
  const tierRows = buildPackageQuotaTiers(plan, t)
  let detailsText = t(
    'This plan is not included in the Collective Benefit Program and is settled immediately after payment.'
  )
  if (isCreditPack) {
    detailsText = t(
      '新额度包永久有效、无周期重置，按钱包同价扣费；仅适用于已接入的渠道。'
    )
  } else if (groupBuyEnabled) {
    detailsText = t(
      'The base quota is available immediately. The collective bonus is settled by the final participation tier after five participants join or 48 hours pass.'
    )
  }
  const bonusQuota =
    isCreditPack && plan.currency === 'CNY' && plan.price_amount > 0
      ? Math.max(0, baseQuota - plan.price_amount * 500_000)
      : undefined
  const isCurrentPlan =
    props.currentSubscription?.subscription.plan_id === plan.id
  const monthlyPassBenefit =
    plan.plan_type === 'monthly'
      ? monthlyPassBenefitKey[
          plan.membership_tier as keyof typeof monthlyPassBenefitKey
        ]
      : undefined

  const room = props.collectiveRoom
  const count = room?.current_count ?? 0
  const expired = Boolean(room?.expires_at && room.expires_at * 1000 <= now)
  const canJoin =
    groupBuyEnabled &&
    room &&
    !room.joined &&
    room.status === 'pending' &&
    !expired &&
    count < room.target_count
  const secondsLeft = Math.max(
    0,
    Math.ceil(((room?.expires_at ?? 0) * 1000 - now) / 1000)
  )
  const remaining = `${Math.floor(secondsLeft / 3600)}:${String(Math.floor((secondsLeft % 3600) / 60)).padStart(2, '0')}:${String(secondsLeft % 60).padStart(2, '0')}`
  const deadline = room?.expires_at
    ? new Intl.DateTimeFormat(toIntlLocale(i18n.language), {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
        timeZoneName: 'short',
      }).format(new Date(room.expires_at * 1000))
    : null

  return (
    <Card
      className={cn(
        'commerce-plan border-border bg-card relative h-full rounded-3xl shadow-sm transition-all',
        isCurrentPlan && 'commerce-plan-current'
      )}
    >
      <CardContent className='flex h-full flex-col gap-3 p-5'>
        <header className='commerce-plan-header'>
          <div>
            <h4 className='text-foreground mt-1 text-lg font-bold tracking-tight'>
              {title}
            </h4>
            <div className='commerce-plan-identity'>
              <span className='text-muted-foreground'>
                {translatePlanSubtitle(plan, t)}
              </span>
              <code>#{plan.id}</code>
            </div>
          </div>
          <div className='flex flex-wrap gap-2'>
            {isCurrentPlan && (
              <span className='inline-flex items-center gap-1.5 rounded-full border border-emerald-500/30 bg-emerald-500/10 px-2.5 py-1 text-xs font-semibold text-emerald-700 dark:text-emerald-400'>
                <span className='size-1.5 animate-pulse rounded-full bg-emerald-500' />
                {t('Currently active')}
              </span>
            )}
            {firstPurchaseDiscountApplied ? (
              <span className='inline-flex items-center rounded-full border border-orange-500/30 bg-orange-500/10 px-2.5 py-1 text-xs font-semibold text-orange-700 dark:text-orange-400'>
                <Trans i18nKey={'套餐首购'} />{' '}
                {Number(firstPurchaseDiscount.toFixed(1))}{' '}
                <Trans i18nKey={'折'} />
              </span>
            ) : null}
          </div>
        </header>

        <div className='commerce-plan-price'>
          <div className='text-muted-foreground text-xs font-medium'>
            {t('Payment price')}
          </div>
          <div className='text-foreground text-3xl tabular-nums'>
            {formatPrice(effectiveAmount, plan.currency)}
          </div>
          {effectiveAmount !== plan.price_amount && (
            <div className='text-muted-foreground mt-1 text-xs line-through'>
              {formatPrice(plan.price_amount, plan.currency)}
            </div>
          )}
        </div>

        <div className='commerce-plan-details'>
          <div className='flex items-center justify-between text-sm'>
            <span className='text-muted-foreground'>
              {isCreditPack
                ? t('到账额度')
                : t('Base quota ({{currency}})', {
                    currency: getCurrencyLabel(),
                  })}
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
          {bonusQuota !== undefined && (
            <div className='flex items-center justify-between text-sm'>
              <span className='text-muted-foreground'>
                {t('赠额')} ·{' '}
                {(bonusQuota / (plan.price_amount * 500_000)).toLocaleString(
                  toIntlLocale(i18n.language),
                  { style: 'percent', maximumFractionDigits: 2 }
                )}
              </span>
              <span className='text-primary font-semibold'>
                +{formatSubscriptionQuotaAmount(bonusQuota)}
              </span>
            </div>
          )}
          {monthlyPassBenefit && (
            <div className='package-plan-benefit flex items-start gap-2'>
              <Gift className='text-primary mt-0.5 h-4 w-4 shrink-0' />
              <div className='min-w-0 text-xs'>
                <div className='text-foreground font-semibold'>
                  {t('Monthly pass bonus')}
                </div>
                <div className='text-primary mt-0.5 font-medium'>
                  {t(monthlyPassBenefit)}
                </div>
              </div>
            </div>
          )}
          {groupBuyEnabled && (
            <div className='package-collective space-y-3'>
              <div className='flex flex-wrap items-center justify-between gap-2'>
                <span className='text-foreground flex items-center gap-1.5 text-sm font-semibold'>
                  <Layers3 className='text-primary h-4 w-4' />
                  {t('Collective benefit plan')}
                </span>
                <span className='text-primary text-xs font-semibold'>
                  {t('Participants: {{count}} / {{target}}', {
                    count,
                    target: room?.target_count ?? 5,
                  })}
                </span>
              </div>
              <div className='grid grid-cols-3 gap-2'>
                {([2, 3, 5] as const).map((threshold, index, thresholds) => {
                  const bonus =
                    [
                      plan.group_buy_bonus_2,
                      plan.group_buy_bonus_3,
                      plan.group_buy_bonus_5,
                    ][index] || 0
                  const achieved = count >= threshold
                  const current =
                    achieved && count < (thresholds[index + 1] ?? Infinity)
                  return (
                    <div
                      key={threshold}
                      className={cn(
                        'package-collective-tier min-w-0 px-1.5 py-2 text-center',
                        current ? 'bg-primary/5' : 'bg-transparent'
                      )}
                    >
                      <div className='text-sm font-semibold'>
                        {translateCollectiveTierLabel(threshold, t)}
                      </div>
                      <div className='text-primary mt-1 text-sm font-bold break-words tabular-nums'>
                        {formatSubscriptionQuotaAmount(
                          baseQuota + parseSubscriptionQuotaUSDToUnits(bonus)
                        )}
                      </div>
                      <div className='text-muted-foreground mt-1 text-xs break-words'>
                        {t('Bonus +{{amount}}', {
                          amount: formatSubscriptionQuotaAmount(
                            parseSubscriptionQuotaUSDToUnits(bonus)
                          ),
                        })}
                      </div>
                      <div
                        className={cn(
                          'mt-1 text-xs',
                          achieved
                            ? 'text-primary font-semibold'
                            : 'text-muted-foreground'
                        )}
                      >
                        {t(
                          current
                            ? 'Current tier'
                            : achieved
                              ? 'Reached'
                              : 'Not reached'
                        )}
                      </div>
                    </div>
                  )
                })}
              </div>
              <p className='text-muted-foreground text-xs'>
                {t('Final quota by tier')}
              </p>
              <div className='border-border space-y-1.5 border-t pt-2 text-xs'>
                <p className='text-foreground flex items-start gap-1.5 font-medium'>
                  <Clock3 className='mt-0.5 h-3.5 w-3.5 shrink-0' />
                  {deadline
                    ? t('Deadline: {{date}}', { date: deadline })
                    : t(
                        'Closes 48 hours after the first paid participant joins.'
                      )}
                </p>
                {deadline && (
                  <p className='text-primary font-semibold tabular-nums'>
                    {t(
                      expired ||
                        count >= (room?.target_count ?? 5) ||
                        room?.status !== 'pending'
                        ? 'Awaiting settlement'
                        : 'Time left: {{time}}',
                      { time: remaining }
                    )}
                  </p>
                )}
                <p className='text-muted-foreground'>
                  {t(
                    'Settles at 5 participants or the deadline. Only the final tier bonus applies.'
                  )}
                </p>
                {groupBuyEnabled && room?.joined && (
                  <p className='text-primary font-medium'>{t('已参与本期')}</p>
                )}
              </div>
            </div>
          )}
        </div>

        {showDetails && (
          <div className='package-plan-expanded border-border space-y-2 border-t pt-3'>
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
            {monthlyPassBenefit && (
              <div className='text-muted-foreground text-xs leading-relaxed'>
                {t(
                  '仅官方分组实际扣月卡额度时额外乘 0.1；第三方分组和余额扣费不享受倍率卡优惠。'
                )}
              </div>
            )}
            <div className='text-muted-foreground text-xs leading-relaxed'>
              {detailsText}
            </div>
          </div>
        )}

        <Button
          type='button'
          variant='ghost'
          size='sm'
          onClick={() => setShowDetails(!showDetails)}
          className='min-h-11 w-full'
          aria-expanded={showDetails}
        >
          {t(showDetails ? 'Hide details' : 'View details')}
          <ChevronDown
            className={cn(
              'h-3.5 w-3.5 transition-transform',
              showDetails && 'rotate-180'
            )}
          />
        </Button>

        <div className='commerce-plan-cta space-y-2'>
          <Button
            className='min-h-12 w-full justify-center gap-1.5 rounded-2xl text-[14.5px] font-semibold whitespace-normal'
            disabled={limitReached || props.record.action === 'disabled'}
            onClick={() =>
              props.onPurchase(
                canJoin ? (room.id > 0 ? 'join_group' : 'group_buy') : 'normal',
                canJoin ? room.id : 0
              )
            }
          >
            {limitReached
              ? t('Purchase limit reached')
              : canJoin
                ? t(room.id > 0 ? '购买并参与本期' : '购买并开启本期')
                : actionLabel}
            {!limitReached && <ArrowRight className='ml-1 h-4 w-4 shrink-0' />}
          </Button>
          {groupBuyEnabled && room?.joined && (
            <p className='text-muted-foreground text-center text-xs'>
              {t('Additional purchases do not join this round again.')}
            </p>
          )}
          {groupBuyEnabled && room && !canJoin && !room.joined && (
            <p className='text-muted-foreground text-center text-xs'>
              {t('This round is closed. This purchase grants base quota only.')}
            </p>
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
            <div role='status' className='text-destructive text-base'>
              {blockedReason}
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
