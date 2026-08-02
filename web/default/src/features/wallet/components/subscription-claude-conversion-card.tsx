import { useEffect, useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { ArrowRightLeft, Loader2, Sparkles } from 'lucide-react'
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
import { Input } from '@/components/ui/input'
import { createSubscriptionClaudeConversion } from '@/features/subscriptions/api'
import {
  formatSubscriptionQuotaAmount,
  parseSubscriptionQuotaUSDToUnits,
  subscriptionQuotaUnitsToUSD,
} from '@/features/subscriptions/lib'
import type {
  SelfSubscriptionData,
  UserSubscription,
} from '@/features/subscriptions/types'

interface SubscriptionClaudeConversionCardProps {
  subscriptionData?: SelfSubscriptionData | null
  loading?: boolean
  compact?: boolean
  mode?: 'wallet' | 'dashboard'
  planTitles?: Record<number, { title: string; subtitle: string }>
  onRefresh?: () => Promise<void>
}

function buildRequestId(): string {
  if (
    typeof crypto !== 'undefined' &&
    typeof crypto.randomUUID === 'function'
  ) {
    return crypto.randomUUID()
  }
  return `subscription-claude-${Date.now()}`
}

function formatDateTime(timestamp?: number): string {
  if (!timestamp) return '--'
  return new Date(timestamp * 1000).toLocaleString()
}

function getEligibleSubscriptions(data?: SelfSubscriptionData | null) {
  return (data?.subscriptions || []).filter(
    (item) => item.subscription.conversion_preview?.eligible
  )
}

function getPlanLabel(
  subscription: UserSubscription | undefined,
  planTitles: Record<number, { title: string; subtitle: string }> | undefined,
  fallback: string
) {
  if (!subscription) return fallback
  return planTitles?.[subscription.plan_id]?.title || fallback
}

export function SubscriptionClaudeConversionCard(
  props: SubscriptionClaudeConversionCardProps
) {
  const { t } = useTranslation()
  const eligibleSubscriptions = useMemo(
    () => getEligibleSubscriptions(props.subscriptionData),
    [props.subscriptionData]
  )
  const recentConversions = props.subscriptionData?.recent_conversions || []
  const config = props.subscriptionData?.conversion_config
  const ratioText =
    config && config.ratio_denominator > 0
      ? `${config.ratio_numerator}:${config.ratio_denominator}`
      : '1:10'

  const [selectedSubscriptionId, setSelectedSubscriptionId] =
    useState<number>(0)
  const [sourceQuotaInput, setSourceQuotaInput] = useState('')
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (eligibleSubscriptions.length === 0) {
      setSelectedSubscriptionId(0)
      return
    }
    const exists = eligibleSubscriptions.some(
      (item) => item.subscription.id === selectedSubscriptionId
    )
    if (!exists) {
      setSelectedSubscriptionId(eligibleSubscriptions[0].subscription.id)
    }
  }, [eligibleSubscriptions, selectedSubscriptionId])

  const selectedRecord = eligibleSubscriptions.find(
    (item) => item.subscription.id === selectedSubscriptionId
  )
  const selectedSubscription = selectedRecord?.subscription
  const selectedPlanMeta = selectedSubscription
    ? props.planTitles?.[selectedSubscription.plan_id]
    : undefined
  const sourceQuotaUSD = Number(sourceQuotaInput || 0)
  const sourceQuota = parseSubscriptionQuotaUSDToUnits(sourceQuotaUSD)
  const maxSourceQuota = Number(
    selectedSubscription?.conversion_preview?.max_source_quota || 0
  )
  const maxSourceQuotaUSD = subscriptionQuotaUnitsToUSD(maxSourceQuota)
  const previewClaudeQuota =
    sourceQuota > 0 && Number(config?.ratio_denominator || 0) > 0
      ? Math.floor(
          (sourceQuota * Number(config?.ratio_numerator || 0)) /
            Number(config?.ratio_denominator || 1)
        )
      : 0

  const totalConvertibleQuota = eligibleSubscriptions.reduce((sum, item) => {
    return (
      sum + Number(item.subscription.conversion_preview?.max_source_quota || 0)
    )
  }, 0)
  const totalPreviewClaudeQuota = eligibleSubscriptions.reduce((sum, item) => {
    return (
      sum +
      Number(item.subscription.conversion_preview?.preview_claude_quota || 0)
    )
  }, 0)

  const canSubmit =
    Boolean(config?.enabled) &&
    Boolean(selectedSubscriptionId) &&
    Number.isFinite(sourceQuotaUSD) &&
    sourceQuota > 0 &&
    sourceQuota <= maxSourceQuota &&
    previewClaudeQuota > 0 &&
    !submitting

  const submitConversion = async () => {
    if (!canSubmit || !selectedSubscriptionId) return
    setSubmitting(true)
    try {
      const result = await createSubscriptionClaudeConversion({
        subscriptionId: selectedSubscriptionId,
        sourceQuota,
        requestId: buildRequestId(),
      })
      if (!result.success || !result.data) {
        toast.error(
          result.message || t('Failed to convert plan quota to Claude quota.')
        )
        return
      }
      toast.success(
        t('{{plan}} converted to Claude quota', {
          plan: getPlanLabel(
            selectedSubscription,
            props.planTitles,
            t('Current plan')
          ),
        })
      )
      setSourceQuotaInput('')
      setConfirmOpen(false)
      await props.onRefresh?.()
      if (typeof window !== 'undefined') {
        window.dispatchEvent(new Event('subscription:changed'))
      }
    } catch {
      toast.error(t('Failed to convert plan quota to Claude quota.'))
    } finally {
      setSubmitting(false)
    }
  }

  if (props.mode === 'dashboard') {
    return (
      <div className='border-border bg-card rounded-2xl border px-4 py-4'>
        <div className='flex items-start justify-between gap-3'>
          <div>
            <div className='text-foreground flex items-center gap-2 text-sm font-semibold'>
              <Sparkles className='text-primary h-4 w-4' />
              {t('Convert plan quota to Claude')}
            </div>
            <div className='text-muted-foreground mt-1 text-xs leading-5'>
              {t(
                'Only active non-day plans are eligible. Enter USD and convert at {{ratio}} into permanent Claude quota; temporary plan quota may lose value.',
                { ratio: ratioText }
              )}
            </div>
          </div>
          <div className='border-border bg-muted text-muted-foreground rounded-full border px-3 py-1 text-xs'>
            {t('Maximum {{amount}}', {
              amount: formatSubscriptionQuotaAmount(totalConvertibleQuota),
            })}
          </div>
        </div>

        <div className='mt-3 grid gap-2 sm:grid-cols-3'>
          <QuickStat
            label={t('Convertible plan quota (USD)')}
            value={formatSubscriptionQuotaAmount(totalConvertibleQuota)}
          />
          <QuickStat label={t('Conversion rule')} value={ratioText} />
          <QuickStat
            label={t('Estimated maximum Claude quota')}
            value={formatQuota(totalPreviewClaudeQuota)}
          />
        </div>

        <Button
          className='mt-3 w-full justify-between'
          render={<Link to='/wallet' search={{ wallet_type: 'claude' }} />}
        >
          <span>{t('Convert in wallet')}</span>
          <ArrowRightLeft className='h-4 w-4' />
        </Button>
      </div>
    )
  }

  return (
    <div className='app-page-shell p-4'>
      <div className='flex items-start justify-between gap-3'>
        <div>
          <div className='text-foreground flex items-center gap-2 text-sm font-semibold'>
            <ArrowRightLeft className='text-primary h-4 w-4' />
            {t('Convert plan quota to Claude')}
          </div>
          <div className='text-muted-foreground mt-1 text-xs leading-5'>
            {t(
              'Only active non-day plans are eligible. Enter USD at the current {{ratio}} ratio. Converting temporary plan quota into permanent Claude quota cannot be undone.',
              { ratio: ratioText }
            )}
          </div>
        </div>
        <div className='border-border bg-background/80 text-foreground rounded-full border px-3 py-1 text-xs font-semibold'>
          Claude {formatQuota(props.subscriptionData?.claude_quota || 0)}
        </div>
      </div>

      {props.loading ? (
        <div className='border-border/70 bg-background/72 text-muted-foreground mt-3 rounded-2xl border px-3 py-6 text-center text-xs'>
          {t('Loading convertible plans...')}
        </div>
      ) : !config?.enabled ? (
        <div className='border-border/70 bg-background/60 text-muted-foreground mt-3 rounded-2xl border border-dashed px-3 py-3 text-xs'>
          {t('Plan-to-Claude conversion is currently disabled.')}
        </div>
      ) : eligibleSubscriptions.length === 0 ? (
        <div className='border-border/70 bg-background/60 text-muted-foreground mt-3 rounded-2xl border border-dashed px-3 py-3 text-xs'>
          {t(
            'No convertible plans are available. Only active non-day subscriptions are eligible.'
          )}
        </div>
      ) : (
        <>
          <div className='mt-3 space-y-2'>
            {eligibleSubscriptions.map((item) => {
              const checked = item.subscription.id === selectedSubscriptionId
              return (
                <button
                  key={item.subscription.id}
                  type='button'
                  onClick={() =>
                    setSelectedSubscriptionId(item.subscription.id)
                  }
                  className={`w-full rounded-2xl border px-3 py-3 text-left transition ${
                    checked
                      ? 'border-foreground bg-background/80'
                      : 'border-border/70 bg-muted/32'
                  }`}
                >
                  <div className='flex items-start justify-between gap-3'>
                    <div className='min-w-0'>
                      <div className='text-foreground text-sm font-semibold'>
                        {props.planTitles?.[item.subscription.plan_id]?.title ||
                          t('Subscription #{{id}}', {
                            id: item.subscription.id,
                          })}
                      </div>
                      <div className='text-muted-foreground mt-1 text-xs'>
                        {props.planTitles?.[item.subscription.plan_id]
                          ?.subtitle || t('Subscription')}{' '}
                        · {t('Expires')}:{' '}
                        {formatDateTime(item.subscription.end_time)}
                      </div>
                    </div>
                    <div className='text-muted-foreground text-right text-xs'>
                      <div>
                        {t('Maximum convertible')}{' '}
                        {formatSubscriptionQuotaAmount(
                          item.subscription.conversion_preview
                            ?.max_source_quota || 0
                        )}
                      </div>
                      <div className='text-foreground mt-1'>
                        {t('Maximum received')}{' '}
                        {formatQuota(
                          item.subscription.conversion_preview
                            ?.preview_claude_quota || 0
                        )}
                      </div>
                    </div>
                  </div>
                </button>
              )
            })}
          </div>

          <div className='app-subtle-panel mt-3 px-3 py-3'>
            <div className='text-muted-foreground text-[11px] font-medium'>
              {t('Enter quota to convert (USD)')}
            </div>
            <div className='mt-2 grid grid-cols-[minmax(0,1fr)_auto] gap-2'>
              <Input
                type='number'
                min='0.01'
                step='0.01'
                value={sourceQuotaInput}
                onChange={(event) => setSourceQuotaInput(event.target.value)}
                placeholder={t('Maximum {{amount}}', {
                  amount: maxSourceQuotaUSD.toFixed(2),
                })}
                className='h-10'
              />
              <Button
                className='h-10 px-4'
                disabled={!canSubmit}
                onClick={() => setConfirmOpen(true)}
              >
                {t('Convert')}
              </Button>
            </div>
            <div className='mt-2 grid gap-2 text-xs sm:grid-cols-2'>
              <QuickStat
                label={t('Plan quota used for this conversion (USD)')}
                value={formatSubscriptionQuotaAmount(sourceQuota)}
              />
              <QuickStat
                label={t('Estimated Claude quota')}
                value={formatQuota(previewClaudeQuota || 0)}
              />
            </div>
            {selectedSubscription ? (
              <div className='text-muted-foreground mt-2 text-xs'>
                {t('Approximate remaining convertible quota after conversion:')}{' '}
                {formatSubscriptionQuotaAmount(
                  Math.max(0, maxSourceQuota - sourceQuota)
                )}
              </div>
            ) : null}
            <div className='text-muted-foreground mt-2 text-xs leading-5'>
              {t(
                'USD input is for clarity; the backend still settles against the plan quota. Permanent Claude quota is rounded down.'
              )}
            </div>
          </div>
        </>
      )}

      {recentConversions.length > 0 ? (
        <div className='app-subtle-panel mt-3 px-3 py-3'>
          <div className='text-foreground text-sm font-semibold'>
            {t('Recent conversions')}
          </div>
          <div className='mt-2 space-y-2'>
            {recentConversions.slice(0, 3).map((item) => (
              <div
                key={item.id}
                className='flex items-center justify-between gap-3 text-xs'
              >
                <div className='text-muted-foreground'>
                  {formatDateTime(item.created_at)} ·{' '}
                  {t('Subscription #{{id}}', {
                    id: item.user_subscription_id,
                  })}
                </div>
                <div className='text-foreground font-medium'>
                  {formatSubscriptionQuotaAmount(item.source_quota)} →{' '}
                  {formatQuota(item.target_claude_quota)}
                </div>
              </div>
            ))}
          </div>
        </div>
      ) : null}

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Confirm conversion')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This will deduct {{amount}} from {{plan}} and credit approximately {{claude}} permanent Claude quota. This action cannot be undone.',
                {
                  amount: formatSubscriptionQuotaAmount(sourceQuota),
                  plan: selectedPlanMeta?.title || t('Current plan'),
                  claude: formatQuota(previewClaudeQuota || 0),
                }
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              disabled={!canSubmit}
              onClick={(event) => {
                event.preventDefault()
                void submitConversion()
              }}
            >
              {submitting ? (
                <Loader2 className='mr-1 h-4 w-4 animate-spin' />
              ) : null}
              {t('Confirm conversion')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

function QuickStat(props: { label: string; value: string }) {
  return (
    <div className='app-subtle-panel px-3 py-3'>
      <div className='text-muted-foreground text-[11px] font-medium'>
        {props.label}
      </div>
      <div className='text-foreground mt-1 font-mono text-sm font-semibold tabular-nums'>
        {props.value}
      </div>
    </div>
  )
}
