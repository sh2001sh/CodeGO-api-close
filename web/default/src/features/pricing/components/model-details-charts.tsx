/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useMemo } from 'react'
import { getIntlLocale } from '@/i18n/format'
import { VChart } from '@visactor/react-vchart'
import { useTranslation } from 'react-i18next'
import { useThemeRadiusPx } from '@/lib/theme-radius'
import { useChartTheme } from '@/lib/use-chart-theme'
import { cn } from '@/lib/utils'
import { VCHART_OPTION } from '@/lib/vchart'
import { useThemeCustomization } from '@/context/theme-customization-provider'
import type { LatencyTimePoint, UptimeDayPoint } from '../lib/mock-stats'

function formatHourLabel(iso: string): string {
  const date = new Date(iso)
  const hours = date.getHours()
  return `${String(hours).padStart(2, '0')}:00`
}

function formatDayLabel(date: string): string {
  const parsed = new Date(date)
  if (date.includes('T')) {
    return parsed.toLocaleString(getIntlLocale(), {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
    })
  }
  return parsed.toLocaleDateString(getIntlLocale(), {
    month: 'short',
    day: 'numeric',
  })
}

// ---------------------------------------------------------------------------
// Latency trend chart (24h, multi-group point-line chart)
// ---------------------------------------------------------------------------

