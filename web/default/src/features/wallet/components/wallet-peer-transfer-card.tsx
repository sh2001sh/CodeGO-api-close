import { useCallback, useEffect, useMemo, useState } from 'react'
import { Loader2, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatUsdAmount, quotaUnitsToUsd } from '@/lib/format'
import { Button } from '@/components/ui/button'
import {
  SecureVerificationDialog,
  useSecureVerification,
} from '@/features/auth/secure-verification'
import {
  configureWalletTransferPassword,
  createWalletTransfer,
  getWalletTransfers,
  isApiSuccess,
  lookupWalletTransferRecipient,
} from '../api'
import type {
  ConfigureWalletTransferPasswordRequest,
  WalletTransferOverview,
  WalletTransferRecipient,
} from '../types'
import { WalletPeerTransferPanel } from './wallet-peer-transfer-panel'
import { WalletTransferConfirmDialog } from './wallet-transfer-confirm-dialog'
import { WalletTransferPasswordDialog } from './wallet-transfer-password-dialog'

export function WalletPeerTransferCard(props: {
  onUserRefresh?: () => Promise<void>
  onOpenHistory?: () => void
}) {
  const { t } = useTranslation()
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
  const [passwordDialogOpen, setPasswordDialogOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [passwordSubmitting, setPasswordSubmitting] = useState(false)
  const [currentTimestamp, setCurrentTimestamp] = useState(0)

  const loadOverview = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const response = await getWalletTransfers()
      if (!isApiSuccess(response) || !response.data) {
        throw new Error(response.message || t('Failed to load transfer data.'))
      }
      setOverview(response.data)
      setCurrentTimestamp(Math.floor(Date.now() / 1000))
    } catch (reason) {
      setError(
        reason instanceof Error
          ? reason.message
          : t('Failed to load transfer data.')
      )
    } finally {
      setLoading(false)
    }
  }, [t])

  const finishPasswordSetup = useCallback(async () => {
    toast.success(t('Payment password saved.'))
    setPasswordDialogOpen(false)
    await loadOverview()
  }, [loadOverview, t])

  const verification = useSecureVerification({
    onSuccess: () => void finishPasswordSetup(),
  })

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void loadOverview()
  }, [loadOverview])

  const quotaPerUSD = overview?.quota_per_usd || 500_000
  const amountUSD = Number(amount || 0)
  const amountQuota = Math.round(amountUSD * quotaPerUSD)
  const feeBPS = overview?.fee_bps ?? 100
  const feeQuota = Math.ceil((amountQuota * feeBPS) / 10_000)
  const totalDebitQuota = amountQuota + feeQuota
  const balanceUSD = quotaUnitsToUsd(overview?.balance || 0)
  const maxTransferUSD = quotaUnitsToUsd(
    Math.floor(((overview?.balance || 0) * 10_000) / (10_000 + feeBPS))
  )
  const minimumUSD = quotaUnitsToUsd(overview?.min_quota || quotaPerUSD / 100)
  const locked = (overview?.security.locked_until || 0) > currentTimestamp
  const amountValid =
    Number.isFinite(amountUSD) &&
    amountUSD >= minimumUSD &&
    totalDebitQuota <= (overview?.balance || 0)
  const canContinue =
    Boolean(recipient) &&
    amountValid &&
    Boolean(overview?.security.password_set) &&
    !locked &&
    !submitting

  const amountLabel = useMemo(
    () => formatUsdAmount(amountUSD || 0),
    [amountUSD]
  )
  const feeLabel = formatUsdAmount(quotaUnitsToUsd(feeQuota))
  const totalDebitLabel = formatUsdAmount(quotaUnitsToUsd(totalDebitQuota))

  const lookupRecipient = async () => {
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
      toast.error(
        reason instanceof Error ? reason.message : t('Recipient not found.')
      )
    } finally {
      setRecipientLoading(false)
    }
  }

  const configurePassword = async (
    request: ConfigureWalletTransferPasswordRequest
  ) => {
    const perform = async () => {
      const response = await configureWalletTransferPassword(request)
      if (!isApiSuccess(response)) {
        throw new Error(
          response.message || t('Failed to save payment password.')
        )
      }
      return response
    }

    setPasswordSubmitting(true)
    try {
      const passwordSet = overview?.security.password_set ?? false
      const needsAccountPassword =
        overview?.security.requires_account_password ?? true
      if (!passwordSet && !needsAccountPassword) {
        const result = await verification.withVerification(perform, {
          title: t('Verify before setting a payment password'),
          description: t(
            'Confirm your identity with 2FA or Passkey before enabling quota transfers.'
          ),
        })
        if (result) await finishPasswordSetup()
        return
      }
      await perform()
      await finishPasswordSetup()
    } catch (reason) {
      toast.error(
        reason instanceof Error
          ? reason.message
          : t('Failed to save payment password.')
      )
    } finally {
      setPasswordSubmitting(false)
    }
  }

  const submitTransfer = async (paymentPassword: string) => {
    if (!recipient || !amountValid) return
    setSubmitting(true)
    try {
      const response = await createWalletTransfer({
        recipient_external_id: recipient.external_id,
        amount_quota: amountQuota,
        payment_password: paymentPassword,
        request_id: buildWalletTransferRequestId(),
      })
      if (!isApiSuccess(response) || !response.data) {
        throw new Error(response.message || t('Quota transfer failed.'))
      }
      toast.success(t('Quota transfer completed.'))
      setConfirmOpen(false)
      setRecipient(null)
      setRecipientId('')
      setAmount('')
      await Promise.all([loadOverview(), props.onUserRefresh?.()])
    } catch (reason) {
      toast.error(
        reason instanceof Error ? reason.message : t('Quota transfer failed.')
      )
      await loadOverview()
    } finally {
      setSubmitting(false)
    }
  }

  if (loading) {
    return (
      <section className='app-page-shell flex min-h-52 items-center justify-center gap-2 p-5'>
        <Loader2 className='text-primary size-4 animate-spin' />
        <span className='text-muted-foreground text-sm'>
          {t('Loading transfer settings...')}
        </span>
      </section>
    )
  }

  if (error || !overview) {
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
      <WalletPeerTransferPanel
        overview={overview}
        locked={locked}
        recipientId={recipientId}
        recipient={recipient}
        recipientLoading={recipientLoading}
        amount={amount}
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
        onOpenPassword={() => setPasswordDialogOpen(true)}
        onReview={() => setConfirmOpen(true)}
      />

      <WalletTransferConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        recipient={recipient}
        amountLabel={amountLabel}
        feeLabel={feeLabel}
        totalDebitLabel={totalDebitLabel}
        submitting={submitting}
        onConfirm={submitTransfer}
      />
      <WalletTransferPasswordDialog
        open={passwordDialogOpen}
        onOpenChange={setPasswordDialogOpen}
        passwordSet={overview.security.password_set}
        requiresAccountPassword={overview.security.requires_account_password}
        submitting={passwordSubmitting}
        onSubmit={configurePassword}
      />
      <SecureVerificationDialog
        open={verification.open}
        onOpenChange={verification.setOpen}
        methods={verification.methods}
        state={verification.state}
        onVerify={(method, code) => {
          void verification.executeVerification(method, code)
        }}
        onCancel={verification.cancel}
        onCodeChange={verification.setCode}
        onMethodChange={verification.switchMethod}
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
