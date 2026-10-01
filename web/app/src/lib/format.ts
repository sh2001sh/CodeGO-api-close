import dayjs from 'dayjs'

export function shanghaiDay(value = new Date()): string {
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).formatToParts(value)
  return ['year', 'month', 'day']
    .map((type) => parts.find((part) => part.type === type)?.value)
    .join('-')
}

export function credits(value: number | string | bigint | undefined): string {
  if (value === undefined || value === null) return '—'
  try {
    const amount = BigInt(value)
    const sign = amount < 0n ? '-' : ''
    const absolute = amount < 0n ? -amount : amount
    const fraction = String(absolute % 1_000_000n)
      .padStart(6, '0')
      .replace(/0+$/, '')
    return `${sign}${(absolute / 1_000_000n).toLocaleString()}${fraction ? `.${fraction}` : ''} credits`
  } catch {
    return '—'
  }
}

export function toMicroCredits(value: string): string {
  if (!/^\d+(\.\d{1,6})?$/.test(value)) throw new Error('请输入最多六位小数的正数')
  const [whole, fraction = ''] = value.split('.')
  const amount = BigInt(whole) * 1_000_000n + BigInt(fraction.padEnd(6, '0'))
  if (amount <= 0n || amount > 9_223_372_036_854_775_807n) throw new Error('金额超出允许范围')
  return amount.toString()
}

export function date(value: string | number | null | undefined): string {
  if (!value) return '—'
  const parsed = dayjs(value)
  return parsed.isValid() ? parsed.format('YYYY-MM-DD HH:mm') : '—'
}
