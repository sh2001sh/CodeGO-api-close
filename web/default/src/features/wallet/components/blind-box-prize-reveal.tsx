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
import { CalendarClock, Hash, Sparkles, Star } from 'lucide-react'
import {
  AnimatePresence,
  motion,
  useReducedMotion,
  type Variants,
} from 'motion/react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  RARITY_BADGE,
  RARITY_RING,
  type RewardRarity,
} from '../lib/blind-box-rarity'
import type { BlindBoxRecord } from '../types'

const EASE_OUT_QUINT = [0.22, 1, 0.36, 1] as const

/**
 * Reward rarity drives reveal drama: legendary rewards (subscription / pity /
 * high-value) get glow + scale punch, common rewards get a quiet entrance.
 */
export function classifyReward(record: BlindBoxRecord): RewardRarity {
  if (record.reward_type === 'subscription') return 'legendary'
  if (record.is_pity) return 'legendary'
  if (record.reward_type === 'claude_quota') {
    return record.reward_usd >= 2 ? 'epic' : 'common'
  }
  if (record.reward_type === 'quota') {
    return record.reward_usd >= 30 ? 'epic' : 'common'
  }
  return 'common'
}

function highestRarity(records: BlindBoxRecord[]): RewardRarity {
  if (records.some((r) => classifyReward(r) === 'legendary')) return 'legendary'
  if (records.some((r) => classifyReward(r) === 'epic')) return 'epic'
  return 'common'
}

const REVEAL_CONTAINER: Variants = {
  initial: {},
  animate: { transition: { staggerChildren: 0.12, delayChildren: 0.08 } },
}

const REVEAL_ITEM: Variants = {
  initial: { opacity: 0, y: 18, scale: 0.94 },
  animate: {
    opacity: 1,
    y: 0,
    scale: 1,
    transition: { duration: 0.42, ease: EASE_OUT_QUINT },
  },
}

const REDUCED_CONTAINER: Variants = {
  initial: {},
  animate: { transition: { staggerChildren: 0 } },
}

const REDUCED_ITEM: Variants = {
  initial: { opacity: 0 },
  animate: { opacity: 1, transition: { duration: 0.18 } },
}

function rewardTypeLabel(record: BlindBoxRecord) {
  if (record.reward_type === 'subscription') return '套餐'
  if (record.reward_type === 'claude_quota') return 'Claude'
  if (record.reward_type === 'prop') return '道具'
  return '额度'
}

function isManualUseProp(record: BlindBoxRecord) {
  return ['consume_discount_95', 'consume_discount_90'].includes(
    record.prop_type || ''
  )
}

export function PrizeRevealHeader(props: {
  summary: string
  openCount: number
  records: BlindBoxRecord[]
}) {
  const reduced = useReducedMotion()
  const rarity = highestRarity(props.records)
  const celebratory = rarity === 'legendary'

  return (
    <motion.div
      initial={reduced ? { opacity: 0 } : { opacity: 0, y: 10, scale: 0.97 }}
      animate={{ opacity: 1, y: 0, scale: 1 }}
      transition={{ duration: reduced ? 0.18 : 0.4, ease: EASE_OUT_QUINT }}
      className={cn(
        'relative overflow-hidden rounded-xl border p-4',
        celebratory ? 'border-primary/40 bg-primary/8' : 'app-subtle-panel'
      )}
    >
      {celebratory && !reduced ? (
        <motion.div
          aria-hidden
          className='text-primary/30 pointer-events-none absolute -top-6 -right-6'
          initial={{ opacity: 0, rotate: -20, scale: 0.6 }}
          animate={{ opacity: 1, rotate: 0, scale: 1 }}
          transition={{ duration: 0.6, ease: EASE_OUT_QUINT, delay: 0.1 }}
        >
          <Sparkles className='size-24' />
        </motion.div>
      ) : null}
      <div className='relative'>
        <div className='flex items-center gap-2'>
          {celebratory ? (
            <Star className='fill-primary text-primary size-5 shrink-0' />
          ) : null}
          <div className='text-foreground text-lg font-semibold'>
            {celebratory ? `恭喜！${props.summary}` : props.summary}
          </div>
        </div>
        <div className='text-muted-foreground mt-1 text-sm'>
          共抽取 {props.openCount} 次，奖励已到账
        </div>
      </div>
    </motion.div>
  )
}

export function PrizeRevealList(props: {
  records: BlindBoxRecord[]
  onUseReward?: (record: BlindBoxRecord) => void
  formatTimestamp: (timestamp?: number) => string
}) {
  const reduced = useReducedMotion()
  const variants = reduced
    ? REDUCED_CONTAINER
    : {
        ...REVEAL_CONTAINER,
        animate: {
          transition: {
            staggerChildren: props.records.length > 20 ? 0.015 : 0.12,
            delayChildren: 0.08,
          },
        },
      }

  return (
    <motion.div
      className='grid gap-3'
      variants={variants}
      initial='initial'
      animate='animate'
    >
      <AnimatePresence>
        {props.records.map((record) => (
          <PrizeRevealCard
            key={record.id}
            record={record}
            reduced={!!reduced}
            onUseReward={props.onUseReward}
            formatTimestamp={props.formatTimestamp}
          />
        ))}
      </AnimatePresence>
    </motion.div>
  )
}

