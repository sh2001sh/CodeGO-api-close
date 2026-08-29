import type { Dispatch, SetStateAction } from 'react'
import { toast } from 'sonner'
import {
  giftBalanceBlindBoxes,
  isApiSuccess,
  lookupWalletTransferRecipient,
  openBalanceBlindBox,
  purchaseBalanceBlindBoxes,
} from '../api'
import type {
  BalanceBlindBoxOverview,
  BlindBoxRecord,
  WalletTransferRecipient,
} from '../types'
import { newBalanceBoxRequestId } from './balance-blind-box-controls'

export interface BalanceBoxActionContext {
  balance?: BalanceBlindBoxOverview
  count: number
  canPurchase: boolean
  canUseInventory: boolean
  recipient: WalletTransferRecipient | null
  recipientId: string
  onRefresh: () => Promise<void>
  setBusy: Dispatch<SetStateAction<boolean>>
  setRecipient: Dispatch<SetStateAction<WalletTransferRecipient | null>>
  setRecipientId: Dispatch<SetStateAction<string>>
  setConfirmGift: Dispatch<SetStateAction<boolean>>
}

export async function purchaseBalanceBoxInventory(
  context: BalanceBoxActionContext
) {
  if (!context.canPurchase) return
  context.setBusy(true)
  try {
    const response = await purchaseBalanceBlindBoxes(
      newBalanceBoxRequestId(),
      context.count
    )
    if (!isApiSuccess(response)) throw new Error(response.message || '购买失败')
    toast.success(`${context.count} 个统一盲盒已购买并存入库存`)
    await context.onRefresh()
  } catch (error) {
    toast.error(error instanceof Error ? error.message : '统一盲盒购买失败')
  } finally {
    context.setBusy(false)
  }
}

export async function openBalanceBoxInventory(
  context: BalanceBoxActionContext
) {
  if (!context.canUseInventory) return
  context.setBusy(true)
  try {
    const response = await openBalanceBlindBox(
      newBalanceBoxRequestId(),
      context.count
    )
    if (!isApiSuccess(response) || !response.data?.records?.length) {
      throw new Error(response.message || '开启失败')
    }
    window.dispatchEvent(
      new CustomEvent('blind-box:changed', {
        detail: {
          records: response.data.records as BlindBoxRecord[],
          openCount: response.data.records.length,
        },
      })
    )
    // the prize dialog is the feedback; a toast here would spoil the reel
  } catch (error) {
    toast.error(error instanceof Error ? error.message : '统一盲盒开启失败')
  } finally {
    context.setBusy(false)
  }
}

export async function lookupBalanceBoxRecipient(
  context: BalanceBoxActionContext
) {
  const externalId = context.recipientId.trim().toUpperCase()
  context.setRecipient(null)
  if (!/^[A-Z0-9]{6}$/.test(externalId)) {
    toast.error('请输入对方 6 位公开 ID')
    return
  }
  context.setBusy(true)
  try {
    const response = await lookupWalletTransferRecipient(externalId)
    if (!isApiSuccess(response) || !response.data) {
      throw new Error(response.message || '未找到该用户')
    }
    context.setRecipient(response.data)
    context.setRecipientId(response.data.external_id)
  } catch (error) {
    toast.error(error instanceof Error ? error.message : '未找到该用户')
  } finally {
    context.setBusy(false)
  }
}

export async function giftBalanceBoxInventory(
  context: BalanceBoxActionContext
) {
  if (!context.recipient || !context.canUseInventory) return
  context.setBusy(true)
  try {
    const response = await giftBalanceBlindBoxes(
      newBalanceBoxRequestId(),
      context.recipient.external_id,
      context.count
    )
    if (!isApiSuccess(response)) throw new Error(response.message || '赠送失败')
    toast.success(
      `已向 ${context.recipient.external_id} 赠送 ${context.count} 个统一盲盒`
    )
    context.setRecipient(null)
    context.setRecipientId('')
    await context.onRefresh()
  } catch (error) {
    toast.error(error instanceof Error ? error.message : '统一盲盒赠送失败')
  } finally {
    context.setBusy(false)
    context.setConfirmGift(false)
  }
}
