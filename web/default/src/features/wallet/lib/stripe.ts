import { normalizeInterfaceLanguage } from '@/i18n/languages'

export type StripeCurrency = 'HKD' | 'CNY' | 'USD'

export function getStripeCurrencyForLanguage(
  language?: string
): StripeCurrency {
  const normalized = normalizeInterfaceLanguage(language)
  if (normalized === 'zh-TW') return 'HKD'
  if (normalized === 'zh') return 'CNY'
  return 'USD'
}

export function getStripeUnitPrice(
  currency: StripeCurrency,
  status?: Record<string, unknown> | null
): number {
  const defaults = { HKD: 1.5, CNY: 1.4, USD: 0.2 }
  const value = Number(
    status?.[`stripe_${currency.toLowerCase()}_unit_price`] ??
      defaults[currency]
  )
  return Number.isFinite(value) && value > 0 ? value : defaults[currency]
}

export function getStripePlanAmount(
  amount: number,
  baseCurrency: string,
  currency: StripeCurrency,
  status?: Record<string, unknown> | null
): number {
  const base =
    baseCurrency.toUpperCase() === 'USD'
      ? amount * Number(status?.usd_exchange_rate || 1)
      : amount
  return (
    Math.round(
      (base * getStripeUnitPrice(currency, status) + Number.EPSILON) * 100
    ) / 100
  )
}