export function LatencyTrendChart(props: {
  series: LatencyTimePoint[]
  className?: string
}) {
  const { t } = useTranslation()
  const { resolvedTheme, themeReady } = useChartTheme()

  const spec = useMemo(() => {
    if (props.series.length === 0) return null
    const data = props.series.map((point) => ({
      time: formatHourLabel(point.timestamp),
      group: point.group,
      ttft: point.ttft_ms,
    }))
    return {
      type: 'line' as const,
      animation: false,
      color: [resolvedTheme === 'dark' ? '#f3a27f' : '#b8562e'],
      data: [{ id: 'latency', values: data }],
      xField: 'time',
      yField: 'ttft',
      seriesField: 'group',
      smooth: false,
      point: {
        visible: true,
        style: { size: 5, stroke: '#ffffff', lineWidth: 1.5 },
      },
      line: {
        style: { lineWidth: 2 },
      },
      legends: { visible: false },
      tooltip: {
        mark: {
          title: { value: (d: { time: string }) => d.time },
          content: [
            {
              key: t('Average TTFT'),
              value: (d: { ttft: number }) => `${Math.round(d.ttft)} ms`,
            },
          ],
        },
      },
      axes: [
        {
          orient: 'bottom',
          label: {
            style: { fill: 'currentColor', fontSize: 10 },
          },
          tick: { visible: false },
        },
        {
          orient: 'left',
          label: {
            formatMethod: (val: number | string) => `${val} ms`,
            style: { fill: 'currentColor', fontSize: 10 },
          },
          grid: { visible: true, style: { lineDash: [3, 3] } },
        },
      ],
    }
  }, [props.series, t, resolvedTheme])

  if (props.series.length === 0) {
    return (
      <div
        className={cn(
          'text-muted-foreground flex h-48 items-center justify-center rounded-lg border text-xs',
          props.className
        )}
      >
        {t('No latency data available')}
      </div>
    )
  }

  return (
    <div className={props.className}>
      <div className='h-64 sm:h-72' aria-hidden='true'>
        {themeReady && spec && (
          <VChart
            key={`latency-${resolvedTheme}`}
            spec={{
              ...spec,
              theme: resolvedTheme === 'dark' ? 'dark' : 'light',
              background: 'transparent',
            }}
            option={VCHART_OPTION}
          />
        )}
      </div>
      <details className='model-source-details'>
        <summary>{t('数据')}</summary>
        <div className='overflow-x-auto'>
          <table className='w-full text-sm'>
            <thead>
              <tr>
                <th>{t('时间')}</th>
                <th>{t('Group')}</th>
                <th>{t('Average TTFT')}</th>
              </tr>
            </thead>
            <tbody>
              {props.series.map((point) => (
                <tr key={`${point.timestamp}-${point.group}`}>
                  <td>
                    {new Date(point.timestamp).toLocaleString(getIntlLocale())}
                  </td>
                  <td>{point.group}</td>
                  <td>{Math.round(point.ttft_ms)} ms</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Uptime trend chart (24h, point-line chart)
// ---------------------------------------------------------------------------

export function UptimeTrendChart(props: {
  series: UptimeDayPoint[]
  className?: string
}) {
  const { t } = useTranslation()
  const { resolvedTheme, themeReady } = useChartTheme()

  const spec = useMemo(() => {
    if (props.series.length === 0) return null

    const data = props.series.map((point) => ({
      date: formatDayLabel(point.date),
      uptime: point.uptime_pct,
      incidents: point.incidents,
      outage: point.outage_minutes,
    }))

    return {
      type: 'line' as const,
      animation: false,
      data: [{ id: 'uptime', values: data }],
      xField: 'date',
      yField: 'uptime',
      smooth: false,
      line: {
        style: {
          stroke: resolvedTheme === 'dark' ? '#8ad7ad' : '#17603a',
          lineWidth: 2,
        },
      },
      point: {
        visible: true,
        style: {
          size: 5,
          stroke: '#ffffff',
          lineWidth: 1.5,
          fill: (datum: { uptime: number }) => {
            if (datum.uptime >= 99.9)
              return resolvedTheme === 'dark' ? '#8ad7ad' : '#17603a'
            if (datum.uptime >= 99.0)
              return resolvedTheme === 'dark' ? '#f1c36f' : '#7b4b00'
            return resolvedTheme === 'dark' ? '#ffaaa4' : '#a72727'
          },
        },
      },
      tooltip: {
        mark: {
          title: { value: (d: { date: string }) => d.date },
          content: [
            {
              key: t('Success rate'),
              value: (d: { uptime: number }) => `${d.uptime.toFixed(2)}%`,
            },
          ],
        },
      },
      axes: [
        {
          orient: 'bottom',
          label: {
            style: { fill: 'currentColor', fontSize: 10 },
            autoLimit: true,
          },
          tick: { visible: false },
        },
        {
          orient: 'left',
          min: 0,
          max: 100,
          label: {
            formatMethod: (val: number | string) => `${val}%`,
            style: { fill: 'currentColor', fontSize: 10 },
          },
          grid: { visible: true, style: { lineDash: [3, 3] } },
        },
      ],
    }
  }, [props.series, t, resolvedTheme])

  if (props.series.length === 0) {
    return (
      <div
        className={cn(
          'text-muted-foreground flex h-48 items-center justify-center rounded-lg border text-xs',
          props.className
        )}
      >
        {t('No uptime data available')}
      </div>
    )
  }

  return (
    <div className={props.className}>
      <div className='h-56 sm:h-64' aria-hidden='true'>
        {themeReady && spec && (
          <VChart
            key={`uptime-trend-${resolvedTheme}`}
            spec={{
              ...spec,
              theme: resolvedTheme === 'dark' ? 'dark' : 'light',
              background: 'transparent',
            }}
            option={VCHART_OPTION}
          />
        )}
      </div>
      <details className='model-source-details'>
        <summary>{t('数据')}</summary>
        <div className='overflow-x-auto'>
          <table className='w-full text-sm'>
            <thead>
              <tr>
                <th>{t('时间')}</th>
                <th>{t('Success rate')}</th>
              </tr>
            </thead>
            <tbody>
              {props.series.map((point) => (
                <tr key={point.date}>
                  <td>
                    {new Date(point.date).toLocaleString(getIntlLocale())}
                  </td>
                  <td>{point.uptime_pct.toFixed(2)}%</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Throughput by group (horizontal bar)
// ---------------------------------------------------------------------------

export function ThroughputBarChart(props: {
  rows: { group: string; throughput_tps: number }[]
  className?: string
}) {
  const { t } = useTranslation()
  const { resolvedTheme, themeReady } = useChartTheme()
  const { customization } = useThemeCustomization()
  const barRadius = useThemeRadiusPx(
    '--radius-sm',
    `${customization.preset}:${customization.radius}`
  )

  const filtered = useMemo(
    () => props.rows.filter((r) => r.throughput_tps > 0),
    [props.rows]
  )

  const spec = useMemo(() => {
    if (filtered.length === 0) return null
    return {
      type: 'bar' as const,
      animation: false,
      direction: 'horizontal' as const,
      data: [{ id: 'tput', values: filtered.map((r) => ({ ...r })) }],
      xField: 'throughput_tps',
      yField: 'group',
      bar: {
        style: {
          fill: resolvedTheme === 'dark' ? '#f3a27f' : '#b8562e',
          ...(barRadius == null ? {} : { cornerRadius: barRadius }),
        },
      },
      label: {
        visible: true,
        position: 'right',
        style: { fontSize: 11, fill: 'currentColor' },
        formatMethod: (text: string) => `${text} t/s`,
      },
      axes: [
        {
          orient: 'left',
          label: { style: { fill: 'currentColor', fontSize: 10 } },
          tick: { visible: false },
        },
        {
          orient: 'bottom',
          label: { style: { fill: 'currentColor', fontSize: 10 } },
          grid: { visible: true, style: { lineDash: [3, 3] } },
        },
      ],
      tooltip: {
        mark: {
          title: { value: (d: { group: string }) => d.group },
          content: [
            {
              key: t('Throughput'),
              value: (d: { throughput_tps: number }) =>
                `${d.throughput_tps.toFixed(1)} t/s`,
            },
          ],
        },
      },
    }
  }, [barRadius, filtered, t, resolvedTheme])

  if (filtered.length === 0) {
    return null
  }

  return (
    <div className={cn('h-48 sm:h-56', props.className)}>
      {themeReady && spec && (
        <VChart
          key={`tput-${resolvedTheme}`}
          spec={{
            ...spec,
            theme: resolvedTheme === 'dark' ? 'dark' : 'light',
            background: 'transparent',
          }}
          option={VCHART_OPTION}
        />
      )}
    </div>
  )
}
