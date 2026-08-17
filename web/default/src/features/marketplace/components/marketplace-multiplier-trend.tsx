import { useMemo, useState } from 'react'
import { TrendingDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useMarketplaceMultiplierTrends } from '../hooks'
import {
  buildChartConfig,
  buildChartRows,
  METRICS,
  RANGES,
  SOURCE_COLORS,
  toggleSource,
} from '../lib/multiplier-trend-data'
import type { MultiplierTrendMetric } from '../types'
import { MultiplierTrendChart } from './marketplace-multiplier-trend-chart'

export function MarketplaceMultiplierTrend(props: {
  model: string
  onModelChange: (model: string) => void
}) {
  const [metric, setMetric] = useState<MultiplierTrendMetric>('reliable_min')
  const [rangeHours, setRangeHours] = useState(24)
  const [selectedSources, setSelectedSources] = useState<string[] | null>(null)
  const query = useMarketplaceMultiplierTrends(rangeHours, props.model)
  const sources = useMemo(
    () => query.data?.sources.map((item) => item.source) ?? [],
    [query.data?.sources]
  )
  const visibleSources = selectedSources
    ? selectedSources.filter((source) => sources.includes(source))
    : sources.slice(0, 4)
  const chartRows = useMemo(
    () => buildChartRows(query.data?.sources ?? [], metric),
    [metric, query.data?.sources]
  )
  const chartConfig = useMemo(() => buildChartConfig(sources), [sources])

  return (
    <section className='border-border border-b'>
      <div className='flex flex-col gap-4 px-4 py-5 sm:px-5'>
        <TrendHeader
          model={props.model}
          models={query.data?.models ?? []}
          rangeHours={rangeHours}
          onModelChange={props.onModelChange}
          onRangeChange={setRangeHours}
        />
        <MetricSelector metric={metric} onChange={setMetric} />
        <MultiplierTrendChart
          loading={query.isLoading}
          error={query.isError}
          rows={chartRows}
          sources={sources}
          visibleSources={visibleSources}
          metric={metric}
          bucketSeconds={query.data?.bucket_seconds ?? 1800}
          config={chartConfig}
          onRetry={() => void query.refetch()}
        />
        <SourceSelector
          sources={sources}
          visibleSources={visibleSources}
          onToggle={(source) =>
            setSelectedSources((current) =>
              toggleSource(current ?? sources.slice(0, 4), source)
            )
          }
        />
      </div>
    </section>
  )
}

function TrendHeader(props: {
  model: string
  models: string[]
  rangeHours: number
  onModelChange: (model: string) => void
  onRangeChange: (hours: number) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='flex flex-wrap items-start justify-between gap-4'>
      <div className='flex min-w-0 items-start gap-3'>
        <span className='bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center rounded-lg'>
          <TrendingDown className='size-4' />
        </span>
        <div>
          <h4 className='text-sm font-semibold'>{t('市场倍率走势')}</h4>
          <p className='text-muted-foreground mt-1 max-w-2xl text-xs leading-5'>
            {t(
              '可靠最低仅统计通过检测、达到样本门槛且 Wilson 成功率不低于 90% 的分组。'
            )}
          </p>
        </div>
      </div>
      <div className='flex flex-wrap items-center gap-2'>
        <ModelSelector {...props} />
        <RangeSelector
          value={props.rangeHours}
          onChange={props.onRangeChange}
        />
      </div>
    </div>
  )
}

function ModelSelector(props: {
  model: string
  models: string[]
  onModelChange: (model: string) => void
}) {
  const { t } = useTranslation()
  return (
    <Select
      value={props.model || '__all__'}
      onValueChange={(value) => {
        if (value) props.onModelChange(value === '__all__' ? '' : value)
      }}
    >
      <SelectTrigger size='sm' className='max-w-52'>
        <SelectValue placeholder={t('全部模型')} />
      </SelectTrigger>
      <SelectContent alignItemWithTrigger={false}>
        <SelectItem value='__all__'>{t('全部模型')}</SelectItem>
        {props.models.map((model) => (
          <SelectItem key={model} value={model}>
            {model}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

function RangeSelector(props: {
  value: number
  onChange: (value: number) => void
}) {
  const { t } = useTranslation()
  return (
    <Tabs
      value={String(props.value)}
      onValueChange={(value) => props.onChange(Number(value))}
    >
      <TabsList className='h-8'>
        {RANGES.map((range) => (
          <TabsTrigger
            key={range.value}
            value={String(range.value)}
            className='h-6 px-2 text-xs'
          >
            {t(range.label)}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  )
}

function MetricSelector(props: {
  metric: MultiplierTrendMetric
  onChange: (metric: MultiplierTrendMetric) => void
}) {
  const { t } = useTranslation()
  return (
    <Tabs
      value={props.metric}
      onValueChange={(value) => props.onChange(value as MultiplierTrendMetric)}
    >
      <TabsList variant='line' className='max-w-full justify-start'>
        {METRICS.map((item) => (
          <TabsTrigger key={item.value} value={item.value}>
            {t(item.label)}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  )
}

function SourceSelector(props: {
  sources: string[]
  visibleSources: string[]
  onToggle: (source: string) => void
}) {
  const { t } = useTranslation()
  if (props.sources.length === 0) return null
  return (
    <div className='flex flex-wrap gap-2' aria-label={t('显示来源')}>
      {props.sources.map((source, index) => {
        const active = props.visibleSources.includes(source)
        return (
          <button
            key={source}
            type='button'
            aria-pressed={active}
            onClick={() => props.onToggle(source)}
            className={cn(
              'border-border focus-visible:ring-ring inline-flex h-8 items-center gap-2 rounded-md border px-2.5 text-xs transition-opacity outline-none focus-visible:ring-2',
              !active && 'opacity-45'
            )}
          >
            <span
              className='size-2 rounded-sm'
              style={{
                backgroundColor: SOURCE_COLORS[index % SOURCE_COLORS.length],
              }}
            />
            {source}
          </button>
        )
      })}
    </div>
  )
}
