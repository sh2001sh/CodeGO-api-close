import { classifyRequestHealth } from '@/lib/request-health'
import type { MarketplaceGroup } from '../types'

const RECENT_REQUEST_SEGMENTS = 24
const DEFAULT_BUCKET_SECONDS = 900

type RecentRequestBucket = MarketplaceGroup['recent_request_series'][number]

/** Fills missing intervals in the six-hour, fifteen-minute status strip. */
export function normalizeRecentRequestSeries(
  series: MarketplaceGroup['recent_request_series'] | null | undefined,
  bucketSeconds: number,
  nowSeconds = Math.floor(Date.now() / 1000),
  segmentCount = RECENT_REQUEST_SEGMENTS
): RecentRequestBucket[] {
  const size = bucketSeconds > 0 ? bucketSeconds : DEFAULT_BUCKET_SECONDS
  const count = Math.max(1, segmentCount)
  const normalizedInput = (series ?? []).map((bucket) => ({
    ...bucket,
    ts: normalizeTimestamp(bucket.ts, size),
  }))
  const newestServerBucket = normalizedInput.reduce(
    (latest, bucket) => Math.max(latest, bucket.ts),
    0
  )
  const browserBucket = nowSeconds - (nowSeconds % size)
  const maximumClockDrift = count * size
  const currentBucketStart =
    newestServerBucket > 0 &&
    Math.abs(browserBucket - newestServerBucket) > maximumClockDrift
      ? newestServerBucket
      : browserBucket
  const windowStart = currentBucketStart - (count - 1) * size
  const bucketsByTimestamp = new Map(
    normalizedInput.map((bucket) => [bucket.ts, bucket])
  )

  return Array.from({ length: count }, (_, index) => {
    const ts = windowStart + index * size
    return (
      bucketsByTimestamp.get(ts) ?? { ts, success_rate: 0, request_count: 0 }
    )
  })
}

function normalizeTimestamp(timestamp: number, bucketSeconds: number) {
  const seconds =
    timestamp > 10_000_000_000 ? Math.floor(timestamp / 1000) : timestamp
  return seconds - (seconds % bucketSeconds)
}

/** Resolves the latest visible health state when an older API omits it. */
export function resolveRecentRequestStatus(
  series: RecentRequestBucket[]
): MarketplaceGroup['latest_request_status'] {
  for (let index = series.length - 1; index >= 0; index--) {
    const bucket = series[index]
    if (bucket.request_count <= 0) continue
    return classifyRequestHealth(bucket.success_rate, bucket.request_count)
  }
  return 'unknown'
}
