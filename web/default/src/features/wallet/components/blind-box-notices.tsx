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
import { AlertCircle } from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import type { BlindBoxStatistics } from '../types'

const EASE_OUT_QUINT = [0.22, 1, 0.36, 1] as const

export function BlindBoxDisabledNotice() {
  const reduced = Boolean(useReducedMotion())

  return (
    <motion.div
      className='flex items-start gap-3 rounded-xl border border-amber-500/30 bg-amber-500/[0.06] px-4 py-3.5'
      initial={reduced ? { opacity: 0 } : { opacity: 0, y: -8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.32, ease: EASE_OUT_QUINT }}
    >
      <AlertCircle
        className='mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400'
        aria-hidden='true'
      />
      <div className='min-w-0 text-xs leading-5'>
        <div className='text-foreground text-sm font-semibold'>
          盲盒活动暂未开放
        </div>
        <p className='text-muted-foreground mt-0.5'>
          购买入口已暂停，已支付但未抽取的盲盒不会失效，活动恢复后可继续抽取。
        </p>
      </div>
    </motion.div>
  )
}

function summarizeRewards(
  statistics: BlindBoxStatistics | undefined,
  rewardTypes: string[]
) {
  const entries = (statistics?.rewards || []).filter((reward) =>
    rewardTypes.includes(reward.reward_type)
  )
  const openedCount = entries.reduce(
    (total, entry) => total + entry.opened_count,
    0
  )
  if (openedCount <= 0) return '—'
  if (rewardTypes.some((type) => type === 'quota' || type === 'claude_quota')) {
    const rewardUsd = entries.reduce(
      (total, entry) => total + entry.reward_usd,
      0
    )
    return `${openedCount} 次 · $${rewardUsd.toFixed(2)}`
  }
  return `${openedCount} 次`
}

export function BlindBoxStatsPanel(props: { statistics?: BlindBoxStatistics }) {
  const rows = [
    {
      label: '通用额度',
      value: summarizeRewards(props.statistics, ['quota', 'claude_quota']),
    },
    { label: '道具', value: summarizeRewards(props.statistics, ['prop']) },
    {
      label: '月卡',
      value: summarizeRewards(props.statistics, ['subscription']),
    },
  ]

  return (
    <div className='codego-panel p-4'>
      <div className='flex items-center justify-between gap-2'>
        <div className='flex items-center gap-2.5'>
          <span aria-hidden className='bg-primary block h-3 w-[3px]' />
          <div className='text-foreground text-[13px] font-semibold'>
            我的战绩
          </div>
        </div>
        <span className='codego-stat-label'>
          保底 {props.statistics?.pity_wins || 0}
        </span>
      </div>
      <div className='mt-3 flex items-baseline justify-between gap-3 border-t border-border/60 py-2.5'>
        <span className='codego-stat-label'>累计开启</span>
        <span className='text-foreground text-xl leading-none font-semibold tabular-nums'>
          {props.statistics?.total_opened || 0}
          <span className='text-muted-foreground text-xs font-normal'>
            {' '}
            次
          </span>
        </span>
      </div>
      <dl>
        {rows.map((row) => (
          <div
            key={row.label}
            className='flex items-baseline justify-between gap-3 border-b border-border/60 py-2 last:border-b-0'
          >
            <dt className='codego-stat-label'>{row.label}</dt>
            <dd className='text-foreground font-mono text-xs font-medium tabular-nums'>
              {row.value}
            </dd>
          </div>
        ))}
      </dl>
    </div>
  )
}
