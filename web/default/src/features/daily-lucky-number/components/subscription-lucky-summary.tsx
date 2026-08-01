import { ArrowUpRight, CalendarDays } from 'lucide-react'
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import type {
  PlanRecord,
  SubscriptionPlan,
  UserSubscriptionRecord,
} from '@/features/subscriptions/types'
import {
  formatLuckyUsd,
  getMatchedDigits,
  getRemainingDays,
  normalizeMembershipTier,
  normalizeLuckyNumber,
} from '../lib'
import type { LuckyDrawView, LuckyRewardView } from '../types'
import { LuckyNumberCode } from './lucky-number-code'
import { TierBadge } from './tier-badge'

function isPlanRecord(
  value?: SubscriptionPlan | PlanRecord | null
): value is PlanRecord {
  return Boolean(value && 'plan' in value)
}

function getTodayReward(
  subscriptionId: number,
  draw: LuckyDrawView | undefined,
  rewards: LuckyRewardView[]
): LuckyRewardView | undefined {
  if (!draw) return undefined
  return rewards.find(
    (item) =>
      item.draw_date === draw.draw_date &&
      item.reward.user_subscription_id === subscriptionId
  )
}

export function SubscriptionLuckySummary(props: {
  record: UserSubscriptionRecord
  plan?: SubscriptionPlan | PlanRecord | null
  draw?: LuckyDrawView
  rewards?: LuckyRewardView[]
  compact?: boolean
  showLink?: boolean
  className?: string
}) {
  const { t } = useTranslation()
  const subscription = props.record.subscription
  const plan = isPlanRecord(props.plan) ? props.plan.plan : props.plan
  const tier = normalizeMembershipTier(
    subscription.membership_tier || plan?.membership_tier
  )
  const number = subscription.lucky_number
  const suffix = normalizeLuckyNumber(number?.lucky_suffix)
  const todayReward = getTodayReward(
    subscription.id,
    props.draw,
    props.rewards ?? []
  )
  const matchedDigits = props.draw
    ? (todayReward?.reward.matched_digits ??
      getMatchedDigits(suffix, props.draw.winning_number))
    : 0
  const rewardUsd = todayReward?.reward_usd ?? 0

  if (props.compact) {
    return (
      <div className={cn('flex min-w-0 items-center gap-2', props.className)}>
        <TierBadge tier={tier} compact />
        <span className='text-foreground font-mono text-xs font-semibold tabular-nums'>
          {suffix || '----'}
        </span>
        {props.draw ? (
          <span
            className={cn(
              'text-muted-foreground truncate text-xs',
              matchedDigits > 0 && 'text-success font-medium'
            )}
          >
            {matchedDigits > 0
              ? t('{{digits}} digits · +{{amount}}', {
                  digits: matchedDigits,
                  amount: formatLuckyUsd(rewardUsd),
                })
              : t('No match today')}
          </span>
        ) : null}
      </div>
    )
  }

  return (
    <div className={cn('space-y-3', props.className)}>
      <div className='flex flex-wrap items-start justify-between gap-3'>
        <div className='flex min-w-0 items-start gap-2.5'>
          <TierBadge tier={tier} />
          <div className='min-w-0'>
            <div className='text-foreground truncate text-sm font-semibold'>
              {plan?.title || t('Monthly subscription')}
            </div>
            <div className='text-muted-foreground mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs'>
              <CalendarDays className='size-3.5' aria-hidden='true' />
              <span>{t('{{count}} days left', { count: getRemainingDays(subscription.end_time) })}</span>
              <span aria-hidden='true'>·</span>
              <span>{t('Lucky suffix')} {suffix || '----'}</span>
            </div>
          </div>
        </div>
        {props.showLink ? (
          <Button
            variant='ghost'
            size='sm'
            render={<Link to='/daily-lucky-number' />}
          >
            {t('View activity')}
            <ArrowUpRight data-icon='inline-end' />
          </Button>
        ) : null}
      </div>

      <div className='bg-muted/35 flex flex-wrap items-center justify-between gap-2 rounded-lg border px-3 py-2.5'>
        <LuckyNumberCode
          cardCode={number?.card_code}
          luckySuffix={number?.lucky_suffix}
        />
        <span
          className={cn(
            'text-muted-foreground text-xs',
            matchedDigits > 0 && 'text-success font-medium'
          )}
        >
          {props.draw
            ? matchedDigits > 0
              ? t('Today: {{digits}}-digit match · +{{amount}}', {
                  digits: matchedDigits,
                  amount: formatLuckyUsd(rewardUsd),
                })
              : t('No match today')
            : t('Enters from the next draw')}
        </span>
      </div>
    </div>
  )
}
