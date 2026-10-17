import { useRef, useState } from 'react'
import { Gift, Loader2, RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatQuota } from '@/lib/format'
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
import { Skeleton } from '@/components/ui/skeleton'
import {
  consumeSubscriptionResetOpportunity,
  redeemSubscriptionResetOpportunity,
} from '@/features/subscriptions/api'
import type { SelfSubscriptionData } from '@/features/subscriptions/types'

interface WalletResetOpportunityPanelProps {
  subscriptionData?: SelfSubscriptionData | null
  loading?: boolean
  onRefresh?: () => Promise<void>
}

export function WalletResetOpportunityPanel(
  props: WalletResetOpportunityPanelProps
) {
  const { t } = useTranslation()
  const [action, setAction] = useState<'refresh' | 'redeem' | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const submittingRef = useRef(false)
  const [requestId, setRequestId] = useState(() => crypto.randomUUID())
  const preview = props.subscriptionData?.reset_redemption_preview
  const refreshPreview = props.subscriptionData?.reset_use_preview
  const available = props.subscriptionData?.reset_opportunity.available_count
  const canRedeem = preview?.eligible === true && Number(available) > 0
  const canRefresh = refreshPreview?.eligible === true && Number(available) > 0
  const refreshing = action === 'refresh'
  const canSubmit = refreshing ? canRefresh : canRedeem

  const submit = async () => {
    if (!action || !canSubmit || submittingRef.current) return
    submittingRef.current = true
    setSubmitting(true)
    try {
      const response = refreshing
        ? await consumeSubscriptionResetOpportunity()
        : await redeemSubscriptionResetOpportunity(requestId)
      if (!response.success || !response.data) {
        toast.error(response.message || t('操作失败'))
        return
      }
      setRequestId(crypto.randomUUID())
      if ('granted_quota' in response.data) {
        toast.success(
          t('已兑换套餐 {{amount}}', {
            amount: formatQuota(response.data.granted_quota),
          })
        )
      } else {
        toast.success(t('已刷新旧套餐额度'))
      }
      await props.onRefresh?.()
      window.dispatchEvent(new Event('subscription:changed'))
      setAction(null)
    } catch (error) {
      handleServerError(error)
    } finally {
      submittingRef.current = false
      setSubmitting(false)
    }
  }

  if (props.loading) return <Skeleton className='h-48 w-full rounded-2xl' />

  return (
    <section
      className='bg-muted/30 space-y-5 rounded-2xl p-5 sm:p-7'
      aria-labelledby='reset-redemption-title'
    >
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <h3
          id='reset-redemption-title'
          className='flex items-center gap-2 text-sm font-semibold'
        >
          <Gift className='text-primary size-4' aria-hidden />
          {t('刷新权益')}
        </h3>
        <span className='text-sm tabular-nums'>
          {t('可用次数 {{count}}', { count: available ?? '--' })}
        </span>
      </div>
      <div className='bg-background space-y-4 rounded-xl p-4 sm:p-5'>
        <h4 className='flex items-center gap-2 text-sm font-semibold'>
          <RotateCcw className='size-4' aria-hidden />
          {t('刷新旧套餐额度')}
        </h4>
        <dl className='grid grid-cols-2 gap-x-4 gap-y-3 text-sm'>
          <dt className='text-muted-foreground'>{t('旧套餐')}</dt>
          <dd className='text-right font-medium break-words'>
            {refreshPreview?.plan_title || '--'}
          </dd>
          <dt className='text-muted-foreground'>{t('恢复额度')}</dt>
          <dd className='text-right font-semibold tabular-nums'>
            {refreshPreview
              ? formatQuota(refreshPreview.cleared_used_amount)
              : '--'}
          </dd>
        </dl>
        {refreshPreview?.ineligible_reason && (
          <p role='status' className='text-muted-foreground text-sm'>
            {refreshPreview.ineligible_reason}
          </p>
        )}
        <Button
          variant='outline'
          disabled={!canRefresh || submitting}
          onClick={() => setAction('refresh')}
        >
          <RotateCcw aria-hidden />
          {t('刷新旧套餐额度')}
        </Button>
      </div>
      <div className='bg-background space-y-4 rounded-xl p-4 sm:p-5'>
        <h4 className='flex items-center gap-2 text-sm font-semibold'>
          <Gift className='size-4' aria-hidden />
          {t('刷新次数兑套餐')}
        </h4>
        <dl className='grid grid-cols-2 gap-x-4 gap-y-3 text-sm'>
          <dt className='text-muted-foreground'>{t('兑换套餐')}</dt>
          <dd className='text-right font-medium'>
            {preview?.plan_title || '--'}
          </dd>
          <dt className='text-muted-foreground'>{t('套餐额度')}</dt>
          <dd className='text-right font-semibold tabular-nums'>
            {preview?.granted_quota ? formatQuota(preview.granted_quota) : '--'}
          </dd>
          <dt className='text-muted-foreground'>{t('消耗次数')}</dt>
          <dd className='text-right'>{t('1 次 / 份')}</dd>
        </dl>
        {preview?.ineligible_reason && (
          <p role='status' className='text-muted-foreground text-sm'>
            {preview.ineligible_reason}
          </p>
        )}
        <Button
          disabled={!canRedeem || submitting}
          onClick={() => setAction('redeem')}
        >
          {submitting ? (
            <Loader2 className='animate-spin' aria-hidden />
          ) : (
            <Gift aria-hidden />
          )}
          {t('兑换 1 份套餐')}
        </Button>
      </div>
      <details className='text-sm leading-7'>
        <summary className='cursor-pointer font-medium'>
          {t('权益规则')}
        </summary>
        <p className='text-muted-foreground mt-3'>
          {t(
            '刷新旧套餐与兑换新套餐共用可用次数，每次操作成功扣除 1 次。旧套餐刷新每月最多 1 次，保留原有效期与扣费倍率；已转换的旧套餐不可刷新，加油及额外充值额度不恢复。'
          )}
        </p>
        <p className='text-muted-foreground mt-2'>
          {t(
            '每 1 次可用刷新次数可兑换 1 份当前最低档套餐，成功后扣除 1 次；剩余次数可继续兑换。兑换套餐为永久额度，按钱包同价消费。兑换属于历史权益赠送，不作为现金实付，不产生邀请返利或现金退款。'
          )}
        </p>
      </details>
      <AlertDialog
        open={action !== null}
        onOpenChange={(open) => {
          if (!open && !submittingRef.current) setAction(null)
        }}
      >
        <AlertDialogContent className='commerce-dialog'>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {refreshing ? t('确认刷新旧套餐') : t('确认兑换套餐')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {refreshing
                ? t(
                    '扣除 1 次刷新机会，刷新 {{plan}}（#{{id}}），恢复 {{amount}} 额度。原有效期与扣费倍率保持。此操作无法撤销。',
                    {
                      plan: refreshPreview?.plan_title || '--',
                      id: refreshPreview?.subscription_id || '--',
                      amount: formatQuota(
                        refreshPreview?.cleared_used_amount || 0
                      ),
                    }
                  )
                : t(
                    '扣除 1 次刷新机会，兑换 {{plan}}，获得 {{amount}} 永久套餐额度。成功后剩余 {{count}} 次。此操作无法撤销。',
                    {
                      plan: preview?.plan_title || '--',
                      amount: formatQuota(preview?.granted_quota || 0),
                      count: Math.max(0, Number(available || 0) - 1),
                    }
                  )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={submitting}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={!canSubmit || submitting}
              onClick={(event) => {
                event.preventDefault()
                void submit()
              }}
            >
              {submitting && <Loader2 className='animate-spin' aria-hidden />}
              {refreshing ? t('确认刷新') : t('确认兑换')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  )
}
