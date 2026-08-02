import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  ArrowRight,
  ArrowRightLeft,
  History,
  Loader2,
  RefreshCw,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  createWalletQuotaConversion,
  getWalletQuotaConversions,
  isApiSuccess,
} from '../api'
import type {
  WalletQuotaConversionDirection,
  WalletQuotaConversionOverview,
} from '../types'
import {
  AmountPanel,
  DirectionButton,
  formatInputAmount,
  formatUSD,
  ReceivePanel,
} from './wallet-quota-conversion-parts'

const STANDARD_TO_CLAUDE = 'standard_to_claude'
const CLAUDE_TO_STANDARD = 'claude_to_standard'

export function WalletQuotaConversionCard(props: {
  onUserRefresh?: () => Promise<void>
  onOpenHistory?: () => void
}) {
  const { t } = useTranslation()
  const [overview, setOverview] =
    useState<WalletQuotaConversionOverview | null>(null)
  const [direction, setDirection] =
    useState<WalletQuotaConversionDirection>(STANDARD_TO_CLAUDE)
  const [amount, setAmount] = useState('')
  const [selectedMaxSourceQuota, setSelectedMaxSourceQuota] = useState<
    number | null
  >(null)
  const [loading, setLoading] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [error, setError] = useState('')

  const loadOverview = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const response = await getWalletQuotaConversions()
      if (!isApiSuccess(response) || !response.data) {
        throw new Error(
          response.message || t('Failed to load conversion data.')
        )
      }
      setOverview(response.data)
    } catch (reason) {
      setError(
        reason instanceof Error
          ? reason.message
          : t('Failed to load conversion data.')
      )
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    void loadOverview()
  }, [loadOverview])

  const quotaPerUSD = overview?.quota_per_usd || 500_000
  const standardPerClaude = overview?.standard_per_claude || 4
  const sourceBalance =
    direction === STANDARD_TO_CLAUDE
      ? overview?.standard_quota || 0
      : overview?.claude_quota || 0
  const convertibleSourceBalance =
    direction === STANDARD_TO_CLAUDE
      ? sourceBalance - (sourceBalance % standardPerClaude)
      : sourceBalance
  const enteredSourceAmountUSD = Number(amount || 0)
  const sourceQuota =
    selectedMaxSourceQuota ?? Math.round(enteredSourceAmountUSD * quotaPerUSD)
  const sourceAmountUSD = sourceQuota / quotaPerUSD
  const targetQuota =
    direction === STANDARD_TO_CLAUDE
      ? Math.floor(sourceQuota / standardPerClaude)
      : sourceQuota * standardPerClaude
  const targetAmountUSD = targetQuota / quotaPerUSD
  const maxAmountUSD = convertibleSourceBalance / quotaPerUSD
  const canSubmit =
    !loading &&
    !submitting &&
    Number.isFinite(enteredSourceAmountUSD) &&
    sourceAmountUSD > 0 &&
    sourceQuota <= convertibleSourceBalance &&
    targetQuota > 0 &&
    (direction !== STANDARD_TO_CLAUDE || sourceQuota % standardPerClaude === 0)

  const directionMeta = useMemo(() => {
    const standardToClaude = direction === STANDARD_TO_CLAUDE
    return {
      sourceLabel: standardToClaude ? t('Standard balance') : t('Claude quota'),
      targetLabel: standardToClaude ? t('Claude quota') : t('Standard balance'),
      rate: standardToClaude
        ? `4 ${t('Standard balance')} = 1 ${t('Claude quota')}`
        : `1 ${t('Claude quota')} = 4 ${t('Standard balance')}`,
    }
  }, [direction, t])

  const switchDirection = (next: WalletQuotaConversionDirection) => {
    setDirection(next)
    setAmount('')
    setSelectedMaxSourceQuota(null)
  }

  const submit = async () => {
    if (!canSubmit) return
    setSubmitting(true)
    try {
      const response = await createWalletQuotaConversion({
        direction,
        source_quota: sourceQuota,
        request_id: buildRequestId(),
      })
      if (!isApiSuccess(response) || !response.data) {
        throw new Error(response.message || t('Conversion failed.'))
      }
      toast.success(t('Quota conversion completed.'))
      setAmount('')
      setSelectedMaxSourceQuota(null)
      setConfirmOpen(false)
      await Promise.all([loadOverview(), props.onUserRefresh?.()])
    } catch (reason) {
      toast.error(
        reason instanceof Error ? reason.message : t('Conversion failed.')
      )
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <section className='app-page-shell p-4 sm:p-5'>
      <div className='flex flex-wrap items-start justify-between gap-3'>
        <div>
          <div className='text-foreground flex items-center gap-2 text-sm font-semibold'>
            <ArrowRightLeft className='text-primary size-4' />
            {t('Wallet quota conversion')}
          </div>
          <p className='text-muted-foreground mt-1 text-xs leading-5'>
            {t(
              'Convert standard and Claude balances in either direction at a fixed 4:1 rate.'
            )}
          </p>
        </div>
        <div className='flex items-center gap-2'>
          {props.onOpenHistory ? (
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={props.onOpenHistory}
            >
              <History className='size-4' />
              {t('Conversion records')}
            </Button>
          ) : null}
          <span className='border-border bg-muted/45 text-foreground rounded-full border px-3 py-1 text-xs font-medium'>
            4 : 1
          </span>
        </div>
      </div>

      {loading ? (
        <div className='text-muted-foreground flex min-h-40 items-center justify-center gap-2 text-sm'>
          <Loader2 className='size-4 animate-spin' />
          {t('Loading conversion balances...')}
        </div>
      ) : error ? (
        <div className='border-destructive/30 bg-destructive/5 mt-4 rounded-lg border px-4 py-5 text-center'>
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
        </div>
      ) : (
        <>
          <div className='bg-muted/45 mt-4 grid grid-cols-2 rounded-lg p-1'>
            <DirectionButton
              active={direction === STANDARD_TO_CLAUDE}
              onClick={() => switchDirection(STANDARD_TO_CLAUDE)}
            >
              {t('Standard to Claude')}
            </DirectionButton>
            <DirectionButton
              active={direction === CLAUDE_TO_STANDARD}
              onClick={() => switchDirection(CLAUDE_TO_STANDARD)}
            >
              {t('Claude to standard')}
            </DirectionButton>
          </div>

          <div className='mt-4 grid items-stretch gap-2 md:grid-cols-[minmax(0,1fr)_2.5rem_minmax(0,1fr)]'>
            <AmountPanel
              label={directionMeta.sourceLabel}
              balance={sourceBalance / quotaPerUSD}
              input={amount}
              max={maxAmountUSD}
              onInput={(value) => {
                setAmount(value)
                setSelectedMaxSourceQuota(null)
              }}
              onMax={() => {
                setAmount(formatInputAmount(maxAmountUSD))
                setSelectedMaxSourceQuota(convertibleSourceBalance)
              }}
            />
            <button
              type='button'
              aria-label={t('Reverse conversion direction')}
              title={t('Reverse conversion direction')}
              className='border-border bg-background text-muted-foreground hover:text-foreground focus-visible:ring-ring mx-auto flex size-10 items-center justify-center rounded-full border transition focus-visible:ring-2 focus-visible:outline-none md:my-auto'
              onClick={() =>
                switchDirection(
                  direction === STANDARD_TO_CLAUDE
                    ? CLAUDE_TO_STANDARD
                    : STANDARD_TO_CLAUDE
                )
              }
            >
              <ArrowRight className='size-4 md:hidden' />
              <ArrowRightLeft className='hidden size-4 md:block' />
            </button>
            <ReceivePanel
              label={directionMeta.targetLabel}
              amount={targetAmountUSD}
              rate={directionMeta.rate}
            />
          </div>

          <div className='mt-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
            <p className='text-muted-foreground text-xs leading-5'>
              {t('Conversion is immediate and cannot be undone.')}
            </p>
            <Button
              type='button'
              disabled={!canSubmit}
              onClick={() => setConfirmOpen(true)}
            >
              <ArrowRightLeft className='size-4' />
              {t('Confirm conversion')}
            </Button>
          </div>
        </>
      )}

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Confirm wallet conversion')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This will deduct {{source}} from {{sourceWallet}} and credit {{target}} to {{targetWallet}} immediately.',
                {
                  source: formatUSD(sourceAmountUSD),
                  sourceWallet: directionMeta.sourceLabel,
                  target: formatUSD(targetAmountUSD),
                  targetWallet: directionMeta.targetLabel,
                }
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={submitting}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={!canSubmit}
              onClick={(event) => {
                event.preventDefault()
                void submit()
              }}
            >
              {submitting ? <Loader2 className='size-4 animate-spin' /> : null}
              {t('Confirm conversion')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  )
}

function buildRequestId() {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) {
    return crypto.randomUUID()
  }
  return `wallet-conversion-${Date.now()}`
}
