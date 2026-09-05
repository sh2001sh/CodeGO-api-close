import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useMarketplaceFailedModelRemoval } from '../hooks'
import { hasGPT56Model } from '../lib/verification'
import type { MarketplaceChannel } from '../types'
import {
  ConnectivityTestStatusView,
  ModelConsistencyStatusView,
} from './model-verification'

export function ChannelVerificationStatus(props: {
  channel: MarketplaceChannel
}) {
  const { t } = useTranslation()
  const { channel } = props
  const removal = useMarketplaceFailedModelRemoval()
  const removeModel = async (model: string) => {
    try {
      await removal.mutateAsync({ channelId: channel.id, model })
      toast.success(t('已剔除失败模型 {{model}}', { model }))
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('剔除模型失败'))
    }
  }
  return (
    <div>
      <ConnectivityTestStatusView
        status={channel.connectivity_test_status}
        results={channel.model_verification_results}
        checkedAt={channel.connectivity_test_checked_at}
        summary={channel.verification_summary}
        required={!hasGPT56Model(channel.declared_models)}
        showErrors
        onRemoveModel={(model) => void removeModel(model)}
        removingModel={removal.isPending ? removal.variables?.model : undefined}
      />
      <div className='mt-2'>
        <ModelConsistencyStatusView
          status={channel.model_consistency_status}
          checkedAt={channel.verification_completed_at}
        />
      </div>
      <RecentModelStatus
        models={channel.declared_models}
        results={channel.model_verification_results}
        checkedAt={channel.connectivity_test_checked_at}
      />
    </div>
  )
}

function RecentModelStatus(props: {
  models: string[]
  results: MarketplaceChannel['model_verification_results']
  checkedAt?: string | null
}) {
  const { t } = useTranslation()
  const byModel = new Map(props.results.map((result) => [result.model, result]))
  if (props.models.length === 0) return null
  return (
    <details className='border-border bg-muted/10 mt-3 rounded-md border px-3 py-2'>
      <summary className='cursor-pointer text-xs font-medium'>
        {t('渠道模型近期状态')} · {t('{{count}} 个模型', { count: props.models.length })}
      </summary>
      <div className='text-muted-foreground mt-2 mb-2 text-[11px]'>
        {props.checkedAt
          ? t('最近检测 {{time}}', { time: new Date(props.checkedAt).toLocaleString() })
          : t('尚未完成连通性检测')}
      </div>
      <div className='grid gap-1.5 sm:grid-cols-2'>
        {props.models.map((model) => {
          const result = byModel.get(model)
          const passed = result?.status === 'passed' && result.listed
          const running = !result && props.checkedAt == null
          return (
            <div key={model} className='border-border/70 flex items-center justify-between gap-2 rounded border px-2.5 py-2 text-xs'>
              <span className='min-w-0 truncate font-medium' title={model}>{model}</span>
              <span className={passed ? 'text-success shrink-0' : running ? 'text-primary shrink-0' : 'text-destructive shrink-0'}>
                {passed ? t('正常') : running ? t('待检测') : t('异常')}
                {result?.latency_ms ? ` · ${result.latency_ms}ms` : ''}
              </span>
            </div>
          )
        })}
      </div>
    </details>
  )
}
