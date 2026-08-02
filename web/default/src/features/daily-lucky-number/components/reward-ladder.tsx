import { ArrowRight, Trophy } from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  formatLuckyUsd,
  getMembershipTierMultiplier,
  normalizeLuckyNumberRules,
} from '../lib'
import { EASE_OUT_QUINT } from '../motion'
import type { LuckyNumberRules, MembershipTier } from '../types'
import { TierBadge } from './tier-badge'

interface LadderStep {
  digits: number
  baseUsd: number
  example: string
}

function buildSteps(rules: LuckyNumberRules): LadderStep[] {
  return [
    { digits: 1, baseUsd: rules.base_reward_1_usd, example: '末 1 位相同' },
    { digits: 2, baseUsd: rules.base_reward_2_usd, example: '末 2 位相同' },
    { digits: 3, baseUsd: rules.base_reward_3_usd, example: '末 3 位相同' },
    { digits: 4, baseUsd: rules.base_reward_4_usd, example: '四位全中' },
  ]
}

export function RewardLadder(props: {
  rules?: Partial<LuckyNumberRules> | null
  tier: MembershipTier
  matchedDigits: number
  onOpenRules: () => void
}) {
  const reduced = Boolean(useReducedMotion())
  const rules = normalizeLuckyNumberRules(props.rules)
  const steps = buildSteps(rules)
  const multiplier = getMembershipTierMultiplier(props.tier, rules)
  const maxUsd = Math.max(...steps.map((step) => step.baseUsd), 1)

  return (
    <section className='app-page-shell overflow-hidden'>
      <div className='border-border/70 flex flex-wrap items-center justify-between gap-3 border-b px-4 py-4 sm:px-5'>
        <div className='flex min-w-0 items-center gap-3'>
          <span className='bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center rounded-lg'>
            <Trophy className='size-4' aria-hidden='true' />
          </span>
          <div className='min-w-0'>
            <h2 className='text-foreground text-base font-semibold'>
              奖励阶梯
            </h2>
            <p className='text-muted-foreground mt-0.5 text-xs leading-5'>
              连续命中位数越多档位越高，金额按你当前最高月卡倍率换算
            </p>
          </div>
        </div>
        <div className='flex items-center gap-2'>
          <TierBadge tier={props.tier} />
          <span className='text-primary text-xs font-semibold tabular-nums'>
            {multiplier.toFixed(1)}x
          </span>
        </div>
      </div>

      <div className='border-border/70 bg-border/70 grid gap-px border-t sm:grid-cols-2 xl:grid-cols-4'>
        {steps.map((step, index) => (
          <LadderTile
            key={step.digits}
            step={step}
            multiplier={multiplier}
            widthRatio={(step.baseUsd / maxUsd) * 100}
            active={props.matchedDigits === step.digits}
            reached={props.matchedDigits >= step.digits}
            delay={index * 0.08}
            reduced={reduced}
          />
        ))}
      </div>

      <div className='border-border/70 bg-muted/20 space-y-2 border-t px-4 py-3.5 sm:px-5'>
        <p className='text-muted-foreground text-xs leading-5'>
          四位全中时还会额外平分当期奖池；奖池初始{' '}
          {formatLuckyUsd(rules.jackpot_initial_usd)}，每天无人全中增加{' '}
          {formatLuckyUsd(rules.jackpot_increment_usd)}，上限{' '}
          {formatLuckyUsd(rules.jackpot_cap_usd)}。
        </p>
        <Button
          variant='link'
          size='sm'
          className='px-0'
          onClick={props.onOpenRules}
        >
          查看完整规则与各档倍率
          <ArrowRight data-icon='inline-end' />
        </Button>
      </div>
    </section>
  )
}

function LadderTile(props: {
  step: LadderStep
  multiplier: number
  widthRatio: number
  active: boolean
  reached: boolean
  delay: number
  reduced: boolean
}) {
  const finalUsd = props.step.baseUsd * props.multiplier

  return (
    <div
      className={cn(
        'bg-card px-4 py-4 sm:px-5',
        props.active && 'bg-primary/[0.05]'
      )}
    >
      <div className='flex items-start justify-between gap-3'>
        <div className='flex min-w-0 items-center gap-2.5'>
          <span
            className={cn(
              'flex size-7 shrink-0 items-center justify-center rounded-lg border font-mono text-xs font-semibold tabular-nums',
              props.reached
                ? 'border-primary/40 bg-primary/12 text-primary'
                : 'border-border bg-muted/50 text-muted-foreground'
            )}
          >
            {props.step.digits}
          </span>
          <div className='min-w-0'>
            <div className='text-foreground text-sm font-medium'>
              命中 {props.step.digits} 位
            </div>
            <div className='text-muted-foreground text-[11px]'>
              {props.step.example}
            </div>
          </div>
        </div>
        {props.active ? (
          <span className='border-primary/30 bg-primary/10 text-primary shrink-0 rounded-full border px-2 py-0.5 text-[10px] font-semibold'>
            本期命中
          </span>
        ) : null}
      </div>

      <div className='mt-4 flex items-end justify-between gap-3'>
        <div className='text-foreground font-mono text-xl font-semibold tabular-nums'>
          {formatLuckyUsd(finalUsd)}
        </div>
        <div className='text-muted-foreground text-right text-[11px] tabular-nums'>
          基础 {formatLuckyUsd(props.step.baseUsd)} ×{' '}
          {props.multiplier.toFixed(1)}
        </div>
      </div>

      <div className='bg-muted mt-2.5 h-1 overflow-hidden rounded-full'>
        <motion.div
          className={cn(
            'h-full rounded-full',
            props.reached ? 'bg-primary' : 'bg-muted-foreground/35'
          )}
          initial={props.reduced ? false : { width: 0 }}
          animate={{ width: `${Math.max(6, props.widthRatio)}%` }}
          transition={{
            duration: 0.7,
            ease: EASE_OUT_QUINT,
            delay: props.reduced ? 0 : props.delay,
          }}
        />
      </div>
    </div>
  )
}
