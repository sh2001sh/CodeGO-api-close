import interfaceI18n from '@/i18n/config'
import type { TFunction } from 'i18next'
import { formatQuotaWithCurrency, getCurrencyDisplay } from '@/lib/currency'
import type { SubscriptionPlan } from '../types'
import { formatDuration, formatResetPeriod } from './format'

const DAY_PASS_KEYWORDS = ['day pass', '日卡']
const DEFAULT_QUOTA_PER_UNIT = 500000

const PLAN_DISCOUNT_TEXT_MAP: Array<{
  match: (title: string) => boolean
  text: string
}> = [
  {
    match: (title) => title.includes('lite'),
    text: '比官方 Plus 优惠约 89.7%',
  },
  {
    match: (title) => title.includes('standard'),
    text: '比官方 Plus 优惠约 90.8%',
  },
  { match: (title) => title.includes('pro'), text: '比官方 Plus 优惠约 93.0%' },
  {
    match: (title) => title.includes('ultra'),
    text: '比官方 Plus 优惠约 94.5%',
  },
  {
    match: (title) =>
      (title.includes('50') && title.includes('日卡')) ||
      title.includes('day pass 50'),
    text: '比官方 Plus 优惠约 87.7%',
  },
  {
    match: (title) =>
      (title.includes('100') && title.includes('日卡')) ||
      title.includes('day pass 100'),
    text: '比官方 Plus 优惠约 87.7%',
  },
]

function trimText(value?: string | null): string {
  return String(value || '')
    .replaceAll(String.fromCharCode(0), '')
    .trim()
}

function formatNumber(value: number): string {
  const abs = Math.abs(value)
  if (abs === 0) return '0'
  if (abs >= 100) {
    return value.toFixed(Number.isInteger(value) ? 0 : 2).replace(/\.00$/, '')
  }
  if (abs >= 1) {
    return value
      .toFixed(2)
      .replace(/\.00$/, '')
      .replace(/(\.\d)0$/, '$1')
  }
  if (abs >= 0.01) {
    return value.toFixed(4).replace(/0+$/, '').replace(/\.$/, '')
  }
  return value.toFixed(6).replace(/0+$/, '').replace(/\.$/, '')
}

function getSubscriptionQuotaPerUnit(): number {
  const quotaPerUnit = Number(getCurrencyDisplay().config.quotaPerUnit || 0)
  return quotaPerUnit > 0 ? quotaPerUnit : DEFAULT_QUOTA_PER_UNIT
}

export function subscriptionQuotaUnitsToUSD(amount?: number | null): number {
  return Number(amount || 0) / getSubscriptionQuotaPerUnit()
}

export function parseSubscriptionQuotaUSDToUnits(
  amount?: number | string | null
): number {
  const numericAmount = Number(amount || 0)
  if (!Number.isFinite(numericAmount)) return 0
  return Math.round(numericAmount * getSubscriptionQuotaPerUnit())
}

export function normalizeSubscriptionText(value?: string | null): string {
  return trimText(value)
}

export function formatSubscriptionPlanTitle(value?: string | null): string {
  return normalizeSubscriptionText(value).replace(
    /([A-Za-z])(?=[\u4e00-\u9fff])/g,
    '$1 '
  )
}

export function getSubscriptionDisabledReasonText(
  value?: string | null
): string {
  const normalized = normalizeSubscriptionText(value)
  switch (normalized) {
    case 'HKD':
      return 'HK$'
    case 'cannot subscribe to a lower-tier plan while your current package is active':
    case 'cannot subscribe to a lower-tier plan while your current package still has remaining quota':
      return interfaceI18n.t(
        '当前还有更高档且未用完的生效套餐，暂不支持直接降级。'
      )
    case 'renewal requires at least 30% of the current package quota to be used':
      return interfaceI18n.t(
        '当前套餐至少使用 30%（剩余额度不高于 70%）后才可续费。'
      )
    default:
      return normalized
  }
}

export function isDayPassPlan(
  plan?: Partial<SubscriptionPlan> | null
): boolean {
  if (!plan) return false
  if (plan.plan_type === 'daily') return true
  if (plan.duration_unit === 'day' && Number(plan.duration_value || 0) <= 2) {
    return true
  }
  const title = normalizeSubscriptionText(plan.title).toLowerCase()
  return DAY_PASS_KEYWORDS.some((keyword) => title.includes(keyword))
}

export function isMonthlyCardPlan(
  plan?: Partial<SubscriptionPlan> | null
): boolean {
  if (!plan || isDayPassPlan(plan)) return false
  if (plan.plan_type === 'monthly') return true
  return plan.duration_unit === 'month'
}

export function getSubscriptionCurrencyLabel(currency?: string | null): string {
  const normalized = trimText(currency).toUpperCase()
  switch (normalized) {
    case 'CNY':
    case 'RMB':
      return '¥'
    case 'EUR':
      return 'EUR '
    case 'USD':
    default:
      return 'credits '
  }
}

export function formatSubscriptionPlanPrice(
  amount?: number | null,
  currency?: string | null
): string {
  const value = Number(amount || 0)
  const label = getSubscriptionCurrencyLabel(currency)
  return label === 'credits '
    ? `${formatNumber(value)} credits`
    : `${label}${formatNumber(value)}`
}

export function formatSubscriptionQuotaAmount(amount?: number | null): string {
  return formatQuotaWithCurrency(Number(amount || 0), {
    digitsLarge: 2,
    digitsSmall: 4,
    abbreviate: true,
  })
}

