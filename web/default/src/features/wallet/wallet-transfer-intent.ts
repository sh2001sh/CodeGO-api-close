import { createWalletTransfer, isApiSuccess } from './api'
import type {
  WalletTransferHistoryItem,
  WalletTransferRecipient,
} from './types'

export interface WalletTransferIntent {
  requestId: string
  recipient: WalletTransferRecipient
  amountQuota: number
  feeQuota: number
  quotaPerUSD: number
}

type TransferStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

function storageKey(userId: number): string {
  return `wallet-transfer-pending:${userId}`
}

export function restoreWalletTransferIntent(
  userId: number,
  storage: TransferStorage = sessionStorage
): WalletTransferIntent | null {
  const saved = storage.getItem(storageKey(userId))
  if (!saved) return null
  const intent: WalletTransferIntent = JSON.parse(saved)
  if (
    !intent ||
    typeof intent.requestId !== 'string' ||
    !intent.requestId ||
    typeof intent.recipient?.external_id !== 'string' ||
    typeof intent.recipient?.display_name_masked !== 'string' ||
    !Number.isSafeInteger(intent.amountQuota) ||
    intent.amountQuota <= 0 ||
    !Number.isSafeInteger(intent.feeQuota) ||
    intent.feeQuota < 0 ||
    !Number.isFinite(intent.quotaPerUSD) ||
    intent.quotaPerUSD <= 0
  ) {
    throw new Error('Invalid pending transfer')
  }
  return intent
}

type TransferResult =
  | {
      status: 'completed'
      transfer: WalletTransferHistoryItem
      storageError: boolean
    }
  | { status: 'rejected'; message: string; intent: WalletTransferIntent | null }
  | { status: 'unknown'; intent: WalletTransferIntent }

function clearWalletTransferIntent(
  userId: number,
  intent: WalletTransferIntent,
  storage: TransferStorage
): void {
  if (
    restoreWalletTransferIntent(userId, storage)?.requestId === intent.requestId
  ) {
    storage.removeItem(storageKey(userId))
  }
}

export async function submitWalletTransferIntent(
  userId: number,
  draft: WalletTransferIntent,
  paymentPassword: string,
  storage: TransferStorage = sessionStorage
): Promise<TransferResult> {
  const pending = restoreWalletTransferIntent(userId, storage)
  const intent = pending || draft
  // Persist before sending: navigation or a lost response must retain the ID.
  storage.setItem(storageKey(userId), JSON.stringify(intent))

  let response
  try {
    response = await createWalletTransfer({
      recipient_external_id: intent.recipient.external_id,
      amount_quota: intent.amountQuota,
      payment_password: paymentPassword,
      request_id: intent.requestId,
    })
  } catch {
    return { status: 'unknown', intent }
  }

  if (!response || typeof response !== 'object') {
    return { status: 'unknown', intent }
  }
  if (
    isApiSuccess(response) &&
    response.data?.request_id === intent.requestId &&
    response.data.amount_quota === intent.amountQuota &&
    response.data.counterparty_external_id === intent.recipient.external_id &&
    Number.isFinite(response.data.balance_after)
  ) {
    let storageError = false
    try {
      clearWalletTransferIntent(userId, intent, storage)
    } catch {
      // The transfer succeeded; a storage failure cannot change that result.
      storageError = true
    }
    return { status: 'completed', transfer: response.data, storageError }
  }

  if (response.success === false) {
    // A rejection after an unknown result cannot disprove an earlier commit.
    if (!pending) {
      try {
        clearWalletTransferIntent(userId, intent, storage)
        return {
          status: 'rejected',
          message: response.message || '',
          intent: null,
        }
      } catch {
        return { status: 'rejected', message: response.message || '', intent }
      }
    }
    return { status: 'rejected', message: response.message || '', intent }
  }
  return { status: 'unknown', intent }
}
