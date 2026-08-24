import type { TFunction } from 'i18next'
import { BadgeCheck, Calculator, Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatLogQuota } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { UsageLog } from '../../data/schema'
import { isPerCallBilling } from '../../lib/utils'
import type { LogOtherData } from '../../types'

interface BillingCalculationSectionProps {
  log: UsageLog
  other: LogOtherData
}

export function BillingCalculationSection(
  props: BillingCalculationSectionProps
) {
  const { t } = useTranslation()
  const calculation = buildCalculation(props.log, props.other, t)
  const hasUsageDiscount = calculation.usageDiscountMultiplier < 1
  const hasPackageDiscount = calculation.packageMultiplier < 1
  const appliedMultiplier = hasPackageDiscount
    ? calculation.packageMultiplier
    : calculation.usageDiscountMultiplier

  return (
    <section className='min-w-0 space-y-1.5'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <span className='flex items-center gap-1.5 text-xs font-semibold'>
          <Calculator className='size-3.5' aria-hidden='true' />
          {t('Charge Calculation')}
        </span>
        {hasUsageDiscount || hasPackageDiscount ? (
          <span className='text-success flex items-center gap-1 text-xs font-medium'>
            <BadgeCheck className='size-3.5' aria-hidden='true' />
            {t('{{multiplier}}x multiplier card applied', {
              multiplier: formatRatioCompact(appliedMultiplier),
            })}
          </span>
        ) : null}
      </div>

      <div className='bg-muted/30 min-w-0 overflow-hidden rounded-md border'>
        <CalculationRow
          index={1}
          label={t('Base charge')}
          formula={calculation.baseFormula}
        />
        {hasUsageDiscount ? (
          <CalculationRow
            index={2}
            label={props.other.usage_discount_title || t('Multiplier discount')}
            formula={`${formatLogQuota(calculation.beforeDiscount)} × ${formatRatio(calculation.usageDiscountMultiplier)} = ${formatLogQuota(calculation.afterDiscount)}`}
            meta={t('Saved {{quota}}', {
              quota: formatLogQuota(calculation.usageSavedQuota),
            })}
            highlighted
          />
        ) : null}
        {calculation.subscriptionFormula ? (
          <CalculationRow
            index={hasUsageDiscount ? 3 : 2}
            label={t('Plan group conversion')}
            formula={calculation.subscriptionFormula}
          />
        ) : null}
        {hasPackageDiscount ? (
          <CalculationRow
            index={hasUsageDiscount ? 4 : 3}
            label={t('Plan multiplier card')}
            formula={`${formatLogQuota(calculation.beforePackageDiscount)} × ${formatRatio(calculation.packageMultiplier)} = ${formatLogQuota(calculation.finalCharge)}`}
            meta={t('Saved {{quota}}', {
              quota: formatLogQuota(calculation.packageSavedQuota),
            })}
            highlighted
          />
        ) : null}
        <CalculationRow
          index={calculation.finalStepIndex}
          label={t('Final charge')}
          formula={formatLogQuota(calculation.finalCharge)}
          strong
        />
        {!calculation.hasDiscountAudit ? (
          <div className='text-muted-foreground flex items-start gap-1.5 border-t px-3 py-2 text-[11px] leading-4'>
            <Info className='mt-0.5 size-3 shrink-0' aria-hidden='true' />
            <span>{t('Legacy billing audit unavailable')}</span>
          </div>
        ) : null}
      </div>
    </section>
  )
}

function CalculationRow(props: {
  index: number
  label: string
  formula: string
  meta?: string
  highlighted?: boolean
  strong?: boolean
}) {
  return (
    <div
      className={cn(
        'grid min-w-0 grid-cols-[1.5rem_minmax(0,1fr)] gap-2 px-3 py-2.5 not-last:border-b',
        props.highlighted && 'bg-success/8'
      )}
    >
      <span className='bg-background text-muted-foreground flex size-5 items-center justify-center rounded-full border font-mono text-[10px]'>
        {props.index}
      </span>
      <div className='min-w-0'>
        <div className='flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1'>
          <span
            className={cn(
              'text-xs font-medium',
              props.highlighted && 'text-success',
              props.strong && 'font-semibold'
            )}
          >
            {props.label}
          </span>
          {props.meta ? (
            <span className='text-success text-[11px]'>{props.meta}</span>
          ) : null}
        </div>
        <code
          className={cn(
            'text-muted-foreground mt-1 block min-w-0 font-mono text-[11px] leading-4 break-words whitespace-normal',
            props.strong && 'text-foreground text-xs'
          )}
        >
          {props.formula}
        </code>
      </div>
    </div>
  )
}

