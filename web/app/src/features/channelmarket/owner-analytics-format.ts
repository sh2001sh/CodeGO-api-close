export interface OwnerAnalyticsFilters {
  from: string
  to: string
  channel_id?: string
  model?: string
}

export function localDateTime(value: Date): string {
  const local = new Date(value.getTime() - value.getTimezoneOffset() * 60_000)
  return local.toISOString().slice(0, 16)
}

export function analyticsRange(
  from: string,
  to: string,
): Pick<OwnerAnalyticsFilters, 'from' | 'to'> {
  const start = new Date(from)
  const end = new Date(to)
  if (!Number.isFinite(start.getTime()) || !Number.isFinite(end.getTime()) || start >= end) {
    throw new Error('请选择有效时间范围，结束时间须晚于开始时间')
  }
  if (end.getTime() - start.getTime() > 366 * 86400_000) {
    throw new Error('统计时间范围不能超过 366 天')
  }
  return { from: start.toISOString(), to: end.toISOString() }
}

export function analyticsQuery(filters?: OwnerAnalyticsFilters): string {
  if (!filters) return ''
  const query = new URLSearchParams({ from: filters.from, to: filters.to })
  if (filters.channel_id) query.set('channel_id', filters.channel_id)
  if (filters.model) query.set('model', filters.model)
  return `?${query}`
}

export function successPercent(
  success: number | string | bigint,
  requests: number | string | bigint,
): string {
  const total = BigInt(requests)
  if (total === 0n) return '—'
  return `${Number((BigInt(success) * 10_000n) / total) / 100}%`
}

// Keep exact int64 values for labels; only the normalized pixel coordinate becomes a number.
export function trendHeight(value: number | string | bigint, maximum: bigint): number {
  if (maximum <= 0n) return 0
  const amount = BigInt(value)
  return amount > 0n ? Number((amount * 10_000n) / maximum) / 10_000 : 0
}

export type OwnerLogCursor = { before: string; before_id: number | string | bigint }

export function ownerLogCursor(row: {
  created_at: string
  id: number | string | bigint
}): OwnerLogCursor {
  return { before: row.created_at, before_id: row.id }
}
