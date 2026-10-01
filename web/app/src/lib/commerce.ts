import type { Schema } from './types'
export type Plan = Schema['Plan']
export type Order = Schema['Order']
export type Subscription = Schema['Subscription']

export function currencyDigits(currency: string): number {
  const zero = [
    'bif',
    'clp',
    'djf',
    'gnf',
    'jpy',
    'kmf',
    'krw',
    'mga',
    'pyg',
    'rwf',
    'ugx',
    'vnd',
    'vuv',
    'xaf',
    'xof',
    'xpf',
  ]
  const three = ['bhd', 'jod', 'kwd', 'omr', 'tnd']
  return zero.includes(currency.toLowerCase()) ? 0 : three.includes(currency.toLowerCase()) ? 3 : 2
}

export function paymentAmount(value: number | string | bigint, currency: string): string {
  const digits = currencyDigits(currency)
  const scale = 10n ** BigInt(digits)
  const amount = BigInt(value)
  const absolute = amount < 0n ? -amount : amount
  const fraction = digits ? `.${String(absolute % scale).padStart(digits, '0')}` : ''
  return `${currency.toUpperCase()} ${amount < 0n ? '-' : ''}${(absolute / scale).toLocaleString()}${fraction}`
}

export function followPayment(value: string): void {
  const target = new URL(value, window.location.origin)
  if (target.protocol !== 'https:' && target.origin !== window.location.origin)
    throw new Error('支付地址无效')
  if (target.protocol !== 'https:' && target.protocol !== 'http:') throw new Error('支付地址无效')
  window.location.assign(target.href)
}

export function minorAmount(value: string, currency = 'usd'): number {
  const digits = currencyDigits(currency)
  if (!(digits ? new RegExp(`^\\d+(\\.\\d{1,${digits}})?$`) : /^\d+$/).test(value))
    throw new Error(`支付金额最多支持 ${digits} 位小数`)
  const [whole, fraction = ''] = value.split('.')
  const minor = BigInt(whole) * 10n ** BigInt(digits) + BigInt(fraction.padEnd(digits, '0') || '0')
  if (minor <= 0n || minor > BigInt(Number.MAX_SAFE_INTEGER)) throw new Error('金额超出允许范围')
  return Number(minor)
}
