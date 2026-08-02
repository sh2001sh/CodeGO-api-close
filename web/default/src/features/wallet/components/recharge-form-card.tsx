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
import { Gift, ExternalLink, Loader2, Receipt, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatUsdAmount } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'
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
  getMinTopupAmount,
  calculatePresetPricing,
} from '../lib'
import type {
  PaymentMethod,
  PresetAmount,
  TopupInfo,
  CreemProduct,
  WaffoPayMethod,
  WalletType,
} from '../types'
import { CreemProductsSection } from './creem-products-section'

interface RechargeFormCardProps {
  topupInfo: TopupInfo | null
  presetAmounts: PresetAmount[]
  selectedPreset: number | null
  onSelectPreset: (preset: PresetAmount) => void
  selectedWalletType: WalletType
  onWalletTypeChange: (walletType: WalletType) => void
  topupAmount: number
  onTopupAmountChange: (amount: number) => void
  paymentAmount: number
  calculating: boolean
  onPaymentMethodSelect: (method: PaymentMethod) => void
  paymentLoading: string | null
  redemptionCode: string
  onRedemptionCodeChange: (code: string) => void
  onRedeem: () => void
  redeeming: boolean
  topupLink?: string
  loading?: boolean
  priceRatio?: number
  usdExchangeRate?: number
  onOpenBilling?: () => void
  creemProducts?: CreemProduct[]
  enableCreemTopup?: boolean
  onCreemProductSelect?: (product: CreemProduct) => void
  enableWaffoTopup?: boolean
  waffoPayMethods?: WaffoPayMethod[]
  waffoMinTopup?: number
  onWaffoMethodSelect?: (method: WaffoPayMethod, index: number) => void
  enableWaffoPancakeTopup?: boolean
  showRedemptionSection?: boolean
  compact?: boolean
}

