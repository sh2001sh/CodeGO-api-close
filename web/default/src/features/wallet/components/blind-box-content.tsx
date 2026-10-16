/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useTranslation } from 'react-i18next'
import { Skeleton } from '@/components/ui/skeleton'
import type { BlindBoxSelfData, PaymentMethod } from '../types'
import { BalanceBlindBoxPanel } from './balance-blind-box-panel'
import { BlindBoxDisabledNotice } from './blind-box-notices'
import { BlindBoxPoolShowcase } from './blind-box-pool-showcase'
import { BlindBoxPropRules } from './blind-box-rules-panel'

export interface BlindBoxContentProps {
  data: BlindBoxSelfData | null
  loading: boolean
  selectedQuantity: number
  selectedPaymentMethod: PaymentMethod | null
  amountDue: number | null
  paying: boolean
  onQuantityChange: (value: number) => void
  onPaymentMethodChange: (method: PaymentMethod) => void
  onPay: (count: number) => void
  onOpenProps: () => void
  onRefresh: () => Promise<void>
}

export function BlindBoxContent(props: BlindBoxContentProps) {
  const { t } = useTranslation()
  if (props.loading && !props.data) {
    return (
      <div
        className='blind-box-loading'
        aria-busy='true'
        aria-label={t('加载中')}
      >
        <Skeleton className='h-96 w-full rounded-2xl' />
        <Skeleton className='h-64 w-full rounded-2xl' />
      </div>
    )
  }

  return (
    <div className='blind-box-content'>
      {props.data && !props.data.enabled ? <BlindBoxDisabledNotice /> : null}
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
      <div className='blind-box-rewards'>
        <BlindBoxPoolShowcase
          data={props.data}
          tiers={props.data?.inventory?.tiers || props.data?.tiers || []}
          title={t('统一盲盒奖池')}
          hideSubscription
        />
        <BlindBoxPropRules />
      </div>
    </div>
  )
}
