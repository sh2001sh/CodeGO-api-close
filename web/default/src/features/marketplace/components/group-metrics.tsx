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
import { useTranslation } from 'react-i18next'
import {
  formatDuration,
  formatMultiplier,
  formatNumber,
  formatPercent,
} from '../lib/format'
import type { MarketplaceGroup } from '../types'

/** 分组卡核心指标：细线分隔的开放数据列，无线框无图标。 */
export function GroupMetrics({ group }: { group: MarketplaceGroup }) {
  const { t } = useTranslation()
  const recentRequests = group.recent_request_series.reduce(
    (total, bucket) => total + bucket.request_count,
    0
  )
  return (
    <div className='codego-marketplace-metrics border-border/60 divide-border/50 grid grid-cols-2 divide-x sm:grid-cols-5'>
      <div className='min-w-0 px-3 first:pl-0'>
        <MetricLabel>{t('倍率')}</MetricLabel>
        <div className='text-primary app-numeric mt-0.5 text-base font-semibold'>
          {formatMultiplier(group.multiplier)}x
        </div>
      </div>
      <div className='min-w-0 px-3 first:pl-0'>
        <MetricLabel>{t('成功率')}</MetricLabel>
        <div className='app-numeric mt-0.5 text-sm font-semibold'>
          {formatPercent(group.success_rate)}
        </div>
      </div>
      <div className='min-w-0 px-3 first:pl-0'>
        <MetricLabel
          title={t(
            '从近窗口原始请求日志中的首字延迟样本按最近邻秩计算，不包含前序失败重试；没有可用日志时才回退到指标桶估算。P95 与端到端耗时可在详情中查看。'
          )}
        >
          {t('首字 P50')}
        </MetricLabel>
        <div
          className='app-numeric mt-0.5 text-sm font-semibold'
          title={
            group.attempt_ttft_p50_ms == null
              ? t('暂无足够样本，展开详情可查看完整检测状态')
              : undefined
          }
        >
          {formatDuration(group.attempt_ttft_p50_ms)}
        </div>
      </div>
      <div className='min-w-0 px-3 first:pl-0'>
        <MetricLabel title={t('近 6 小时实际请求数量')}>
          {t('近期请求')}
        </MetricLabel>
        <div className='app-numeric mt-0.5 text-sm font-semibold'>
          {recentRequests > 0 ? formatNumber(recentRequests) : t('暂无请求')}
        </div>
      </div>
      <div className='min-w-0 px-3 first:pl-0'>
        <MetricLabel title={t('近窗口缓存命中率')}>{t('缓存命中')}</MetricLabel>
        <div className='app-numeric mt-0.5 text-sm font-semibold'>
          {formatCacheHitRate(group.cache_hit_rate)}
        </div>
      </div>
    </div>
  )
}

function MetricLabel(props: { children: React.ReactNode; title?: string }) {
  return (
    <div
      className='text-muted-foreground truncate text-[11px]'
      title={props.title}
    >
      {props.children}
    </div>
  )
}

function formatCacheHitRate(value: number | undefined) {
  return value == null ? '--' : `${value.toFixed(2)}%`
}
