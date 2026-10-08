// Buckets the current page of rows by calendar day (UTC) for the chart.
// The stat/summary endpoints return one aggregate for the whole filtered
// range, not a time series, so the chart reflects only the loaded page.
import type { ChartPoint } from './chart'

export function bucketByDay<T>(
  rows: readonly T[],
  dateOf: (row: T) => string,
  valueOf: (row: T) => number,
): ChartPoint[] {
  const buckets = new Map<string, number>()
  for (const row of rows) {
    const day = dateOf(row).slice(0, 10)
    if (!day) continue
    buckets.set(day, (buckets.get(day) ?? 0) + valueOf(row))
  }
  return [...buckets.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([label, value]) => ({ label, value }))
}