function buildCalculation(log: UsageLog, other: LogOtherData, t: TFunction) {
  const hasDiscountAudit =
    other.quota_before_discount != null &&
    other.quota_after_discount != null &&
    other.usage_discount_multiplier != null
  const beforeDiscount = other.quota_before_discount ?? log.quota
  const afterDiscount = other.quota_after_discount ?? log.quota
  const usageDiscountMultiplier = validDiscountMultiplier(
    other.usage_discount_multiplier
  )
  const usageSavedQuota =
    other.usage_discount_quota ?? Math.max(0, beforeDiscount - afterDiscount)
  const isSubscription = other.billing_source === 'subscription'
  const packageMultiplier = isSubscription
    ? validDiscountMultiplier(other.subscription_package_multiplier)
    : 1
  const beforePackageDiscount = isSubscription
    ? subscriptionChargeBeforePackage(other, afterDiscount, packageMultiplier)
    : afterDiscount
  const finalCharge = log.quota
  const subscriptionFormula = isSubscription
    ? buildSubscriptionFormula(
        other,
        afterDiscount,
        beforePackageDiscount,
        packageMultiplier,
        t
      )
    : null
  const stepCount =
    2 +
    (usageDiscountMultiplier < 1 ? 1 : 0) +
    (subscriptionFormula ? 1 : 0) +
    (packageMultiplier < 1 ? 1 : 0)

  return {
    hasDiscountAudit,
    beforeDiscount,
    afterDiscount,
    usageDiscountMultiplier,
    usageSavedQuota,
    packageMultiplier,
    beforePackageDiscount,
    packageSavedQuota: Math.max(0, beforePackageDiscount - finalCharge),
    finalCharge,
    subscriptionFormula,
    finalStepIndex: stepCount,
    baseFormula: buildBaseFormula(other, beforeDiscount, t),
  }
}

function buildSubscriptionFormula(
  other: LogOtherData,
  walletQuota: number,
  convertedQuota: number,
  packageMultiplier: number,
  t: TFunction
) {
  const walletRatio = billingGroupRatio(other)
  const planRatio = validPositiveRatio(other.subscription_group_multiplier)
  if (walletRatio > 0 && planRatio > 0) {
    return t('Plan conversion formula', {
      quota: formatLogQuota(walletQuota),
      walletRatio: formatRatio(walletRatio),
      planRatio: formatRatio(planRatio),
      converted: formatLogQuota(convertedQuota),
    })
  }

  const quotaScale = validPositiveRatio(other.subscription_quota_scale)
  const groupScale = quotaScale / packageMultiplier
  return t('Plan scale formula', {
    quota: formatLogQuota(walletQuota),
    scale: formatRatio(groupScale || 1),
    converted: formatLogQuota(convertedQuota),
  })
}

function subscriptionChargeBeforePackage(
  other: LogOtherData,
  walletQuota: number,
  packageMultiplier: number
) {
  const walletRatio = billingGroupRatio(other)
  const planRatio = validPositiveRatio(other.subscription_group_multiplier)
  if (walletRatio > 0 && planRatio > 0) {
    return Math.round((walletQuota / walletRatio) * planRatio)
  }
  const quotaScale = validPositiveRatio(other.subscription_quota_scale)
  return Math.round(walletQuota * (quotaScale / packageMultiplier || 1))
}

function buildBaseFormula(other: LogOtherData, quota: number, t: TFunction) {
  const groupRatio = effectiveGroupRatio(other)
  if (other.billing_mode === 'tiered_expr') {
    return t('Dynamic pricing formula', {
      tier: other.matched_tier || t('Matched Tier'),
      quota: formatLogQuota(quota),
    })
  }
  if (isPerCallBilling(other.model_price)) {
    return t('Per-call billing formula', {
      price: other.model_price,
      groupRatio: formatRatio(groupRatio),
      quota: formatLogQuota(quota),
    })
  }
  return t('Token billing formula', {
    modelRatio: formatRatio(other.model_ratio ?? 1),
    groupRatio: formatRatio(groupRatio),
    quota: formatLogQuota(quota),
  })
}

function effectiveGroupRatio(other: LogOtherData) {
  return billingGroupRatio(other)
}

function billingGroupRatio(other: LogOtherData) {
  if (other.billing_source === 'subscription') {
    const originalRatio = validPositiveRatio(other.subscription_group_ratio)
    if (originalRatio > 0) return originalRatio
  }
  const userRatio = other.user_group_ratio
  return userRatio != null && Number.isFinite(userRatio) && userRatio !== -1
    ? userRatio
    : (other.group_ratio ?? 1)
}

function validDiscountMultiplier(value: number | undefined) {
  return value != null && Number.isFinite(value) && value > 0 && value < 1
    ? value
    : 1
}

function validPositiveRatio(value: number | undefined) {
  return value != null && Number.isFinite(value) && value > 0 ? value : 0
}

function formatRatio(value: number) {
  return Number.isFinite(value) ? value.toFixed(4) : '1.0000'
}

function formatRatioCompact(value: number) {
  return formatRatio(value).replace(/\.?0+$/, '')
}
