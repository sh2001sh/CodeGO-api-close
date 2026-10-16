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
import i18n from '@/i18n/config'
import { CheckCircle2, ChevronDown, ShieldCheck, Zap } from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { useTranslation, Trans } from 'react-i18next'
import { cn } from '@/lib/utils'
import type { BalanceBlindBoxOverview } from '../types'

const EASE_OUT_QUINT = [0.22, 1, 0.36, 1] as const

export function BlindBoxPityTrack(props: {
  balance?: BalanceBlindBoxOverview
}) {
  useTranslation()
  const balance = props.balance
  if (!balance) return null
  const firstEligible = Boolean(
    balance?.first_draw_eligible && balance.first_draw_guarantee_usd > 0
  )

  return (
    <section className='overflow-hidden'>
      <GuaranteeHeader firstEligible={firstEligible} />

      <div className='grid gap-6 sm:grid-cols-[0.88fr_1.12fr]'>
        <FirstPurchaseGuarantee
          rewardMin={balance?.first_draw_reward_min_usd || 0}
          rewardMax={balance?.first_draw_reward_max_usd || 0}
        />
        <div className='space-y-6'>
          <GuaranteeProgress
            label={i18n.t('小保底')}
            progress={balance?.small_pity_progress || 0}
            threshold={balance?.small_pity_threshold || 0}
            rewardMin={balance?.small_pity_reward_min_usd || 0}
            rewardMax={balance?.small_pity_reward_max_usd || 0}
          />
          <GuaranteeProgress
            label={i18n.t('大保底')}
            progress={balance?.pity_progress || 0}
            threshold={balance?.pity_threshold || 0}
            rewardMin={balance?.pity_reward_min_usd || 0}
            rewardMax={balance?.pity_reward_max_usd || 0}
          />
        </div>
      </div>
    </section>
  )
}

function GuaranteeHeader(props: { firstEligible: boolean }) {
  useTranslation()
  return (
    <div className='mb-6 flex flex-wrap items-start justify-between gap-4'>
      <div>
        <div className='flex items-center gap-2'>
          <ShieldCheck className='text-primary size-4' aria-hidden='true' />
          <h3 className='text-foreground text-sm font-semibold'>
            <Trans i18nKey={'首购与保底'} />
          </h3>
        </div>
      </div>
      <span
        className={cn(
          'inline-flex shrink-0 items-center gap-1 rounded-md px-2 py-1 text-sm font-medium',
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
        {props.firstEligible
          ? i18n.t('首购权益待使用')
          : i18n.t('首购权益已使用')}
      </span>
    </div>
  )
}

function FirstPurchaseGuarantee(props: {
  rewardMin: number
  rewardMax: number
}) {
  useTranslation()
  return (
    <div>
      <div className='text-muted-foreground text-xs'>
        <Trans i18nKey={'首购首抽保底'} />
      </div>
      <div className='text-foreground mt-1 text-lg font-semibold tabular-nums'>
        {formatRewardRange(props.rewardMin, props.rewardMax)}{' '}
        <Trans i18nKey={'通用额度'} />
      </div>
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
  useTranslation()
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
          <span className='text-muted-foreground ml-1.5 text-xs'>
            <Trans i18nKey={'第'} /> {threshold} <Trans i18nKey={'抽内'} />
          </span>
        </div>
        <span className='text-foreground text-xs font-semibold tabular-nums'>
          {ready
            ? i18n.t('下一抽触发')
            : i18n.t('已累计 {{param0}} 次', { param0: progress })}
        </span>
      </div>
      <div className='bg-background/65 mt-2 h-1.5 overflow-hidden rounded-full'>
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
      <div className='text-muted-foreground mt-2 text-sm tabular-nums'>
        <Trans i18nKey={'保底额度'} />{' '}
        {formatRewardRange(props.rewardMin, props.rewardMax)} credits
      </div>
    </div>
  )
}

function formatRewardRange(minimum: number, maximum: number) {
  return minimum === maximum
    ? minimum.toFixed(2)
    : `${minimum.toFixed(2)}–${maximum.toFixed(2)}`
}

export function BlindBoxPropRules() {
  useTranslation()

  const rules = [
    {
      title: i18n.t('再来一抽'),
      detail: i18n.t(
        '揭晓后立即补发 1 个待开启盲盒，不占每日购买数量，可重复抽中'
      ),
    },
    {
      title: i18n.t('九折充值卡'),
      detail: i18n.t(
        '低概率权益；下一笔套餐购买或余额充值自动享受九折，仅使用一次'
      ),
    },
  ]

  return (
    <details className='blind-box-rules group'>
      <summary className='flex min-h-14 cursor-pointer list-none items-center justify-between gap-3'>
        <span className='text-foreground text-base font-semibold'>
          <Trans i18nKey={'道具生效规则'} />
        </span>
        <ChevronDown
          className='size-4 transition-transform group-open:rotate-180'
          aria-hidden
        />
      </summary>
      <div className='pb-4'>
        {rules.map((rule) => (
          <div
            key={rule.title}
            className='border-border/60 grid gap-x-8 gap-y-0.5 border-b py-2.5 last:border-b-0 sm:grid-cols-[minmax(0,220px)_minmax(0,1fr)]'
          >
            <div className='text-foreground text-[13px] font-medium'>
              {rule.title}
            </div>
            <div className='text-muted-foreground text-xs leading-5'>
              {rule.detail}
            </div>
          </div>
        ))}
      </div>
    </details>
  )
}
