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
import { Clock, Gauge, ShieldCheck, Zap } from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { cn } from '@/lib/utils'
import type { BlindBoxSelfData } from '../types'

const EASE_OUT_QUINT = [0.22, 1, 0.36, 1] as const

export function BlindBoxPityTrack(props: {
  firstPurchaseEligible: boolean
  firstPurchaseUsd: number
  pityProgress: number
  pityThreshold: number
  remainingPity: number
  pityGuaranteeUsd: number
}) {
  const reduced = Boolean(useReducedMotion())
  const pct =
    props.pityThreshold > 0
      ? Math.min(100, (props.pityProgress / props.pityThreshold) * 100)
      : 0
  const ready = props.remainingPity <= 0

  return (
    <section className='app-subtle-panel p-4'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex items-center gap-2'>
          <ShieldCheck className='text-primary size-4' aria-hidden='true' />
          <span className='text-foreground text-sm font-semibold'>
            保底进度
          </span>
          {props.firstPurchaseEligible ? (
            <span className='border-primary/30 bg-primary/10 text-primary inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[10px] font-semibold'>
              <Zap className='size-2.5' aria-hidden='true' />
              首购保底 ${props.firstPurchaseUsd.toFixed(0)}
            </span>
          ) : null}
        </div>
        <span className='text-foreground text-sm font-semibold tabular-nums'>
          {props.pityProgress}
          <span className='text-muted-foreground font-normal'>
            {' '}
            / {props.pityThreshold}
          </span>
        </span>
      </div>

      <div className='bg-muted mt-3 h-2 overflow-hidden rounded-full'>
        <motion.div
          className={cn(
            'h-full rounded-full',
            ready ? 'bg-success' : 'bg-primary'
          )}
          initial={reduced ? false : { width: 0 }}
          animate={{ width: `${pct}%` }}
          transition={{ duration: 0.7, ease: EASE_OUT_QUINT }}
        />
      </div>

      <p className='text-muted-foreground mt-2.5 text-xs leading-5'>
        {props.firstPurchaseEligible
          ? `首次购买盲盒后，首抽普通额度最低保底 $${props.firstPurchaseUsd.toFixed(0)}。`
          : ready
            ? `下次抽取必定触发保底，按 $${props.pityGuaranteeUsd.toFixed(0)} 档位及以上发放。`
            : `连续 ${props.pityThreshold} 次未开出高价值奖励时触发保底，再抽 ${props.remainingPity} 次达成；保底按 $${props.pityGuaranteeUsd.toFixed(0)} 档位及以上发放。`}
      </p>
    </section>
  )
}

export function BlindBoxZeroHourCard(props: { data: BlindBoxSelfData | null }) {
  const reduced = Boolean(useReducedMotion())
  const zeroHour = props.data?.zero_hour
  if (!zeroHour) return null

  const pointCap = zeroHour.point_cap || 0
  const pct =
    pointCap > 0 ? Math.min(100, (zeroHour.points / pointCap) * 100) : 0

  return (
    <section
      className={cn(
        'overflow-hidden rounded-2xl border',
        zeroHour.active
          ? 'border-success/35 bg-success/[0.05]'
          : 'border-amber-500/30 bg-amber-500/[0.05]'
      )}
    >
      <div className='flex flex-wrap items-start justify-between gap-3 px-4 py-3.5'>
        <div className='flex min-w-0 items-start gap-3'>
          <motion.span
            className='flex size-9 shrink-0 items-center justify-center rounded-lg bg-amber-500/15 text-amber-600 dark:text-amber-400'
            animate={reduced ? undefined : { scale: [1, 1.09, 1] }}
            transition={{ duration: 2.6, repeat: Infinity, ease: 'easeInOut' }}
          >
            <Gauge className='size-4' aria-hidden='true' />
          </motion.span>
          <div className='min-w-0'>
            <h3 className='text-foreground text-sm font-semibold'>
              隐藏道具：1 小时 0 倍率卡
            </h3>
            <p className='text-muted-foreground mt-0.5 text-xs leading-5'>
              抽中并启用后 1 小时内，default 分组的非生图模型按 0 倍率计费
            </p>
          </div>
        </div>
        {zeroHour.active ? (
          <span className='border-success/30 bg-success/10 text-success shrink-0 rounded-full border px-2 py-0.5 text-[10px] font-semibold'>
            生效中
          </span>
        ) : null}
      </div>

      <div className='space-y-2.5 px-4 pb-4'>
        <div>
          <div className='flex items-baseline justify-between gap-2'>
            <span className='text-muted-foreground text-xs'>累积进度</span>
            <span className='text-foreground font-mono text-xs font-medium tabular-nums'>
              {zeroHour.points} / {pointCap}
            </span>
          </div>
          <div className='bg-muted mt-1.5 h-1.5 overflow-hidden rounded-full'>
            <motion.div
              className='h-full rounded-full bg-amber-500'
              initial={reduced ? false : { width: 0 }}
              animate={{ width: `${pct}%` }}
              transition={{ duration: 0.7, ease: EASE_OUT_QUINT }}
            />
          </div>
        </div>

        <div className='grid grid-cols-2 gap-2'>
          <StatTile
            label='当前概率'
            value={`${(zeroHour.current_probability * 100).toFixed(3)}%`}
          />
          <StatTile
            label='概率上限'
            value={`${(zeroHour.max_probability * 100).toFixed(3)}%`}
          />
        </div>

        <ul className='text-muted-foreground space-y-1 text-xs leading-5'>
          <li>每成功结算 $1 增加 1 点，每个实际支付的盲盒增加 5 点。</li>
          <li>点数越高概率越高，抽中后进度归零重新累积。</li>
          <li>
            启用后使用 zero-hour 分组，仅限本人，单用户并发最多 5
            个请求，到期后分组自动隐藏。
          </li>
        </ul>
      </div>
    </section>
  )
}

export function BlindBoxPropRules() {
  const rules = [
    { title: '充值九折卡', detail: '下次充值自动抵扣一次，仅生效 1 次' },
    { title: '套餐九折卡', detail: '下次购买套餐自动抵扣一次，仅生效 1 次' },
    { title: '0.95 倍率卡', detail: '在本页点击使用后生效，持续 24 小时' },
    { title: '0.9 倍率卡', detail: '在本页点击使用后生效，持续 24 小时' },
    {
      title: '1 小时 0 倍率卡',
      detail: '在「我的道具」启用，使用 zero-hour 分组，持续 1 小时',
    },
  ]

  return (
    <section className='app-subtle-panel p-4'>
      <div className='flex items-center gap-2'>
        <Clock className='text-muted-foreground size-4' aria-hidden='true' />
        <h3 className='text-foreground text-sm font-semibold'>道具生效规则</h3>
      </div>
      <div className='mt-3 grid gap-2 sm:grid-cols-2'>
        {rules.map((rule) => (
          <div
            key={rule.title}
            className='border-border/70 bg-background/60 rounded-lg border px-3 py-2'
          >
            <div className='text-foreground text-xs font-medium'>
              {rule.title}
            </div>
            <div className='text-muted-foreground mt-0.5 text-[11px] leading-5'>
              {rule.detail}
            </div>
          </div>
        ))}
      </div>
    </section>
  )
}

function StatTile(props: { label: string; value: string }) {
  return (
    <div className='border-border/70 bg-background/60 rounded-lg border px-3 py-2'>
      <div className='text-muted-foreground text-[11px]'>{props.label}</div>
      <div className='text-foreground mt-0.5 font-mono text-sm font-semibold tabular-nums'>
        {props.value}
      </div>
    </div>
  )
}
