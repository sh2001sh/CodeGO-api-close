/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { motion, useReducedMotion, type Variants } from 'motion/react'
import { Skeleton } from '@/components/ui/skeleton'
import type { BlindBoxSelfData, PaymentMethod } from '../types'
import { BalanceBlindBoxPanel } from './balance-blind-box-panel'
import { BlindBoxDisabledNotice } from './blind-box-notices'
import { BlindBoxPoolShowcase } from './blind-box-pool-showcase'
import { BlindBoxPropRules } from './blind-box-rules-panel'

const EASE_OUT_QUINT = [0.22, 1, 0.36, 1] as const
const STACK: Variants = {
  initial: {},
  animate: { transition: { staggerChildren: 0.06, delayChildren: 0.02 } },
}
const STACK_ITEM: Variants = {
  initial: { opacity: 0, y: 10 },
  animate: {
    opacity: 1,
    y: 0,
    transition: { duration: 0.32, ease: EASE_OUT_QUINT },
  },
}
const REDUCED_STACK: Variants = { initial: {}, animate: {} }
const REDUCED_ITEM: Variants = { initial: {}, animate: {} }

export interface BlindBoxContentProps {
  data: BlindBoxSelfData | null
  loading: boolean
  selectedQuantity: number
  selectedPaymentMethod: PaymentMethod | null
  amountDue: number
  paying: boolean
  onQuantityChange: (value: number) => void
  onPaymentMethodChange: (method: PaymentMethod) => void
  onPay: (count: number) => void
  onOpenProps: () => void
  onRefresh: () => Promise<void>
}

export function BlindBoxContent(props: BlindBoxContentProps) {
  const reduced = Boolean(useReducedMotion())

  if (props.loading && !props.data) return <BlindBoxContentSkeleton />

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

      <motion.div variants={reduced ? REDUCED_ITEM : STACK_ITEM}>
        <BalanceBlindBoxPanel
          data={props.data}
          loading={props.loading}
          onRefresh={props.onRefresh}
          cashMethods={props.data?.pay_methods || []}
          selectedCashMethod={props.selectedPaymentMethod}
          cashAmountDue={props.amountDue}
          cashPaying={props.paying}
          onCashMethodChange={props.onPaymentMethodChange}
          onCashQuantityChange={props.onQuantityChange}
          onCashPurchase={props.onPay}
          onOpenProps={props.onOpenProps}
        />
      </motion.div>

      <motion.div variants={reduced ? REDUCED_ITEM : STACK_ITEM}>
        <BlindBoxPoolShowcase
          data={props.data}
          tiers={props.data?.inventory?.tiers || props.data?.tiers || []}
          title='统一盲盒奖池'
          description=''
          hideSubscription
        />
      </motion.div>

      <motion.div variants={reduced ? REDUCED_ITEM : STACK_ITEM}>
        <BlindBoxPropRules />
      </motion.div>
    </motion.div>
  )
}

function BlindBoxContentSkeleton() {
  return (
    <div className='space-y-4'>
      <div className='app-page-shell space-y-5 p-6'>
        <div className='flex flex-col items-center gap-4'>
          <Skeleton className='size-32 rounded-xl' />
          <Skeleton className='h-6 w-48' />
          <Skeleton className='h-4 w-72 max-w-full' />
        </div>
        <Skeleton className='h-10 w-full' />
        <Skeleton className='h-28 w-full rounded-lg' />
      </div>
      <Skeleton className='h-64 w-full rounded-xl' />
    </div>
  )
}
