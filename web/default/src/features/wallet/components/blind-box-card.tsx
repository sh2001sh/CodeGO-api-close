import { useCallback, useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { History, RefreshCw, Ticket } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  calculateBlindBoxAmount,
  getBlindBoxSelf,
  isApiSuccess,
  activateBlindBoxProp,
  pauseBlindBoxProp,
  convertBlindBoxProp,
} from '../api'
import type {
  BlindBoxProp,
  BlindBoxRecord,
  BlindBoxSelfData,
  PaymentMethod,
} from '../types'
import { BlindBoxContent } from './blind-box-content'
import {
  canQuoteBlindBoxAmount,
  EMPTY_PAYMENT_STATE,
  EMPTY_PRIZE_STATE,
  type PrizeDialogState,
} from './blind-box-dialog-data'
import { BlindBoxPrizeDialog } from './blind-box-dialogs'
import { BlindBoxHistorySheet } from './blind-box-history-sheet'
import { BlindBoxPaymentDialog } from './blind-box-payment-dialog'
import { BlindBoxPropGiftDialog } from './blind-box-prop-gift-dialog'
import { BlindBoxPropsDialog } from './blind-box-props-dialog'
import { BlindBoxSidebar } from './blind-box-sidebar'
import './blind-box-workspace.css'
import { useBlindBoxChangedEvent } from './use-blind-box-changed-event'
import { useBlindBoxPayment } from './use-blind-box-payment'

interface BlindBoxCardProps {
  onSubscriptionRefresh: () => Promise<void>
  onUserRefresh: () => Promise<void>
  paymentResult?: 'success' | 'pending' | 'fail'
}

