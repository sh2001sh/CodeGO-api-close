import { useCallback, useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  calculateBlindBoxAmount,
  getBlindBoxOrderStatus,
  getBlindBoxSelf,
  isApiSuccess,
  openBlindBoxes,
  requestBlindBoxPayment,
  activateBlindBoxProp,
  pauseBlindBoxProp,
} from '../api'
import { submitPaymentForm } from '../lib'
import type {
  BlindBoxOrderStatus,
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
  getBlindBoxMethodLabel,
  type BlindBoxPaymentState,
  type PrizeDialogState,
} from './blind-box-dialogs'
import { BlindBoxHistorySheet } from './blind-box-history-sheet'
import { BlindBoxPaymentDialog } from './blind-box-payment-dialog'
import { BlindBoxSidebar } from './blind-box-sidebar'
import { BlindBoxPropsList } from './blind-box-view-parts'

interface BlindBoxCardProps {
  onSubscriptionRefresh: () => Promise<void>
  onUserRefresh: () => Promise<void>
  paymentResult?: 'success' | 'pending' | 'fail'
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
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
  const [paying, setPaying] = useState(false)
  const [openingCount, setOpeningCount] = useState<number | null>(null)
  const openingRef = useRef(false)
  const [showHistory, setShowHistory] = useState(false)
  const [showProps, setShowProps] = useState(false)
  const [paymentState, setPaymentState] =
    useState<BlindBoxPaymentState>(EMPTY_PAYMENT_STATE)
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

  useEffect(() => {
    if (typeof window === 'undefined') return

    const handleBlindBoxChanged = (event: Event) => {
      const detail =
        event instanceof CustomEvent && isRecord(event.detail)
          ? event.detail
          : null
      const records = Array.isArray(detail?.records)
        ? (detail.records as BlindBoxRecord[])
        : []
      const openCount = Number(detail?.openCount || records.length || 0)
      if (records.length > 0) {
        setPrizeState({
          open: true,
          records,
          openCount,
        })
      }
      void refreshAll()
    }

    window.addEventListener('blind-box:changed', handleBlindBoxChanged)
    return () => {
      window.removeEventListener('blind-box:changed', handleBlindBoxChanged)
    }
  }, [refreshAll])

  useEffect(() => {
    if (!props.paymentResult) return

    const syncPaymentResult = async () => {
      if (props.paymentResult === 'success') {
        toast.success('支付成功，系统正在同步盲盒结果。')
      } else if (props.paymentResult === 'pending') {
        toast.message('支付处理中，结果稍后会同步回来。')
      } else {
        toast.error('支付未完成，请重新发起购买。')
      }

      await refreshAll()
      if (typeof window !== 'undefined') {
        window.history.replaceState({}, '', window.location.pathname)
      }
    }

    void syncPaymentResult()
  }, [props.paymentResult, refreshAll])

  useEffect(() => {
    if (
      !paymentState.open ||
      paymentState.stage !== 'pending' ||
      !paymentState.orderId
    ) {
      return
    }

    let active = true

    const pollOrder = async () => {
      try {
        const response = await getBlindBoxOrderStatus(paymentState.orderId)
        if (!active || !response.success || !response.data) return

        const order = response.data as BlindBoxOrderStatus
        if (order.status === 'success') {
          const refreshed = await getBlindBoxSelf()
          if (isApiSuccess(refreshed) && refreshed.data) {
            setData(refreshed.data)
            toast.success(
              `${order.quantity || paymentState.quantity} 个盲盒已到账，请选择逐个开启或全部打开。`
            )
          }
          await Promise.all([
            props.onSubscriptionRefresh(),
            props.onUserRefresh(),
          ])
          setPaymentState(EMPTY_PAYMENT_STATE)
          return
        }

        if (order.status === 'expired') {
          setPaymentState((current) => ({
            ...current,
            stage: 'failed',
            message: '订单已过期或支付未完成，请重新发起购买。',
          }))
        }
      } catch (error) {
        const errorMsg = error instanceof Error ? error.message : ''
        if (errorMsg.includes('timeout') || errorMsg.includes('超时')) {
          setPaymentState((current) => ({
            ...current,
            stage: 'failed',
            message: '支付超时，请检查网络连接后重试',
          }))
        }
      }
    }

    void pollOrder()
    const timer = window.setInterval(() => {
      void pollOrder()
    }, 2000)

    return () => {
      active = false
      window.clearInterval(timer)
    }
  }, [paymentState, props])

  const availableBoxes = data?.overview?.available_boxes || 0
  const pendingBoxes = data?.overview?.pending_boxes || 0
  const remainingQuota = data?.overview?.remaining_quota || 0
  const claudeQuota = data?.overview?.claude_quota || 0
  const effectivePityThreshold =
    data?.overview?.effective_pity_threshold || data?.pity_threshold || 1
  const pityProgress = data?.overview?.pity_progress || 0
  const remainingPity = Math.max(0, effectivePityThreshold - pityProgress)

