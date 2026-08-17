import {
  Activity,
  CircleDollarSign,
  Gauge,
  ShieldCheck,
  Timer,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatNumber } from '../lib/format'
import type { MarketplaceGroup } from '../types'

export function GroupDetails(props: { group: MarketplaceGroup }) {
  const { t } = useTranslation()
  const group = props.group
  const evidence = [
    {
      icon: ShieldCheck,
      label: t('检测状态'),
      value:
        group.verification_status === 'passed'
          ? t('通过')
          : group.verification_status === 'failed'
            ? t('未通过')
            : group.verification_status === 'running'
              ? t('检测中')
              : group.verification_status || t('未检测'),
    },
    {
      icon: Gauge,
      label: t('综合评分'),
      value: group.observing ? t('观测中') : group.score.toFixed(1),
    },
    {
      icon: CircleDollarSign,
      label: t('基础消耗 1000 时'),
      value: t('余额 {{wallet}} · 套餐 {{subscription}}', {
        wallet: formatNumber(Math.round(1000 * group.multiplier)),
        subscription: formatNumber(
          Math.round(1000 * group.subscription_multiplier)
        ),
      }),
    },
  ]
  return (
    <div className='px-4 py-5 sm:px-6'>
      <div className='border-border grid border-y sm:grid-cols-3'>
        {evidence.map(({ icon: Icon, label, value }) => (
          <div
            key={label}
            className='border-border flex items-center gap-3 border-b px-3 py-3 last:border-r-0 sm:border-r sm:border-b-0'
          >
            <Icon className='text-primary size-4' />
            <div>
              <div className='text-muted-foreground text-xs'>{label}</div>
              <div className='mt-0.5 text-sm font-medium tabular-nums'>
                {value}
              </div>
            </div>
          </div>
        ))}
      </div>
      <div className='mt-4 grid gap-3 text-xs sm:grid-cols-3'>
        <SecondaryMetric
          icon={Timer}
          label={t('总延迟')}
          value={
            group.avg_latency_ms
              ? `${formatNumber(Math.round(group.avg_latency_ms))} ms`
              : '--'
          }
        />
        <SecondaryMetric
          icon={Activity}
          label={t('输出速度')}
          value={group.avg_tps ? `${group.avg_tps.toFixed(1)} TPS` : '--'}
        />
        <SecondaryMetric
          icon={Gauge}
          label={t('缓存命中')}
          value={`${group.cache_hit_rate.toFixed(1)}%`}
        />
      </div>
      {group.observing && (
        <p className='text-muted-foreground mt-3 text-xs'>
          {t('当前仍处于样本观测期，已记录 {{requests}} 次请求。', {
            requests: group.request_count,
          })}
        </p>
      )}
    </div>
  )
}

function SecondaryMetric(props: {
  icon: typeof Gauge
  label: string
  value: string
}) {
  const Icon = props.icon
  return (
    <div className='flex items-center gap-2'>
      <Icon className='text-muted-foreground size-3.5' />
      <span className='text-muted-foreground'>{props.label}</span>
      <span className='ml-auto font-medium tabular-nums'>{props.value}</span>
    </div>
  )
}