export function BlindBoxCard(props: BlindBoxCardProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [data, setData] = useState<BlindBoxSelfData | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [selectedQuantity, setSelectedQuantity] = useState(1)
  const [selectedPaymentMethod, setSelectedPaymentMethod] =
    useState<PaymentMethod | null>(null)
  const cashQuoteEnabled = canQuoteBlindBoxAmount(
    data,
    selectedQuantity,
    selectedPaymentMethod,
    !loading && !loadError
  )
  const amountQuote = useQuery({
    queryKey: ['blind-box', 'amount', selectedQuantity],
    enabled: cashQuoteEnabled,
    retry: false,
    queryFn: async ({ signal }) => {
      const response = await calculateBlindBoxAmount(
        { quantity: selectedQuantity },
        signal
      )
      const amount = Number.parseFloat(response.data ?? '')
      if (!isApiSuccess(response) || !Number.isFinite(amount) || amount < 0) {
        throw new Error(response.message || '')
      }
      return amount
    },
  })
  const amountDue =
    cashQuoteEnabled && amountQuote.isSuccess && !amountQuote.isFetching
      ? amountQuote.data
      : null
  const [showHistory, setShowHistory] = useState(false)
  const [showProps, setShowProps] = useState(false)
  const [convertingPropId, setConvertingPropId] = useState<number | null>(null)
  const [giftProp, setGiftProp] = useState<BlindBoxProp | null>(null)
  const [prizeState, setPrizeState] =
    useState<PrizeDialogState>(EMPTY_PRIZE_STATE)

  const fetchSelf = useCallback(async () => {
    try {
      setLoading(true)
      const response = await getBlindBoxSelf()
      if (!isApiSuccess(response) || !response.data) {
        setLoadError(true)
        return
      }

      setLoadError(false)
      setData(response.data)
      setSelectedQuantity((current) => Math.max(1, current || 1))
      setSelectedPaymentMethod((current) => {
        if (
          current &&
          response.data?.pay_methods?.some(
            (method) => method.type === current.type
          )
        ) {
          return current
        }
        return response.data?.pay_methods?.[0] || null
      })
    } catch {
      setLoadError(true)
      toast.error(t('加载盲盒数据失败'))
    } finally {
      setLoading(false)
    }
  }, [t])

  const refreshAll = useCallback(async () => {
    await Promise.all([
      fetchSelf(),
      props.onSubscriptionRefresh(),
      props.onUserRefresh(),
    ])
  }, [fetchSelf, props])

  const {
    paying,
    paymentState,
    setPaymentState,
    handlePay,
    handleOpenExternal,
    handleCancelPayment,
    handleContinuePaymentInBackground,
    handleRetryPayment,
  } = useBlindBoxPayment({
    paymentResult: props.paymentResult,
    data,
    setData,
    selectedQuantity,
    setSelectedQuantity,
    selectedPaymentMethod,
    setSelectedPaymentMethod,
    amountDue,
    refreshAll,
    onSubscriptionRefresh: props.onSubscriptionRefresh,
    onUserRefresh: props.onUserRefresh,
  })

  useEffect(() => {
    void fetchSelf()
  }, [fetchSelf])

  useBlindBoxChangedEvent(setPrizeState, refreshAll)

  const availableBoxes = data?.inventory?.inventory_count || 0
  const pendingBoxes = 0
  const quota = data?.overview?.quota || 0
  const handleUseReward = useCallback(
    (record: BlindBoxRecord) => {
      if (
        record.reward_type !== 'prop' ||
        !record.prop_id ||
        ![
          'consume_discount_95',
          'consume_discount_90',
          'consume_discount_10',
          'zero_hour_multiplier',
          'monthly_pass_multiplier',
        ].includes(record.prop_type || '')
      ) {
        return
      }
      void (async () => {
        try {
          const response = await activateBlindBoxProp(record.prop_id as number)
          if (!isApiSuccess(response)) {
            throw new Error(response.message || t('启用失败'))
          }
          toast.success(
            record.prop_type === 'zero_hour_multiplier'
              ? t('历史 0 倍率道具已启用。')
              : record.prop_type === 'consume_discount_10'
                ? t(
                    '盲盒 0.1 倍率卡已启用，全部现有官方分组通用，累计 15 分钟并可随时暂停。'
                  )
                : record.prop_type === 'monthly_pass_multiplier'
                  ? t(
                      '套餐 0.1 倍率卡已启用；仅官方分组实际扣月卡额度时生效，第三方分组不适用。'
                    )
                  : t(
                      '{{param0}} 已启用，仅官方渠道可用，24 小时后自动失效。',
                      { param0: record.reward_title }
                    )
          )
          await refreshAll()
          await queryClient.invalidateQueries({ queryKey: ['user-groups'] })
        } catch (error) {
          toast.error(error instanceof Error ? error.message : t('启用失败'))
        }
      })()
    },
    [queryClient, refreshAll, t]
  )

  const handleUseProp = useCallback(
    async (prop: BlindBoxProp) => {
      if (
        prop.status !== 'available' &&
        !(
          [
            'monthly_pass_multiplier',
            'consume_discount_10',
            'zero_hour_multiplier',
          ].includes(prop.prop_type) && prop.status === 'paused'
        )
      ) {
        return
      }
      try {
        const response = await activateBlindBoxProp(prop.id)
        if (!isApiSuccess(response)) {
          throw new Error(response.message || t('Failed to use prop'))
        }
        toast.success(
          [
            'monthly_pass_multiplier',
            'consume_discount_10',
            'zero_hour_multiplier',
          ].includes(prop.prop_type)
            ? t('{{param0}} 已开启，可随时暂停。', {
                param0: prop.title,
              })
            : t('{{title}} is now active.', { title: prop.title })
        )
        await refreshAll()
        await queryClient.invalidateQueries({ queryKey: ['user-groups'] })
      } catch (error) {
        toast.error(
          error instanceof Error ? error.message : t('Failed to use prop')
        )
      }
    },
    [queryClient, refreshAll, t]
  )

  const handlePauseProp = useCallback(
    async (prop: BlindBoxProp) => {
      if (prop.status !== 'active') return
      try {
        const response = await pauseBlindBoxProp(prop.id)
        if (!isApiSuccess(response)) {
          throw new Error(response.message || t('暂停失败'))
        }
        toast.success(
          t('{{param0}} 已暂停，剩余时间已保留。', {
            param0: prop.title,
          })
        )
        await refreshAll()
        await queryClient.invalidateQueries({ queryKey: ['user-groups'] })
      } catch (error) {
        toast.error(error instanceof Error ? error.message : t('暂停失败'))
      }
    },
    [queryClient, refreshAll, t]
  )

  const handleConvertProp = useCallback(
    async (prop: BlindBoxProp) => {
      if (
        prop.status !== 'available' ||
        prop.prop_type !== 'subscription_discount_90'
      ) {
        return
      }
      setConvertingPropId(prop.id)
      try {
        const response = await convertBlindBoxProp(prop.id, 'topup_discount_90')
        if (!isApiSuccess(response) || !response.data?.prop) {
          throw new Error(response.message || t('转换失败'))
        }
        toast.success(
          t('已转换为{{param0}}', {
            param0: response.data.prop.title,
          })
        )
        await refreshAll()
      } catch (error) {
        toast.error(error instanceof Error ? error.message : t('转换失败'))
      } finally {
        setConvertingPropId(null)
      }
    },
    [refreshAll, t]
  )

  return (
    <div className='blind-box-workspace'>
      <div className='blind-box-toolbar'>
        <Button variant='outline' onClick={() => setShowHistory(true)}>
          <History className='size-4' aria-hidden />
          {t('开奖历史')}
        </Button>
        <Button variant='outline' onClick={() => setShowProps(true)}>
          <Ticket className='size-4' aria-hidden />
          {t('我的道具')}
        </Button>
        <Button
          variant='outline'
          disabled={loading}
          onClick={() => void refreshAll()}
        >
          <RefreshCw className='size-4' aria-hidden />
          {t('Refresh')}
        </Button>
      </div>
      {loadError ? (
        <div role='alert' className='commerce-error mb-6'>
          <span>{t('加载盲盒数据失败')}</span>
          <Button
            variant='outline'
            onClick={() => void fetchSelf()}
            disabled={loading}
          >
            {t('重试')}
          </Button>
        </div>
      ) : null}
      {cashQuoteEnabled && amountQuote.isError ? (
        <div role='alert' className='commerce-error mb-6'>
          <span>
            {t('Payment amount')}: {amountQuote.error.message || t('加载失败')}
          </span>
          <Button
            variant='outline'
            onClick={() => void amountQuote.refetch()}
            disabled={amountQuote.isFetching}
          >
            {t('重试')}
          </Button>
        </div>
      ) : null}
      {data || loading ? (
        <div className='commerce-blind-box'>
          <div className='min-w-0'>
            <BlindBoxContent
              data={data}
              loading={loading || loadError}
              selectedQuantity={selectedQuantity}
              selectedPaymentMethod={selectedPaymentMethod}
              amountDue={amountDue}
              paying={paying || loading || loadError}
              onQuantityChange={setSelectedQuantity}
              onPaymentMethodChange={setSelectedPaymentMethod}
              onPay={() => void handlePay()}
              onOpenProps={() => setShowProps(true)}
              onRefresh={refreshAll}
            />
          </div>

          {data ? (
            <BlindBoxSidebar
              quota={quota}
              availableBoxes={availableBoxes}
              pendingBoxes={pendingBoxes}
              records={data?.overview?.recent_records || []}
              props={data?.props || []}
              statistics={data?.statistics}
              onOpenHistory={() => setShowHistory(true)}
              onOpenProps={() => setShowProps(true)}
            />
          ) : null}
        </div>
      ) : null}

      <BlindBoxPaymentDialog
        state={paymentState}
        onOpenChange={(open) => {
          setPaymentState(
            open ? { ...paymentState, open } : EMPTY_PAYMENT_STATE
          )
        }}
        onOpenExternal={handleOpenExternal}
        onCancel={handleCancelPayment}
        onContinueInBackground={() => {
          handleContinuePaymentInBackground()
          toast.message(t('支付正在后台处理，完成后会自动同步结果'))
        }}
        onRetry={handleRetryPayment}
      />

      <BlindBoxPrizeDialog
        state={prizeState}
        tiers={data?.inventory?.tiers || data?.tiers || []}
        onOpenChange={(open) =>
          setPrizeState((current) => ({
            ...current,
            open,
          }))
        }
        onUseReward={handleUseReward}
      />

      <BlindBoxHistorySheet open={showHistory} onOpenChange={setShowHistory} />

      <BlindBoxPropsDialog
        open={showProps}
        props={data?.props || []}
        disabled={paying || loading || loadError}
        convertingPropId={convertingPropId}
        onOpenChange={setShowProps}
        onUse={(prop) => void handleUseProp(prop)}
        onPause={(prop) => void handlePauseProp(prop)}
        onConvert={(prop) => void handleConvertProp(prop)}
        onGift={(prop) => {
          setShowProps(false)
          setGiftProp(prop)
        }}
      />

      <BlindBoxPropGiftDialog
        open={giftProp != null}
        prop={giftProp}
        onOpenChange={(open) => {
          if (!open) setGiftProp(null)
        }}
        onGifted={async () => {
          setGiftProp(null)
          await refreshAll()
        }}
      />
    </div>
  )
}
