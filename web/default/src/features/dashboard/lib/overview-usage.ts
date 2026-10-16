const ROLLING_DAY_SECONDS = 24 * 60 * 60

/** Keep empty intervals in place so sparse traffic retains its real spacing. */
export function buildOverviewUsageTimeline(
  rows: Array<{ created_at: number; quota: number }>,
  startTimestamp: number,
  endTimestamp: number,
  bucketSeconds: number
) {
  if (
    !Number.isFinite(startTimestamp) ||
    !Number.isFinite(endTimestamp) ||
    !Number.isFinite(bucketSeconds) ||
    bucketSeconds <= 0 ||
    endTimestamp < startTimestamp
  )
    return []

  const firstBucket = Math.floor(startTimestamp / bucketSeconds) * bucketSeconds
  const lastBucket = Math.floor(endTimestamp / bucketSeconds) * bucketSeconds
  const totals = new Map<number, number>()
  for (const row of rows) {
    if (!Number.isFinite(row.created_at) || !Number.isFinite(row.quota))
      continue
    if (row.created_at < startTimestamp || row.created_at > endTimestamp)
      continue
    const timestamp = Math.floor(row.created_at / bucketSeconds) * bucketSeconds
    totals.set(timestamp, (totals.get(timestamp) ?? 0) + row.quota)
  }
  return Array.from(
    { length: Math.floor((lastBucket - firstBucket) / bucketSeconds) + 1 },
    (_, index) => {
      const created_at = firstBucket + index * bucketSeconds
      return { created_at, quota: totals.get(created_at) ?? 0 }
    }
  )
}

/** Group summaries can contain requests without a latency measurement. */
export function getOverviewGroupLatency(
  groups: Array<{ avg_latency_ms: number | null; request_count: number }>
): number | null {
  let total = 0
  let weight = 0
  for (const group of groups) {
    if (
      group.avg_latency_ms == null ||
      !Number.isFinite(group.avg_latency_ms) ||
      group.avg_latency_ms <= 0 ||
      group.request_count <= 0
    )
      continue
    total += group.avg_latency_ms * group.request_count
    weight += group.request_count
  }
  return weight > 0 ? total / weight : null
}

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
  return aggregateUsageByBucket(rows, 60 * 60, limit)
}

export function aggregateUsageByBucket(
  rows: Array<{ created_at: number; quota: number }>,
  bucketSeconds: number,
  limit: number
) {
  const buckets = new Map<number, number>()
  for (const row of rows) {
    const timestamp = Number(row.created_at)
    if (!Number.isFinite(timestamp)) continue
    const bucket = Math.floor(timestamp / bucketSeconds) * bucketSeconds
    buckets.set(bucket, (buckets.get(bucket) ?? 0) + (Number(row.quota) || 0))
  }
  return Array.from(buckets, ([created_at, quota]) => ({ created_at, quota }))
    .sort((left, right) => left.created_at - right.created_at)
    .slice(-Math.max(0, limit))
}
