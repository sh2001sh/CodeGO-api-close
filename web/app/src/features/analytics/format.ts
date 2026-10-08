// Display helpers for the oversized int64 fields returned by audit/log
// endpoints (typed as number | string | bigint by the generated client).

export function displayInt(value: number | string | bigint | undefined | null): string {
  if (value === undefined || value === null) return '—'
  try {
    return BigInt(value).toLocaleString()
  } catch {
    return String(value)
  }
}

/** For chart buckets only; counts/tokens in a single page stay well inside
 * the safe integer range, so this never needs bigint precision. */
export function toChartNumber(value: number | string | bigint | undefined | null): number {
  if (value === undefined || value === null) return 0
  const n = Number(value)
  return Number.isFinite(n) ? n : 0
}
