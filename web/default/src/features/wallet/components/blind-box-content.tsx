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
import { motion, useReducedMotion, type Variants } from 'motion/react'
import { Skeleton } from '@/components/ui/skeleton'
import type { BlindBoxSelfData, PaymentMethod } from '../types'
import { BlindBoxDisabledNotice } from './blind-box-notices'
import { BlindBoxPoolShowcase } from './blind-box-pool-showcase'
import {
  BlindBoxPityTrack,
  BlindBoxPropRules,
  BlindBoxZeroHourCard,
} from './blind-box-rules-panel'
import { BlindBoxStage } from './blind-box-stage'

const EASE_OUT_QUINT = [0.22, 1, 0.36, 1] as const

const STACK: Variants = {
  initial: {},
  animate: { transition: { staggerChildren: 0.08, delayChildren: 0.03 } },
}

const STACK_ITEM: Variants = {
  initial: { opacity: 0, y: 14 },
  animate: {
    opacity: 1,
    y: 0,
    transition: { duration: 0.42, ease: EASE_OUT_QUINT },
  },
}

const REDUCED_STACK: Variants = { initial: {}, animate: {} }
const REDUCED_ITEM: Variants = {
  initial: { opacity: 0 },
  animate: { opacity: 1, transition: { duration: 0.18 } },
}

export interface BlindBoxContentProps {
  data: BlindBoxSelfData | null
  loading: boolean
  selectedQuantity: number
  selectedPaymentMethod: PaymentMethod | null
  amountDue: number
  paying: boolean
  openingCount: number | null
  availableBoxes: number
  effectivePityThreshold: number
  pityProgress: number
  remainingPity: number
  onQuantityChange: (value: number) => void
  onPaymentMethodChange: (method: PaymentMethod) => void
  onPay: () => void
  onManualOpen: (count: number) => void
  onOpenProps: () => void
  mode: 'standard' | 'balance'
  balanceOpening: boolean
  onModeChange: (mode: 'standard' | 'balance') => void
  onBalanceOpen: () => void
}

export function BlindBoxContent(props: BlindBoxContentProps) {
  const reduced = Boolean(useReducedMotion())

  if (props.loading && !props.data) {
    return <BlindBoxContentSkeleton />
  }

  return (
    <motion.div
      className='space-y-4'
      variants={reduced ? REDUCED_STACK : STACK}
      initial='initial'
      animate='animate'
    >
      {props.data && !props.data.enabled ? (
        <motion.div variants={reduced ? REDUCED_ITEM : STACK_ITEM}>
          <BlindBoxDisabledNotice />
        </motion.div>
      ) : null}

      <BlindBoxModeSwitch mode={props.mode} onChange={props.onModeChange} />

      {props.mode === 'balance' ? (
        <>
          <BalanceBlindBoxPanel
            data={props.data}
            loading={props.loading}
            opening={props.balanceOpening}
            onOpen={props.onBalanceOpen}
          />
          <BlindBoxPoolShowcase
            data={props.data}
            tiers={props.data?.balance_blind_box?.tiers || []}
            title='余额盲盒奖池'
            description='每次消耗 $15 余额，按下列公开概率获得一项奖励，不产生每日幸运号'
            hideSubscription
          />
        </>
      ) : (
        <>
          <motion.div variants={reduced ? REDUCED_ITEM : STACK_ITEM}>
            <BlindBoxStage
              enabled={Boolean(props.data?.enabled)}
              loading={props.loading}
              unitPrice={props.data?.unit_price || 0}
              countOptions={props.data?.count_options || []}
              quantity={props.selectedQuantity}
              payMethods={props.data?.pay_methods || []}
              paymentMethod={props.selectedPaymentMethod}
              amountDue={props.amountDue}
              paying={props.paying}
              availableBoxes={props.availableBoxes}
              openingCount={props.openingCount}
              propCount={props.data?.props?.length || 0}
              onQuantityChange={props.onQuantityChange}
              onPaymentMethodChange={props.onPaymentMethodChange}
              onPay={props.onPay}
              onManualOpen={props.onManualOpen}
              onOpenProps={props.onOpenProps}
            />
          </motion.div>

          <motion.div variants={reduced ? REDUCED_ITEM : STACK_ITEM}>
            <BlindBoxPityTrack
              firstPurchaseEligible={Boolean(
                props.data?.first_purchase_guarantee_eligible
              )}
              firstPurchaseUsd={props.data?.first_purchase_guarantee_usd || 0}
              pityProgress={props.pityProgress}
              pityThreshold={props.effectivePityThreshold}
              remainingPity={props.remainingPity}
              pityGuaranteeUsd={props.data?.pity_guarantee_usd || 0}
            />
          </motion.div>

          <motion.div variants={reduced ? REDUCED_ITEM : STACK_ITEM}>
            <BlindBoxPoolShowcase data={props.data} />
          </motion.div>

          <motion.div variants={reduced ? REDUCED_ITEM : STACK_ITEM}>
            <BlindBoxZeroHourCard data={props.data} />
          </motion.div>

          <motion.div variants={reduced ? REDUCED_ITEM : STACK_ITEM}>
            <BlindBoxPropRules />
          </motion.div>
        </>
      )}
    </motion.div>
  )
}

