import type {
  PerformanceGroup,
  PerfModelSummary,
} from '@/features/performance-metrics/types'

export type UsageMetric = 'request_count' | 'input_tokens'

export function rankModels(
  models: PerfModelSummary[],
  metric: UsageMetric,
  search = ''
): PerfModelSummary[] {
  const needle = search.trim().toLocaleLowerCase()
  return models
    .filter(
      (model) =>
        Number.isFinite(model.request_count) &&
        model.request_count > 0 &&
        Number.isFinite(model[metric]) &&
        model[metric] >= 0 &&
        model.model_name.toLocaleLowerCase().includes(needle)
    )
    .sort(
      (a: PerfModelSummary, b: PerfModelSummary) =>
        b[metric] - a[metric] || a.model_name.localeCompare(b.model_name)
    )
}

export type UsagePoint = { timestamp: number; group: string; value: number }

// Preserve sparse server buckets: absence is not an observed zero.
export function toUsageSeries(
  groups: PerformanceGroup[],
  metric: UsageMetric
): UsagePoint[] {
  const points = new Map<string, UsagePoint>()
  for (const group of groups) {
    for (const point of group.series) {
      const value = point[metric]
      if (
        !Number.isFinite(point.ts) ||
        point.ts <= 0 ||
        !Number.isFinite(value) ||
        value < 0
      )
        continue
      const key = `${group.group}\u0000${point.ts}`
      const previous = points.get(key)
      points.set(key, {
        timestamp: point.ts,
        group: group.group,
        value: (previous?.value ?? 0) + value,
      })
    }
  }
  return [...points.values()].sort(
    (a, b) => a.timestamp - b.timestamp || a.group.localeCompare(b.group)
  )
}
