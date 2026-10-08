import { safeLocalReturn } from '../../lib/auth-navigation'
import { parse } from 'lossless-json'

export function multiplierText(value: unknown): string {
  if (typeof value !== 'string' || !/^\d+$/.test(value)) return '—'
  const amount = BigInt(value)
  const decimals = String(amount % 1_000_000n)
    .padStart(6, '0')
    .replace(/0+$/, '')
  return `${amount / 1_000_000n}${decimals ? `.${decimals}` : ''}`
}

export function notificationAction(value: unknown): string | undefined {
  const local = safeLocalReturn(value)
  if (!local) return
  const pathname = local.split(/[?#]/, 1)[0]
  // Notifications link only to existing product destinations, never auth or API actions.
  if (
    ![
      '/channel-market',
      '/billing',
      '/orders',
      '/admin/orders',
      '/wallet',
      '/my-channels',
      '/profile',
      '/referral-rewards',
    ].includes(pathname)
  )
    return
  return local
}

export function exactCount(value: unknown): bigint | undefined {
  if (typeof value === 'number' && (!Number.isSafeInteger(value) || value < 0)) return
  if (
    typeof value !== 'number' &&
    typeof value !== 'bigint' &&
    (typeof value !== 'string' || !/^\d+$/.test(value))
  )
    return
  const count = BigInt(value)
  if (count >= 0n && count <= 9_223_372_036_854_775_807n) return count
}

export function unreadEvent(value: string): bigint | undefined {
  try {
    const data: unknown = parse(value, undefined, (raw) =>
      /^-?\d+$/.test(raw) ? BigInt(raw) : Number(raw),
    )
    if (!data || typeof data !== 'object' || !('unread_count' in data)) return
    return exactCount(data.unread_count)
  } catch {
    return
  }
}

export function observedThroughID(value: unknown): string | undefined {
  if (typeof value !== 'string' || !/^[1-9]\d*$/.test(value)) return
  if (BigInt(value) > 9_223_372_036_854_775_807n) return
  return value
}
