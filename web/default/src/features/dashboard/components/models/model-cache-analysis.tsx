import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { VChart } from '@visactor/react-vchart'
import { DatabaseZap } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatCompactNumber, formatNumber } from '@/lib/format'
import { VCHART_OPTION } from '@/lib/vchart'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { getPerfMetricsSummary } from '@/features/performance-metrics/api'
import type { PerfModelSummary } from '@/features/performance-metrics/types'

const CACHE_WINDOW_HOURS = 24

export function ModelCacheAnalysis() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['perf-metrics-cache-summary', CACHE_WINDOW_HOURS],
    queryFn: () => getPerfMetricsSummary(CACHE_WINDOW_HOURS),
    staleTime: 60 * 1000,
    retry: false,
  })
  const rows = useMemo(
    () => buildRows(query.data?.data.models ?? []),
    [query.data]
  )
  const summary = useMemo(() => summarize(rows), [rows])

  if (query.isLoading) return <CacheAnalysisSkeleton />
  if (query.isError) {
    return (
      <div className='text-destructive rounded-lg border px-5 py-12 text-center text-sm'>
        {t('缓存分析数据加载失败，请稍后重试。')}
      </div>
    )
  }
  if (rows.length === 0) {
    return (
      <div className='text-muted-foreground rounded-lg border px-5 py-16 text-center text-sm'>
        {t('当前时间段暂无缓存 Token 数据。')}
      </div>
    )
  }

  return (
    <div className='space-y-3'>
      <CacheSummary summary={summary} />
      <div className='grid gap-3 xl:grid-cols-[minmax(0,0.9fr)_minmax(0,1.5fr)]'>
        <CacheRateChart rows={rows} />
        <CacheDetailsTable rows={rows} requestCount={summary.requests} />
      </div>
    </div>
  )
}

function CacheSummary(props: { summary: ReturnType<typeof summarize> }) {
  const { t } = useTranslation()
  return (
    <div className='grid overflow-hidden rounded-lg border sm:grid-cols-3 sm:divide-x'>
      <SummaryMetric
        label={t('综合缓存命中率')}
        value={`${props.summary.hitRate.toFixed(1)}%`}
      />
      <SummaryMetric
        label={t('缓存读取 Token')}
        value={formatCompactNumber(props.summary.cacheRead)}
      />
      <SummaryMetric
        label={t('缓存写入 Token')}
        value={formatCompactNumber(props.summary.cacheWrite)}
      />
    </div>
  )
}

function CacheRateChart(props: { rows: CacheRow[] }) {
  const { t } = useTranslation()
  return (
    <section className='overflow-hidden rounded-lg border'>
      <header className='flex items-center gap-2 border-b px-4 py-3'>
        <DatabaseZap className='text-primary size-4' />
        <div>
          <h2 className='text-sm font-semibold'>{t('模型缓存命中率')}</h2>
          <p className='text-muted-foreground mt-0.5 text-xs'>
            {t('按近 24 小时缓存命中率排序，最多展示 12 个模型。')}
          </p>
        </div>
      </header>
      <div className='h-[360px] p-2'>
        <VChart spec={buildChartSpec(props.rows)} option={VCHART_OPTION} />
      </div>
    </section>
  )
}

