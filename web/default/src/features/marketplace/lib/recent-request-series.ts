import type { MarketplaceGroup } from '../types'

const RECENT_REQUEST_SEGMENTS = 12
const DEFAULT_BUCKET_SECONDS = 1800

type RecentRequestBucket = MarketplaceGroup['recent_request_series'][number]

/** Normalizes partial or missing API data into a fixed six-hour status strip. */
export function normalizeRecentRequestSeries(
  series: MarketplaceGroup['recent_request_series'] | null | undefined,
  bucketSeconds: number,
  nowSeconds = Math.floor(Date.now() / 1000)
): RecentRequestBucket[] {
  const size = bucketSeconds > 0 ? bucketSeconds : DEFAULT_BUCKET_SECONDS
  const currentBucketStart = nowSeconds - (nowSeconds % size)
  const windowStart = currentBucketStart - (RECENT_REQUEST_SEGMENTS - 1) * size
  const bucketsByTimestamp = new Map(
    (series ?? []).map((bucket) => [bucket.ts, bucket])
  )

  return Array.from({ length: RECENT_REQUEST_SEGMENTS }, (_, index) => {
    const ts = windowStart + index * size
    return (
      bucketsByTimestamp.get(ts) ?? { ts, success_rate: 0, request_count: 0 }
    )
  })
}

/** Resolves the latest visible health state when an older API omits it. */
export function resolveRecentRequestStatus(
  status: MarketplaceGroup['latest_request_status'] | undefined,
  series: RecentRequestBucket[]
): MarketplaceGroup['latest_request_status'] {
  if (status && status !== 'unknown') return status
  for (let index = series.length - 1; index >= 0; index--) {
    const bucket = series[index]
    if (bucket.request_count <= 0) continue
    if (bucket.success_rate >= 90) return 'healthy'
    if (bucket.success_rate >= 85) return 'unstable'
    return 'failed'
  }
  return 'unknown'
}