function BlindBoxModeSwitch(props: {
  mode: 'standard' | 'balance'
  onChange: (mode: 'standard' | 'balance') => void
}) {
  return (
    <div
      className='bg-muted grid grid-cols-2 rounded-lg p-1'
      role='tablist'
      aria-label='盲盒类型'
    >
      {(
        [
          ['standard', '普通盲盒'],
          ['balance', '余额盲盒'],
        ] as const
      ).map(([value, label]) => (
        <button
          key={value}
          type='button'
          role='tab'
          aria-selected={props.mode === value}
          onClick={() => props.onChange(value)}
          className={`min-h-10 rounded-md px-3 text-sm font-medium transition-colors ${props.mode === value ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}`}
        >
          {label}
        </button>
      ))}
    </div>
  )
}

function BalanceBlindBoxPanel(props: {
  data: BlindBoxSelfData | null
  loading: boolean
  opening: boolean
  onOpen: () => void
}) {
  const balance = props.data?.balance_blind_box
  const canOpen = Boolean(
    balance?.enabled &&
    !props.loading &&
    !props.opening &&
    balance.balance_usd >= balance.price_usd
  )
  const headlineTiers = (balance?.tiers || [])
    .filter((tier) => tier.max_usd >= 80)
    .sort((left, right) => right.max_usd - left.max_usd)
    .slice(0, 4)

  return (
    <section className='app-page-shell overflow-hidden'>
      <div className='border-b border-teal-500/20 bg-teal-500/[0.06] px-4 py-6 sm:px-6'>
        <div className='mx-auto max-w-xl text-center'>
          <div className='text-xs font-semibold text-teal-700 dark:text-teal-300'>
            余额盲盒 · 固定 $15 / 次
          </div>
          <h2 className='text-foreground mt-2 text-xl font-semibold'>
            使用余额抽取高价值奖励
          </h2>
          <p className='text-muted-foreground mt-2 text-xs leading-5'>
            奖池沿用盲盒道具与额度奖励，最高可得
            $1000；本盲盒不会生成每日幸运号。
          </p>
        </div>
      </div>
      <div className='space-y-4 px-4 py-4 sm:px-6 sm:py-5'>
        <div className='grid gap-3 sm:grid-cols-3'>
          <Metric
            label='当前余额'
            value={`$${(balance?.balance_usd || 0).toFixed(2)}`}
          />
          <Metric
            label='抽取后余额'
            value={`$${Math.max(0, (balance?.balance_usd || 0) - (balance?.price_usd || 15)).toFixed(2)}`}
          />
          <Metric
            label='独立保底'
            value={`${balance?.pity_progress || 0}/${balance?.pity_threshold || 50}`}
          />
        </div>
        <div className='rounded-lg border border-teal-500/20 bg-teal-500/[0.04] p-3 text-xs leading-5'>
          连续 49 次未获得 $35 及以上额度，第 50 次至少获得
          $35。道具规则与普通盲盒一致，幸运号规则除外。
        </div>
        {headlineTiers.length > 0 ? (
          <div className='grid grid-cols-2 gap-2 sm:grid-cols-4'>
            {headlineTiers.map((tier) => (
              <div
                key={tier.name}
                className='rounded-lg border border-amber-500/25 bg-amber-500/[0.06] p-3 text-center'
              >
                <div className='text-sm font-semibold text-amber-700 dark:text-amber-300'>
                  {tier.name}
                </div>
                <div className='text-muted-foreground mt-1 text-[11px]'>
                  {(tier.probability * 100).toFixed(3)}%
                </div>
              </div>
            ))}
          </div>
        ) : null}
        <button
          type='button'
          onClick={props.onOpen}
          disabled={!canOpen}
          className='disabled:bg-muted disabled:text-muted-foreground flex min-h-11 w-full items-center justify-center rounded-lg bg-teal-600 px-4 text-sm font-semibold text-white transition-colors hover:bg-teal-700 disabled:cursor-not-allowed'
        >
          {props.opening
            ? '抽取中…'
            : canOpen
              ? '使用 $15 余额抽取'
              : `余额不足，还差 $${Math.max(0, (balance?.price_usd || 15) - (balance?.balance_usd || 0)).toFixed(2)}`}
        </button>
      </div>
    </section>
  )
}

function Metric(props: { label: string; value: string }) {
  return (
    <div className='border-border/70 bg-muted/30 rounded-lg border p-3'>
      <div className='text-muted-foreground text-[11px]'>{props.label}</div>
      <div className='text-foreground mt-1 font-mono text-lg font-semibold tabular-nums'>
        {props.value}
      </div>
    </div>
  )
}

function BlindBoxContentSkeleton() {
  return (
    <div className='space-y-4'>
      <div className='app-page-shell space-y-5 p-6'>
        <div className='flex flex-col items-center gap-4'>
          <Skeleton className='size-32 rounded-2xl' />
          <Skeleton className='h-6 w-48' />
          <Skeleton className='h-4 w-72' />
        </div>
        <Skeleton className='h-9 w-full' />
        <Skeleton className='h-20 w-full rounded-xl' />
      </div>
      <Skeleton className='h-24 w-full rounded-xl' />
      <Skeleton className='h-64 w-full rounded-2xl' />
    </div>
  )
}
