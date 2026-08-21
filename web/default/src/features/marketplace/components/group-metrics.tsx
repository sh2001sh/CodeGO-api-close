import { CircleDollarSign, Gauge, Layers3, Timer } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  formatDuration,
  formatMultiplier,
  formatNumber,
  formatPercent,
} from '../lib/format'
import type { MarketplaceGroup } from '../types'

export function GroupMetrics({ group }: { group: MarketplaceGroup }) {
  const { t } = useTranslation()
  return (
    <div className='grid grid-cols-2 gap-x-4 gap-y-2 sm:grid-cols-4'>
      <PrimaryMetric
        icon={CircleDollarSign}
        label={t('倍率')}
        value={`${formatMultiplier(group.multiplier)}x`}
        emphasized
      />
      <PrimaryMetric
        icon={Gauge}
        label={t('成功率')}
        value={formatPercent(group.success_rate)}
      />
      <PrimaryMetric
        icon={Timer}
        label={t('上游首字 P50（精确）')}
        value={formatDuration(group.attempt_ttft_p50_ms)}
        title={t(
          '从近窗口原始请求日志中的首字延迟样本按最近邻秩计算，不包含前序失败重试；没有可用日志时才回退到指标桶估算。P95 与端到端耗时可在详情中查看。'
        )}
      />
      <PrimaryMetric
        icon={Layers3}
        label={t('并发')}
        value={`${formatNumber(group.current_concurrency)} / ${formatConcurrencyLimit(group.max_concurrency, t('不限'))}`}
      />
    </div>
  )
}

function formatConcurrencyLimit(limit: number | undefined, unlimited: string) {
  return !limit ? unlimited : formatNumber(limit)
}

function PrimaryMetric(props: {
  icon: typeof Gauge
  label: string
  value: string
  title?: string
  emphasized?: boolean
}) {
  const Icon = props.icon
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground flex items-center gap-1 text-[11px]'>
        <Icon className='size-3' />
        <span title={props.title}>{props.label}</span>
      </div>
      <div
        className={
          props.emphasized
            ? 'text-primary mt-0.5 text-base font-semibold tabular-nums'
            : 'mt-0.5 text-sm font-semibold tabular-nums'
        }
      >
        {props.value}
      </div>
    </div>
  )
}
