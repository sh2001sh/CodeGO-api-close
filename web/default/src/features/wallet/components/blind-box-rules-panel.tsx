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
import { CheckCircle2, Clock, ShieldCheck, Zap } from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { cn } from '@/lib/utils'
import type { BalanceBlindBoxOverview } from '../types'

const EASE_OUT_QUINT = [0.22, 1, 0.36, 1] as const

export function BlindBoxPityTrack(props: {
  balance?: BalanceBlindBoxOverview
}) {
  const balance = props.balance
  const firstEligible = Boolean(
    balance?.first_draw_eligible && balance.first_draw_guarantee_usd > 0
  )

  return (
    <section className='app-subtle-panel overflow-hidden'>
      <GuaranteeHeader firstEligible={firstEligible} />

      <div className='bg-border/70 grid gap-px sm:grid-cols-[0.88fr_1.12fr]'>
        <FirstPurchaseGuarantee
          eligible={firstEligible}
          rewardMin={balance?.first_draw_reward_min_usd || 0}
          rewardMax={balance?.first_draw_reward_max_usd || 0}
        />
        <div className='bg-background/65 space-y-4 px-4 py-4 sm:px-5'>
          <GuaranteeProgress
            label='小保底'
            progress={balance?.small_pity_progress || 0}
            threshold={balance?.small_pity_threshold || 0}
            rewardMin={balance?.small_pity_reward_min_usd || 0}
            rewardMax={balance?.small_pity_reward_max_usd || 0}
          />
          <GuaranteeProgress
            label='大保底'
            progress={balance?.pity_progress || 0}
            threshold={balance?.pity_threshold || 0}
            rewardMin={balance?.pity_reward_min_usd || 0}
            rewardMax={balance?.pity_reward_max_usd || 0}
          />
        </div>
      </div>

      <p className='text-muted-foreground border-border/70 border-t px-4 py-3 text-[11px] leading-5 sm:px-5'>
        三类保底使用独立的有界奖池，顶级大奖仅来自普通池。奖励达到对应档位后进度自动重置，已入库盲盒的封存结果不会改变。
      </p>
    </section>
  )
}

function GuaranteeHeader(props: { firstEligible: boolean }) {
  return (
    <div className='border-border/70 flex items-start justify-between gap-4 border-b px-4 py-3.5 sm:px-5'>
      <div>
        <div className='flex items-center gap-2'>
          <ShieldCheck className='text-primary size-4' aria-hidden='true' />
          <h3 className='text-foreground text-sm font-semibold'>首购与保底</h3>
        </div>
        <p className='text-muted-foreground mt-1 text-xs leading-5'>
          开启库存时查看保底状态；命中保底的盲盒会在奖励揭晓时明确标注类型。
        </p>
      </div>
      <span
        className={cn(
          'inline-flex shrink-0 items-center gap-1 rounded-md border px-2 py-1 text-[10px] font-semibold',
          props.firstEligible
            ? 'border-primary/30 bg-primary/10 text-primary'
            : 'border-border bg-background/70 text-muted-foreground'
        )}
      >
        {props.firstEligible ? (
          <Zap className='size-3' aria-hidden='true' />
        ) : (
          <CheckCircle2 className='size-3' aria-hidden='true' />
        )}
        {props.firstEligible ? '首购权益待使用' : '首购权益已使用'}
      </span>
    </div>
  )
}

function FirstPurchaseGuarantee(props: {
  eligible: boolean
  rewardMin: number
  rewardMax: number
}) {
  return (
    <div className='bg-background/65 px-4 py-4 sm:px-5'>
      <div className='text-muted-foreground text-[11px]'>首购首抽保底</div>
      <div className='text-foreground mt-1 text-lg font-semibold tabular-nums'>
        {formatRewardRange(props.rewardMin, props.rewardMax)} 通用额度
      </div>
      <p className='text-muted-foreground mt-2 text-xs leading-5'>
        {props.eligible
          ? '首次购买批次的第一个盲盒进入独立首购池，购买多个也只触发一次。'
          : '首购保底已经使用，后续按常规奖池和连续保底规则结算。'}
      </p>
    </div>
  )
}

function GuaranteeProgress(props: {
  label: string
  progress: number
  threshold: number
  rewardMin: number
  rewardMax: number
}) {
  const reduced = Boolean(useReducedMotion())
  const progress = Math.max(0, props.progress)
  const threshold = Math.max(0, props.threshold)
  const missesBeforeGuarantee = Math.max(0, threshold - 1)
  const remainingMisses = Math.max(0, missesBeforeGuarantee - progress)
  const ready = threshold > 0 && remainingMisses === 0
  const pct =
    missesBeforeGuarantee > 0
      ? Math.min(100, (progress / missesBeforeGuarantee) * 100)
      : 100

  return (
    <div>
      <div className='flex items-baseline justify-between gap-3'>
        <div>
          <span className='text-foreground text-xs font-semibold'>
            {props.label}
          </span>
          <span className='text-muted-foreground ml-1.5 text-[10px]'>
            第 {threshold} 抽内
          </span>
        </div>
        <span className='text-foreground text-xs font-semibold tabular-nums'>
          {ready ? '下一抽触发' : `已累计 ${progress} 次`}
        </span>
      </div>
      <div className='bg-muted mt-2 h-1.5 overflow-hidden rounded-full'>
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
      <p className='text-muted-foreground mt-1.5 text-[11px] leading-4'>
        {ready
          ? `${formatRewardRange(props.rewardMin, props.rewardMax)} 通用额度保底已就绪。`
          : `再出现 ${remainingMisses} 次低奖，下一抽进入 ${formatRewardRange(props.rewardMin, props.rewardMax)} 通用额度保底池。`}
      </p>
    </div>
  )
}

function formatRewardRange(minimum: number, maximum: number) {
  return minimum === maximum
    ? minimum.toFixed(2)
    : `${minimum.toFixed(2)}–${maximum.toFixed(2)}`
}

export function BlindBoxPropRules() {
  const rules = [
    {
      title: '充值九折卡',
      detail: '下次统一额度充值自动按九折结算，仅使用一次',
    },
    {
      title: '0.95 倍率卡',
      detail: '仅官方渠道，启用后连续 24 小时按 0.95 倍率扣减',
    },
    {
      title: '0.90 倍率卡',
      detail: '仅官方渠道，启用后连续 24 小时按 0.90 倍率扣减',
    },
    {
      title: '0.10 倍率体验卡',
      detail: '仅官方指定 GPT 分组，累计 15 分钟，可暂停',
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
      <p className='text-muted-foreground mt-3 text-xs leading-5'>
        请求未命中官方渠道时，倍率卡不生效。
      </p>
    </section>
  )
}
