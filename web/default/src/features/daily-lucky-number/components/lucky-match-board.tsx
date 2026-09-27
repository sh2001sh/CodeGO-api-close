import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import i18n from '@/i18n/config'
import {
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  Gift,
  Ticket,
  TrendingUp,
} from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { useTranslation, Trans } from 'react-i18next'
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
  BlindBoxLuckyNumber,
  LuckyNumberRules,
  LuckyNumberSubscription,
  LuckyRewardView,
} from '../types'
import { LuckyDigits } from './lucky-digits'
import { TierBadge } from './tier-badge'

const BLIND_BOX_PAGE_SIZE = 6

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

function findBlindBoxReward(
  openRecordId: number,
  draw: LuckyDrawView | undefined,
  rewards: LuckyRewardView[]
) {
  if (!draw) return undefined
  return rewards.find(
    (item) =>
      item.draw_date === draw.draw_date &&
      item.reward.participation_type === 'blind_box' &&
      item.reward.blind_box_open_record_id === openRecordId
  )
}

export function LuckyMatchBoard(props: {
  subscriptions: LuckyNumberSubscription[]
  blindBoxNumbers: BlindBoxLuckyNumber[]
  draw?: LuckyDrawView
  rewards: LuckyRewardView[]
  rules?: Partial<LuckyNumberRules> | null
}) {
  useTranslation()
  const reduced = Boolean(useReducedMotion())
  const { container, item } = stackVariants(reduced)
  const rules = normalizeLuckyNumberRules(props.rules)
  const published = Boolean(props.draw?.winning_number)
  const numberCount = props.subscriptions.length + props.blindBoxNumbers.length
  const [blindBoxPage, setBlindBoxPage] = useState(1)
  const blindBoxPageCount = Math.max(
    1,
    Math.ceil(props.blindBoxNumbers.length / BLIND_BOX_PAGE_SIZE)
  )
  const currentBlindBoxPage = Math.min(blindBoxPage, blindBoxPageCount)
  const visibleBlindBoxNumbers = props.blindBoxNumbers.slice(
    (currentBlindBoxPage - 1) * BLIND_BOX_PAGE_SIZE,
    currentBlindBoxPage * BLIND_BOX_PAGE_SIZE
  )

  return (
    <section className='space-y-3'>
      <div className='flex flex-wrap items-end justify-between gap-2'>
        <div className='min-w-0'>
          <h2 className='text-foreground text-base font-semibold tracking-tight'>
            <Trans i18nKey={'我的对号板'} />
          </h2>
          <p className='text-muted-foreground mt-1 text-sm leading-6'>
            <Trans
              i18nKey={
                '号码从最右侧一位一位往左比，连续对上几位就拿对应档位；只结算最高档位。'
              }
            />
          </p>
        </div>
        <span className='text-muted-foreground shrink-0 text-xs tabular-nums'>
          {numberCount} <Trans i18nKey={'个有效号码'} />
        </span>
      </div>

      {numberCount === 0 ? (
        <div className='app-page-shell'>
          <Empty className='border-0'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <Ticket aria-hidden='true' />
              </EmptyMedia>
              <EmptyTitle>
                <Trans i18nKey={'暂时没有可参与的号码'} />
              </EmptyTitle>
              <EmptyDescription>
                <Trans
                  i18nKey={
                    '购买符合条件的月卡，或开启盲盒获得当前开奖周期有效的号码。'
                  }
                />
              </EmptyDescription>
            </EmptyHeader>
            <Button render={<Link to='/packages' />}>
              <Trans i18nKey={'查看套餐'} />
            </Button>
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
          {props.blindBoxNumbers.length > 0 ? (
            <div className='space-y-2 pt-1'>
              <div className='flex flex-wrap items-center justify-between gap-2 px-1'>
                <div className='text-foreground flex items-center gap-2 text-sm font-semibold'>
                  <Gift className='text-primary size-4' aria-hidden='true' />
                  <Trans i18nKey={'今日盲盒号码'} />
                </div>
                <span className='text-muted-foreground text-xs'>
                  <Trans i18nKey={'20:00 至次日 19:59 · 基础 1.0x'} />
                </span>
              </div>
              <div className='grid gap-3 lg:grid-cols-2'>
                {visibleBlindBoxNumbers.map((entry) => (
                  <motion.div
                    key={entry.blind_box_open_record_id}
                    variants={item}
                  >
                    <BlindBoxMatchCard
                      entry={entry}
                      draw={props.draw}
                      rewards={props.rewards}
                      published={published}
                    />
                  </motion.div>
                ))}
              </div>
              {blindBoxPageCount > 1 ? (
                <div className='flex items-center justify-center gap-2 pt-1'>
                  <Button
                    variant='outline'
                    size='icon-sm'
                    onClick={() =>
                      setBlindBoxPage((value) => Math.max(1, value - 1))
                    }
                    disabled={currentBlindBoxPage <= 1}
                    aria-label={i18n.t('上一页盲盒号码')}
                  >
                    <ChevronLeft aria-hidden='true' />
                  </Button>
                  <span className='text-muted-foreground min-w-24 text-center text-xs tabular-nums'>
                    <Trans i18nKey={'第'} /> {currentBlindBoxPage} /{' '}
                    {blindBoxPageCount} <Trans i18nKey={'页 · 共'} />{' '}
                    {props.blindBoxNumbers.length} <Trans i18nKey={'个'} />
                  </span>
                  <Button
                    variant='outline'
                    size='icon-sm'
                    onClick={() =>
                      setBlindBoxPage((value) =>
                        Math.min(blindBoxPageCount, value + 1)
                      )
                    }
                    disabled={currentBlindBoxPage >= blindBoxPageCount}
                    aria-label={i18n.t('下一页盲盒号码')}
                  >
                    <ChevronRight aria-hidden='true' />
                  </Button>
                </div>
              ) : null}
            </div>
          ) : null}
        </motion.div>
      )}
    </section>
  )
}

