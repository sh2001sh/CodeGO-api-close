import { useCallback, useEffect, useRef, useState } from 'react'
import { isAxiosError } from 'axios'
import { Loader2, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { formatUsdAmount } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { SecureVerificationDialog } from '@/features/auth/secure-verification'
import {
  getWalletTransfers,
  isApiSuccess,
  lookupWalletTransferRecipient,
} from '../api'
import type { WalletTransferOverview, WalletTransferRecipient } from '../types'
import {
  restoreWalletTransferIntent,
  submitWalletTransferIntent,
  type WalletTransferIntent,
} from '../wallet-transfer-intent'
import { useWalletTransferPassword } from './use-wallet-transfer-password'
import { WalletPeerTransferPanel } from './wallet-peer-transfer-panel'
import { WalletTransferConfirmDialog } from './wallet-transfer-confirm-dialog'
import { WalletTransferPasswordDialog } from './wallet-transfer-password-dialog'

interface WalletPeerTransferCardProps {
  onUserRefresh?: () => Promise<void>
  onOpenHistory?: () => void
}

export function WalletPeerTransferCard(props: WalletPeerTransferCardProps) {
  const userId = useAuthStore((state) => state.auth.user?.id)
  if (!userId) return null
  return <WalletPeerTransferForm key={userId} {...props} userId={userId} />
}

function WalletPeerTransferForm(
  props: WalletPeerTransferCardProps & { userId: number }
) {
  const { t } = useTranslation()
  const [restored] = useState(() => {
    try {
      return {
        intent: restoreWalletTransferIntent(props.userId),
        failed: false,
      }
    } catch {
      return { intent: null, failed: true }
    }
  })
  const [pendingIntent, setPendingIntent] =
    useState<WalletTransferIntent | null>(restored.intent)
  const [storageBlocked, setStorageBlocked] = useState(restored.failed)
  const [transferError, setTransferError] = useState(
    restored.failed ? t('Unable to save transfer status. Please reload.') : ''
  )
  const submittingRef = useRef(false)
  const overviewRequest = useRef(0)
  const [overview, setOverview] = useState<WalletTransferOverview | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [recipientId, setRecipientId] = useState('')
  const [recipient, setRecipient] = useState<WalletTransferRecipient | null>(
    null
  )
  const [recipientLoading, setRecipientLoading] = useState(false)
  const [amount, setAmount] = useState('')
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [currentTimestamp, setCurrentTimestamp] = useState(0)

  const loadOverview = useCallback(async () => {
    const request = ++overviewRequest.current
    setLoading(true)
    setError('')
    try {
      const response = await getWalletTransfers()
      if (request !== overviewRequest.current) return
      if (!isApiSuccess(response) || !response.data) {
        throw new Error(response.message || t('Failed to load transfer data.'))
      }
      setOverview(response.data)
      setCurrentTimestamp(Math.floor(Date.now() / 1000))
    } catch (reason) {
      if (request !== overviewRequest.current) return
      setError(
        !isAxiosError(reason) && reason instanceof Error
          ? reason.message
          : t('Failed to load transfer data.')
      )
    } finally {
      if (request === overviewRequest.current) setLoading(false)
    }
  }, [t])

  const password = useWalletTransferPassword({
    security: overview?.security,
    reload: loadOverview,
  })

  useEffect(() => {
    void loadOverview()
  }, [loadOverview])

  const quotaPerUSD = overview?.quota_per_usd || 500_000
  const amountUSD = Number(amount || 0)
  const amountQuota = Math.round(amountUSD * quotaPerUSD)
  const feeBPS = overview?.fee_bps ?? 100
  const feeQuota = Math.ceil((amountQuota * feeBPS) / 10_000)
  const totalDebitQuota = amountQuota + feeQuota
  const balanceUSD = (overview?.balance || 0) / quotaPerUSD
  const maxTransferUSD =
    Math.floor(((overview?.balance || 0) * 10_000) / (10_000 + feeBPS)) /
    quotaPerUSD
  const minimumUSD = (overview?.min_quota || quotaPerUSD / 100) / quotaPerUSD
  const locked = (overview?.security.locked_until || 0) > currentTimestamp
  const amountValid =
    Number.isFinite(amountUSD) &&
    amountUSD >= minimumUSD &&
    totalDebitQuota <= (overview?.balance || 0)
  const canContinue =
    (Boolean(pendingIntent) || (Boolean(recipient) && amountValid)) &&
    Boolean(overview?.security.password_set) &&
    !locked &&
    !submitting &&
    !storageBlocked

  const reviewQuotaPerUSD = pendingIntent?.quotaPerUSD || quotaPerUSD
  const reviewAmountQuota = pendingIntent?.amountQuota ?? amountQuota
  const reviewFeeQuota = pendingIntent?.feeQuota ?? feeQuota
  const amountLabel = formatUsdAmount(reviewAmountQuota / reviewQuotaPerUSD)
  const feeLabel = formatUsdAmount(reviewFeeQuota / reviewQuotaPerUSD)
  const totalDebitLabel = formatUsdAmount(
    (reviewAmountQuota + reviewFeeQuota) / reviewQuotaPerUSD
  )

  const lookupRecipient = async () => {
    if (pendingIntent || recipientLoading || submittingRef.current) return
    const normalized = recipientId.trim().toUpperCase()
    if (normalized.length !== 6) {
      toast.error(t('Enter a valid 6-character recipient ID.'))
      return
    }
    setRecipientLoading(true)
    setRecipient(null)
    try {
      const response = await lookupWalletTransferRecipient(normalized)
      if (!isApiSuccess(response) || !response.data) {
        throw new Error(response.message || t('Recipient not found.'))
      }
      setRecipientId(response.data.external_id)
      setRecipient(response.data)
    } catch (reason) {
      let message = t('Recipient not found.')
      if (isAxiosError(reason)) message = t('Failed to load')
      else if (reason instanceof Error) message = reason.message
      toast.error(message)
    } finally {
      setRecipientLoading(false)
    }
  }

  const submitTransfer = async (paymentPassword: string) => {
    if (submittingRef.current || storageBlocked) return
    if (!pendingIntent && (!recipient || !amountValid)) return
    const draft = pendingIntent || {
      requestId: buildWalletTransferRequestId(),
      recipient: recipient!,
      amountQuota,
      feeQuota,
      quotaPerUSD,
    }
    submittingRef.current = true
    setSubmitting(true)
    setTransferError('')
    try {
      const result = await submitWalletTransferIntent(
        props.userId,
        draft,
        paymentPassword
      )
      if (result.status !== 'completed') {
        setPendingIntent(result.intent)
        if (result.status === 'rejected') {
          setTransferError(result.message || t('Quota transfer failed.'))
        } else {
          setTransferError(t('Transfer result unknown. Retry this transfer.'))
        }
        void loadOverview()
        return
      }
      toast.success(t('Quota transfer completed.'))
      setPendingIntent(null)
      setConfirmOpen(false)
      setRecipient(null)
      setRecipientId('')
      setAmount('')
      setOverview((current) =>
        current
          ? { ...current, balance: result.transfer.balance_after }
          : current
      )
      if (result.storageError) {
        setStorageBlocked(true)
        setTransferError(t('Unable to save transfer status. Please reload.'))
      }
      void loadOverview()
      void Promise.resolve()
        .then(() => props.onUserRefresh?.())
        .catch(() => setError(t('Refresh failed')))
    } catch {
      setStorageBlocked(true)
      setTransferError(t('Unable to save transfer status. Please reload.'))
    } finally {
      submittingRef.current = false
      setSubmitting(false)
    }
  }

  if (loading && !overview) {
    return (
      <section className='app-page-shell flex min-h-52 items-center justify-center gap-2 p-5'>
        <Loader2 className='text-primary size-4 animate-spin' />
        <span className='text-muted-foreground text-sm'>
          {t('Loading transfer settings...')}
        </span>
      </section>
    )
  }

  if (!overview) {
    return (
      <section className='app-page-shell p-5 text-center'>
        <p className='text-destructive text-sm'>{error}</p>
        <Button
          type='button'
          variant='outline'
          size='sm'
          className='mt-3'
          onClick={() => void loadOverview()}
        >
          <RefreshCw className='size-4' />
          {t('Retry')}
        </Button>
      </section>
    )
  }

  return (
    <>
      {error ? (
        <div role='alert' className='flex items-center gap-3 text-sm'>
          <p className='text-destructive'>{error}</p>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={loading}
            onClick={() => void loadOverview()}
          >
            <RefreshCw className='size-4' />
            {t('Retry')}
          </Button>
        </div>
      ) : null}
      <WalletPeerTransferPanel
        overview={overview}
        locked={locked}
        recipientId={pendingIntent?.recipient.external_id ?? recipientId}
        recipient={pendingIntent?.recipient ?? recipient}
        recipientLoading={recipientLoading}
        amount={
          pendingIntent
            ? String(pendingIntent.amountQuota / pendingIntent.quotaPerUSD)
            : amount
        }
        disabled={Boolean(pendingIntent) || submitting || storageBlocked}
        retrying={Boolean(pendingIntent)}
        error={
          transferError ||
          (pendingIntent
            ? t('Transfer result unknown. Retry this transfer.')
            : '')
        }
        balanceUSD={balanceUSD}
        maxTransferUSD={maxTransferUSD}
        minimumUSD={minimumUSD}
        canContinue={canContinue}
        onRecipientIdChange={(value) => {
          setRecipientId(value)
          setRecipient(null)
        }}
        onLookupRecipient={() => void lookupRecipient()}
        onAmountChange={setAmount}
        onOpenHistory={props.onOpenHistory}
        onOpenPassword={() => password.setOpen(true)}
        onReview={() => setConfirmOpen(true)}
      />

      <WalletTransferConfirmDialog
        open={confirmOpen}
        onOpenChange={(open) => {
          if (!submittingRef.current) setConfirmOpen(open)
        }}
        recipient={pendingIntent?.recipient ?? recipient}
        amountLabel={amountLabel}
        feeLabel={feeLabel}
        totalDebitLabel={totalDebitLabel}
        submitting={submitting}
        retrying={Boolean(pendingIntent)}
        error={transferError}
        onConfirm={submitTransfer}
      />
      <WalletTransferPasswordDialog
        open={password.open}
        onOpenChange={password.setOpen}
        passwordSet={overview.security.password_set}
        requiresAccountPassword={overview.security.requires_account_password}
        emailBound={overview.security.email_bound}
        emailMasked={overview.security.email_masked}
        emailSending={password.emailSending}
        emailSecondsLeft={password.emailSecondsLeft}
        emailCountdownActive={password.emailCountdownActive}
        submitting={password.submitting}
        onSendEmailCode={() => void password.sendEmailCode()}
        onBindEmail={() => window.location.assign('/profile')}
        onSubmit={password.configure}
      />
      <SecureVerificationDialog
        open={password.verification.open}
        onOpenChange={password.verification.setOpen}
        methods={password.verification.methods}
        state={password.verification.state}
        onVerify={(method, code) => {
          void password.verification.executeVerification(method, code)
        }}
        onCancel={password.verification.cancel}
        onCodeChange={password.verification.setCode}
        onMethodChange={password.verification.switchMethod}
      />
    </>
  )
}

function buildWalletTransferRequestId() {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) {
    return crypto.randomUUID()
  }
  return `wallet-transfer-${Date.now()}`
}
