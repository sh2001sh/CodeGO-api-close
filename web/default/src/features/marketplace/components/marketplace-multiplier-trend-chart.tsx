import { Activity, RefreshCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { CartesianGrid, Line, LineChart, Tooltip, XAxis, YAxis } from 'recharts'
import { Button } from '@/components/ui/button'
import { ChartContainer, type ChartConfig } from '@/components/ui/chart'
import { Skeleton } from '@/components/ui/skeleton'
import { formatMultiplier } from '../lib/format'
import {
  formatAxisTime,
  formatTimeRange,
  SOURCE_COLORS,
  type TrendChartRow,
} from '../lib/multiplier-trend-data'
import type { MultiplierTrendMetric } from '../types'

interface MultiplierTrendChartProps {
  loading: boolean
  error: boolean
  rows: TrendChartRow[]
  sources: string[]
  visibleSources: string[]
  metric: MultiplierTrendMetric
  bucketSeconds: number
  config: ChartConfig
  onRetry: () => void
}

export function MultiplierTrendChart(props: MultiplierTrendChartProps) {
  if (props.loading) return <Skeleton className='h-72 w-full rounded-md' />
  if (props.error) return <TrendChartError onRetry={props.onRetry} />
  if (props.rows.length === 0 || props.sources.length === 0)
    return <TrendChartEmpty />
  return <TrendLineChart {...props} />
}

function TrendLineChart(props: MultiplierTrendChartProps) {
  return (
    <ChartContainer
      config={props.config}
      className='aspect-auto h-72 w-full sm:h-80'
    >
      <LineChart
        data={props.rows}
        margin={{ top: 10, right: 12, left: -12, bottom: 0 }}
      >
        <CartesianGrid vertical={false} strokeDasharray='3 3' />
        <XAxis
          dataKey='timestamp'
          type='number'
          domain={['dataMin', 'dataMax']}
          tickFormatter={formatAxisTime}
          tickLine={false}
          axisLine={false}
          minTickGap={28}
        />
        <YAxis
          tickFormatter={(value) => `${formatMultiplier(Number(value))}x`}
          tickLine={false}
          axisLine={false}
          width={54}
          domain={['auto', 'auto']}
        />
        <Tooltip
          content={(tooltipProps) => (
            <TrendTooltip
              active={tooltipProps.active}
              payload={tooltipProps.payload}
              bucketSeconds={props.bucketSeconds}
              metric={props.metric}
            />
          )}
        />
        {props.sources.map((source, index) => (
          <Line
            key={source}
            dataKey={source}
            name={source}
            type='stepAfter'
            stroke={SOURCE_COLORS[index % SOURCE_COLORS.length]}
            strokeWidth={2}
            dot={props.rows.length <= 2 ? { r: 3 } : false}
            activeDot={{ r: 4 }}
            connectNulls={false}
            hide={!props.visibleSources.includes(source)}
            isAnimationActive={false}
          />
        ))}
      </LineChart>
    </ChartContainer>
  )
}

function TrendChartError(props: { onRetry: () => void }) {
  const { t } = useTranslation()
  return (
    <div className='border-border bg-muted/20 flex h-72 flex-col items-center justify-center gap-3 rounded-md border border-dashed text-center'>
      <Activity className='text-muted-foreground size-5' />
      <p className='text-muted-foreground text-sm'>{t('倍率走势加载失败')}</p>
      <Button size='sm' variant='outline' onClick={props.onRetry}>
        <RefreshCcw />
        {t('重试')}
      </Button>
    </div>
  )
}

function TrendChartEmpty() {
  const { t } = useTranslation()
  return (
    <div className='border-border bg-muted/20 flex h-72 items-center justify-center rounded-md border border-dashed px-6 text-center'>
      <p className='text-muted-foreground max-w-lg text-sm leading-6'>
        {t('暂无倍率历史，市场刷新后将从当前时间开始记录。')}
      </p>
    </div>
  )
}

function TrendTooltip(props: {
  active?: boolean
  payload?: ReadonlyArray<{
    name?: unknown
    value?: unknown
    payload?: unknown
    color?: string
  }>
  bucketSeconds: number
  metric: MultiplierTrendMetric
}) {
  const { t } = useTranslation()
  if (!props.active || !props.payload?.length) return null
  const row = props.payload[0]?.payload as TrendChartRow | undefined
  if (!row) return null
  const start = new Date(row.timestamp * 1000)
  const end = new Date((row.timestamp + props.bucketSeconds) * 1000)
  return (
    <div className='border-border bg-background min-w-56 rounded-md border p-3 text-xs shadow-xl'>
      <div className='font-medium'>{formatTimeRange(start, end)}</div>
      <div className='mt-2 grid gap-2'>
        {props.payload.map((item) => {
          const source = String(item.name ?? '')
          const point = row.values[source]
          if (!point || item.value == null) return null
          return (
            <div key={source} className='grid gap-1'>
              <div className='flex items-center justify-between gap-4'>
                <span className='flex items-center gap-2'>
                  <span
                    className='size-2 rounded-sm'
                    style={{ backgroundColor: item.color }}
                  />
                  {source}
                </span>
                <strong className='tabular-nums'>
                  {formatMultiplier(Number(item.value))}x
                </strong>
              </div>
              <div className='text-muted-foreground pl-4'>
                {props.metric === 'reliable_min'
                  ? t('{{eligible}} / {{total}} 个可靠候选', {
                      eligible: point.eligible_count,
                      total: point.total_count,
                    })
                  : t('共 {{total}} 个挂牌分组', { total: point.total_count })}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
