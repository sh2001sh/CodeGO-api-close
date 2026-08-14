import {
  useCallback,
  useEffect,
  useState,
  type Dispatch,
  type SetStateAction,
} from 'react'
import { toast } from 'sonner'
import {
  getBlindBoxOrderStatus,
  getBlindBoxSelf,
  isApiSuccess,
  requestBlindBoxPayment,
} from '../api'
import { submitPaymentForm } from '../lib'
import type {
  BlindBoxOrderStatus,
  BlindBoxSelfData,
  PaymentMethod,
} from '../types'
import {
  EMPTY_PAYMENT_STATE,
  getBlindBoxMethodLabel,
  type BlindBoxPaymentState,
} from './blind-box-dialogs'

interface UseBlindBoxPaymentOptions {
  paymentResult?: 'success' | 'pending' | 'fail'
  data: BlindBoxSelfData | null
  setData: Dispatch<SetStateAction<BlindBoxSelfData | null>>
  selectedQuantity: number
  setSelectedQuantity: Dispatch<SetStateAction<number>>
  selectedPaymentMethod: PaymentMethod | null
  setSelectedPaymentMethod: Dispatch<SetStateAction<PaymentMethod | null>>
  amountDue: number
  refreshAll: () => Promise<void>
  onSubscriptionRefresh: () => Promise<void>
  onUserRefresh: () => Promise<void>
}

export function useBlindBoxPayment(options: UseBlindBoxPaymentOptions) {
  const [paying, setPaying] = useState(false)
  const [paymentState, setPaymentState] =
    useState<BlindBoxPaymentState>(EMPTY_PAYMENT_STATE)

  useEffect(() => {
    if (!options.paymentResult) return
    const syncPaymentResult = async () => {
      if (options.paymentResult === 'success') {
        toast.success('支付成功，系统正在同步盲盒结果。')
      } else if (options.paymentResult === 'pending') {
        toast.message('支付处理中，结果稍后会同步回来。')
      } else {
        toast.error('支付未完成，请重新发起购买。')
      }
      await options.refreshAll()
      if (typeof window !== 'undefined') {
        window.history.replaceState({}, '', window.location.pathname)
      }
    }
    void syncPaymentResult()
  }, [options.paymentResult, options.refreshAll])

  useEffect(() => {
    if (
      !paymentState.open ||
      paymentState.stage !== 'pending' ||
      !paymentState.orderId
    )
      return
    let active = true
    const pollOrder = async () => {
      try {
        const response = await getBlindBoxOrderStatus(paymentState.orderId)
        if (!active || !response.success || !response.data) return
        const order = response.data as BlindBoxOrderStatus
        if (order.status === 'success') {
          const refreshed = await getBlindBoxSelf()
          if (isApiSuccess(refreshed) && refreshed.data) {
            options.setData(refreshed.data)
            toast.success(
              `${order.quantity || paymentState.quantity} 个盲盒已到账，请选择逐个开启或全部打开。`
            )
          }
          await Promise.all([
            options.onSubscriptionRefresh(),
            options.onUserRefresh(),
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
        const message = error instanceof Error ? error.message : ''
        if (message.includes('timeout') || message.includes('超时')) {
          setPaymentState((current) => ({
            ...current,
            stage: 'failed',
            message: '支付超时，请检查网络连接后重试',
          }))
        }
      }
    }
    void pollOrder()
    const timer = window.setInterval(() => void pollOrder(), 2000)
    return () => {
      active = false
      window.clearInterval(timer)
    }
  }, [
    options.onSubscriptionRefresh,
    options.onUserRefresh,
    options.setData,
    paymentState,
  ])

  const handlePay = useCallback(async () => {
    const method = options.selectedPaymentMethod
    if (!method) {
      toast.error('请选择支付方式')
      return
    }
    setPaying(true)
    try {
      const response = await requestBlindBoxPayment({
        quantity: options.selectedQuantity,
        payment_method: method.type,
      })
      if (!isApiSuccess(response))
        throw new Error(
          friendlyPaymentError(response.message || '发起支付失败')
        )
      const payload = isRecord(response.data) ? response.data : {}
      const formFields = isRecord(payload.form) ? payload.form : null
      setPaymentState({
        open: true,
        stage: 'pending',
        orderId: String(payload.order_id || ''),
        amountDue: Number(payload.amount_due || options.amountDue),
        methodLabel: getBlindBoxMethodLabel(method),
        payUrl: String(payload.pay_url || response.url || ''),
        qrCodeUrl: String(payload.qrcode_url || ''),
        formUrl: formFields ? String(response.url || '') : '',
        formFields,
        quantity: Number(payload.quantity || options.selectedQuantity),
        message: '请在当前弹窗内扫码支付，付款完成后这里会自动显示结果。',
        pollingStartTime: Date.now(),
        retryPayload: {
          quantity: options.selectedQuantity,
          paymentMethod: method.type,
        },
      })
    } catch (error) {
      const message = error instanceof Error ? error.message : '发起支付失败'
      toast.error(message)
      setPaymentState({
        ...EMPTY_PAYMENT_STATE,
        open: true,
        stage: 'failed',
        amountDue: options.amountDue,
        methodLabel: getBlindBoxMethodLabel(method),
        quantity: options.selectedQuantity,
        message,
        retryPayload: {
          quantity: options.selectedQuantity,
          paymentMethod: method.type,
        },
      })
    } finally {
      setPaying(false)
    }
  }, [
    options.amountDue,
    options.selectedPaymentMethod,
    options.selectedQuantity,
  ])

  const handleOpenExternal = useCallback(() => {
    if (paymentState.formUrl && paymentState.formFields) {
      submitPaymentForm(paymentState.formUrl, paymentState.formFields)
    } else if (paymentState.payUrl) {
      window.open(paymentState.payUrl, '_blank', 'noopener,noreferrer')
    }
  }, [paymentState.formFields, paymentState.formUrl, paymentState.payUrl])

  const handleRetryPayment = useCallback(() => {
    if (!paymentState.retryPayload) return
    const method = options.data?.pay_methods?.find(
      (item) => item.type === paymentState.retryPayload?.paymentMethod
    )
    if (!method) {
      toast.error('支付方式不可用，请重新选择')
      setPaymentState(EMPTY_PAYMENT_STATE)
      return
    }
    options.setSelectedQuantity(paymentState.retryPayload.quantity)
    options.setSelectedPaymentMethod(method)
    setPaymentState(EMPTY_PAYMENT_STATE)
    window.setTimeout(() => void handlePay(), 100)
  }, [
    handlePay,
    options.data?.pay_methods,
    options.setSelectedPaymentMethod,
    options.setSelectedQuantity,
    paymentState.retryPayload,
  ])

  return {
    paying,
    paymentState,
    setPaymentState,
    handlePay,
    handleOpenExternal,
    handleRetryPayment,
  }
}

function friendlyPaymentError(message: string) {
  if (message.includes('余额不足') || message.includes('insufficient'))
    return '余额不足，请先充值'
  if (message.includes('超时') || message.includes('timeout'))
    return '网络超时，请检查网络连接后重试'
  if (message.includes('限额') || message.includes('limit'))
    return '已达到购买限额，请稍后再试'
  return message
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
