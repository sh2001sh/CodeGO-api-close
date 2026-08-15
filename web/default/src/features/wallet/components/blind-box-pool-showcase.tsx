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
import {
  Boxes,
  Crown,
  PackageOpen,
  Sparkles,
  Wallet,
  type LucideIcon,
} from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { cn } from '@/lib/utils'
import {
  RARITY_BADGE,
  RARITY_RING,
  classifyTier,
  formatTierAmount,
  groupTiersByRewardType,
} from '../lib/blind-box-rarity'
import type { BlindBoxSelfData, BlindBoxTier } from '../types'

const EASE_OUT_QUINT = [0.22, 1, 0.36, 1] as const

const GRID = {
  initial: {},
  animate: { transition: { staggerChildren: 0.05 } },
}

const CELL = {
  initial: { opacity: 0, y: 12 },
  animate: {
    opacity: 1,
    y: 0,
    transition: { duration: 0.38, ease: EASE_OUT_QUINT },
  },
}

const REDUCED_CELL = {
  initial: { opacity: 0 },
  animate: { opacity: 1, transition: { duration: 0.18 } },
}

export function BlindBoxPoolShowcase(props: {
  data: BlindBoxSelfData | null
  tiers?: BlindBoxTier[]
  title?: string
  description?: string
  hideSubscription?: boolean
}) {
  const reduced = Boolean(useReducedMotion())
  const grouped = groupTiersByRewardType(props.tiers || props.data?.tiers || [])
  const hiddenProbability = props.data?.subscription_prize_probability || 0
  const hiddenTitle = props.data?.subscription_plan_title || 'Lite 月卡'

  return (
    <section className='app-page-shell overflow-hidden'>
      <div className='border-border/70 flex flex-wrap items-center justify-between gap-3 border-b px-4 py-4 sm:px-5'>
        <div className='flex min-w-0 items-center gap-3'>
          <span className='bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center rounded-lg'>
            <Boxes className='size-4' aria-hidden='true' />
          </span>
          <div className='min-w-0'>
            <h2 className='text-foreground text-base font-semibold'>
              {props.title || '奖池一览'}
            </h2>
            <p className='text-muted-foreground mt-0.5 text-xs leading-5'>
              {props.description || '每次抽取都从下面所有奖励中按概率开出一项'}
            </p>
          </div>
        </div>
        <span className='text-muted-foreground text-xs'>概率已公开</span>
      </div>

      <div className='space-y-5 px-4 py-4 sm:px-5 sm:py-5'>
        {!props.hideSubscription ? (
          <PoolGroup
            icon={Crown}
            title='隐藏款'
            hint='最稀有的一档，抽中直接获得月卡'
            reduced={reduced}
          >
            <PoolCell
              label={hiddenTitle}
              probability={hiddenProbability}
              rarity='legendary'
              note='直接发放一张月卡，享受对应档位权益'
              reduced={reduced}
            />
          </PoolGroup>
        ) : null}

        {grouped.claude.length > 0 ? (
          <PoolGroup
            icon={Sparkles}
            title='通用额度'
            hint='直接进入通用额度钱包，永久有效'
            reduced={reduced}
          >
            {grouped.claude.map((tier) => (
              <TierCell key={tier.name} tier={tier} reduced={reduced} />
            ))}
          </PoolGroup>
        ) : null}

        {grouped.quota.length > 0 ? (
          <PoolGroup
            icon={Wallet}
            title='官方 GPT 专属额度'
            hint='直接进入官方 GPT 专属额度钱包，永久有效'
            reduced={reduced}
          >
            {grouped.quota.map((tier) => (
              <TierCell key={tier.name} tier={tier} reduced={reduced} />
            ))}
          </PoolGroup>
        ) : null}

        {grouped.props.length > 0 ? (
          <PoolGroup
            icon={PackageOpen}
            title='道具'
            hint='折扣卡自动抵扣，倍率卡需手动启用'
            reduced={reduced}
          >
            {grouped.props.map((tier) => (
              <TierCell key={tier.name} tier={tier} reduced={reduced} />
            ))}
          </PoolGroup>
        ) : null}
      </div>
    </section>
  )
}

function PoolGroup(props: {
  icon: LucideIcon
  title: string
  hint: string
  reduced: boolean
  children: React.ReactNode
}) {
  return (
    <div>
      <div className='flex flex-wrap items-baseline gap-x-2 gap-y-0.5'>
        <span className='text-foreground inline-flex items-center gap-1.5 text-sm font-semibold'>
          <props.icon
            className='text-muted-foreground size-3.5'
            aria-hidden='true'
          />
          {props.title}
        </span>
        <span className='text-muted-foreground text-xs'>{props.hint}</span>
      </div>
      <motion.div
        className='mt-2.5 grid gap-2 sm:grid-cols-2 xl:grid-cols-3'
        variants={GRID}
        initial='initial'
        animate='animate'
      >
        {props.children}
      </motion.div>
    </div>
  )
}

function TierCell(props: { tier: BlindBoxTier; reduced: boolean }) {
  return (
    <PoolCell
      label={formatTierAmount(props.tier)}
      probability={props.tier.probability}
      rarity={classifyTier(props.tier)}
      note={props.tier.name}
      reduced={props.reduced}
    />
  )
}

function PoolCell(props: {
  label: string
  probability: number
  rarity: keyof typeof RARITY_RING
  note?: string
  reduced: boolean
}) {
  const badge = RARITY_BADGE[props.rarity]

  return (
    <motion.div
      variants={props.reduced ? REDUCED_CELL : CELL}
      whileHover={props.reduced ? undefined : { y: -3 }}
      transition={{ duration: 0.2, ease: EASE_OUT_QUINT }}
      className={cn('rounded-xl border p-3', RARITY_RING[props.rarity])}
    >
      <div className='flex items-start justify-between gap-2'>
        <div className='text-foreground min-w-0 text-sm font-medium'>
          {props.label}
        </div>
        {badge ? (
          <span
            className={cn(
              'shrink-0 rounded-full border px-2 py-0.5 text-[10px] font-semibold',
              badge.cls
            )}
          >
            {badge.label}
          </span>
        ) : null}
      </div>
      {props.note && props.note !== props.label ? (
        <div className='text-muted-foreground mt-1 truncate text-[11px]'>
          {props.note}
        </div>
      ) : null}
      <div className='mt-2.5 flex items-center gap-2'>
        <div className='bg-muted h-1 flex-1 overflow-hidden rounded-full'>
          <motion.div
            className='bg-primary/70 h-full rounded-full'
            initial={props.reduced ? false : { width: 0 }}
            animate={{
              width: `${Math.min(100, Math.max(3, props.probability * 100))}%`,
            }}
            transition={{ duration: 0.6, ease: EASE_OUT_QUINT }}
          />
        </div>
        <span className='text-muted-foreground shrink-0 font-mono text-xs font-medium tabular-nums'>
          {formatProbability(props.probability)}
        </span>
      </div>
    </motion.div>
  )
}

function formatProbability(probability: number) {
  if (probability <= 0) return '0%'
  const percentage = probability * 100
  if (percentage < 0.001) return '<0.001%'
  return `${percentage.toFixed(3).replace(/0+$/, '').replace(/\.$/, '')}%`
}