export function getSubscriptionPlanSubtitle(
  plan?: Partial<SubscriptionPlan> | null
): string {
  if (plan?.plan_type === 'credit_pack') return '永久额度包'
  const subtitle = normalizeSubscriptionText(plan?.subtitle)
  if (subtitle) return subtitle
  if (plan?.plan_type === 'starter') return '新人专区'
  if (plan?.plan_type === 'weekly') return '周卡'
  return isDayPassPlan(plan) ? '日卡' : '月卡'
}

export function getSubscriptionPlanDiscountText(
  plan?: Partial<SubscriptionPlan> | null
): string {
  const title = normalizeSubscriptionText(plan?.title).toLowerCase()
  const matched = PLAN_DISCOUNT_TEXT_MAP.find((item) => item.match(title))
  return matched?.text || ''
}

export function getSubscriptionPlanActionLabel(
  action: string | undefined,
  t: TFunction
): string {
  switch (action) {
    case 'renew':
      return interfaceI18n.t('立即续费')
    case 'upgrade':
      return interfaceI18n.t('立即升级')
    case 'disabled':
      return interfaceI18n.t('当前不可订阅')
    case 'subscribe':
      return interfaceI18n.t('立即订阅')
    default:
      return t('Subscribe')
  }
}

export function getSubscriptionPlanDescription(
  plan: Partial<SubscriptionPlan>,
  totalAmount: number,
  periodAmount: number,
  t: TFunction
): string {
  if (isDayPassPlan(plan)) {
    const totalText =
      totalAmount > 0
        ? formatSubscriptionQuotaAmount(totalAmount)
        : interfaceI18n.t('不限')
    return interfaceI18n.t(
      '有效期 {{param0}}，总额度 {{param1}}，日卡额度独立结算，扣费时默认优先于月卡。',
      { param0: formatDuration(plan, t), param1: totalText }
    )
  }

  if (isMonthlyCardPlan(plan)) {
    const totalText =
      totalAmount > 0
        ? formatSubscriptionQuotaAmount(totalAmount)
        : interfaceI18n.t('不限')
    return interfaceI18n.t(
      '有效期 {{param0}}，本月可用额度 {{param1}}，一个月内可自由使用，用完或到期后结束。',
      { param0: formatDuration(plan, t), param1: totalText }
    )
  }

  if (periodAmount > 0) {
    const totalText =
      totalAmount > 0
        ? formatSubscriptionQuotaAmount(totalAmount)
        : interfaceI18n.t('不限')
    return interfaceI18n.t(
      '额度按周期刷新，周期额度 {{param0}}，总额度 {{param1}}。',
      { param0: formatSubscriptionQuotaAmount(periodAmount), param1: totalText }
    )
  }

  if (totalAmount > 0) {
    return interfaceI18n.t('有效期 {{param0}}，总额度 {{param1}}。', {
      param0: formatDuration(plan, t),
      param1: formatSubscriptionQuotaAmount(totalAmount),
    })
  }

  return interfaceI18n.t('有效期 {{param0}}，总额度不限。', {
    param0: formatDuration(plan, t),
  })
}

export function getSubscriptionPlanDetailText(
  plan: Partial<SubscriptionPlan>,
  totalAmount: number,
  periodAmount: number,
  t: TFunction
): string {
  const detailParts = [
    interfaceI18n.t('有效期 {{param0}}', { param0: formatDuration(plan, t) }),
  ]
  const resetLabel = formatResetPeriod(plan, t)

  if (isDayPassPlan(plan)) {
    detailParts.push(
      totalAmount > 0
        ? interfaceI18n.t('总额度 {{param0}}', {
            param0: formatSubscriptionQuotaAmount(totalAmount),
          })
        : interfaceI18n.t('总额度不限')
    )
    detailParts.push(interfaceI18n.t('日卡额度独立结算，扣费时默认优先于月卡'))
    return detailParts.join('；')
  }

  if (isMonthlyCardPlan(plan)) {
    if (totalAmount > 0) {
      detailParts.push(
        interfaceI18n.t('本月可用额度 {{param0}}', {
          param0: formatSubscriptionQuotaAmount(totalAmount),
        })
      )
    } else {
      detailParts.push(interfaceI18n.t('本月可用额度不限'))
    }
    detailParts.push(interfaceI18n.t('月卡不设置周刷新或周期额度'))
    detailParts.push(interfaceI18n.t('适合持续使用 Code Go 与相关模型'))
    return detailParts.join('；')
  }

  if (resetLabel !== t('No Reset')) {
    detailParts.push(
      interfaceI18n.t('额度重置 {{param0}}', { param0: resetLabel })
    )
  }
  if (periodAmount > 0) {
    detailParts.push(
      `${isMonthlyCardPlan(plan) ? interfaceI18n.t('月度额度') : interfaceI18n.t('周期额度')} ${formatSubscriptionQuotaAmount(periodAmount)}`
    )
  }
  if (totalAmount > 0) {
    detailParts.push(
      interfaceI18n.t('总额度 {{param0}}', {
        param0: formatSubscriptionQuotaAmount(totalAmount),
      })
    )
  } else {
    detailParts.push(interfaceI18n.t('总额度不限'))
  }
  detailParts.push(interfaceI18n.t('适合持续使用 Code Go 与相关模型'))
  return detailParts.join('；')
}
