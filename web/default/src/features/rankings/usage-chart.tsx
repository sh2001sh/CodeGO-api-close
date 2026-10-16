import { useMemo } from 'react'
import { getIntlLocale } from '@/i18n/format'
import { toIntlLocale } from '@/i18n/languages'
import { VChart } from '@visactor/react-vchart'
import { useTranslation } from 'react-i18next'
import { useChartTheme } from '@/lib/use-chart-theme'
import { VCHART_OPTION } from '@/lib/vchart'
import type { PerformanceGroup } from '@/features/performance-metrics/types'
import { toUsageSeries, type UsageMetric } from './data'

export function UsageChart(props: {
  groups: PerformanceGroup[]
  metric: UsageMetric
  hours: number
}) {
  const { t, i18n } = useTranslation()
  const { resolvedTheme, themeReady } = useChartTheme()
  const points = useMemo(
    () => toUsageSeries(props.groups, props.metric),
    [props.groups, props.metric]
  )
  const metricLabel =
    props.metric === 'request_count'
      ? t('rankings.requestsMetric')
      : t('rankings.inputTokens')
  const spec = useMemo(() => {
    const locale = toIntlLocale(i18n.language)
    const number = new Intl.NumberFormat(locale, {
      notation: 'compact',
      maximumFractionDigits: 1,
    })
    const date = new Intl.DateTimeFormat(
      locale,
      props.hours > 24
        ? { month: 'short', day: 'numeric', hour: '2-digit' }
        : { hour: '2-digit', minute: '2-digit' }
    )
    const dark = resolvedTheme === 'dark'
    const text = dark ? '#b7b6c0' : '#55555e'
    const grid = dark ? '#2c2c32' : '#e9e6e0'
    const timestamps = [...new Set(points.map((point) => point.timestamp))]
    let interval = Infinity
    for (let index = 1; index < timestamps.length; index++) {
      interval = Math.min(interval, timestamps[index] - timestamps[index - 1])
    }
    if (!Number.isFinite(interval)) interval = 3600
    // Width is a display choice; missing buckets remain absent observations.
    const values = points.map((point) => ({
      ...point,
      barStart: point.timestamp - interval * 0.43,
      barEnd: point.timestamp + interval * 0.43,
    }))
    return {
      type: 'bar' as const,
      animation: false,
      data: [{ id: 'usage', values }],
      xField: 'barStart',
      // VChart's root bar transformer does not forward x2Field to its series.
      series: [{ type: 'bar' as const, x2Field: 'barEnd' }],
      yField: 'value',
      seriesField: 'group',
      stack: true,
      color: dark
        ? ['#ff995c', '#b7c4c0', '#d4aa65', '#8c9aa6', '#ac9dc0']
        : ['#ea580c', '#849e94', '#d7ac62', '#3b4652', '#aca3be'],
      bar: { style: { cornerRadius: 1 } },
      legends: {
        visible: true,
        orient: 'bottom',
        item: { label: { style: { fill: text, fontSize: 12 } } },
      },
      axes: [
        {
          orient: 'bottom',
          type: 'linear',
          zero: false,
          nice: false,
          min: timestamps[0] - interval / 2,
          max: timestamps[timestamps.length - 1] + interval / 2,
          label: {
            formatMethod: (value: number | string) =>
              date.format(Number(value) * 1000),
            style: { fill: text, fontSize: 12 },
            autoHide: true,
            autoRotate: false,
          },
          tick: { visible: false },
          domainLine: { visible: false },
        },
        {
          orient: 'left',
          min: 0,
          label: {
            formatMethod: (value: number | string) =>
              number.format(Number(value)),
            style: { fill: text, fontSize: 12 },
          },
          tick: { visible: false },
          domainLine: { visible: false },
          grid: { visible: true, style: { stroke: grid, lineDash: [3, 3] } },
        },
      ],
      tooltip: {
        mark: {
          title: {
            value: (datum: { timestamp: number }) =>
              new Date(datum.timestamp * 1000).toLocaleString(getIntlLocale()),
          },
          content: [
            {
              key: (datum: { group: string }) => datum.group,
              value: (datum: { value: number }) =>
                `${new Intl.NumberFormat(getIntlLocale()).format(datum.value)} ${metricLabel}`,
            },
          ],
        },
      },
    }
  }, [points, resolvedTheme, props.hours, metricLabel, i18n.language])

  if (!points.length)
    return (
      <div className='rankings-chart-placeholder'>
        {t('rankings.trendEmpty')}
      </div>
    )
  return (
    <>
      <div className='rankings-usage-chart' aria-hidden='true'>
        {themeReady && (
          <VChart
            key={`usage-${resolvedTheme}`}
            spec={{
              ...spec,
              theme: resolvedTheme === 'dark' ? 'dark' : 'light',
              background: 'transparent',
            }}
            option={VCHART_OPTION}
          />
        )}
      </div>
      <details className='rankings-chart-data'>
        <summary>{t('rankings.chartTable')}</summary>
        <div className='rankings-data-scroll'>
          <table>
            <caption className='sr-only'>{t('rankings.usageTrend')}</caption>
            <thead>
              <tr>
                <th scope='col'>{t('时间')}</th>
                <th scope='col'>{t('Group')}</th>
                <th scope='col'>{metricLabel}</th>
              </tr>
            </thead>
            <tbody>
              {points.map((point) => (
                <tr key={`${point.group}-${point.timestamp}`}>
                  <td>
                    {new Date(point.timestamp * 1000).toLocaleString(
                      getIntlLocale()
                    )}
                  </td>
                  <th scope='row'>{point.group}</th>
                  <td>
                    {new Intl.NumberFormat(getIntlLocale()).format(point.value)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
    </>
  )
}
