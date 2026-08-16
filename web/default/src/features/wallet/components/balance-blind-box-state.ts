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
  const local = useBalanceBoxLocalState()
  const state = deriveBalanceBoxState(
    balance,
    local.purchaseCount,
    local.inventoryActionCount,
    props.loading,
    local.busy || props.cashPaying
  )
  const contexts = createBalanceBoxContexts({
    balance,
    state,
    local,
    onRefresh: props.onRefresh,
  })
  const handlers = createBalanceBoxHandlers(
    contexts.purchase,
    contexts.inventory,
    state.inventoryMaxCount,
    local,
    props.onCashQuantityChange
  )
  return buildBalanceBoxViewProps(props, balance, local, state, handlers)
}

function useBalanceBoxLocalState() {
  const [mode, setMode] = useState<ActionMode>('purchase')
  const [purchaseCount, setPurchaseCount] = useState(1)
  const [inventoryActionCount, setInventoryActionCount] = useState(1)
  const [busy, setBusy] = useState(false)
  const [recipientId, setRecipientId] = useState('')
  const [recipient, setRecipient] = useState<WalletTransferRecipient | null>(
    null
  )
  const [confirmGift, setConfirmGift] = useState(false)
  return {
    mode,
    setMode,
    purchaseCount,
    setPurchaseCount,
    inventoryActionCount,
    setInventoryActionCount,
    busy,
    setBusy,
    recipientId,
    setRecipientId,
    recipient,
    setRecipient,
    confirmGift,
    setConfirmGift,
  }
}

type BalanceBoxLocalState = ReturnType<typeof useBalanceBoxLocalState>
type DerivedBalanceBoxState = ReturnType<typeof deriveBalanceBoxState>

function buildBalanceBoxViewProps(
  props: Parameters<typeof useBalanceBlindBoxPanelState>[0],
  balance: BalanceBlindBoxOverview | undefined,
  local: BalanceBoxLocalState,
  state: DerivedBalanceBoxState,
  handlers: ReturnType<typeof createBalanceBoxHandlers>
): BalanceBoxPanelViewProps {
  return {
    balance,
    mode: local.mode,
    count: state.purchaseCount,
    maxCount: state.purchaseMaxCount,
    inventoryActionCount: state.inventoryActionCount,
    inventoryMaxCount: state.inventoryMaxCount,
    busy: local.busy || props.cashPaying,
    recipient: local.recipient,
    recipientId: local.recipientId,
    canPurchase: state.canPurchase,
    canUseInventory: state.canUseInventory,
    confirmGift: local.confirmGift,
    cashMethods: props.cashMethods,
    selectedCashMethod: props.selectedCashMethod,
    cashAmountDue: props.cashAmountDue,
    cashPaying: props.cashPaying,
    onCashMethodChange: props.onCashMethodChange,
    onCashPurchase: () => props.onCashPurchase(state.purchaseCount),
    onOpenProps: props.onOpenProps,
    ...handlers,
  }
}

function createBalanceBoxContexts(args: {
  balance?: BalanceBlindBoxOverview
  state: DerivedBalanceBoxState
  local: BalanceBoxLocalState
  onRefresh: () => Promise<void>
}) {
  const shared = {
    balance: args.balance,
    recipient: args.local.recipient,
    recipientId: args.local.recipientId,
    onRefresh: args.onRefresh,
    setBusy: args.local.setBusy,
    setRecipient: args.local.setRecipient,
    setRecipientId: args.local.setRecipientId,
    setConfirmGift: args.local.setConfirmGift,
  }
  return {
    purchase: createBalanceBoxActionContext({
      ...shared,
      safeCount: args.state.purchaseCount,
      canPurchase: args.state.canPurchase,
      canUseInventory: false,
    }),
    inventory: createBalanceBoxActionContext({
      ...shared,
      safeCount: args.state.inventoryActionCount,
      canPurchase: false,
      canUseInventory: args.state.canUseInventory,
    }),
  }
}

function deriveBalanceBoxState(
  balance: BalanceBlindBoxOverview | undefined,
  purchaseCount: number,
  inventoryActionCount: number,
  loading: boolean,
  busy: boolean
) {
  const purchaseMaxCount = Math.max(1, balance?.remaining_purchase_limit || 1)
  const inventoryMaxCount = Math.max(
    1,
    Math.min(100, balance?.inventory_count || 1)
  )
  const safePurchaseCount = Math.min(
    Math.max(1, purchaseCount),
    purchaseMaxCount
  )
  const safeInventoryActionCount = Math.min(
    Math.max(1, inventoryActionCount),
    inventoryMaxCount
  )
  return {
    purchaseMaxCount,
    inventoryMaxCount,
    purchaseCount: safePurchaseCount,
    inventoryActionCount: safeInventoryActionCount,
    canPurchase: canPurchaseBalanceBoxes(
      balance,
      safePurchaseCount,
      loading,
      busy
    ),
    canUseInventory: canUseBalanceBoxInventory(
      balance,
      safeInventoryActionCount,
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
  purchaseContext: BalanceBoxActionContext,
  inventoryContext: BalanceBoxActionContext,
  inventoryMaxCount: number,
  local: BalanceBoxLocalState,
  onCashQuantityChange: (count: number) => void
) {
  return {
    onModeChange: (next: ActionMode) => {
      local.setMode(next)
    },
    onCountChange: (value: number) => {
      local.setPurchaseCount(value)
      onCashQuantityChange(value)
    },
    onInventoryCountChange: local.setInventoryActionCount,
    onRecipientIdChange: (value: string) => {
      local.setRecipientId(value)
      local.setRecipient(null)
    },
    onPurchase: () => {
      local.setMode('purchase')
      void purchaseBalanceBoxInventory(purchaseContext)
    },
    onOpen: () => void openBalanceBoxInventory(inventoryContext),
    onOpenCount: (count: number) => {
      const safeCount = Math.min(Math.max(1, count), inventoryMaxCount)
      local.setMode('open')
      void openBalanceBoxInventory({ ...inventoryContext, count: safeCount })
    },
    onLookup: () => void lookupBalanceBoxRecipient(inventoryContext),
    onGift: () => void giftBalanceBoxInventory(inventoryContext),
    onConfirmGiftChange: local.setConfirmGift,
  }
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
