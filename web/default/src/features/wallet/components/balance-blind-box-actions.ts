import type { Dispatch, SetStateAction } from 'react'
import interfaceI18n from '@/i18n/config'
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
    if (!isApiSuccess(response))
      throw new Error(response.message || interfaceI18n.t('购买失败'))
    toast.success(
      interfaceI18n.t('{{param0}} 个统一盲盒已购买并存入库存', {
        param0: context.count,
      })
    )
    await context.onRefresh()
  } catch (error) {
    toast.error(
      error instanceof Error
        ? error.message
        : interfaceI18n.t('统一盲盒购买失败')
    )
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
      throw new Error(response.message || interfaceI18n.t('开启失败'))
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
    toast.error(
      error instanceof Error
        ? error.message
        : interfaceI18n.t('统一盲盒开启失败')
    )
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
    toast.error(interfaceI18n.t('请输入对方 6 位公开 ID'))
    return
  }
  context.setBusy(true)
  try {
    const response = await lookupWalletTransferRecipient(externalId)
    if (!isApiSuccess(response) || !response.data) {
      throw new Error(response.message || interfaceI18n.t('未找到该用户'))
    }
    context.setRecipient(response.data)
    context.setRecipientId(response.data.external_id)
  } catch (error) {
    toast.error(
      error instanceof Error ? error.message : interfaceI18n.t('未找到该用户')
    )
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
    if (!isApiSuccess(response))
      throw new Error(response.message || interfaceI18n.t('赠送失败'))
    toast.success(
      interfaceI18n.t('已向 {{param0}} 赠送 {{param1}} 个统一盲盒', {
        param0: context.recipient.external_id,
        param1: context.count,
      })
    )
    context.setRecipient(null)
    context.setRecipientId('')
    await context.onRefresh()
  } catch (error) {
    toast.error(
      error instanceof Error
        ? error.message
        : interfaceI18n.t('统一盲盒赠送失败')
    )
  } finally {
    context.setBusy(false)
    context.setConfirmGift(false)
  }
}
