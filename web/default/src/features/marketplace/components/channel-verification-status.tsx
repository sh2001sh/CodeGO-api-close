import { CheckCircle2, Loader2, ShieldCheck, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import type { MarketplaceChannel } from '../types'
import { ModelConnectivityResults, ModelConsistencyBadge } from './model-verification'

const stageLabels: Record<string, string> = {
  basic_security: '连接安全检查',
  model_list: '读取上游模型列表',
  model_match: '核对已发布模型',
  inference: '执行实际模型调用',
  protocol: '检测完成',
}

export function ChannelVerificationStatus(props: {
  channel: MarketplaceChannel
}) {
  const { t } = useTranslation()
  const { channel } = props
  const running =
    channel.lifecycle_status === 'verifying' ||
    ['queued', 'running'].includes(channel.verification_status)
  const failed = channel.verification_status === 'failed'
  const passed = channel.verification_status === 'passed'

  if (!running && !failed && !passed) return null

  const presentation = verificationPresentation(channel, running, failed, t)

  return (
    <div
      className={cn(
        'mt-3 flex items-start gap-2.5 rounded-md px-3 py-2.5 text-xs',
        running && 'bg-info/10 text-info',
        failed && 'bg-destructive/10 text-destructive',
        passed && 'bg-success/10 text-success'
      )}
      role={failed ? 'alert' : 'status'}
    >
      <presentation.Icon
        className={cn('mt-0.5 size-4 shrink-0', running && 'animate-spin')}
      />
      <div className='min-w-0'>
        <div className='font-medium'>{presentation.title}</div>
        <div className='mt-0.5 leading-5 opacity-85'>
          {presentation.detail}
        </div>
        {channel.verification_detector_version && (
          <div className='mt-0.5 flex items-center gap-1 opacity-70'>
            <ShieldCheck className='size-3' />
            Detector {channel.verification_detector_version}
          </div>
        )}
        <div className='mt-2'>
          <ModelConsistencyBadge status={channel.model_consistency_status} />
        </div>
        <ModelConnectivityResults
          results={channel.model_verification_results}
          showErrors
        />
      </div>
    </div>
  )
}

function verificationPresentation(
  channel: MarketplaceChannel,
  running: boolean,
  failed: boolean,
  t: (value: string) => string
) {
  if (running) {
    return {
      Icon: Loader2,
      title: stageLabels[channel.verification_stage] || t('正在准备检测'),
      detail: t('正在执行真实连接与模型调用检测，完成后将自动更新状态。'),
    }
  }
  if (failed) {
    return {
      Icon: XCircle,
      title: t('检测未通过'),
      detail:
        channel.verification_summary || t('请检查渠道配置后重新检测。'),
    }
  }
  return {
    Icon: CheckCircle2,
    title: t('检测已通过'),
    detail: channel.verification_summary || t('渠道可以正常提供服务。'),
  }
}
