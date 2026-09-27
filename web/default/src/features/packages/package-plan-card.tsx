import { useEffect, useState } from 'react'
import { toIntlLocale } from '@/i18n/languages'
import {
  ArrowRight,
  ChevronDown,
  Clock3,
  Gift,
  Layers3,
  Sparkles,
} from 'lucide-react'
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
  collectiveRoom?: GroupBuyItem
  record: PlanRecord
  purchaseCount: number
  onPurchase: (
    purchaseType?: SubscriptionPurchaseType,
    groupBuyId?: number
  ) => void
  currentSubscription?: UserSubscriptionRecord
  onFuel?: (
    subscription: UserSubscriptionRecord,
    title: string,
    config: { minimumQuota: number; quotaStep: number }
  ) => void
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
  // surfaced to screen readers; visual chrome stays clean
  const tierRows = buildPackageQuotaTiers(plan, t)
  const isCurrentPlan =
    props.currentSubscription?.subscription.plan_id === plan.id
  const canFuel =
    isCurrentPlan &&
    props.currentSubscription?.subscription.status === 'active' &&
    plan.fuel_enabled === true &&
    Number(plan.fuel_min_quota || 0) > 0 &&
    Number(plan.fuel_quota_step || 0) > 0
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
        'border-border bg-card hover:border-primary/40 relative h-full overflow-hidden py-0 transition-colors duration-200',
        // Keep the recommended card the same 1px geometry as its siblings;
        // a 2px border made the first card visibly heavier and shifted its
        // content compared with the other plans.
        isRecommended &&
          'border-primary/70 bg-primary/[0.035] ring-primary/20 ring-1'
      )}
    >
      <CardContent className='flex h-full flex-col gap-3 p-4'>
        <div className='text-left'>
          <div className='flex items-center'>
            {isRecommended && (
              <span className='text-primary inline-flex items-center gap-1 text-xs font-semibold'>
                <Sparkles className='h-3.5 w-3.5' />
                {t('Most popular')}
              </span>
            )}
          </div>
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
              <Trans i18nKey={'套餐首购'} />{' '}
              {Number(firstPurchaseDiscount.toFixed(1))}{' '}
              <Trans i18nKey={'折'} />
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
              {t('Base quota ({{currency}})', { currency: getCurrencyLabel() })}
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
          {monthlyPassBenefit && (
            <div className='border-primary/20 bg-primary/[0.06] flex items-start gap-2 rounded-md border px-2.5 py-2'>
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
            <div className='border-primary/25 bg-primary/[0.04] space-y-3 rounded-lg border p-3'>
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
                        'min-w-0 rounded-md border px-1.5 py-2 text-center',
                        current
                          ? 'border-primary bg-primary/10'
                          : 'border-border bg-background/60'
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
                {room?.joined && (
                  <p className='text-primary font-medium'>{t('已参与本期')}</p>
                )}
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
            {monthlyPassBenefit && (
              <div className='text-muted-foreground text-xs leading-relaxed'>
                {t(
                  '仅官方分组实际扣月卡额度时额外乘 0.1；第三方分组和余额扣费不享受倍率卡优惠。'
                )}
              </div>
            )}
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

        <Button
          type='button'
          variant='outline'
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
            className='min-h-11 w-full whitespace-normal'
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
          {room?.joined && (
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
            <div className='sr-only'>{blockedReason}</div>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
