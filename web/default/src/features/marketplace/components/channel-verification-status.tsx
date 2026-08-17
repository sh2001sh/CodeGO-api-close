import { hasGPT56Model } from '../lib/verification'
import type { MarketplaceChannel } from '../types'
import {
  ConnectivityTestStatusView,
  ModelConsistencyStatusView,
} from './model-verification'

export function ChannelVerificationStatus(props: {
  channel: MarketplaceChannel
}) {
  const { channel } = props
  return (
    <div>
      <ConnectivityTestStatusView
        status={channel.connectivity_test_status}
        results={channel.model_verification_results}
        checkedAt={channel.connectivity_test_checked_at}
        required={!hasGPT56Model(channel.declared_models)}
        showErrors
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
