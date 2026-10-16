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
import { useState, useEffect } from 'react'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatUsdAmount } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  formatPaymentAmount,
  getDiscountLabel,
  getPaymentIcon,
  calculatePresetPricing,
  getMinTopupAmount,
} from '../lib'
import type {
  PaymentMethod,
  PresetAmount,
  TopupInfo,
  CreemProduct,
  WaffoPayMethod,
} from '../types'
import { CreemProductsSection } from './creem-products-section'
import { RedemptionCodePanel } from './redemption-code-panel'

interface RechargeFormCardProps {
  topupInfo: TopupInfo | null
  presetAmounts: PresetAmount[]
  selectedPreset: number | null
  onSelectPreset: (preset: PresetAmount) => void
  topupAmount: number
  onTopupAmountChange: (amount: number) => void
  paymentAmount: number
  calculating: boolean
  onPaymentMethodSelect: (method: PaymentMethod) => void
  selectedPaymentMethod?: PaymentMethod
  paymentLoading: string | null
  redemptionCode: string
  onRedemptionCodeChange: (code: string) => void
  onRedeem: () => void
  redeeming: boolean
  topupLink?: string
  loading?: boolean
  usdExchangeRate?: number
  creemProducts?: CreemProduct[]
  enableCreemTopup?: boolean
  onCreemProductSelect?: (product: CreemProduct) => void
  enableWaffoTopup?: boolean
  waffoPayMethods?: WaffoPayMethod[]
  onWaffoMethodSelect?: (method: WaffoPayMethod, index: number) => void
  enableWaffoPancakeTopup?: boolean
  showRedemptionSection?: boolean
}

