import type { ChartConfig } from '@/components/ui/chart'
import type {
  MarketplaceMultiplierTrendPoint,
  MarketplaceMultiplierTrendSource,
  MultiplierTrendMetric,
} from '../types'

// Dawn-brand categorical palette: copper leads, cool slate and sage give
// adjacent series separation without leaving the homepage color world.
export const SOURCE_COLORS = [
  '#B8562E', // copper
  '#5B7CA0', // slate blue
  '#C9A227', // ochre
  '#7D8471', // sage
  '#8A6A4A', // walnut
  '#A2547A', // dusk plum
] as const

export const METRICS: Array<{ value: MultiplierTrendMetric; label: string }> = [
  { value: 'reliable_min', label: '可靠最低' },
  { value: 'listed_min', label: '挂牌最低' },
  { value: 'median', label: '中位倍率' },
]

export const RANGES = [
  { value: 24, label: '24 小时' },
  { value: 24 * 7, label: '7 天' },
  { value: 24 * 30, label: '30 天' },
]

export interface TrendChartRow {
  timestamp: number
  values: Record<string, MarketplaceMultiplierTrendPoint>
  [source: string]:
    | number
    | Record<string, MarketplaceMultiplierTrendPoint>
    | null
}

export function buildChartRows(
  sources: MarketplaceMultiplierTrendSource[],
  metric: MultiplierTrendMetric
) {
  const rows = new Map<number, TrendChartRow>()
  for (const source of sources) {
    for (const point of source.points) {
      const row: TrendChartRow = rows.get(point.timestamp) ?? {
        timestamp: point.timestamp,
        values: {},
      }
      row[source.source] = point[metric]
      row.values[source.source] = point
      rows.set(point.timestamp, row)
    }
  }
  return [...rows.values()].sort((a, b) => a.timestamp - b.timestamp)
}

export function buildChartConfig(sources: string[]): ChartConfig {
  return Object.fromEntries(
    sources.map((source, index) => [
      source,
      { label: source, color: SOURCE_COLORS[index % SOURCE_COLORS.length] },
    ])
  )
}

export function toggleSource(current: string[], source: string) {
  return current.includes(source)
    ? current.filter((item) => item !== source)
    : [...current, source]
}

export function formatAxisTime(timestamp: number) {
  const date = new Date(timestamp * 1000)
  return `${date.getMonth() + 1}/${date.getDate()} ${String(date.getHours()).padStart(2, '0')}:00`
}

export function formatTimeRange(start: Date, end: Date) {
  const formatter = new Intl.DateTimeFormat(undefined, {
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
  return `${formatter.format(start)} - ${formatter.format(end)}`
}
