import { useCallback, useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  calculateBlindBoxAmount,
  getBlindBoxSelf,
  isApiSuccess,
  openBlindBoxes,
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
  BlindBoxPrizeDialog,
  EMPTY_PAYMENT_STATE,
  EMPTY_PRIZE_STATE,
  type PrizeDialogState,
} from './blind-box-dialogs'
import { BlindBoxHistorySheet } from './blind-box-history-sheet'
import { BlindBoxPaymentDialog } from './blind-box-payment-dialog'
import { BlindBoxPropsDialog } from './blind-box-props-dialog'
import { BlindBoxSidebar } from './blind-box-sidebar'
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
  const [selectedQuantity, setSelectedQuantity] = useState(1)
  const [selectedPaymentMethod, setSelectedPaymentMethod] =
    useState<PaymentMethod | null>(null)
  const [amountDue, setAmountDue] = useState(0)
  const [openingCount, setOpeningCount] = useState<number | null>(null)
  const openingRef = useRef(false)
  const [showHistory, setShowHistory] = useState(false)
  const [showProps, setShowProps] = useState(false)
  const [convertingPropId, setConvertingPropId] = useState<number | null>(null)
  const [blindBoxMode, setBlindBoxMode] = useState<'standard' | 'balance'>(
    'standard'
  )
  const [prizeState, setPrizeState] =
    useState<PrizeDialogState>(EMPTY_PRIZE_STATE)

  const fetchSelf = useCallback(async () => {
    try {
      setLoading(true)
      const response = await getBlindBoxSelf()
      if (!isApiSuccess(response) || !response.data) return

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
      toast.error('加载盲盒数据失败')
    } finally {
      setLoading(false)
    }
  }, [])

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

  useEffect(() => {
    if (selectedQuantity <= 0) return

    const loadAmount = async () => {
      const response = await calculateBlindBoxAmount({
        quantity: selectedQuantity,
      })
      if (isApiSuccess(response) && response.data) {
        setAmountDue(parseFloat(response.data))
      } else {
        setAmountDue(0)
      }
    }

    void loadAmount()
  }, [selectedQuantity])

  useBlindBoxChangedEvent(setPrizeState, refreshAll)

  const availableBoxes = data?.overview?.available_boxes || 0
  const pendingBoxes = data?.overview?.pending_boxes || 0
  const remainingQuota = data?.overview?.remaining_quota || 0
  const claudeQuota = data?.overview?.claude_quota || 0
  const effectivePityThreshold =
    data?.overview?.effective_pity_threshold || data?.pity_threshold || 1
  const pityProgress = data?.overview?.pity_progress || 0
  const remainingPity = Math.max(0, effectivePityThreshold - pityProgress)

  const handleManualOpen = useCallback(
    async (count: number) => {
      // State updates are asynchronous. Keep an immediate guard so a fast
      // double-click cannot consume two boxes before the button re-renders.
      if (openingRef.current) return
      openingRef.current = true
      setOpeningCount(count)
      try {
        const response = await openBlindBoxes({ count })
        if (!response.success || !response.data) {
          throw new Error(response.message || '处理失败')
        }

        setPrizeState({
          open: true,
          records: response.data.records || [],
          openCount: response.data.open_count || count,
        })
        await refreshAll()
      } catch (error) {
        const message = error instanceof Error ? error.message : ''
        toast.error(
          message.includes('429') ||
            message.includes('频繁') ||
            message.includes('too many')
            ? '开启过于频繁，请稍后再试'
            : message || '处理失败'
        )
      } finally {
        setOpeningCount(null)
        openingRef.current = false
      }
    },
    [refreshAll]
  )

  const handleUseReward = useCallback(
    (record: BlindBoxRecord) => {
      if (
        record.reward_type !== 'prop' ||
        !record.prop_id ||
        ![
          'consume_discount_95',
          'consume_discount_90',
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
            throw new Error(response.message || '启用失败')
          }
          toast.success(
            record.prop_type === 'zero_hour_multiplier'
              ? `${record.reward_title} 已启用，倍率卡专属分组将持续 1 小时。`
              : record.prop_type === 'monthly_pass_multiplier'
                ? `${record.reward_title} 已启用，倍率卡专属分组按 0.1 倍率计费，可随时暂停。`
                : `${record.reward_title} 已启用，24 小时后自动失效。`
          )
          await refreshAll()
          await queryClient.invalidateQueries({ queryKey: ['user-groups'] })
        } catch (error) {
          toast.error(error instanceof Error ? error.message : '启用失败')
        }
      })()
    },
    [queryClient, refreshAll]
  )

  const handleUseProp = useCallback(
    async (prop: BlindBoxProp) => {
      if (
        prop.status !== 'available' &&
        !(
          ['monthly_pass_multiplier', 'zero_hour_multiplier'].includes(
            prop.prop_type
          ) && prop.status === 'paused'
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
          ['monthly_pass_multiplier', 'zero_hour_multiplier'].includes(
            prop.prop_type
          )
            ? `${prop.title} 已开启，可随时暂停。`
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
          throw new Error(response.message || '暂停失败')
        }
        toast.success(`${prop.title} 已暂停，剩余时间已保留。`)
        await refreshAll()
        await queryClient.invalidateQueries({ queryKey: ['user-groups'] })
      } catch (error) {
        toast.error(error instanceof Error ? error.message : '暂停失败')
      }
    },
    [queryClient, refreshAll]
  )

  const handleConvertProp = useCallback(
    async (prop: BlindBoxProp) => {
      if (
        prop.status !== 'available' ||
        !['topup_discount_90', 'subscription_discount_90'].includes(
          prop.prop_type
        )
      ) {
        return
      }
      const targetType =
        prop.prop_type === 'topup_discount_90'
          ? 'subscription_discount_90'
          : 'topup_discount_90'
      setConvertingPropId(prop.id)
      try {
        const response = await convertBlindBoxProp(prop.id, targetType)
        if (!isApiSuccess(response) || !response.data?.prop) {
          throw new Error(response.message || '转换失败')
        }
        toast.success(`已转换为${response.data.prop.title}`)
        await refreshAll()
      } catch (error) {
        toast.error(error instanceof Error ? error.message : '转换失败')
      } finally {
        setConvertingPropId(null)
      }
    },
    [refreshAll]
  )

  return (
    <>
      <div className='grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_320px]'>
        <div className='min-w-0'>
          <BlindBoxContent
            data={data}
            loading={loading}
            selectedQuantity={selectedQuantity}
            selectedPaymentMethod={selectedPaymentMethod}
            amountDue={amountDue}
            paying={paying}
            openingCount={openingCount}
            availableBoxes={availableBoxes}
            effectivePityThreshold={effectivePityThreshold}
            pityProgress={pityProgress}
            remainingPity={remainingPity}
            onQuantityChange={setSelectedQuantity}
            onPaymentMethodChange={setSelectedPaymentMethod}
            onPay={() => void handlePay()}
            onManualOpen={(count) => void handleManualOpen(count)}
            onOpenProps={() => setShowProps(true)}
            mode={blindBoxMode}
            onModeChange={setBlindBoxMode}
            onRefresh={refreshAll}
          />
        </div>

        <BlindBoxSidebar
          remainingQuota={remainingQuota}
          claudeQuota={claudeQuota}
          availableBoxes={availableBoxes}
          pendingBoxes={pendingBoxes}
          records={data?.overview?.recent_records || []}
          props={data?.props || []}
          statistics={data?.statistics}
          onOpenHistory={() => setShowHistory(true)}
          onOpenProps={() => setShowProps(true)}
        />
      </div>

      <BlindBoxPaymentDialog
        state={paymentState}
        onOpenChange={(open) => {
          setPaymentState(
            open ? { ...paymentState, open } : EMPTY_PAYMENT_STATE
          )
        }}
        onOpenExternal={handleOpenExternal}
        onContinueInBackground={() => {
          toast.message('支付正在后台处理，完成后会自动同步结果')
        }}
        onRetry={handleRetryPayment}
      />

      <BlindBoxPrizeDialog
        state={prizeState}
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
        disabled={openingCount !== null || paying}
        convertingPropId={convertingPropId}
        onOpenChange={setShowProps}
        onUse={(prop) => void handleUseProp(prop)}
        onPause={(prop) => void handlePauseProp(prop)}
        onConvert={(prop) => void handleConvertProp(prop)}
      />
    </>
  )
}
