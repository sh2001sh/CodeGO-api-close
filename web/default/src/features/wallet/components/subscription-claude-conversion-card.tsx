import { useMemo, useState } from 'react'
import { ArrowRightLeft, Loader2, Percent } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useSystemConfigStore } from '@/stores/system-config-store'
import { formatQuota, formatUsdAmount } from '@/lib/format'
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
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Slider } from '@/components/ui/slider'
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
  const quotaPerUnit = useSystemConfigStore(
    (state) => state.config.currency.quotaPerUnit
  )
  const eligibleSubscriptions = useMemo(
    () =>
      (props.subscriptionData?.subscriptions ?? []).filter(
        (item) => item.subscription.conversion_preview?.eligible === true
      ),
    [props.subscriptionData]
  )
  const blockedReasons = useMemo(
    () =>
      Array.from(
        new Set(
          (props.subscriptionData?.subscriptions ?? [])
            .map(
              (item) => item.subscription.conversion_preview?.ineligible_reason
            )
            .filter((reason): reason is string => Boolean(reason))
        )
      ),
    [props.subscriptionData]
  )
  const [selectedId, setSelectedId] = useState(0)
  const [conversionPercent, setConversionPercent] = useState(0)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  const selected =
    eligibleSubscriptions.find((item) => item.subscription.id === selectedId) ??
    eligibleSubscriptions[0]
  const preview = selected?.subscription.conversion_preview
  const maxPercent = Math.max(1, preview?.max_conversion_percent ?? 1)

  const planMeta = selected
    ? props.planTitles?.[selected.subscription.plan_id]
    : undefined
  const selectedConversionPercent = Math.min(
    maxPercent,
    Math.max(1, conversionPercent || maxPercent)
  )
  const unusedPercent = Number(preview?.unused_ratio || 0) * 100
  const convertsAllRemaining = selectedConversionPercent === maxPercent
  const selectedRatio = convertsAllRemaining
    ? Number(preview?.unused_ratio || 0)
    : selectedConversionPercent / 100
  const remainingPercent = Math.max(
    0,
    convertsAllRemaining ? 0 : unusedPercent - selectedConversionPercent
  )
  const estimatedQuota = Math.floor(
    Number(preview?.plan_price_amount || 0) * selectedRatio * quotaPerUnit
  )
  const endsPass = convertsAllRemaining
  const quickPercentages = Array.from(
    new Set([25, 50, maxPercent].filter((value) => value <= maxPercent))
  )

  const updatePercent = (value: number) => {
    if (!Number.isFinite(value)) return
    setConversionPercent(Math.min(maxPercent, Math.max(1, Math.round(value))))
  }

  const submit = async () => {
    if (!selected || !preview?.eligible) return
    setSubmitting(true)
    try {
      const result = await createSubscriptionClaudeConversion({
        subscriptionId: selected.subscription.id,
        conversionPercent: selectedConversionPercent,
        requestId: buildRequestId(),
      })
      if (!result.success || !result.data) {
        toast.error(result.message || t('Monthly pass conversion failed.'))
        return
      }
      toast.success(
        result.data.subscription_ended
          ? t(
              'Converted all remaining monthly-pass quota into {{amount}} permanent universal credit.',
              { amount: formatQuota(result.data.target_quota) }
            )
          : t(
              'Converted {{percent}}% into {{amount}} permanent universal credit.',
              {
                percent: result.data.conversion_percent,
                amount: formatQuota(result.data.target_quota),
              }
            )
      )
      setConfirmOpen(false)
      await props.onRefresh?.()
    } finally {
      setSubmitting(false)
    }
  }

  if (props.loading) {
    return (
      <div
        className='space-y-3'
        aria-label={t('Loading monthly pass conversion')}
      >
        <Skeleton className='h-5 w-36' />
        <Skeleton className='h-16 w-full' />
        <Skeleton className='h-9 w-full' />
      </div>
    )
  }

  return (
    <section className='space-y-4'>
      <div className='flex items-center gap-2'>
        <ArrowRightLeft className='text-primary size-4' aria-hidden='true' />
        <h3 className='text-sm font-semibold'>
          {t('Monthly pass conversion')}
        </h3>
      </div>

      {eligibleSubscriptions.length === 0 ? (
        <div className='text-muted-foreground space-y-1 text-sm leading-6'>
          <p>
            {t('No active monthly pass is currently eligible for conversion.')}
          </p>
          {blockedReasons.map((reason) => (
            <p key={reason} className='text-warning'>
              {reason}
            </p>
          ))}
        </div>
      ) : (
        <>
          <div className='grid gap-2 sm:grid-cols-2'>
            {eligibleSubscriptions.map((item) => {
              const itemPreview = item.subscription.conversion_preview
              const meta = props.planTitles?.[item.subscription.plan_id]
              const selectedItem = item.subscription.id === selectedId
              return (
                <button
                  key={item.subscription.id}
                  type='button'
                  onClick={() => {
                    setSelectedId(item.subscription.id)
                    setConversionPercent(
                      itemPreview?.max_conversion_percent ?? 1
                    )
                  }}
                  aria-pressed={selectedItem}
                  className={`focus-visible:ring-ring rounded-md border p-3 text-left transition-colors focus-visible:ring-2 focus-visible:outline-none ${
                    selectedItem
                      ? 'border-primary bg-primary/5'
                      : 'hover:bg-muted/50'
                  }`}
                >
                  <div className='flex items-center justify-between gap-3'>
                    <span className='text-sm font-medium'>
                      {meta?.title ||
                        `${t('Monthly pass')} #${item.subscription.id}`}
                    </span>
                    <span className='text-muted-foreground text-xs'>
                      {t('{{percent}}% available', {
                        percent: (
                          Number(itemPreview?.unused_ratio || 0) * 100
                        ).toFixed(2),
                      })}
                    </span>
                  </div>
                  <div className='text-muted-foreground mt-1 text-xs'>
                    {t('List price {{price}} · used {{used}}%', {
                      price: formatUsdAmount(
                        itemPreview?.plan_price_amount || 0
                      ),
                      used: (
                        100 -
                        Number(itemPreview?.unused_ratio || 0) * 100
                      ).toFixed(0),
                    })}
                  </div>
                </button>
              )
            })}
          </div>

          <div className='space-y-3 rounded-md border p-3'>
            <div className='flex items-center justify-between gap-3'>
              <label
                htmlFor='monthly-pass-conversion-percent'
                className='text-sm font-medium'
              >
                {t('Conversion percentage')}
              </label>
              <div className='relative w-24'>
                <Input
                  id='monthly-pass-conversion-percent'
                  type='number'
                  min={1}
                  max={maxPercent}
                  step={1}
                  value={selectedConversionPercent}
                  onChange={(event) =>
                    updatePercent(Number(event.target.value))
                  }
                  className='pr-7 text-right tabular-nums'
                />
                <Percent className='text-muted-foreground pointer-events-none absolute top-2 right-2 size-4' />
              </div>
            </div>
            <Slider
              min={1}
              max={maxPercent}
              step={1}
              value={[selectedConversionPercent]}
              onValueChange={(value) =>
                updatePercent(Array.isArray(value) ? value[0] : value)
              }
              aria-label={t('Conversion percentage')}
            />
            <div className='flex flex-wrap gap-2'>
              {quickPercentages.map((value) => (
                <Button
                  key={value}
                  type='button'
                  variant={
                    selectedConversionPercent === value
                      ? 'secondary'
                      : 'outline'
                  }
                  size='sm'
                  onClick={() => setConversionPercent(value)}
                >
                  {value === maxPercent
                    ? t('Convert all remaining')
                    : `${value}%`}
                </Button>
              ))}
            </div>
            <dl className='grid grid-cols-2 gap-x-4 gap-y-2 text-sm'>
              <dt className='text-muted-foreground'>{t('Estimated credit')}</dt>
              <dd className='text-right font-semibold tabular-nums'>
                {formatQuota(estimatedQuota)}
              </dd>
              <dt className='text-muted-foreground'>
                {t('Monthly pass remaining')}
              </dt>
              <dd className='text-right tabular-nums'>
                {remainingPercent.toFixed(0)}%
              </dd>
            </dl>
          </div>
        </>
      )}

      <Button
        type='button'
        className='w-full'
        disabled={!selected || submitting}
        onClick={() => setConfirmOpen(true)}
      >
        {submitting ? <Loader2 className='animate-spin' /> : <ArrowRightLeft />}
        {convertsAllRemaining
          ? t('Convert all remaining, receive {{amount}}', {
              amount: formatQuota(estimatedQuota),
            })
          : t('Convert {{percent}}%, receive {{amount}}', {
              percent: selectedConversionPercent,
              amount: formatQuota(estimatedQuota),
            })}
      </Button>

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Confirm monthly pass conversion')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'Convert {{percent}}% of {{plan}} for approximately {{credit}} permanent universal credit. This consumes {{quota}} monthly-pass quota and leaves {{remaining}}%. After any conversion, this monthly pass cannot use an invitation quota refresh. {{ending}} This cannot be undone.',
                {
                  percent: convertsAllRemaining
                    ? unusedPercent.toFixed(2)
                    : selectedConversionPercent,
                  plan: planMeta?.title || t('monthly pass'),
                  credit: formatQuota(estimatedQuota),
                  quota: formatSubscriptionQuotaAmount(
                    convertsAllRemaining
                      ? Number(preview?.remaining_quota || 0)
                      : Math.floor(
                          Number(selected?.subscription.amount_total || 0) *
                            selectedRatio
                        )
                  ),
                  remaining: remainingPercent.toFixed(0),
                  ending: endsPass
                    ? t('The monthly pass will end after this conversion.')
                    : t(
                        'The monthly pass will remain active with its remaining quota.'
                      ),
                }
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={submitting}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction disabled={submitting} onClick={submit}>
              {t('Confirm conversion')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  )
}
