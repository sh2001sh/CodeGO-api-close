import { useEffect, useMemo, useState } from 'react'
import { ArrowRightLeft, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatQuota } from '@/lib/format'
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
import { createSubscriptionClaudeConversion } from '@/features/subscriptions/api'
import { formatSubscriptionQuotaAmount } from '@/features/subscriptions/lib'
import type { SelfSubscriptionData } from '@/features/subscriptions/types'

interface SubscriptionClaudeConversionCardProps {
  subscriptionData?: SelfSubscriptionData | null
  loading?: boolean
  mode?: 'wallet' | 'dashboard'
  planTitles?: Record<number, { title: string; subtitle: string }>
  onRefresh?: () => Promise<void>
}

function buildRequestId(): string {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) {
    return crypto.randomUUID()
  }
  return `monthly-pass-conversion-${Date.now()}`
}

export function SubscriptionClaudeConversionCard(
  props: SubscriptionClaudeConversionCardProps
) {
  const { t } = useTranslation()
  const eligibleSubscriptions = useMemo(
    () =>
      (props.subscriptionData?.subscriptions ?? []).filter(
        (item) => item.subscription.conversion_preview?.eligible === true
      ),
    [props.subscriptionData]
  )
  const [selectedId, setSelectedId] = useState(0)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (!eligibleSubscriptions.some((item) => item.subscription.id === selectedId)) {
      setSelectedId(eligibleSubscriptions[0]?.subscription.id ?? 0)
    }
  }, [eligibleSubscriptions, selectedId])

  const selected = eligibleSubscriptions.find(
    (item) => item.subscription.id === selectedId
  )
  const preview = selected?.subscription.conversion_preview
  const planMeta = selected
    ? props.planTitles?.[selected.subscription.plan_id]
    : undefined

  const submit = async () => {
    if (!selectedId || !preview?.eligible) return
    setSubmitting(true)
    try {
      const result = await createSubscriptionClaudeConversion({
        subscriptionId: selectedId,
        requestId: buildRequestId(),
      })
      if (!result.success || !result.data) {
        toast.error(result.message || t('Monthly pass conversion failed.'))
        return
      }
      toast.success(
        t('Converted to {{amount}} permanent universal credit.', {
          amount: formatQuota(result.data.target_quota),
        })
      )
      setConfirmOpen(false)
      await props.onRefresh?.()
    } finally {
      setSubmitting(false)
    }
  }

  if (props.loading) {
    return (
      <div className='flex min-h-36 items-center justify-center'>
        <Loader2 className='text-muted-foreground size-5 animate-spin' />
      </div>
    )
  }

  return (
    <section className='space-y-3'>
      <div className='flex items-center gap-2'>
        <ArrowRightLeft className='text-primary size-4' aria-hidden='true' />
        <h3 className='text-sm font-semibold'>{t('Monthly pass conversion')}</h3>
      </div>

      {eligibleSubscriptions.length === 0 ? (
        <p className='text-muted-foreground text-sm'>
          {t('No active monthly pass is currently eligible for conversion.')}
        </p>
      ) : (
        <div className='grid gap-2'>
          {eligibleSubscriptions.map((item) => {
            const itemPreview = item.subscription.conversion_preview
            const meta = props.planTitles?.[item.subscription.plan_id]
            const selectedItem = item.subscription.id === selectedId
            return (
              <button
                key={item.subscription.id}
                type='button'
                onClick={() => setSelectedId(item.subscription.id)}
                className={`rounded-md border p-3 text-left transition-colors ${
                  selectedItem
                    ? 'border-primary bg-primary/5'
                    : 'hover:bg-muted/50'
                }`}
              >
                <div className='flex items-center justify-between gap-3'>
                  <span className='text-sm font-medium'>
                    {meta?.title || `${t('Monthly pass')} #${item.subscription.id}`}
                  </span>
                  <span className='text-sm font-semibold'>
                    {formatQuota(itemPreview?.preview_quota || 0)}
                  </span>
                </div>
                <div className='text-muted-foreground mt-1 text-xs'>
                  {t('List price {{price}}, unused {{percent}}%', {
                    price: Number(itemPreview?.plan_price_amount || 0).toFixed(2),
                    percent: (
                      Number(itemPreview?.unused_ratio || 0) * 100
                    ).toFixed(2),
                  })}
                </div>
              </button>
            )
          })}
        </div>
      )}

      <Button
        type='button'
        className='w-full'
        disabled={!selected || submitting}
        onClick={() => setConfirmOpen(true)}
      >
        {submitting ? <Loader2 className='animate-spin' /> : <ArrowRightLeft />}
        {t('Convert selected monthly pass')}
      </Button>

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Confirm monthly pass conversion')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'The entire {{plan}} will end immediately. Its remaining {{remaining}} quota, equal to {{percent}}% of the pass, will convert from the {{price}} list price into approximately {{credit}} permanent universal credit. This cannot be undone.',
                {
                  plan: planMeta?.title || t('monthly pass'),
                  remaining: formatSubscriptionQuotaAmount(
                    preview?.remaining_quota || 0
                  ),
                  percent: (Number(preview?.unused_ratio || 0) * 100).toFixed(2),
                  price: Number(preview?.plan_price_amount || 0).toFixed(2),
                  credit: formatQuota(preview?.preview_quota || 0),
                }
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={submitting}>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction disabled={submitting} onClick={submit}>
              {t('Confirm conversion')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  )
}
