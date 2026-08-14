import type { BlindBoxSelfData } from '../types'
import { useBalanceBlindBoxPanelState } from './balance-blind-box-state'
import { BalanceBoxPanelView } from './balance-blind-box-view'

export function BalanceBlindBoxPanel(props: {
  data: BlindBoxSelfData | null
  loading: boolean
  onRefresh: () => Promise<void>
}) {
  const viewProps = useBalanceBlindBoxPanelState(props)
  return <BalanceBoxPanelView {...viewProps} />
}
