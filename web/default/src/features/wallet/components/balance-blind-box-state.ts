import { useEffect, useState, type Dispatch, type SetStateAction } from 'react'
import type {
  BalanceBlindBoxOverview,
  BlindBoxSelfData,
  WalletTransferRecipient,
} from '../types'
import {
  giftBalanceBoxInventory,
  lookupBalanceBoxRecipient,
  openBalanceBoxInventory,
  purchaseBalanceBoxInventory,
  simulateBalanceBoxDraws,
  type BalanceBoxActionContext,
} from './balance-blind-box-actions'
import type {
  ActionMode,
  BalanceBoxPanelViewProps,
} from './balance-blind-box-view'

export function useBalanceBlindBoxPanelState(props: {
  data: BlindBoxSelfData | null
  loading: boolean
  onRefresh: () => Promise<void>
}): BalanceBoxPanelViewProps {
  const balance = props.data?.balance_blind_box
  const [mode, setMode] = useState<ActionMode>('purchase')
  const [count, setCount] = useState(1)
  const [busy, setBusy] = useState(false)
  const [recipientId, setRecipientId] = useState('')
  const [recipient, setRecipient] = useState<WalletTransferRecipient | null>(
    null
  )
  const [confirmGift, setConfirmGift] = useState(false)
  const simulationActive = Boolean(
    balance?.simulation?.active &&
    (balance.simulation.expires_at || 0) > Date.now() / 1000
  )
  useEffect(() => {
    if (mode === 'simulate' && !simulationActive) setMode('purchase')
  }, [mode, simulationActive])
  const { maxCount, safeCount, canPurchase, canUseInventory, canSimulate } =
    deriveBalanceBoxState(mode, balance, count, props.loading, busy)
  const context = createBalanceBoxActionContext({
    balance,
    safeCount,
    canPurchase,
    canUseInventory,
    canSimulate,
    recipient,
    recipientId,
    onRefresh: props.onRefresh,
    setBusy,
    setRecipient,
    setRecipientId,
    setConfirmGift,
  })
  const handlers = createBalanceBoxHandlers(
    context,
    setMode,
    setCount,
    setRecipientId,
    setRecipient,
    setConfirmGift
  )
  return {
    balance,
    mode,
    count: safeCount,
    maxCount,
    busy,
    recipient,
    recipientId,
    canPurchase,
    canUseInventory,
    canSimulate,
    confirmGift,
    ...handlers,
  }
}

function deriveBalanceBoxState(
  mode: ActionMode,
  balance: BalanceBlindBoxOverview | undefined,
  count: number,
  loading: boolean,
  busy: boolean
) {
  const maxCount = getBalanceBoxMaxCount(mode, balance)
  const safeCount = Math.min(Math.max(1, count), maxCount)
  return {
    maxCount,
    safeCount,
    canPurchase: canPurchaseBalanceBoxes(balance, safeCount, loading, busy),
    canUseInventory: canUseBalanceBoxInventory(
      balance,
      safeCount,
      loading,
      busy
    ),
    canSimulate: Boolean(
      balance?.simulation?.active &&
      (balance.simulation.expires_at || 0) > Date.now() / 1000 &&
      !loading &&
      !busy
    ),
  }
}

function createBalanceBoxActionContext(args: {
  balance?: BalanceBlindBoxOverview
  safeCount: number
  canPurchase: boolean
  canUseInventory: boolean
  canSimulate: boolean
  recipient: WalletTransferRecipient | null
  recipientId: string
  onRefresh: () => Promise<void>
  setBusy: Dispatch<SetStateAction<boolean>>
  setRecipient: Dispatch<SetStateAction<WalletTransferRecipient | null>>
  setRecipientId: Dispatch<SetStateAction<string>>
  setConfirmGift: Dispatch<SetStateAction<boolean>>
}): BalanceBoxActionContext {
  return { ...args, count: args.safeCount }
}

function createBalanceBoxHandlers(
  context: BalanceBoxActionContext,
  setMode: Dispatch<SetStateAction<ActionMode>>,
  setCount: Dispatch<SetStateAction<number>>,
  setRecipientId: Dispatch<SetStateAction<string>>,
  setRecipient: Dispatch<SetStateAction<WalletTransferRecipient | null>>,
  setConfirmGift: Dispatch<SetStateAction<boolean>>
) {
  return {
    onModeChange: (next: ActionMode) => {
      setMode(next)
      setCount(1)
    },
    onCountChange: setCount,
    onRecipientIdChange: (value: string) => {
      setRecipientId(value)
      setRecipient(null)
    },
    onPurchase: () => void purchaseBalanceBoxInventory(context),
    onOpen: () => void openBalanceBoxInventory(context),
    onSimulate: () => void simulateBalanceBoxDraws(context),
    onLookup: () => void lookupBalanceBoxRecipient(context),
    onGift: () => void giftBalanceBoxInventory(context),
    onConfirmGiftChange: setConfirmGift,
  }
}

function getBalanceBoxMaxCount(
  mode: ActionMode,
  balance?: BalanceBlindBoxOverview
) {
  if (mode === 'purchase')
    return Math.max(1, balance?.remaining_purchase_limit || 1)
  if (mode === 'simulate') return 100
  return Math.max(1, Math.min(100, balance?.inventory_count || 1))
}

function canPurchaseBalanceBoxes(
  balance: BalanceBlindBoxOverview | undefined,
  count: number,
  loading: boolean,
  busy: boolean
) {
  return Boolean(
    balance?.enabled &&
    !loading &&
    !busy &&
    balance.remaining_purchase_limit >= count &&
    balance.balance_usd >= balance.price_usd * count
  )
}

function canUseBalanceBoxInventory(
  balance: BalanceBlindBoxOverview | undefined,
  count: number,
  loading: boolean,
  busy: boolean
) {
  return Boolean(
    balance?.enabled && !loading && !busy && balance.inventory_count >= count
  )
}
