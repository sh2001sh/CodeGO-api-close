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
import { memo } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  formatDuration,
  formatMultiplier,
  formatNumber,
  formatPercent,
} from '../lib/format'
import type { MarketplaceGroup } from '../types'

/** 分组卡核心指标：细线分隔的开放数据列，无线框无图标。 */
export const GroupMetrics = memo(function GroupMetrics(props: {
  group: MarketplaceGroup
}) {
  const { t } = useTranslation()
  const recentRequests = props.group.recent_request_series.reduce(
    (total, bucket) => total + bucket.request_count,
    0
  )
  return (
    <div className='codego-marketplace-metrics flex min-w-0 flex-wrap items-baseline gap-x-5 gap-y-1'>
      <Metric
        label={t('倍率')}
        value={`${formatMultiplier(props.group.multiplier)}x`}
        tone='text-primary text-base font-semibold'
      />
      <Metric
        label={t('成功率')}
        value={formatPercent(props.group.success_rate)}
      />
      <Metric
        label={t('首字 P50')}
        value={formatDuration(props.group.attempt_ttft_p50_ms)}
        title={
          props.group.attempt_ttft_p50_ms == null
            ? t('暂无足够样本，展开详情可查看完整检测状态')
            : undefined
        }
      />
      <Metric
        label={t('近期请求')}
        value={
          recentRequests > 0 ? formatNumber(recentRequests) : t('暂无请求')
        }
        muted={recentRequests <= 0}
      />
      <Metric
        label={t('并发')}
        value={`${formatNumber(props.group.current_concurrency)} / ${formatConcurrencyLimit(props.group.max_concurrency, t('不限'))}`}
        danger={
          props.group.max_concurrency > 0 &&
          props.group.current_concurrency >= props.group.max_concurrency
        }
      />
      <Metric
        label={t('缓存命中')}
        value={formatCacheHitRate(props.group.cache_hit_rate)}
        muted={props.group.cache_hit_rate == null}
      />
    </div>
  )
})

function Metric(props: {
  label: string
  value: string
  title?: string
  tone?: string
  muted?: boolean
  danger?: boolean
}) {
  return (
    <span
      className='flex min-w-0 items-baseline gap-1.5 whitespace-nowrap'
      title={props.title}
    >
      <span className='codego-stat-label'>{props.label}</span>
      <span
        className={cn(
          'text-sm font-semibold tabular-nums',
          props.tone ||
            (props.danger
              ? 'text-destructive'
              : props.muted
                ? 'text-muted-foreground/70'
                : 'text-foreground')
        )}
      >
        {props.value}
      </span>
    </span>
  )
}

function formatCacheHitRate(value: number | undefined) {
  return value == null ? '--' : `${value.toFixed(2)}%`
}

function formatConcurrencyLimit(value: number, fallback: string) {
  return value > 0 ? formatNumber(value) : fallback
}
