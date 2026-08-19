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
    </div>
  )
}