function BlindBoxMatchCard(props: {
  entry: BlindBoxLuckyNumber
  draw?: LuckyDrawView
  rewards: LuckyRewardView[]
  published: boolean
}) {
  useTranslation()
  const suffix = normalizeLuckyNumber(props.entry.lucky_suffix)
  const belongsToPublishedDraw = Boolean(
    props.published && props.entry.draw_date === props.draw?.draw_date
  )
  const reward = findBlindBoxReward(
    props.entry.blind_box_open_record_id,
    props.draw,
    props.rewards
  )
  const matchedDigits = belongsToPublishedDraw
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
      <div className='border-border/70 flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3'>
        <div className='flex min-w-0 items-center gap-2.5'>
          <span className='border-primary/25 bg-primary/10 text-primary flex size-8 shrink-0 items-center justify-center rounded-lg border'>
            <Gift className='size-4' aria-hidden='true' />
          </span>
          <div className='min-w-0'>
            <div className='text-foreground text-sm font-semibold'>
              <Trans i18nKey={'盲盒幸运号'} />
            </div>
            <div className='text-muted-foreground mt-0.5 text-xs'>
              <Trans i18nKey={'仅限'} /> {props.entry.draw_date}{' '}
              <Trans i18nKey={'· 1.0x 倍率'} />
            </div>
          </div>
        </div>
        <MatchVerdict
          published={belongsToPublishedDraw}
          matchedDigits={matchedDigits}
          rewardUsd={rewardUsd}
        />
      </div>
      <div className='space-y-2.5 px-4 py-4'>
        <DigitRow
          label={belongsToPublishedDraw ? '本期开奖' : '等待开奖'}
          value={
            belongsToPublishedDraw ? props.draw?.winning_number : undefined
          }
          pending={!belongsToPublishedDraw}
          matchedDigits={matchedDigits}
        />
        <DigitRow
          label={i18n.t('盲盒号码')}
          value={suffix}
          matchedDigits={matchedDigits}
          dim={belongsToPublishedDraw}
        />
        <p className='text-muted-foreground text-xs leading-5'>
          {belongsToPublishedDraw
            ? hit
              ? i18n.t(
                  '从右往左连续对上 {{param0}} 位，按基础 1.0 倍率结算到钱包余额。',
                  { param0: matchedDigits }
                )
              : i18n.t('最右侧一位未对上，本期未中奖。')
            : i18n.t(
                '该号码参与 {{param0}} 20:00 开奖，有效周期为前一日 20:00 至开奖日 19:59。',
                { param0: props.entry.draw_date }
              )}
        </p>
      </div>
    </article>
  )
}

function MatchCard(props: {
  entry: LuckyNumberSubscription
  draw?: LuckyDrawView
  rewards: LuckyRewardView[]
  rules: LuckyNumberRules
  published: boolean
}) {
  useTranslation()
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
              {plan?.title || i18n.t('月卡套餐')}
            </div>
            <div className='text-muted-foreground mt-0.5 flex flex-wrap items-center gap-x-2 text-xs'>
              <CalendarDays className='size-3.5' aria-hidden='true' />
              <span>
                <Trans i18nKey={'剩余'} />{' '}
                {getRemainingDays(subscription.end_time)}{' '}
                <Trans i18nKey={'天'} />
              </span>
              <span aria-hidden='true'>·</span>
              <span className='text-primary inline-flex items-center gap-1 font-medium'>
                <TrendingUp className='size-3' aria-hidden='true' />
                {multiplier.toFixed(1)}
                <Trans i18nKey={'x 倍率'} />
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
          label={i18n.t('今日开奖')}
          value={props.draw?.winning_number}
          pending={!props.published}
          matchedDigits={matchedDigits}
        />
        <DigitRow
          label={i18n.t('我的尾号')}
          value={suffix}
          matchedDigits={matchedDigits}
          dim={props.published}
        />
        <p className='text-muted-foreground pt-0.5 text-xs leading-5'>
          {props.published
            ? hit
              ? i18n.t(
                  '从右往左连续对上 {{param0}} 位，按 {{param1}} 位档位 × {{param2}} 倍率结算到钱包余额。',
                  {
                    param0: matchedDigits,
                    param1: matchedDigits,
                    param2: multiplier.toFixed(1),
                  }
                )
              : i18n.t('最右侧一位未对上，本期不计奖励，也不扣除任何月卡额度。')
            : i18n.t('开奖后两行号码会自动对齐比对，对上的位会高亮显示。')}
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
  ineligible?: boolean
}) {
  if (props.ineligible) {
    return (
      <span className='text-muted-foreground shrink-0 text-xs'>
        <Trans i18nKey={'未参与本次开奖'} />
      </span>
    )
  }
  if (!props.published) {
    return (
      <span className='text-muted-foreground shrink-0 text-xs'>
        <Trans i18nKey={'等待今日开奖'} />
      </span>
    )
  }
  if (props.matchedDigits === 0) {
    return (
      <span className='text-muted-foreground shrink-0 text-xs'>
        <Trans i18nKey={'今日未命中'} />
      </span>
    )
  }

  return (
    <span className='border-success/25 bg-success/10 text-success shrink-0 rounded-full border px-2.5 py-1 text-xs font-semibold tabular-nums'>
      <Trans i18nKey={'命中'} /> {props.matchedDigits}{' '}
      <Trans i18nKey={'位 · +'} />
      {formatLuckyUsd(props.rewardUsd)}
    </span>
  )
}
