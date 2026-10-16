import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'
import {
  formatBillingCurrencyFromUSD,
  formatCurrencyFromUSD,
  formatQuotaWithCurrency,
  getCurrencyLabel,
} from './currency'
import { parseQuotaFromDollars, quotaUnitsToDollars } from './format'

test('credits change only the USD display label and preserve quota round trips', () => {
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG, usdExchangeRate: 7.8 },
  })
  assert.equal(getCurrencyLabel(), 'credits')
  for (const [amount, expected] of [
    [0, '0 credits'],
    [1, '1 credits'],
    [25.5, '25.5 credits'],
    [-0.25, '-0.25 credits'],
    [0.000001, '0.000001 credits'],
  ] as const) {
    assert.equal(formatCurrencyFromUSD(amount, { digitsSmall: 6 }), expected)
    assert.equal(
      formatBillingCurrencyFromUSD(amount, { digitsSmall: 6 }),
      expected
    )
    const quota = Math.round(amount * 500000)
    assert.equal(parseQuotaFromDollars(quotaUnitsToDollars(quota)), quota)
  }
  assert.equal(formatQuotaWithCurrency(12500000), '25 credits')
  assert.equal(formatCurrencyFromUSD(Number.NaN), '-')
  assert.equal(parseQuotaFromDollars(Number.POSITIVE_INFINITY), 0)
})

test('local payment currency display retains its existing exchange rate', () => {
  useSystemConfigStore.getState().setConfig({
    currency: {
      ...DEFAULT_CURRENCY_CONFIG,
      quotaDisplayType: 'CNY',
      usdExchangeRate: 7,
    },
  })
  assert.match(formatCurrencyFromUSD(10), /70/)
  assert.equal(parseQuotaFromDollars(70), 5000000)
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
})
