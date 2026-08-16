import { useState, type Dispatch, type SetStateAction } from 'react'
import type {
  BalanceBlindBoxOverview,
  BlindBoxSelfData,
  PaymentMethod,
  WalletTransferRecipient,
} from '../types'
import {
  giftBalanceBoxInventory,
  lookupBalanceBoxRecipient,
  openBalanceBoxInventory,
  purchaseBalanceBoxInventory,
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
  cashMethods: PaymentMethod[]
  selectedCashMethod: PaymentMethod | null
  cashAmountDue: number
  cashPaying: boolean
  onCashMethodChange: (method: PaymentMethod) => void
  onCashQuantityChange: (count: number) => void
  onCashPurchase: (count: number) => void
  onOpenProps: () => void
}): BalanceBoxPanelViewProps {
  const balance = props.data?.inventory
  const [mode, setMode] = useState<ActionMode>('purchase')
  const [count, setCount] = useState(1)
  const [busy, setBusy] = useState(false)
  const [recipientId, setRecipientId] = useState('')
  const [recipient, setRecipient] = useState<WalletTransferRecipient | null>(
    null
  )
  const [confirmGift, setConfirmGift] = useState(false)
  const { maxCount, safeCount, canPurchase, canUseInventory } =
    deriveBalanceBoxState(
      mode,
      balance,
      count,
      props.loading,
      busy || props.cashPaying
    )
  const context = createBalanceBoxActionContext({
    balance,
    safeCount,
    canPurchase,
    canUseInventory,
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
    setConfirmGift,
    props.onCashQuantityChange
  )
  return {
    balance,
    mode,
    count: safeCount,
    maxCount,
    busy: busy || props.cashPaying,
    recipient,
    recipientId,
    canPurchase,
    canUseInventory,
    confirmGift,
    cashMethods: props.cashMethods,
    selectedCashMethod: props.selectedCashMethod,
    cashAmountDue: props.cashAmountDue,
    cashPaying: props.cashPaying,
    onCashMethodChange: props.onCashMethodChange,
    onCashPurchase: () => props.onCashPurchase(safeCount),
    onOpenProps: props.onOpenProps,
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
  }
}

function createBalanceBoxActionContext(args: {
  balance?: BalanceBlindBoxOverview
  safeCount: number
  canPurchase: boolean
  canUseInventory: boolean
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
  setConfirmGift: Dispatch<SetStateAction<boolean>>,
  onCashQuantityChange: (count: number) => void
) {
  return {
    onModeChange: (next: ActionMode) => {
      setMode(next)
      setCount(1)
      onCashQuantityChange(1)
    },
    onCountChange: (value: number) => {
      setCount(value)
      onCashQuantityChange(value)
    },
    onRecipientIdChange: (value: string) => {
      setRecipientId(value)
      setRecipient(null)
    },
    onPurchase: () => void purchaseBalanceBoxInventory(context),
    onOpen: () => void openBalanceBoxInventory(context),
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