export function RechargeFormCard(props: RechargeFormCardProps) {
  const { t } = useTranslation()
  const [localAmount, setLocalAmount] = useState(props.topupAmount.toString())
  const effectiveMinTopup = getMinTopupAmount(props.topupInfo)
  useEffect(() => {
    setLocalAmount(props.topupAmount.toString())
  }, [props.topupAmount])
  const handleAmountChange = (value: string) => {
    setLocalAmount(value)
    const numValue = Number(value)
    if (Number.isInteger(numValue) && numValue >= effectiveMinTopup) {
      props.onTopupAmountChange(numValue)
    }
  }
  const hasConfigurableTopup =
    props.topupInfo?.enable_online_topup ||
    props.topupInfo?.enable_stripe_topup ||
    props.enableWaffoTopup ||
    props.enableWaffoPancakeTopup ||
    props.topupInfo?.enable_nowpayments_topup
  const hasAnyTopup = hasConfigurableTopup || props.enableCreemTopup
  const hasStandardPaymentMethods =
    Array.isArray(props.topupInfo?.pay_methods) &&
    props.topupInfo.pay_methods.length > 0
  const hasWaffoPaymentMethods =
    Array.isArray(props.waffoPayMethods) && props.waffoPayMethods.length > 0
  const amountValid =
    Number.isInteger(Number(localAmount)) &&
    Number(localAmount) >= effectiveMinTopup
  const redemptionEnabled = props.topupInfo?.enable_redemption !== false
  const showRedemption = props.showRedemptionSection !== false || !hasAnyTopup

  if (props.loading) {
    return (
      <section
        className='commerce-recharge commerce-recharge-loading'
        aria-label={t('Loading')}
        aria-busy='true'
      >
        <Skeleton className='h-6 w-32' />
        <div className='commerce-recharge-skeleton-presets'>
          {Array.from({ length: 6 }).map((_, index) => (
            <Skeleton key={index} className='h-16 rounded-xl' />
          ))}
        </div>
        <Skeleton className='h-12 w-full' />
        <Skeleton className='h-12 w-full' />
      </section>
    )
  }

  return (
    <div className='commerce-recharge-layout'>
      <section className='commerce-recharge' aria-label={t('Top up')}>
        {hasAnyTopup || !redemptionEnabled ? (
          <header className='commerce-recharge-heading'>
            <h2>{t('Balance top-up')}</h2>
          </header>
        ) : null}
        {hasConfigurableTopup ? (
          <>
            <div className='commerce-recharge-group'>
              <Label htmlFor='topup-amount' className='commerce-recharge-label'>
                {t('Recharge quota (USD)')}
              </Label>
              {props.presetAmounts.length > 0 ? (
                <div
                  className='commerce-presets'
                  role='group'
                  aria-label={t('Recharge quota (USD)')}
                >
                  {props.presetAmounts.map((preset, index) => {
                    const discount = 1.0
                    const {
                      displayValue,
                      actualPrice,
                      savedAmount,
                      hasDiscount,
                    } = calculatePresetPricing(
                      preset.value,
                      1,
                      discount,
                      props.usdExchangeRate ?? 1
                    )
                    return (
                      <Button
                        key={index}
                        variant='outline'
                        className='commerce-preset'
                        onClick={() => props.onSelectPreset(preset)}
                        aria-pressed={props.selectedPreset === preset.value}
                      >
                        <span className='commerce-preset-value'>
                          {formatUsdAmount(displayValue)}
                        </span>
                        {hasDiscount ? (
                          <span className='text-success'>
                            {getDiscountLabel(discount)}
                          </span>
                        ) : null}
                        <span className='commerce-preset-payment'>
                          {t('Pay {{amount}}', {
                            amount: formatPaymentAmount(actualPrice),
                          })}
                          {hasDiscount && savedAmount > 0 ? (
                            <span className='text-success'>
                              {' '}
                              {t('Save {{amount}}', {
                                amount: formatPaymentAmount(savedAmount),
                              })}
                            </span>
                          ) : null}
                        </span>
                      </Button>
                    )
                  })}
                </div>
              ) : null}
              <div className='commerce-amount-entry'>
                <div className='relative'>
                  <Input
                    id='topup-amount'
                    type='number'
                    value={localAmount}
                    onChange={(event) => handleAmountChange(event.target.value)}
                    min={effectiveMinTopup}
                    placeholder={t('Minimum {{amount}}', {
                      amount: `${effectiveMinTopup} credits`,
                    })}
                    className='pr-20 font-mono'
                    aria-invalid={!amountValid}
                  />
                  <span className='commerce-amount-unit'>credits</span>
                </div>
                <dl className='commerce-recharge-total'>
                  <dt>
                    {t('Payment amount ({{currency}})', {
                      currency: props.selectedPaymentMethod?.currency || 'CNY',
                    })}
                  </dt>
                  <dd>
                    {props.calculating ? (
                      <Skeleton className='h-5 w-16' />
                    ) : (
                      formatPaymentAmount(props.paymentAmount)
                    )}
                  </dd>
                </dl>
              </div>
            </div>

            {hasStandardPaymentMethods || !hasWaffoPaymentMethods ? (
              <fieldset className='commerce-recharge-group'>
                <legend className='commerce-recharge-label'>
                  {t('Payment Method')}
                </legend>
                {hasStandardPaymentMethods ? (
                  <div className='commerce-recharge-methods'>
                    {props.topupInfo?.pay_methods?.map((method) => {
                      const minTopup = method.min_topup || effectiveMinTopup
                      const disabled =
                        !amountValid || minTopup > props.topupAmount
                      const button = (
                        <Button
                          key={method.type}
                          variant='outline'
                          onClick={() => props.onPaymentMethodSelect(method)}
                          disabled={disabled || !!props.paymentLoading}
                          className='commerce-payment-method'
                          aria-pressed={
                            props.selectedPaymentMethod?.type === method.type
                          }
                        >
                          {props.paymentLoading === method.type ? (
                            <Loader2 className='size-4 animate-spin' />
                          ) : (
                            getPaymentIcon(
                              method.type,
                              'size-4',
                              method.icon,
                              method.name
                            )
                          )}
                          <span>{method.name}</span>
                        </Button>
                      )
                      return (
                        <div key={method.type} className='min-w-0'>
                          {disabled ? (
                            <TooltipProvider>
                              <Tooltip>
                                <TooltipTrigger render={button} />
                                <TooltipContent>
                                  {t('Minimum topup amount: {{amount}}', {
                                    amount: minTopup,
                                  })}
                                </TooltipContent>
                              </Tooltip>
                            </TooltipProvider>
                          ) : (
                            button
                          )}
                          {method.type === 'nowpayments' ? (
                            <p className='commerce-payment-minimum'>
                              {t('Minimum {{amount}} USDT', {
                                amount: minTopup,
                              })}
                            </p>
                          ) : null}
                        </div>
                      )
                    })}
                  </div>
                ) : (
                  <p role='status' className='commerce-recharge-notice'>
                    {t(
                      'No payment methods available. Please contact administrator.'
                    )}
                  </p>
                )}
              </fieldset>
            ) : null}

            {props.enableWaffoTopup &&
            hasWaffoPaymentMethods &&
            props.onWaffoMethodSelect ? (
              <fieldset className='commerce-recharge-group'>
                <legend className='commerce-recharge-label'>
                  {t('Waffo Payment')}
                </legend>
                <div className='commerce-recharge-methods'>
                  {props.waffoPayMethods?.map((method, index) => {
                    const loadingKey = `waffo-${index}`
                    const waffoMin =
                      props.topupInfo?.waffo_min_topup || effectiveMinTopup
                    const belowMin =
                      !amountValid || waffoMin > props.topupAmount
                    const button = (
                      <Button
                        key={`${method.name}-${index}`}
                        variant='outline'
                        onClick={() =>
                          props.onWaffoMethodSelect?.(method, index)
                        }
                        disabled={belowMin || !!props.paymentLoading}
                        className='commerce-payment-method'
                      >
                        {props.paymentLoading === loadingKey ? (
                          <Loader2 className='size-4 animate-spin' />
                        ) : null}
                        {props.paymentLoading !== loadingKey && method.icon ? (
                          <img
                            src={method.icon}
                            alt=''
                            className='size-4 object-contain'
                          />
                        ) : null}
                        {props.paymentLoading !== loadingKey && !method.icon
                          ? getPaymentIcon('waffo')
                          : null}
                        <span>{method.name}</span>
                      </Button>
                    )
                    return belowMin ? (
                      <TooltipProvider key={`${method.name}-${index}`}>
                        <Tooltip>
                          <TooltipTrigger render={button} />
                          <TooltipContent>
                            {t('Minimum topup amount: {{amount}}', {
                              amount: waffoMin,
                            })}
                          </TooltipContent>
                        </Tooltip>
                      </TooltipProvider>
                    ) : (
                      button
                    )
                  })}
                </div>
              </fieldset>
            ) : null}
          </>
        ) : null}

        {!hasAnyTopup ? (
          <p role='status' className='commerce-recharge-notice'>
            {t('Online top-up unavailable')}
          </p>
        ) : null}

        {props.enableCreemTopup &&
        Array.isArray(props.creemProducts) &&
        props.creemProducts.length > 0 &&
        props.onCreemProductSelect ? (
          <div className='commerce-recharge-group'>
            <h3 className='commerce-recharge-label'>{t('Creem Payment')}</h3>
            <CreemProductsSection
              products={props.creemProducts}
              onProductSelect={props.onCreemProductSelect}
            />
          </div>
        ) : null}
      </section>
      {showRedemption && redemptionEnabled ? (
        <RedemptionCodePanel
          className='commerce-redemption-inline'
          title={t('Redemption code')}
          topupLink={props.topupLink}
          redemptionCode={props.redemptionCode}
          onRedemptionCodeChange={props.onRedemptionCodeChange}
          onRedeem={props.onRedeem}
          redeeming={props.redeeming}
        />
      ) : null}
      {props.showRedemptionSection !== false && !redemptionEnabled ? (
        <p role='status' className='commerce-recharge-notice'>
          {t(
            'Redemption codes are disabled until the administrator confirms compliance terms.'
          )}
        </p>
      ) : null}
    </div>
  )
}
