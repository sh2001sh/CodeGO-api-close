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
  const isDiscounted = calculation.discountMultiplier < 1
  const isNinetyPercentCard =
    props.other.usage_discount_source === 'blind_box_multiplier_card' &&
    Math.abs(calculation.discountMultiplier - 0.9) < 0.000001

  return (
    <section className='min-w-0 space-y-1.5'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <span className='flex items-center gap-1.5 text-xs font-semibold'>
          <Calculator className='size-3.5' aria-hidden='true' />
          {t('Charge Calculation')}
        </span>
        {isNinetyPercentCard ? (
          <span className='text-success flex items-center gap-1 text-xs font-medium'>
            <BadgeCheck className='size-3.5' aria-hidden='true' />
            {t('0.9 multiplier card applied')}
          </span>
        ) : null}
      </div>

      <div className='bg-muted/30 min-w-0 overflow-hidden rounded-md border'>
        <CalculationRow
          index={1}
          label={t('Base charge')}
          formula={calculation.baseFormula}
        />
        {isDiscounted ? (
          <CalculationRow
            index={2}
            label={props.other.usage_discount_title || t('Multiplier discount')}
            formula={`${formatLogQuota(calculation.beforeDiscount)} × ${formatRatio(calculation.discountMultiplier)} = ${formatLogQuota(calculation.afterDiscount)}`}
            meta={t('Saved {{quota}}', {
              quota: formatLogQuota(calculation.savedQuota),
            })}
            highlighted
          />
        ) : null}
        <CalculationRow
          index={isDiscounted ? 3 : 2}
          label={t('Final charge')}
          formula={formatLogQuota(calculation.afterDiscount)}
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
  const discountMultiplier = other.usage_discount_multiplier ?? 1
  const savedQuota =
    other.usage_discount_quota ?? Math.max(0, beforeDiscount - afterDiscount)

  return {
    hasDiscountAudit,
    beforeDiscount,
    afterDiscount,
    discountMultiplier,
    savedQuota,
    baseFormula: buildBaseFormula(other, beforeDiscount, t),
  }
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
  const userRatio = other.user_group_ratio
  return userRatio != null && Number.isFinite(userRatio) && userRatio !== -1
    ? userRatio
    : (other.group_ratio ?? 1)
}

function formatRatio(value: number) {
  return Number.isFinite(value) ? value.toFixed(4) : '1.0000'
}
