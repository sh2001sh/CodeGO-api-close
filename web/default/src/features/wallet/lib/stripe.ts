export type StripeCurrency = 'HKD' | 'CNY' | 'USD'

export function isStripeCurrency(value: string): value is StripeCurrency {
  return value === 'HKD' || value === 'CNY' || value === 'USD'
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