function PrizeRevealCard(props: {
  record: BlindBoxRecord
  reduced: boolean
  onUseReward?: (record: BlindBoxRecord) => void
  formatTimestamp: (timestamp?: number) => string
}) {
  const { record } = props
  const rarity = classifyReward(record)
  const badge = RARITY_BADGE[rarity]
  const manualUseProp = record.reward_type === 'prop' && isManualUseProp(record)
  const propActive =
    manualUseProp &&
    (record.prop_status === 'active' || record.prop_status === 'used')
  const propAvailable = manualUseProp && record.prop_status === 'available'

  return (
    <motion.div
      variants={props.reduced ? REDUCED_ITEM : REVEAL_ITEM}
      className={cn('relative rounded-xl border p-4', RARITY_RING[rarity])}
    >
      <div className='flex flex-wrap items-start justify-between gap-3'>
        <div className='min-w-0 flex-1'>
          <div className='flex flex-wrap items-center gap-2'>
            <div className='text-foreground text-base font-semibold'>
              {record.reward_title}
            </div>
            <div className='border-border/70 bg-background/60 text-muted-foreground rounded-full border px-2.5 py-0.5 text-xs font-medium'>
              {record.pool_type === 'balance_15' ? '余额盲盒' : '普通盲盒'} ·{' '}
              {rewardTypeLabel(record)}
            </div>
            {badge ? (
              <div
                className={cn(
                  'rounded-full border px-2.5 py-0.5 text-xs font-medium',
                  badge.cls
                )}
              >
                {badge.label}
              </div>
            ) : null}
            {record.is_pity ? (
              <div className='border-primary/30 bg-primary/10 text-primary rounded-full border px-2.5 py-0.5 text-xs font-medium'>
                保底
              </div>
            ) : null}
          </div>
          <div className='text-muted-foreground mt-1.5 text-xs'>
            {props.formatTimestamp(record.create_time)}
          </div>
        </div>
        {manualUseProp && props.onUseReward ? (
          <Button
            type='button'
            size='sm'
            variant={propActive ? 'secondary' : 'default'}
            onClick={() => props.onUseReward?.(record)}
            disabled={!propAvailable}
          >
            {propActive ? '已启用' : propAvailable ? '立即使用' : '不可用'}
          </Button>
        ) : null}
      </div>
      {record.reward_type === 'prop' ? (
        <div className='text-muted-foreground mt-3 text-xs leading-5'>
          {manualUseProp
            ? propActive
              ? '已启用，持续 24 小时自动生效'
              : propAvailable
                ? '点击立即使用后生效，持续 24 小时'
                : '该道具已失效'
            : record.prop_status === 'used'
              ? '已用于最近一次符合条件的订单'
              : record.prop_status === 'reserved'
                ? '已锁定到待支付订单，支付完成后自动使用'
                : '下次满足条件时自动抵扣一次'}
        </div>
      ) : record.reward_type === 'claude_quota' ? (
        <div className='text-muted-foreground mt-3 text-xs leading-5'>
          已进入 Claude 钱包，永久有效
        </div>
      ) : record.reward_type === 'quota' ? (
        <div className='text-muted-foreground mt-3 text-xs leading-5'>
          已进入可用余额，永久有效
        </div>
      ) : null}
      {record.lucky_number ? (
        <motion.div
          initial={
            props.reduced ? { opacity: 0 } : { opacity: 0, scale: 0.96, y: 6 }
          }
          animate={{ opacity: 1, scale: 1, y: 0 }}
          transition={{
            duration: props.reduced ? 0.15 : 0.32,
            ease: EASE_OUT_QUINT,
            delay: props.reduced ? 0 : 0.12,
          }}
          className='border-primary/25 bg-primary/[0.055] mt-3 flex flex-col gap-2 rounded-lg border px-3 py-2.5 sm:flex-row sm:items-center sm:justify-between'
        >
          <div className='flex items-center gap-2'>
            <Hash className='text-primary size-4' aria-hidden='true' />
            <span className='text-muted-foreground text-xs'>今日幸运号</span>
            <span className='text-foreground font-mono text-lg font-semibold tracking-widest tabular-nums'>
              {record.lucky_number}
            </span>
          </div>
          <div className='text-muted-foreground flex items-center gap-1.5 text-xs'>
            <CalendarClock className='size-3.5' aria-hidden='true' />
            仅参与 {record.lucky_draw_date || '今日'} 开奖，次日失效
          </div>
        </motion.div>
      ) : null}
    </motion.div>
  )
}