export function RechargeFormCard({
  topupInfo,
  presetAmounts,
  selectedPreset,
  onSelectPreset,
  selectedWalletType,
  onWalletTypeChange,
  topupAmount,
  onTopupAmountChange,
  paymentAmount,
  calculating,
  onPaymentMethodSelect,
  paymentLoading,
  redemptionCode,
  onRedemptionCodeChange,
  onRedeem,
  redeeming,
  topupLink,
  loading,
  priceRatio = 1,
  usdExchangeRate = 1,
  onOpenBilling,
  creemProducts,
  enableCreemTopup,
  onCreemProductSelect,
  enableWaffoTopup,
  waffoPayMethods,
  waffoMinTopup,
  onWaffoMethodSelect,
  enableWaffoPancakeTopup,
  showRedemptionSection = true,
  compact = false,
}: RechargeFormCardProps) {
  const { t } = useTranslation()
  const [localAmount, setLocalAmount] = useState(topupAmount.toString())
  const sectionLabelClassName = 'text-muted-foreground text-xs font-medium'
  useEffect(() => {
    setLocalAmount(topupAmount.toString())
  }, [topupAmount])

  const handleAmountChange = (value: string) => {
    setLocalAmount(value)
    const numValue = parseInt(value) || 0
    if (numValue >= 0) {
      onTopupAmountChange(numValue)
    }
  }

  const hasConfigurableTopup =
    topupInfo?.enable_online_topup ||
    topupInfo?.enable_stripe_topup ||
    enableWaffoTopup ||
    enableWaffoPancakeTopup
  const hasAnyTopup = hasConfigurableTopup || enableCreemTopup
  const hasStandardPaymentMethods =
    Array.isArray(topupInfo?.pay_methods) && topupInfo.pay_methods.length > 0
  const hasWaffoPaymentMethods =
    Array.isArray(waffoPayMethods) && waffoPayMethods.length > 0
  const minTopup = getMinTopupAmount(topupInfo)
  const effectiveMinTopup = selectedWalletType === 'claude' ? 1 : minTopup
  const redemptionEnabled = topupInfo?.enable_redemption !== false

  if (loading) {
    return (
      <Card className='gap-0 overflow-hidden py-0'>
        <CardHeader className='border-b p-3 !pb-3 sm:p-5 sm:!pb-5'>
          <Skeleton className='h-6 w-32' />
          <Skeleton className='mt-2 h-4 w-48' />
        </CardHeader>
        <CardContent className='space-y-4 p-3 sm:space-y-6 sm:p-5'>
          <div className='space-y-4 sm:space-y-6'>
            {/* Preset Amounts Skeleton */}
            <div className='space-y-3'>
              <Skeleton className='h-3 w-16' />
              <div className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
                {Array.from({ length: 8 }).map((_, i) => (
                  <Skeleton key={i} className='h-[72px] rounded-lg' />
                ))}
              </div>
            </div>

            {/* Custom Amount Input Skeleton */}
            <div className='space-y-3'>
              <Skeleton className='h-3 w-28' />
              <Skeleton className='h-[42px] w-full' />
            </div>

            {/* Payment Methods Skeleton */}
            <div className='space-y-3'>
              <Skeleton className='h-3 w-32' />
              <div className='flex flex-wrap gap-3'>
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className='h-10 w-24 rounded-lg' />
                ))}
              </div>
            </div>
          </div>

          {showRedemptionSection ? (
            <div className='space-y-3 border-t pt-8'>
              <Skeleton className='h-3 w-24' />
              <div className='flex gap-2'>
                <Skeleton className='h-10 flex-1' />
                <Skeleton className='h-10 w-20' />
              </div>
            </div>
          ) : null}
        </CardContent>
      </Card>
    )
  }

  return (
    <TitledCard
      title={t('Balance top-up')}
      description={t('Choose an amount and complete payment')}
      icon={<WalletCards className='h-4 w-4' />}
      action={
        onOpenBilling ? (
          <Button
            variant='outline'
            size='sm'
            onClick={onOpenBilling}
            className='w-full gap-2 sm:w-auto'
          >
            <Receipt className='h-4 w-4' />
            {t('Billing History')}
          </Button>
        ) : null
      }
      className={compact ? 'rounded-xl' : undefined}
      contentClassName={
        compact ? 'space-y-3 sm:space-y-4' : 'space-y-4 sm:space-y-6'
      }
    >
      {/* Online Topup Section */}
      {hasAnyTopup ? (
        <div
          className={
            compact ? 'space-y-3 sm:space-y-4' : 'space-y-4 sm:space-y-6'
          }
        >
          {hasConfigurableTopup && (
            <>
              <div className='space-y-2.5 sm:space-y-3'>
                <Label className={sectionLabelClassName}>
                  {t('Recharge wallet')}
                </Label>
                <div className='grid grid-cols-1 gap-2 sm:grid-cols-2'>
                  {[
                    {
                      value: 'default' as const,
                      title: t('Standard wallet'),
                      description: t(
                        'For non-Claude models, existing discounts and group rates apply.'
                      ),
                    },
                    {
                      value: 'claude' as const,
                      title: t('Claude quota'),
                      description: t(
                        'Only for Claude models; minimum 1 with fixed 1:1 recharge.'
                      ),
                    },
                  ].map((item) => (
                    <Button
                      key={item.value}
                      type='button'
                      variant='outline'
                      onClick={() => onWalletTypeChange(item.value)}
                      className={cn(
                        compact
                          ? 'h-auto min-h-16 flex-col items-start justify-start gap-1 rounded-xl px-3 py-2.5 text-left whitespace-normal'
                          : 'h-auto min-h-20 flex-col items-start justify-start gap-1 rounded-xl p-3 text-left whitespace-normal',
                        selectedWalletType === item.value
                          ? 'border-foreground bg-foreground/5'
                          : 'border-muted'
                      )}
                    >
                      <span className='text-sm font-semibold'>
                        {item.title}
                      </span>
                      <span
                        className={cn(
                          'text-muted-foreground text-xs leading-relaxed',
                          compact && 'line-clamp-2'
                        )}
                      >
                        {item.description}
                      </span>
                    </Button>
                  ))}
                </div>
              </div>

              {presetAmounts.length > 0 && (
                <div className='space-y-2.5 sm:space-y-3'>
                  <Label className={sectionLabelClassName}>
                    {t('Recharge quota (USD)')}
                  </Label>
                  <div
                    className={cn(
                      'grid gap-1.5 sm:gap-3',
                      compact
                        ? 'grid-cols-2 md:grid-cols-3 xl:grid-cols-5'
                        : 'grid-cols-2 md:grid-cols-4'
                    )}
                  >
                    {presetAmounts.map((preset, index) => {
                      const amountDiscount =
                        selectedWalletType === 'claude'
                          ? 1.0
                          : preset.discount ||
                            topupInfo?.discount?.[preset.value] ||
                            1.0
                      const discount = amountDiscount
                      const effectivePriceRatio =
                        selectedWalletType === 'claude' ? 1 : priceRatio
                      const {
                        displayValue,
                        actualPrice,
                        savedAmount,
                        hasDiscount,
                      } = calculatePresetPricing(
                        preset.value,
                        effectivePriceRatio,
                        discount,
                        usdExchangeRate
                      )
                      return (
                        <Button
                          key={index}
                          variant='outline'
                          className={cn(
                            compact
                              ? 'hover:border-foreground flex min-h-14 flex-col items-start rounded-lg px-3 py-2.5 text-left whitespace-normal sm:min-h-16'
                              : 'hover:border-foreground flex min-h-16 flex-col items-start rounded-lg px-3 py-2.5 text-left whitespace-normal sm:min-h-[72px] sm:p-4',
                            selectedPreset === preset.value
                              ? 'border-foreground bg-foreground/5'
                              : 'border-muted'
                          )}
                          onClick={() => onSelectPreset(preset)}
                        >
                          <div className='flex w-full items-center justify-between'>
                            <div className='text-base font-semibold sm:text-lg'>
                              {formatUsdAmount(displayValue)}
                            </div>
                            {hasDiscount && (
                              <div className='text-success text-xs font-medium'>
                                {getDiscountLabel(discount)}
                              </div>
                            )}
                          </div>
                          <div className='text-muted-foreground mt-1.5 w-full text-xs sm:mt-2'>
                            {t('Pay {{amount}}', {
                              amount: formatPaymentAmount(actualPrice),
                            })}
                            {hasDiscount && savedAmount > 0 && (
                              <span className='text-success'>
                                {' '}
                                {t('Save {{amount}}', {
                                  amount: formatPaymentAmount(savedAmount),
                                })}
                              </span>
                            )}
                          </div>
                        </Button>
                      )
                    })}
                  </div>
                </div>
              )}

              <div className='space-y-2.5 sm:space-y-3'>
                <Label htmlFor='topup-amount' className={sectionLabelClassName}>
                  {t('Recharge quota (USD)')}
                </Label>
                <div
                  className={cn(
                    'grid gap-2',
                    compact
                      ? 'grid-cols-[minmax(0,1fr)_minmax(118px,0.42fr)]'
                      : 'grid-cols-[minmax(0,1fr)_minmax(110px,0.55fr)] lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center'
                  )}
                >
                  <div className='relative'>
                    <Input
                      id='topup-amount'
                      type='number'
                      value={localAmount}
                      onChange={(e) => handleAmountChange(e.target.value)}
                      min={effectiveMinTopup}
                      placeholder={t('Minimum {{amount}} USD', {
                        amount: effectiveMinTopup,
                      })}
                      className={cn(
                        'pr-14 text-base',
                        compact ? 'h-10 sm:text-base' : 'h-9 sm:h-10 sm:text-lg'
                      )}
                    />
                    <span className='text-muted-foreground pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs font-semibold'>
                      USD
                    </span>
                  </div>
                  <div className='bg-muted/30 flex min-h-9 items-center justify-between gap-2 rounded-md border px-3 lg:min-w-52'>
                    <span className='text-muted-foreground truncate text-xs'>
                      {t('Payment amount (CNY)')}
                    </span>
                    {calculating ? (
                      <Skeleton className='h-5 w-16' />
                    ) : (
                      <span className='text-sm font-semibold'>
                        {formatPaymentAmount(paymentAmount)}
                      </span>
                    )}
                  </div>
                </div>
              </div>

              <div className='space-y-2.5 sm:space-y-3'>
                <Label className={sectionLabelClassName}>
                  {t('Payment Method')}
                </Label>
                {hasStandardPaymentMethods ? (
                  <div
                    className={cn(
                      'grid grid-cols-2 gap-1.5 sm:gap-3',
                      compact ? 'lg:grid-cols-4' : 'lg:grid-cols-3'
                    )}
                  >
                    {topupInfo?.pay_methods?.map((method) => {
                      const minTopup =
                        selectedWalletType === 'claude'
                          ? 1
                          : method.min_topup || 0
                      const disabled = minTopup > topupAmount

                      const button = (
                        <Button
                          key={method.type}
                          variant='outline'
                          onClick={() => onPaymentMethodSelect(method)}
                          disabled={disabled || !!paymentLoading}
                          className='h-9 min-w-0 justify-start gap-2 rounded-lg px-3'
                        >
                          {paymentLoading === method.type ? (
                            <Loader2 className='h-4 w-4 animate-spin' />
                          ) : (
                            getPaymentIcon(
                              method.type,
                              'h-4 w-4',
                              method.icon,
                              method.name
                            )
                          )}
                          <span className='truncate'>{method.name}</span>
                        </Button>
                      )

                      return disabled ? (
                        <TooltipProvider key={method.type}>
                          <Tooltip>
                            <TooltipTrigger render={button}></TooltipTrigger>
                            <TooltipContent>
                              {t('Minimum topup amount: {{amount}}', {
                                amount: minTopup,
                              })}
                            </TooltipContent>
                          </Tooltip>
                        </TooltipProvider>
                      ) : (
                        button
                      )
                    })}
                  </div>
                ) : hasWaffoPaymentMethods ? null : (
                  <Alert>
                    <AlertDescription>
                      {t(
                        'No payment methods available. Please contact administrator.'
                      )}
                    </AlertDescription>
                  </Alert>
                )}
              </div>

              {enableWaffoTopup &&
                hasWaffoPaymentMethods &&
                onWaffoMethodSelect && (
                  <div className='space-y-2.5 sm:space-y-3'>
                    <Label className={sectionLabelClassName}>
                      {t('Waffo Payment')}
                    </Label>
                    <div
                      className={cn(
                        'grid grid-cols-2 gap-1.5 sm:gap-3',
                        compact ? 'lg:grid-cols-4' : 'lg:grid-cols-3'
                      )}
                    >
                      {waffoPayMethods?.map((method, index) => {
                        const loadingKey = `waffo-${index}`
                        const waffoMin =
                          selectedWalletType === 'claude'
                            ? 1
                            : waffoMinTopup || 0
                        const belowMin = waffoMin > topupAmount

                        const button = (
                          <Button
                            key={`${method.name}-${index}`}
                            variant='outline'
                            onClick={() => onWaffoMethodSelect(method, index)}
                            disabled={belowMin || !!paymentLoading}
                            className='h-9 min-w-0 justify-start gap-2 rounded-lg px-3'
                          >
                            {paymentLoading === loadingKey ? (
                              <Loader2 className='h-4 w-4 animate-spin' />
                            ) : method.icon ? (
                              <img
                                src={method.icon}
                                alt={method.name}
                                className='h-4 w-4 object-contain'
                              />
                            ) : (
                              getPaymentIcon('waffo')
                            )}
                            <span className='truncate'>{method.name}</span>
                          </Button>
                        )

                        return belowMin ? (
                          <TooltipProvider key={`${method.name}-${index}`}>
                            <Tooltip>
                              <TooltipTrigger render={button}></TooltipTrigger>
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
                  </div>
                )}
            </>
          )}
        </div>
      ) : (
        <Alert>
          <AlertDescription>
            {t(
              'Online topup is not enabled. Please use redemption code or contact administrator.'
            )}
          </AlertDescription>
        </Alert>
      )}

      {/* Creem Products Section */}
      {enableCreemTopup &&
        Array.isArray(creemProducts) &&
        creemProducts.length > 0 &&
        onCreemProductSelect && (
          <div className='space-y-2.5 border-t pt-4 sm:space-y-3 sm:pt-6'>
            <Label className={sectionLabelClassName}>
              {t('Creem Payment')}
            </Label>
            <CreemProductsSection
              products={creemProducts}
              onProductSelect={onCreemProductSelect}
            />
          </div>
        )}

      {/* Redemption Code Section */}
      {showRedemptionSection && redemptionEnabled ? (
        <div
          className={cn(
            'space-y-2.5 border-t pt-4 sm:space-y-3 sm:pt-6',
            compact && 'pt-3 sm:pt-4'
          )}
        >
          <div className='flex items-center gap-2'>
            <Gift className='text-muted-foreground h-4 w-4' />
            <Label htmlFor='redemption-code' className={sectionLabelClassName}>
              {t('Have a Code?')}
            </Label>
          </div>
          <div className='grid grid-cols-[minmax(0,1fr)_auto] gap-2'>
            <Input
              id='redemption-code'
              value={redemptionCode}
              onChange={(e) => onRedemptionCodeChange(e.target.value)}
              placeholder={t('Enter your redemption code')}
              className='h-9 min-w-0'
            />
            <Button
              onClick={onRedeem}
              disabled={redeeming}
              variant='outline'
              className='h-9 px-4'
            >
              {redeeming && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
              {t('Redeem')}
            </Button>
          </div>
          {topupLink && (
            <p className='text-muted-foreground text-xs'>
              {t('Need a redemption code?')}{' '}
              <a
                href={topupLink}
                target='_blank'
                rel='noopener noreferrer'
                className='inline-flex items-center gap-1 underline-offset-4 hover:underline'
              >
                {t('Get one here')}
                <ExternalLink className='h-3 w-3' />
              </a>
            </p>
          )}
        </div>
      ) : showRedemptionSection ? (
        <Alert className='border-t'>
          <AlertDescription>
            {t(
              'Redemption codes are disabled until the administrator confirms compliance terms.'
            )}
          </AlertDescription>
        </Alert>
      ) : null}
    </TitledCard>
  )
}
