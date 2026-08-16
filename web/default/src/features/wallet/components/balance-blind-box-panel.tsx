import type { BlindBoxSelfData, PaymentMethod } from '../types'
import { useBalanceBlindBoxPanelState } from './balance-blind-box-state'
import { BalanceBoxPanelView } from './balance-blind-box-view'

export function BalanceBlindBoxPanel(props: {
  data: BlindBoxSelfData | null
  loading: boolean
  onRefresh: () => Promise<void>
  cashMethods: PaymentMethod[]
  selectedCashMethod: PaymentMethod | null
  cashAmountDue: number
  cashPaying: boolean
  onCashMethodChange: (method: PaymentMethod) => void
  onCashQuantityChange: (count: number) => void
  onCashPurchase: (count: number) => void
  onOpenProps: () => void
}) {
  const viewProps = useBalanceBlindBoxPanelState(props)
  return <BalanceBoxPanelView {...viewProps} />
}