function CacheDetailsTable(props: { rows: CacheRow[]; requestCount: number }) {
  const { t } = useTranslation()
  const labels = [
    t('模型'),
    t('命中率'),
    t('普通输入'),
    t('缓存读取'),
    t('缓存写入'),
    t('请求数'),
  ]
  return (
    <section className='overflow-hidden rounded-lg border'>
      <header className='flex items-center justify-between gap-3 border-b px-4 py-3'>
        <div>
          <h2 className='text-sm font-semibold'>{t('缓存使用明细')}</h2>
          <p className='text-muted-foreground mt-0.5 text-xs'>
            {t('{{models}} 个模型 · {{requests}} 次请求', {
              models: props.rows.length,
              requests: formatNumber(props.requestCount),
            })}
          </p>
        </div>
        <Badge variant='outline'>{t('近 24 小时')}</Badge>
      </header>
      <div className='max-h-[360px] overflow-auto'>
        <table className='w-full min-w-[760px] text-sm'>
          <thead className='bg-muted/40 text-muted-foreground sticky top-0 text-xs'>
            <tr>
              {labels.map((label) => (
                <th
                  key={label}
                  className='px-3 py-2.5 text-right font-medium first:text-left'
                >
                  {label}
                </th>
              ))}
            </tr>
          </thead>
          <tbody className='divide-y'>
            {props.rows.map((row) => (
              <CacheDetailsRow key={row.model} row={row} />
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}

function CacheDetailsRow(props: { row: CacheRow }) {
  const row = props.row
  return (
    <tr className='hover:bg-muted/30'>
      <td
        className='max-w-64 truncate px-3 py-3 font-mono text-xs'
        title={row.model}
      >
        {row.model}
      </td>
      <td className='px-3 py-3 text-right font-semibold tabular-nums'>
        {row.cacheHitRate.toFixed(2)}%
      </td>
      <td className='px-3 py-3 text-right tabular-nums'>
        {formatCompactNumber(row.inputTokens)}
      </td>
      <td className='px-3 py-3 text-right tabular-nums'>
        {formatCompactNumber(row.cacheReadTokens)}
      </td>
      <td className='px-3 py-3 text-right tabular-nums'>
        {formatCompactNumber(row.cacheWriteTokens)}
      </td>
      <td className='px-3 py-3 text-right tabular-nums'>
        {formatNumber(row.requestCount)}
      </td>
    </tr>
  )
}

function buildChartSpec(rows: CacheRow[]) {
  const values = rows
    .slice(0, 12)
    .map((row) => ({ model: row.model, hitRate: row.cacheHitRate }))
  return {
    type: 'bar',
    direction: 'horizontal',
    data: [{ id: 'cache-rate', values }],
    xField: 'hitRate',
    yField: 'model',
    bar: { style: { fill: '#B85834', cornerRadius: 3 } },
    axes: [
      {
        orient: 'bottom',
        min: 0,
        max: 100,
        label: { formatter: (value: string) => `${value}%` },
      },
      { orient: 'left', label: { maxLineWidth: 190 } },
    ],
    tooltip: {
      mark: {
        content: [
          {
            key: (datum: { model: string }) => datum.model,
            value: (datum: { hitRate: number }) =>
              `${datum.hitRate.toFixed(2)}%`,
          },
        ],
      },
    },
    padding: { left: 10, right: 24, top: 8, bottom: 8 },
  }
}

type CacheRow = {
  model: string
  cacheHitRate: number
  inputTokens: number
  cacheReadTokens: number
  cacheWriteTokens: number
  requestCount: number
}

function buildRows(models: PerfModelSummary[]): CacheRow[] {
  return models
    .map((model) => ({
      model: model.model_name,
      cacheHitRate: Number(model.cache_hit_rate) || 0,
      inputTokens: Number(model.input_tokens) || 0,
      cacheReadTokens: Number(model.cache_read_tokens) || 0,
      cacheWriteTokens: Number(model.cache_write_tokens) || 0,
      requestCount: Number(model.request_count) || 0,
    }))
    .filter(
      (row) => row.inputTokens + row.cacheReadTokens + row.cacheWriteTokens > 0
    )
    .sort(
      (left, right) =>
        right.cacheHitRate - left.cacheHitRate ||
        right.cacheReadTokens - left.cacheReadTokens
    )
}

function summarize(rows: CacheRow[]) {
  const values = rows.reduce(
    (sum, row) => ({
      input: sum.input + row.inputTokens,
      cacheRead: sum.cacheRead + row.cacheReadTokens,
      cacheWrite: sum.cacheWrite + row.cacheWriteTokens,
      requests: sum.requests + row.requestCount,
    }),
    { input: 0, cacheRead: 0, cacheWrite: 0, requests: 0 }
  )
  const total = values.input + values.cacheRead + values.cacheWrite
  return {
    ...values,
    hitRate: total > 0 ? (values.cacheRead / total) * 100 : 0,
  }
}

function SummaryMetric(props: { label: string; value: string }) {
  return (
    <div className='px-4 py-3'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='mt-1 text-xl font-semibold tabular-nums'>
        {props.value}
      </div>
    </div>
  )
}

function CacheAnalysisSkeleton() {
  return (
    <div className='space-y-3'>
      <Skeleton className='h-20 w-full rounded-lg' />
      <div className='grid gap-3 xl:grid-cols-2'>
        <Skeleton className='h-[430px] w-full rounded-lg' />
        <Skeleton className='h-[430px] w-full rounded-lg' />
      </div>
    </div>
  )
}
