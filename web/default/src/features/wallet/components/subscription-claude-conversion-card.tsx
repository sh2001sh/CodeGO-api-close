import { useMemo, useRef, useState } from 'react'
import { ArrowRightLeft, Loader2, Percent } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useSystemConfigStore } from '@/stores/system-config-store'
import { formatNumber, formatQuota } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
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
import {
  formatSubscriptionQuotaAmount,
  isCreditPackSubscription,
} from '@/features/subscriptions/lib'
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
        (item) =>
          !isCreditPackSubscription(item.subscription) &&
          item.subscription.conversion_preview?.eligible === true &&
          item.subscription.conversion_preview?.target_type === 'wallet'
      ),
    [props.subscriptionData]
  )
  const blockedReasons = useMemo(
    () =>
      Array.from(
        new Set(
          (props.subscriptionData?.subscriptions ?? [])
            .filter((item) => !isCreditPackSubscription(item.subscription))
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
  const submittingRef = useRef(false)
  const [requestId, setRequestId] = useState(buildRequestId)

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
  const estimatedBaseQuota = Math.floor(
    Number(preview?.plan_price_amount || 0) * selectedRatio * quotaPerUnit
  )
  const bonusPercent = Number(preview?.bonus_percent || 0)
  const estimatedBonusQuota = Math.floor(
    (estimatedBaseQuota * bonusPercent) / 100
  )
  const estimatedQuota = estimatedBaseQuota + estimatedBonusQuota
  const endsPass = convertsAllRemaining
  const quickPercentages = Array.from(
    new Set([25, 50, maxPercent].filter((value) => value <= maxPercent))
  )

  const updatePercent = (value: number) => {
    if (!Number.isFinite(value)) return
    setConversionPercent(Math.min(maxPercent, Math.max(1, Math.round(value))))
    setRequestId(buildRequestId())
  }

  const submit = async () => {
    if (!selected || !preview?.eligible || submittingRef.current) return
    submittingRef.current = true
    setSubmitting(true)
    try {
      const result = await createSubscriptionClaudeConversion({
        subscriptionId: selected.subscription.id,
        conversionPercent: selectedConversionPercent,
        requestId,
      })
      if (!result.success || !result.data) {
        toast.error(result.message || t('Monthly pass conversion failed.'))
        return
      }
      toast.success(
        t('已转入钱包 {{amount}}', {
          amount: formatQuota(result.data.target_quota),
        })
      )
      setConfirmOpen(false)
      setRequestId(buildRequestId())
      await props.onRefresh?.()
    } catch (error) {
      handleServerError(error)
    } finally {
      submittingRef.current = false
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
        <h3 className='text-sm font-semibold'>{t('旧套餐转余额')}</h3>
        {bonusPercent > 0 && (
          <span className='bg-primary/10 text-primary ml-auto rounded-full px-3 py-1 text-xs font-medium'>
            {t('集享增额 +{{percent}}%', { percent: bonusPercent })}
          </span>
        )}
      </div>

      <details className='bg-muted/50 rounded-xl p-4 text-sm'>
        <summary className='cursor-pointer font-medium'>
          {t('转换规则与 299 套餐示例')}
        </summary>
        <dl className='mt-4 grid gap-4 leading-7'>
          <div>
            <dt className='font-medium'>{t('折算方式')}</dt>
            <dd className='text-muted-foreground'>
              {t(
                '基础余额 = 折算基数 × 转换额度占旧套餐总额度的比例；集享增额 = 基础余额 × 对应集享增额比例；钱包到账 = 基础余额 + 集享增额。全部转换按旧套餐剩余额度 ÷ 总额度计算，与剩余天数无关。金额以最小计费单位向下取整。'
              )}
            </dd>
          </div>
          <div>
            <dt className='font-medium'>{t('集享增额')}</dt>
            <dd className='text-muted-foreground'>
              {t(
                '按该旧套餐所属集享计划的有效付费参与人数计算已达成档位：3 至 4 人增额 3%，5 人及以上增额 5%，未达 3 人无增额。占位成员和未成功付款成员不计入，实际比例以转换预览为准。'
              )}
            </dd>
          </div>
          <div>
            <dt className='font-medium'>{t('折算基数')}</dt>
            <dd className='text-muted-foreground'>
              {t(
                '采用该旧套餐对应的单笔成功付款金额数值；无付款订单的赠送套餐沿用原方案价格。当前转换按金额数值折算 credits，不使用货币汇率。多笔续费或升级补差记录需先核定。'
              )}
            </dd>
          </div>
          <div>
            <dt className='font-medium'>{t('299 套餐示例')}</dt>
            <dd className='text-muted-foreground'>
              {t(
                '折算基数为 299：未参与集享且全部未使用，可转 299 credits；剩余 50% 可转 149.5 credits。集享增额为 3% 时，全部未使用到账 307.97 credits；增额为 5% 时到账 313.95 credits。'
              )}
            </dd>
          </div>
          <div>
            <dt className='font-medium'>{t('转换后权益')}</dt>
            <dd className='text-muted-foreground'>
              {t(
                '基础金额与集享增额分别记账，一同计入可用钱包，按钱包价格消费。转换同步扣减旧套餐额度；全部转换后旧套餐结束，部分转换保留原有效期。转换金额及集享增额不作为新增现金实付，不产生邀请返利或现金退款。操作无法撤销；历史转换记录保持原权益。'
              )}
            </dd>
          </div>
          <div>
            <dt className='font-medium'>{t('刷新限制')}</dt>
            <dd className='text-muted-foreground'>
              {t(
                '旧套餐有效期内已使用邀请刷新的，当前不支持转换。未使用的刷新次数可在“刷新次数兑套餐”中兑换最低档套餐，独立于旧套餐转余额。实际可转换额度以确认窗口和服务端校验为准。'
              )}
            </dd>
          </div>
        </dl>
      </details>

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
            {' '}
            {eligibleSubscriptions.map((item) => {
              const itemPreview = item.subscription.conversion_preview
              const meta = props.planTitles?.[item.subscription.plan_id]
              const selectedItem =
                item.subscription.id === selected?.subscription.id
              return (
                <button
                  key={item.subscription.id}
                  type='button'
                  onClick={() => {
                    setRequestId(buildRequestId())
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
                        item.plan?.title ||
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
                    {t('折算基数 {{price}} · 已用 {{used}}%', {
                      price: formatNumber(itemPreview?.plan_price_amount || 0),
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

          <div className='bg-background/70 space-y-4 rounded-xl p-4 sm:p-5'>
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
                  onClick={() => updatePercent(value)}
                >
                  {value === maxPercent
                    ? t('Convert all remaining')
                    : `${value}%`}
                </Button>
              ))}
            </div>
            <dl className='grid grid-cols-2 gap-x-4 gap-y-2 text-sm'>
              <dt className='text-muted-foreground'>{t('基础余额')}</dt>
              <dd className='text-right tabular-nums'>
                {formatQuota(estimatedBaseQuota)}
              </dd>
              {bonusPercent > 0 && (
                <>
                  <dt className='text-muted-foreground'>
                    {t('集享增额 +{{percent}}%', { percent: bonusPercent })}
                  </dt>
                  <dd className='text-primary text-right tabular-nums'>
                    +{formatQuota(estimatedBonusQuota)}
                  </dd>
                </>
              )}
              <dt className='font-medium'>{t('钱包到账')}</dt>
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

      {eligibleSubscriptions.length > 0 && (
        <Button
          type='button'
          className='w-full'
          disabled={!selected || submitting}
          onClick={() => setConfirmOpen(true)}
        >
          {submitting ? (
            <Loader2 className='animate-spin' />
          ) : (
            <ArrowRightLeft />
          )}
          {convertsAllRemaining
            ? t('Convert all remaining, receive {{amount}}', {
                amount: formatQuota(estimatedQuota),
              })
            : t('Convert {{percent}}%, receive {{amount}}', {
                percent: selectedConversionPercent,
                amount: formatQuota(estimatedQuota),
              })}
        </Button>
      )}

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent className='commerce-dialog'>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('确认旧套餐转余额')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                '将 {{plan}} 的 {{percent}}% 转入钱包：基础 {{base}}，集享增额 {{bonus}}，合计 {{credit}}。扣除旧套餐 {{quota}}，旧套餐剩余 {{remaining}}%。{{ending}} 此操作无法撤销。',
                {
                  base: formatQuota(estimatedBaseQuota),
                  bonus: formatQuota(estimatedBonusQuota),
                  percent: convertsAllRemaining
                    ? unusedPercent.toFixed(2)
                    : selectedConversionPercent,
                  plan:
                    planMeta?.title ||
                    selected?.plan?.title ||
                    t('monthly pass'),
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
