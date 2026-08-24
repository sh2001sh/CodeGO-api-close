const ROLLING_DAY_SECONDS = 24 * 60 * 60

/** Return an exact rolling 24-hour window without the legacy chart buffer. */
export function getRolling24HourRange(
  nowSeconds = Math.floor(Date.now() / 1000)
) {
  return {
    start_timestamp: nowSeconds - ROLLING_DAY_SECONDS,
    end_timestamp: nowSeconds,
  }
}

export function aggregateHourlyUsage(
  rows: Array<{ created_at: number; quota: number }>,
  limit = 12
) {
  const buckets = new Map<number, number>()
  for (const row of rows) {
    const timestamp = Number(row.created_at)
    if (!Number.isFinite(timestamp)) continue
    buckets.set(
      timestamp,
      (buckets.get(timestamp) ?? 0) + (Number(row.quota) || 0)
    )
  }
  return Array.from(buckets, ([created_at, quota]) => ({ created_at, quota }))
    .sort((left, right) => left.created_at - right.created_at)
    .slice(-Math.max(0, limit))
}
