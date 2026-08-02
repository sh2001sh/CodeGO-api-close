import { Link } from '@tanstack/react-router'
import { CalendarDays, Ticket, TrendingUp } from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import type {
  PlanRecord,
  SubscriptionPlan,
} from '@/features/subscriptions/types'
import {
  formatLuckyUsd,
  getMatchedDigits,
  getMembershipTierMultiplier,
  getRemainingDays,
  normalizeLuckyNumber,
  normalizeLuckyNumberRules,
  normalizeMembershipTier,
} from '../lib'
import { stackVariants } from '../motion'
import type {
  LuckyDrawView,
  LuckyNumberRules,
  LuckyNumberSubscription,
  LuckyRewardView,
} from '../types'
import { LuckyDigits } from './lucky-digits'
import { TierBadge } from './tier-badge'

function resolvePlan(value?: SubscriptionPlan | PlanRecord | null) {
  if (!value) return null
  return 'plan' in value ? value.plan : value
}

function findReward(
  subscriptionId: number,
  draw: LuckyDrawView | undefined,
  rewards: LuckyRewardView[]
) {
  if (!draw) return undefined
  return rewards.find(
    (item) =>
      item.draw_date === draw.draw_date &&
      item.reward.user_subscription_id === subscriptionId
  )
}

export function LuckyMatchBoard(props: {
  subscriptions: LuckyNumberSubscription[]
  draw?: LuckyDrawView
  rewards: LuckyRewardView[]
  rules?: Partial<LuckyNumberRules> | null
}) {
  const reduced = Boolean(useReducedMotion())
  const { container, item } = stackVariants(reduced)
  const rules = normalizeLuckyNumberRules(props.rules)
  const published = Boolean(props.draw?.winning_number)

  return (
    <section className='space-y-3'>
      <div className='flex flex-wrap items-end justify-between gap-2'>
        <div className='min-w-0'>
          <h2 className='text-foreground text-base font-semibold tracking-tight'>
            我的对号板
          </h2>
          <p className='text-muted-foreground mt-1 text-sm leading-6'>
            号码从最右侧一位一位往左比，连续对上几位就拿对应档位；只结算最高档位。
          </p>
        </div>
        <span className='text-muted-foreground shrink-0 text-xs tabular-nums'>
          {props.subscriptions.length} 个有效号码
        </span>
      </div>

      {props.subscriptions.length === 0 ? (
        <div className='app-page-shell'>
          <Empty className='border-0'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <Ticket aria-hidden='true' />
              </EmptyMedia>
              <EmptyTitle>暂时没有可参与的月卡</EmptyTitle>
              <EmptyDescription>
                购买符合条件的月卡即可获得专属号码，并自动参与下一期开奖。
              </EmptyDescription>
            </EmptyHeader>
            <Button render={<Link to='/packages' />}>查看套餐</Button>
          </Empty>
        </div>
      ) : (
        <motion.div
          className='space-y-3'
          variants={container}
          initial='initial'
          animate='animate'
        >
          {props.subscriptions.map((entry) => (
            <motion.div key={entry.subscription.id} variants={item}>
              <MatchCard
                entry={entry}
                draw={props.draw}
                rewards={props.rewards}
                rules={rules}
                published={published}
              />
            </motion.div>
          ))}
        </motion.div>
      )}
    </section>
  )
}

function MatchCard(props: {
  entry: LuckyNumberSubscription
  draw?: LuckyDrawView
  rewards: LuckyRewardView[]
  rules: LuckyNumberRules
  published: boolean
}) {
  const subscription = props.entry.subscription
  const plan = resolvePlan(props.entry.plan)
  const tier = normalizeMembershipTier(
    subscription.membership_tier || plan?.membership_tier
  )
  const multiplier = getMembershipTierMultiplier(tier, props.rules)
  const suffix = normalizeLuckyNumber(
    subscription.lucky_number?.lucky_suffix ?? props.entry.number?.lucky_suffix
  )
  const reward = findReward(subscription.id, props.draw, props.rewards)
  const matchedDigits = props.published
    ? (reward?.reward.matched_digits ??
      getMatchedDigits(suffix, props.draw?.winning_number))
    : 0
  const rewardUsd = reward?.reward_usd ?? 0
  const hit = matchedDigits > 0

  return (
    <article
      className={cn(
        'app-page-shell overflow-hidden transition-colors',
        hit && 'border-primary/40 bg-primary/[0.035]'
      )}
    >
      <div className='border-border/70 flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3 sm:px-5'>
        <div className='flex min-w-0 items-center gap-2.5'>
          <TierBadge tier={tier} />
          <div className='min-w-0'>
            <div className='text-foreground truncate text-sm font-semibold'>
              {plan?.title || '月卡套餐'}
            </div>
            <div className='text-muted-foreground mt-0.5 flex flex-wrap items-center gap-x-2 text-xs'>
              <CalendarDays className='size-3.5' aria-hidden='true' />
              <span>剩余 {getRemainingDays(subscription.end_time)} 天</span>
              <span aria-hidden='true'>·</span>
              <span className='text-primary inline-flex items-center gap-1 font-medium'>
                <TrendingUp className='size-3' aria-hidden='true' />
                {multiplier.toFixed(1)}x 倍率
              </span>
            </div>
          </div>
        </div>
        <MatchVerdict
          published={props.published}
          matchedDigits={matchedDigits}
          rewardUsd={rewardUsd}
        />
      </div>

      <div className='space-y-2.5 px-4 py-4 sm:px-5'>
        <DigitRow
          label='今日开奖'
          value={props.draw?.winning_number}
          pending={!props.published}
          matchedDigits={matchedDigits}
        />
        <DigitRow
          label='我的尾号'
          value={suffix}
          matchedDigits={matchedDigits}
          dim={props.published}
        />
        <p className='text-muted-foreground pt-0.5 text-xs leading-5'>
          {props.published
            ? hit
              ? `从右往左连续对上 ${matchedDigits} 位，按 ${matchedDigits} 位档位 × ${multiplier.toFixed(1)} 倍率结算到钱包余额。`
              : '最右侧一位未对上，本期不计奖励，也不扣除任何月卡额度。'
            : '开奖后两行号码会自动对齐比对，对上的位会高亮显示。'}
        </p>
      </div>
    </article>
  )
}

function DigitRow(props: {
  label: string
  value?: string
  matchedDigits: number
  pending?: boolean
  dim?: boolean
}) {
  return (
    <div className='flex items-center justify-between gap-3'>
      <span className='text-muted-foreground shrink-0 text-xs'>
        {props.label}
      </span>
      <LuckyDigits
        size='md'
        value={props.value}
        placeholder='0000'
        pending={props.pending}
        matchedDigits={props.matchedDigits}
        dimUnmatched={props.dim}
      />
    </div>
  )
}

function MatchVerdict(props: {
  published: boolean
  matchedDigits: number
  rewardUsd: number
}) {
  if (!props.published) {
    return (
      <span className='text-muted-foreground shrink-0 text-xs'>
        等待今日开奖
      </span>
    )
  }
  if (props.matchedDigits === 0) {
    return (
      <span className='text-muted-foreground shrink-0 text-xs'>今日未命中</span>
    )
  }

  return (
    <span className='border-success/25 bg-success/10 text-success shrink-0 rounded-full border px-2.5 py-1 text-xs font-semibold tabular-nums'>
      命中 {props.matchedDigits} 位 · +{formatLuckyUsd(props.rewardUsd)}
    </span>
  )
}
