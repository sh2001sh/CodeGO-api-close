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

import { motion, useReducedMotion } from 'motion/react'
import { cn } from '@/lib/utils'
import { RARITY_BADGE, classifyTier, formatTierAmount, groupTiersByRewardType, type RARITY_RING } from '../lib/blind-box-rarity'
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
  const maxProbability = Math.max(
    hiddenProbability,
    ...(props.tiers || props.data?.tiers || []).map((tier) => tier.probability),
    0.0001
  )

  return (
    <section className='codego-panel overflow-hidden'>
      <div className='flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3 sm:px-5'>
        <div className='flex items-center gap-2.5'>
          <span aria-hidden className='bg-primary block h-3 w-[3px]' />
          <h2 className='text-foreground text-[13px] font-semibold'>
            {props.title || '奖池一览'}
          </h2>
        </div>
        <span className='codego-stat-label'>概率已公开</span>
      </div>

      <div className='px-4 py-2 sm:px-5'>
        {!props.hideSubscription ? (
          <PoolGroup title='隐藏款'>
            <PoolRow
              label={hiddenTitle}
              probability={hiddenProbability}
              maxProbability={maxProbability}
              rarity='legendary'
              reduced={reduced}
            />
          </PoolGroup>
        ) : null}

        {grouped.credit.length > 0 ? (
          <PoolGroup title='通用额度'>
            {grouped.credit.map((tier) => (
              <TierRow
                key={tier.name}
                tier={tier}
                maxProbability={maxProbability}
                reduced={reduced}
              />
            ))}
          </PoolGroup>
        ) : null}

        {grouped.props.length > 0 ? (
          <PoolGroup title='道具'>
            {grouped.props.map((tier) => (
              <TierRow
                key={tier.name}
                tier={tier}
                maxProbability={maxProbability}
                reduced={reduced}
              />
            ))}
          </PoolGroup>
        ) : null}
      </div>
    </section>
  )
}

function PoolGroup(props: { title: string; children: React.ReactNode }) {
  return (
    <div className='border-border/60 border-b py-4 last:border-b-0'>
      <span className='codego-kicker'>{props.title}</span>
      <motion.div
        className='mt-2'
        variants={GRID}
        initial='initial'
        animate='animate'
      >
        {props.children}
      </motion.div>
    </div>
  )
}

function TierRow(props: { tier: BlindBoxTier; maxProbability: number; reduced: boolean }) {
  return (
    <PoolRow
      label={formatTierAmount(props.tier)}
      probability={props.tier.probability}
      maxProbability={props.maxProbability}
      rarity={classifyTier(props.tier)}
      note={props.tier.name}
      reduced={props.reduced}
    />
  )
}

function PoolRow(props: {
  label: string
  probability: number
  maxProbability: number
  rarity: keyof typeof RARITY_RING
  note?: string
  reduced: boolean
}) {
  const badge = RARITY_BADGE[props.rarity]
  const barWidth = Math.max(
    2,
    Math.min(100, (props.probability / props.maxProbability) * 100)
  )

  return (
    <motion.div
      variants={props.reduced ? REDUCED_CELL : CELL}
      className='grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-5 border-b border-border/50 py-2.5 last:border-b-0'
    >
      <div className='flex min-w-0 items-center gap-2.5'>
        <span
          className={cn(
            'block h-[2px] shrink-0 bg-primary',
            props.rarity === 'legendary'
              ? 'w-8'
              : props.rarity === 'epic'
                ? 'w-5 opacity-70'
                : 'w-2.5 opacity-35'
          )}
          aria-hidden
        />
        <div className='min-w-0'>
          <div className='text-foreground truncate text-[13px] font-medium'>
            {props.label}
          </div>
          {props.note && props.note !== props.label ? (
            <div className='text-muted-foreground/70 mt-0.5 truncate font-mono text-[10px] uppercase'>
              {props.note}
            </div>
          ) : null}
        </div>
      </div>
      <div className='flex shrink-0 items-center gap-4'>
        {badge ? (
          <span
            className={cn(
              'border px-1.5 py-0.5 font-mono text-[9px] font-semibold uppercase',
              badge.cls
            )}
          >
            {badge.label}
          </span>
        ) : null}
        <div className='hidden h-[4px] w-28 overflow-hidden rounded-full bg-muted sm:block'>
          <motion.div
            className={cn(
              'h-full rounded-full',
              props.rarity === 'legendary'
                ? 'bg-warning'
                : props.rarity === 'epic'
                  ? 'bg-primary'
                  : 'bg-foreground/25'
            )}
            initial={props.reduced ? false : { width: 0 }}
            animate={{ width: `${barWidth}%` }}
            transition={{ duration: 0.6, ease: EASE_OUT_QUINT }}
          />
        </div>
        <span className='w-20 text-right font-mono text-xs font-medium text-muted-foreground tabular-nums'>
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
