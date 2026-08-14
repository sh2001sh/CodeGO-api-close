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
import { BalanceBlindBoxPanel } from './balance-blind-box-panel'
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
  onModeChange: (mode: 'standard' | 'balance') => void
  onRefresh: () => Promise<void>
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
            onRefresh={props.onRefresh}
          />
          <BlindBoxPoolShowcase
            data={props.data}
            tiers={props.data?.balance_blind_box?.tiers || []}
            title='余额盲盒奖池'
            description='购买时按下列公开概率封存一项奖励，可持有或转赠；开启不产生每日幸运号'
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
