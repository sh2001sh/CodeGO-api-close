import { CircleDollarSign, Gauge, ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { formatNumber } from '../lib/format'
import type { MarketplaceGroup } from '../types'
import { TokenBindPanel } from './token-bind-panel'
import { ModelConnectivityResults, ModelConsistencyBadge } from './model-verification'

export function GroupDetails(props: { group: MarketplaceGroup }) {
  const { t } = useTranslation()
  const group = props.group
  const evidence = [
    {
      icon: ShieldCheck,
      label: t('检测状态'),
      value: group.verification_status,
    },
    {
      icon: Gauge,
      label: t('综合评分'),
      value: group.observing ? t('观测中') : group.score.toFixed(1),
    },
    {
      icon: CircleDollarSign,
      label: t('基础消耗 1000 时'),
      value: t('实际扣除 {{amount}} 通用额度', {
        amount: formatNumber(Math.round(1000 * group.multiplier)),
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
      <div className='mt-4 flex flex-wrap gap-1.5'>
        <ModelConsistencyBadge status={group.model_consistency_status} />
        {group.models.map((model) => (
          <Badge key={model} variant='secondary'>
            {model}
          </Badge>
        ))}
      </div>
      <ModelConnectivityResults results={group.model_verification_results} />
      {group.observing && (
        <p className='text-muted-foreground mt-3 text-xs'>
          {t('样本进度：{{requests}}/100 请求 · {{users}}/10 用户', {
            requests: group.request_count,
            users: group.independent_consumers,
          })}
        </p>
      )}
      <div className='mt-4'>
        <TokenBindPanel groupId={group.id} />
      </div>
    </div>
  )
}
