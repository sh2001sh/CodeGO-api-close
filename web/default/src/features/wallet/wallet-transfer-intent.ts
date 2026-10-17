import { isAxiosError } from 'axios'
import { createWalletTransfer, getWalletTransferReceipt } from './api'
import type {
  WalletTransferHistoryItem,
  WalletTransferRecipient,
  WalletTransferResponse,
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

export type TransferResult =
  | {
      status: 'completed'
      transfer: WalletTransferHistoryItem
      storageError: boolean
    }
  | { status: 'rejected'; message: string; intent: WalletTransferIntent }
  | { status: 'processing'; intent: WalletTransferIntent }
  | {
      status: 'unknown'
      intent: WalletTransferIntent
      code?: string
      message?: string
    }

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
  storage: TransferStorage = sessionStorage,
  signal?: AbortSignal
): Promise<TransferResult> {
  const pending = restoreWalletTransferIntent(userId, storage)
  const intent = pending || draft
  // Persist before sending: navigation or a lost response must retain the ID.
  storage.setItem(storageKey(userId), JSON.stringify(intent))

  let response
  try {
    response = await createWalletTransfer(
      {
        recipient_external_id: intent.recipient.external_id,
        amount_quota: intent.amountQuota,
        payment_password: paymentPassword,
        request_id: intent.requestId,
      },
      signal
    )
  } catch (error) {
    if (
      isAxiosError<{ success?: boolean; code?: string; message?: string }>(
        error
      )
    ) {
      const failure = error.response?.data
      const knownFailure =
        (error.response?.status === 503 &&
          failure?.code === 'TRANSFER_UNCONFIRMED') ||
        (error.response?.status === 429 &&
          (failure?.code === 'PAYMENT_PASSWORD_LOCKED' ||
            failure?.code === 'TRANSFER_BUSY'))
      if (
        knownFailure &&
        failure?.success === false &&
        typeof failure.message === 'string'
      ) {
        // These codes have sanitized server messages. Keep the original intent:
        // even a password lock cannot disprove an earlier committed request.
        return {
          status: 'unknown',
          intent,
          code: failure.code,
          message: failure.message,
        }
      }
    }
    return { status: 'unknown', intent }
  }

  return resolveWalletTransferResponse(
    userId,
    intent,
    response,
    storage,
    signal
  )
}

function resolveWalletTransferResponse(
  userId: number,
  intent: WalletTransferIntent,
  response: WalletTransferResponse,
  storage: TransferStorage,
  signal?: AbortSignal
): TransferResult {
  if (signal?.aborted || !response || typeof response !== 'object') {
    return { status: 'unknown', intent }
  }
  const receipt = response.data
  if (
    response.success === true &&
    receipt?.request_id === intent.requestId &&
    'direction' in receipt &&
    receipt.direction === 'outgoing' &&
    receipt.status === 'completed' &&
    receipt.amount_quota === intent.amountQuota &&
    receipt.fee_quota === intent.feeQuota &&
    Number.isSafeInteger(intent.amountQuota + intent.feeQuota) &&
    receipt.total_debit_quota === intent.amountQuota + intent.feeQuota &&
    receipt.counterparty_external_id === intent.recipient.external_id &&
    Number.isSafeInteger(receipt.balance_after) &&
    receipt.balance_after >= 0
  ) {
    let storageError = false
    try {
      clearWalletTransferIntent(userId, intent, storage)
    } catch {
      // The transfer succeeded; a storage failure cannot change that result.
      storageError = true
    }
    return { status: 'completed', transfer: receipt, storageError }
  }
  if (
    response.success === true &&
    response.code === 'TRANSFER_PROCESSING' &&
    receipt?.request_id === intent.requestId &&
    receipt.status === 'processing'
  )
    return { status: 'processing', intent }

  // Only a matching completed receipt releases the original intent.
  if (response.success === false) {
    return { status: 'rejected', message: response.message || '', intent }
  }
  return { status: 'unknown', intent }
}

export async function readWalletTransferIntent(
  userId: number,
  intent: WalletTransferIntent,
  signal: AbortSignal,
  storage: TransferStorage = sessionStorage,
  timeout = 10_000
): Promise<TransferResult> {
  try {
    const response = await getWalletTransferReceipt(
      intent.requestId,
      signal,
      timeout
    )
    if (
      response.success === false &&
      !(
        (response.code === 'TRANSFER_REJECTED' ||
          response.code === 'PAYMENT_PASSWORD_LOCKED') &&
        response.data?.request_id === intent.requestId &&
        response.data.status === 'rejected'
      )
    )
      return { status: 'unknown', intent }
    return resolveWalletTransferResponse(
      userId,
      intent,
      response,
      storage,
      signal
    )
  } catch (error) {
    // Missing receipts, transport failures and unavailability cannot prove failure.
    if (
      isAxiosError<{ success?: boolean; code?: string; message?: string }>(
        error
      )
    ) {
      const failure = error.response?.data
      if (
        error.response?.status === 503 &&
        failure?.success === false &&
        failure.code === 'TRANSFER_UNCONFIRMED' &&
        typeof failure.message === 'string'
      ) {
        return {
          status: 'unknown',
          intent,
          code: failure.code,
          message: failure.message,
        }
      }
    }
    return { status: 'unknown', intent }
  }
}

export async function pollWalletTransferIntent(
  userId: number,
  intent: WalletTransferIntent,
  signal: AbortSignal,
  onResult: (result: TransferResult) => void,
  storage: TransferStorage = sessionStorage
): Promise<void> {
  const deadline = Date.now() + 180_000
  while (!signal.aborted && Date.now() < deadline) {
    const result = await readWalletTransferIntent(
      userId,
      intent,
      signal,
      storage,
      Math.max(1, Math.min(10_000, deadline - Date.now()))
    )
    if (signal.aborted) return
    onResult(result)
    if (result.status === 'completed' || result.status === 'rejected') return
    const remaining = deadline - Date.now()
    if (remaining <= 0) return
    await new Promise<void>((resolve) => {
      const finish = () => {
        clearTimeout(timer)
        signal.removeEventListener('abort', finish)
        resolve()
      }
      const timer = setTimeout(finish, Math.min(2_000, remaining))
      signal.addEventListener('abort', finish, { once: true })
      if (signal.aborted) finish()
    })
  }
}