  const startPendingPayment = useCallback(
    (args: {
      orderId: string
      amountDue: number
      quantity: number
      methodLabel: string
      payUrl?: string
      qrCodeUrl?: string
      formUrl?: string
      formFields?: Record<string, unknown> | null
      retryPayload?: { quantity: number; paymentMethod: string }
    }) => {
      setPaymentState({
        open: true,
        stage: 'pending',
        orderId: args.orderId,
        amountDue: args.amountDue,
        methodLabel: args.methodLabel,
        payUrl: args.payUrl || '',
        qrCodeUrl: args.qrCodeUrl || '',
        formUrl: args.formUrl || '',
        formFields: args.formFields || null,
        quantity: args.quantity,
        message: '请在当前弹窗内扫码支付，付款完成后这里会自动显示结果。',
        pollingStartTime: Date.now(),
        retryPayload: args.retryPayload,
      })
    },
    []
  )

  const handlePay = useCallback(async () => {
    if (!selectedPaymentMethod) {
      toast.error('请选择支付方式')
      return
    }

    setPaying(true)
    try {
      const response = await requestBlindBoxPayment({
        quantity: selectedQuantity,
        payment_method: selectedPaymentMethod.type,
      })
      if (!isApiSuccess(response)) {
        const errorMsg = response.message || '发起支付失败'
        let userFriendlyMsg = errorMsg

        if (
          errorMsg.includes('余额不足') ||
          errorMsg.includes('insufficient')
        ) {
          userFriendlyMsg = '余额不足，请先充值'
        } else if (errorMsg.includes('超时') || errorMsg.includes('timeout')) {
          userFriendlyMsg = '网络超时，请检查网络连接后重试'
        } else if (errorMsg.includes('限额') || errorMsg.includes('limit')) {
          userFriendlyMsg = '已达到购买限额，请稍后再试'
        }

        throw new Error(userFriendlyMsg)
      }

      const payload = isRecord(response.data) ? response.data : {}
      const formFields = isRecord(payload.form) ? payload.form : null
      const orderId = String(payload.order_id || '')
      startPendingPayment({
        orderId,
        amountDue: Number(payload.amount_due || amountDue),
        quantity: Number(payload.quantity || selectedQuantity),
        methodLabel: getBlindBoxMethodLabel(selectedPaymentMethod),
        payUrl: String(payload.pay_url || response.url || ''),
        qrCodeUrl: String(payload.qrcode_url || ''),
        formUrl: formFields ? String(response.url || '') : '',
        formFields,
        retryPayload: {
          quantity: selectedQuantity,
          paymentMethod: selectedPaymentMethod.type,
        },
      })
    } catch (error) {
      const errorMsg = error instanceof Error ? error.message : '发起支付失败'
      toast.error(errorMsg)

      setPaymentState({
        open: true,
        stage: 'failed',
        orderId: '',
        amountDue,
        methodLabel: getBlindBoxMethodLabel(selectedPaymentMethod),
        payUrl: '',
        qrCodeUrl: '',
        formUrl: '',
        formFields: null,
        quantity: selectedQuantity,
        message: errorMsg,
        retryPayload: {
          quantity: selectedQuantity,
          paymentMethod: selectedPaymentMethod.type,
        },
      })
    } finally {
      setPaying(false)
    }
  }, [amountDue, selectedPaymentMethod, selectedQuantity, startPendingPayment])

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
          prop.prop_type === 'monthly_pass_multiplier' &&
          prop.status === 'paused'
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
          prop.prop_type === 'monthly_pass_multiplier'
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

  const handleOpenExternal = useCallback(() => {
    if (paymentState.formUrl && paymentState.formFields) {
      submitPaymentForm(paymentState.formUrl, paymentState.formFields)
      return
    }
    if (paymentState.payUrl) {
      window.open(paymentState.payUrl, '_blank', 'noopener,noreferrer')
    }
  }, [paymentState.formFields, paymentState.formUrl, paymentState.payUrl])

  const handleRetryPayment = useCallback(() => {
    if (!paymentState.retryPayload) return

    const method = data?.pay_methods?.find(
      (m) => m.type === paymentState.retryPayload?.paymentMethod
    )
    if (!method) {
      toast.error('支付方式不可用，请重新选择')
      setPaymentState(EMPTY_PAYMENT_STATE)
      return
    }

    setSelectedQuantity(paymentState.retryPayload.quantity)
    setSelectedPaymentMethod(method)
    setPaymentState(EMPTY_PAYMENT_STATE)

    setTimeout(() => {
      void handlePay()
    }, 100)
  }, [paymentState.retryPayload, data?.pay_methods, handlePay])

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

      <Dialog open={showProps} onOpenChange={setShowProps}>
        <DialogContent className='max-h-[calc(100dvh-2rem)] overflow-hidden sm:max-w-lg'>
          <DialogHeader>
            <DialogTitle>我的道具</DialogTitle>
          </DialogHeader>
          <div className='max-h-[calc(100dvh-10rem)] overflow-y-auto pr-1'>
            <BlindBoxPropsList
              props={data?.props || []}
              disabled={openingCount !== null || paying}
              onUse={(prop) => void handleUseProp(prop)}
              onPause={(prop) => void handlePauseProp(prop)}
            />
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}
