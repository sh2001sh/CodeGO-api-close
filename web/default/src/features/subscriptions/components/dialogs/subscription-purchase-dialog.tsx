import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import {
  CalendarClock,
  CheckCircle2,
  CircleSlash,
  Crown,
  ExternalLink,
  Gift,
  Layers3,
  Loader2,
  Percent,
  QrCode,
  XCircle,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
	cancelSubscriptionOrder,
	getSubscriptionOrderStatus,
  paySubscriptionCreem,
  paySubscriptionEpay,
  paySubscriptionStripe,
  paySubscriptionXunhu,
} from '../../api'
import {
  formatDuration,
  formatResetPeriod,
  formatSubscriptionPlanPrice,
  formatSubscriptionQuotaAmount,
  getSubscriptionDisabledReasonText,
  getSubscriptionPlanActionLabel,
  getSubscriptionPlanDetailText,
  getSubscriptionPlanDiscountText,
  getSubscriptionPlanSubtitle,
  isMonthlyCardPlan,
  normalizeSubscriptionText,
} from '../../lib'
import type {
  PlanRecord,
  SubscriptionOrderStatus,
  SubscriptionPayResponse,
  SubscriptionPurchaseType,
} from '../../types'
import { formatLuckyDateTime } from '@/features/daily-lucky-number/lib'
import { LuckyNumberCode } from '@/features/daily-lucky-number/components/lucky-number-code'
import { TierBadge } from '@/features/daily-lucky-number/components/tier-badge'
import { PackageModelScopeNotice } from '../package-model-scope-notice'

interface PaymentMethod {
  type: string
  name?: string
}

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  plan: PlanRecord | null
  enableStripe?: boolean
  enableCreem?: boolean
  enableOnlineTopUp?: boolean
  epayMethods?: PaymentMethod[]
  purchaseLimit?: number
  purchaseCount?: number
  purchaseType?: SubscriptionPurchaseType
  groupBuyId?: number
}

type PaymentStage = 'idle' | 'pending' | 'success' | 'failed' | 'cancelled'

interface PaymentTracker {
  stage: PaymentStage
  orderId: string
  externalUrl: string
  qrCodeUrl: string
  amountDue: number
  methodLabel: string
  actionLabel: string
  message: string
  orderStatus?: SubscriptionOrderStatus
}

const EMPTY_PAYMENT_TRACKER: PaymentTracker = {
  stage: 'idle',
  orderId: '',
  externalUrl: '',
  qrCodeUrl: '',
  amountDue: 0,
  methodLabel: '',
  actionLabel: '',
  message: '',
}

function getMethodLabel(
  type: string,
  methods: PaymentMethod[],
  t: (key: string) => string
): string {
  if (type === 'xunhu' || type === 'wxpay') {
    return '微信支付'
  }
  return (
    normalizeSubscriptionText(
      methods.find((item) => item.type === type)?.name
    ) ||
    type ||
    t('Pay')
  )
}

function submitExternalPaymentForm(
  url: string,
  params: Record<string, unknown>,
  isSafari: boolean
) {
  const form = document.createElement('form')
  form.action = url
  form.method = 'POST'
  if (!isSafari) {
    form.target = '_blank'
  }

  for (const [key, value] of Object.entries(params)) {
    const input = document.createElement('input')
    input.type = 'hidden'
    input.name = key
    input.value = String(value)
    form.appendChild(input)
  }

  document.body.appendChild(form)
  form.submit()
  document.body.removeChild(form)
}

function SummaryItem(props: { label: string; value: ReactNode }) {
  return (
    <div className='app-subtle-panel px-3 py-3'>
      <div className='text-muted-foreground text-[11px] font-medium tracking-wide'>
        {props.label}
      </div>
      <div className='text-foreground mt-1 text-sm font-medium'>
        {props.value}
      </div>
    </div>
  )
}

function StatusItem(props: { label: string; value: ReactNode }) {
  return (
    <div className='app-subtle-panel px-3 py-2.5'>
      <div className='text-muted-foreground text-[11px] font-medium tracking-wide'>
        {props.label}
      </div>
      <div className='text-foreground mt-1 text-sm font-medium'>
        {props.value}
      </div>
    </div>
  )
}

function PurchaseBenefitReveal(props: { order: SubscriptionOrderStatus }) {
  const { t } = useTranslation()
  const luckyNumber = props.order.lucky_number
  const benefit = props.order.blind_box_benefit
  if (!luckyNumber && !benefit) return null

  return (
    <div className='border-success/20 space-y-3 border-t pt-4'>
      <div className='flex items-center gap-2'>
        <CheckCircle2 className='text-success size-4' aria-hidden='true' />
        <span className='text-foreground text-sm font-semibold'>
          {t('Subscription benefits delivered')}
        </span>
      </div>
      {luckyNumber ? (
        <div className='flex min-w-0 flex-wrap items-center gap-2'>
          <TierBadge tier={luckyNumber.membership_tier} compact />
          <LuckyNumberCode
            cardCode={luckyNumber.card_code}
            luckySuffix={luckyNumber.lucky_suffix}
          />
        </div>
      ) : null}
      {benefit && benefit.expected_count > 0 ? (
        <div className='text-muted-foreground flex flex-wrap items-center gap-x-2 gap-y-1 text-xs'>
          <Gift className='text-primary size-3.5' aria-hidden='true' />
          <span>
            {t('{{count}} blind boxes granted', {
              count: benefit.granted_count || benefit.expected_count,
            })}
          </span>
          {benefit.ends_at > 0 ? (
            <span>
              {t('Expires {{time}}', {
                time: formatLuckyDateTime(
                  benefit.ends_at,
                  'Asia/Shanghai',
                  undefined
                ),
              })}
            </span>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}

export function SubscriptionPurchaseDialog(props: Props) {
  const { t } = useTranslation()
  const [paying, setPaying] = useState(false)
  const [selectedEpayMethod, setSelectedEpayMethod] = useState('')
  const [paymentTracker, setPaymentTracker] = useState<PaymentTracker>(
    EMPTY_PAYMENT_TRACKER
  )
  const hasTriggeredSuccessRef = useRef(false)

  const planRecord = props.plan
  const plan = planRecord?.plan
  const paymentMethods = props.epayMethods || []
  const isSafari =
    typeof navigator !== 'undefined' &&
    /^((?!chrome|android).)*safari/i.test(navigator.userAgent)

  useEffect(() => {
    if (!props.open) {
      setSelectedEpayMethod('')
      setPaymentTracker(EMPTY_PAYMENT_TRACKER)
      hasTriggeredSuccessRef.current = false
      return
    }
    if (paymentMethods.length > 0) {
      setSelectedEpayMethod(
        (current) => current || paymentMethods[0]?.type || ''
      )
    }
  }, [paymentMethods, props.open])

  useEffect(() => {
    if (
      !props.open ||
      paymentTracker.stage !== 'pending' ||
      !paymentTracker.orderId
    ) {
      return
    }

    let active = true
    const poll = async () => {
      try {
        const response = await getSubscriptionOrderStatus(
          paymentTracker.orderId
        )
        if (!active || !response.success || !response.data) return

        const order = response.data as SubscriptionOrderStatus
        if (
          order.status === 'success' &&
          order.fulfillment_status === 'completed'
        ) {
          setPaymentTracker((current) => ({
            ...current,
            stage: 'success',
            orderStatus: order,
            message: '支付成功，套餐权益已经发放。',
          }))
          if (!hasTriggeredSuccessRef.current) {
            hasTriggeredSuccessRef.current = true
            window.dispatchEvent(new Event('subscription:changed'))
          }
          return
        }

        if (order.status === 'success') {
          if (order.fulfillment_status === 'failed') {
            setPaymentTracker((current) => ({
              ...current,
              stage: 'failed',
              orderStatus: order,
              message: '支付已确认，但套餐权益发放失败，请联系管理员处理。',
            }))
            return
          }
          setPaymentTracker((current) => ({
            ...current,
            stage: 'pending',
            orderStatus: order,
            message: '支付已确认，正在发放月卡编号和盲盒权益。',
          }))
          return
        }

        if (order.status === 'expired') {
          setPaymentTracker((current) => ({
            ...current,
            stage: 'failed',
            message: '订单已过期或支付未完成，请重新发起支付。',
          }))
        }
      } catch {
        // keep polling
      }
    }

    void poll()
    const timer = window.setInterval(() => {
      void poll()
    }, 2000)

    return () => {
      active = false
      window.clearInterval(timer)
    }
  }, [paymentTracker.orderId, paymentTracker.stage, props.open])

	const selectedEpayMethodLabel = useMemo(
    () => getMethodLabel(selectedEpayMethod, paymentMethods, t),
    [paymentMethods, selectedEpayMethod, t]
	)

	const cancelPendingPayment = () => {
		const orderId = paymentTracker.orderId
		if (orderId && paymentTracker.orderStatus?.status !== 'success') {
      void cancelSubscriptionOrder(orderId)
		}
		setPaymentTracker((current) => ({
			...current,
			stage: 'cancelled',
			message: '已取消未支付订单。',
		}))
	}

	const handleOpenChange = (open: boolean) => {
		if (
      !open &&
      paymentTracker.stage === 'pending' &&
      paymentTracker.orderStatus?.status !== 'success'
    ) {
			cancelPendingPayment()
		}
		props.onOpenChange(open)
	}

  if (!plan || !planRecord) return null

  const hasStripe = props.enableStripe && !!plan.stripe_price_id
  const hasCreem = props.enableCreem && !!plan.creem_product_id
  const hasEpay = props.enableOnlineTopUp && paymentMethods.length > 0
  const totalAmount = Number(plan.total_amount || 0)
  const periodAmount = Number(plan.period_amount || 0)
  const effectiveAmount = Number(
    planRecord.amount_due ?? plan.price_amount ?? 0
  )
  const baseAmount = Number(
    planRecord.base_amount_due ?? plan.price_amount ?? effectiveAmount
  )
  const firstPurchaseDiscountApplied =
    planRecord.first_purchase_discount_applied === true
  const firstPurchaseDiscount = Number(
    (planRecord.first_purchase_discount_multiplier || 0) * 10
  )
  const displayPrice = formatSubscriptionPlanPrice(
    effectiveAmount,
    plan.currency
  )
  const actionLabel = getSubscriptionPlanActionLabel(planRecord.action, t)
  const purchaseType = props.purchaseType || 'normal'
  const groupBuyId = props.groupBuyId || 0
  const isCollectivePurchase =
    purchaseType === 'group_buy' || purchaseType === 'join_group'
  const purchaseModeLabel =
    purchaseType === 'group_buy'
      ? '开启集享计划'
      : purchaseType === 'join_group'
        ? '参与集享计划'
        : actionLabel
  const discountText = getSubscriptionPlanDiscountText(plan)
  const detailText = getSubscriptionPlanDetailText(
    plan,
    totalAmount,
    periodAmount,
    t
  )
  const isMonthlyPlan = isMonthlyCardPlan(plan)
  const limitReached =
    (props.purchaseLimit || 0) > 0 &&
    (props.purchaseCount || 0) >= (props.purchaseLimit || 0)
  const blockedByRule = planRecord.action === 'disabled'
  const blockedMessage =
    getSubscriptionDisabledReasonText(planRecord.disabled_reason) ||
    '当前已有更高等级的有效套餐，暂不支持降级订阅。'
  const disablePurchase =
    paying ||
    limitReached ||
    blockedByRule ||
    paymentTracker.stage === 'pending'
  const summaryItems = [
    {
      label: isCollectivePurchase ? '参与方式' : '购买方式',
      value: purchaseModeLabel,
    },
    {
      label: '有效期',
      value: (
        <span className='flex items-center gap-1.5'>
          <CalendarClock className='h-3.5 w-3.5' />
          {formatDuration(plan, t)}
        </span>
      ),
    },
    {
      label: isMonthlyPlan
        ? '本月可用额度'
        : periodAmount > 0
          ? '周期额度'
          : '总额度',
      value: formatSubscriptionQuotaAmount(
        !isMonthlyPlan && periodAmount > 0 ? periodAmount : totalAmount
      ),
    },
    ...(!isMonthlyPlan && periodAmount > 0
      ? [
          {
            label: '总额度',
            value:
              totalAmount > 0
                ? formatSubscriptionQuotaAmount(totalAmount)
                : '不限',
          },
        ]
      : []),
    ...(!isMonthlyPlan
      ? [
          {
            label: '额度重置',
            value:
              formatResetPeriod(plan, t) === t('No Reset')
                ? '不重置'
                : formatResetPeriod(plan, t),
          },
        ]
      : []),
    {
      label: '支付价格',
      value: formatSubscriptionPlanPrice(effectiveAmount, plan.currency),
    },
  ]

  const startPendingPayment = (
    response: SubscriptionPayResponse,
    methodLabel: string,
    externalUrl: string,
    qrCodeUrl = ''
  ) => {
    const amountDue = Number(response.data?.amount_due ?? effectiveAmount ?? 0)
    setPaymentTracker({
      stage: 'pending',
      orderId: String(response.data?.order_id || ''),
      externalUrl,
      qrCodeUrl,
      amountDue,
      methodLabel,
      actionLabel: purchaseModeLabel,
      message: qrCodeUrl
        ? '请使用微信扫码完成支付，系统会自动等待支付结果回传。'
        : '请在新窗口完成支付，系统会自动等待支付结果回传。',
    })
    toast.success('支付请求已发起')
  }

  const handlePayStripe = async () => {
    setPaying(true)
    try {
      const response = await paySubscriptionStripe({
        plan_id: plan.id,
        purchase_type: purchaseType,
        group_buy_id: groupBuyId,
      })
      const payLink = response.data?.pay_link || ''
      if (
        response.message === 'success' &&
        payLink &&
        response.data?.order_id
      ) {
        window.open(payLink, '_blank')
        startPendingPayment(response, 'Stripe', payLink)
      } else {
        toast.error(response.message || '支付请求失败')
      }
    } catch {
      toast.error('支付请求失败')
    } finally {
      setPaying(false)
    }
  }

  const handlePayCreem = async () => {
    setPaying(true)
    try {
      const response = await paySubscriptionCreem({
        plan_id: plan.id,
        purchase_type: purchaseType,
        group_buy_id: groupBuyId,
      })
      const checkoutUrl = response.data?.checkout_url || ''
      if (
        response.message === 'success' &&
        checkoutUrl &&
        response.data?.order_id
      ) {
        window.open(checkoutUrl, '_blank')
        startPendingPayment(response, 'Creem', checkoutUrl)
      } else {
        toast.error(response.message || '支付请求失败')
      }
    } catch {
      toast.error('支付请求失败')
    } finally {
      setPaying(false)
    }
  }

  const handlePayEpay = async () => {
    if (!selectedEpayMethod) {
      toast.error('请选择支付方式')
      return
    }

    setPaying(true)
    try {
      const isXunhu = selectedEpayMethod === 'xunhu'
      const response = isXunhu
        ? await paySubscriptionXunhu({
            plan_id: plan.id,
            purchase_type: purchaseType,
            group_buy_id: groupBuyId,
          })
        : await paySubscriptionEpay({
            plan_id: plan.id,
            payment_method: selectedEpayMethod,
            purchase_type: purchaseType,
            group_buy_id: groupBuyId,
          })

      if (response.message !== 'success') {
        toast.error(response.message || '支付请求失败')
        return
      }

      if (isXunhu) {
        const payUrl = response.data?.pay_url || ''
        const qrCodeUrl = response.data?.qrcode_url || ''
        if ((payUrl || qrCodeUrl) && response.data?.order_id) {
          startPendingPayment(response, '微信支付', payUrl, qrCodeUrl)
          return
        }
      } else if (
        response.url &&
        response.data?.form &&
        response.data?.order_id
      ) {
        submitExternalPaymentForm(
          response.url,
          response.data.form as Record<string, unknown>,
          isSafari
        )
        startPendingPayment(response, selectedEpayMethodLabel, response.url)
        return
      }

      toast.error('支付请求失败')
    } catch {
      toast.error('支付请求失败')
    } finally {
      setPaying(false)
    }
  }

  const renderPaymentStatus = () => {
    if (paymentTracker.stage === 'idle') return null

    const statusConfig = {
      pending: {
        icon: <Loader2 className='h-5 w-5 animate-spin' />,
        title: '等待支付结果',
        tone: 'border-warning/20 bg-warning/5',
      },
      success: {
        icon: <CheckCircle2 className='text-success h-5 w-5' />,
        title: '支付成功',
        tone: 'border-success/20 bg-success/5',
      },
      failed: {
        icon: <XCircle className='text-destructive h-5 w-5' />,
        title: '支付失败',
        tone: 'border-destructive/20 bg-destructive/5',
      },
      cancelled: {
        icon: <CircleSlash className='text-muted-foreground h-5 w-5' />,
        title: '已取消等待',
        tone: 'border-border/70 bg-muted/40',
      },
      idle: {
        icon: null,
        title: '',
        tone: '',
      },
    }[paymentTracker.stage]
    const isFulfilling =
      paymentTracker.stage === 'pending' &&
      paymentTracker.orderStatus?.status === 'success'

    return (
      <div
        className={cn('space-y-4 rounded-2xl border p-4', statusConfig.tone)}
      >
        <div className='flex items-start gap-3'>
          <div className='bg-background flex h-10 w-10 shrink-0 items-center justify-center rounded-full border'>
            {statusConfig.icon}
          </div>
          <div className='min-w-0'>
            <div className='text-foreground text-sm font-semibold'>
            {isFulfilling ? '正在发放套餐权益' : statusConfig.title}
            </div>
            <p className='text-muted-foreground mt-1 text-sm leading-6'>
              {paymentTracker.message}
            </p>
          </div>
        </div>

        {paymentTracker.stage === 'success' && paymentTracker.orderStatus ? (
          <PurchaseBenefitReveal order={paymentTracker.orderStatus} />
        ) : null}

        <div className='grid gap-2 sm:grid-cols-2'>
          <StatusItem label='操作类型' value={paymentTracker.actionLabel} />
          <StatusItem label='支付方式' value={paymentTracker.methodLabel} />
          <StatusItem
            label='应付金额'
            value={formatSubscriptionPlanPrice(
              paymentTracker.amountDue,
              plan.currency
            )}
          />
          <StatusItem label='订单号' value={paymentTracker.orderId || '-'} />
        </div>

        {paymentTracker.qrCodeUrl && paymentTracker.stage === 'pending' ? (
          <div className='app-subtle-panel space-y-3 p-3'>
            <div className='border-border bg-card mx-auto w-full max-w-[180px] rounded-2xl border p-3 shadow-sm'>
              <img
                src={paymentTracker.qrCodeUrl}
                alt='wechat-pay-qrcode'
                className='mx-auto h-36 w-36 object-contain'
              />
            </div>
            <p className='text-muted-foreground text-center text-xs'>
              请使用微信扫码完成支付。
            </p>
          </div>
        ) : null}

        <div className='flex flex-wrap gap-2'>
          {paymentTracker.externalUrl && paymentTracker.stage === 'pending' ? (
            <Button
              variant='outline'
              onClick={() => window.open(paymentTracker.externalUrl, '_blank')}
            >
              <ExternalLink className='mr-1 h-4 w-4' />
              打开支付页面
            </Button>
          ) : null}

          {paymentTracker.stage === 'pending' ? (
            <Button
              variant='ghost'
			  onClick={cancelPendingPayment}
            >
              取消等待
            </Button>
          ) : (
			<Button variant='default' onClick={() => handleOpenChange(false)}>
              关闭
            </Button>
          )}
        </div>
      </div>
    )
  }

  return (
	<Dialog open={props.open} onOpenChange={handleOpenChange}>
      <DialogContent className='flex max-h-[calc(100vh-1rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl'>
        <DialogHeader className='border-border/70 border-b px-4 py-4 sm:px-5'>
          <DialogTitle className='flex items-center gap-2 text-lg'>
            <Crown className='h-5 w-5' />
            {purchaseModeLabel}
          </DialogTitle>
        </DialogHeader>

        <div className='flex-1 overflow-y-auto px-4 pt-4 pb-4 sm:px-5 sm:pb-5'>
          <div className='space-y-4'>
            <div className='app-page-shell overflow-hidden'>
              <div className='border-border/70 border-b px-4 py-4 sm:px-5'>
                <div className='flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between'>
                  <div className='min-w-0'>
                    <div className='mb-2 flex flex-wrap items-center gap-2'>
                      <span className='bg-foreground text-background rounded-full px-2.5 py-1 text-[11px] font-semibold tracking-[0.18em]'>
                        套餐
                      </span>
                      {discountText ? (
                        <span className='border-warning/20 bg-warning/10 text-warning rounded-full border px-3 py-1 text-[12px] font-semibold'>
                          {discountText}
                        </span>
                      ) : null}
                    </div>
                    <p className='text-primary text-[11px] font-semibold tracking-[0.22em] uppercase'>
                      {getSubscriptionPlanSubtitle(plan)}
                    </p>
                    <h3 className='text-foreground mt-1 truncate text-xl font-semibold tracking-tight sm:text-2xl'>
                      {normalizeSubscriptionText(plan.title) || t('Plan Name')}
                    </h3>
                    <p className='text-muted-foreground mt-2 text-sm leading-6'>
                      {detailText}
                    </p>
                  </div>
                  <div className='shrink-0 text-left sm:text-right'>
                    {firstPurchaseDiscountApplied &&
                    baseAmount > effectiveAmount ? (
                      <div className='text-muted-foreground text-sm line-through'>
                        {formatSubscriptionPlanPrice(baseAmount, plan.currency)}
                      </div>
                    ) : null}
                    <div className='text-primary text-2xl font-semibold tracking-tight sm:text-3xl'>
                      {displayPrice}
                    </div>
                    <div className='text-muted-foreground mt-1 text-xs'>
                      应付金额
                    </div>
                  </div>
                </div>
              </div>

              <div className='px-4 py-4 sm:px-5'>
                <PackageModelScopeNotice className='mb-4' />
                {firstPurchaseDiscountApplied ? (
                  <div className='border-primary/25 bg-primary/5 mb-4 flex items-start gap-3 rounded-lg border px-4 py-3'>
                    <Percent
                      className='text-primary mt-0.5 size-4 shrink-0'
                      aria-hidden='true'
                    />
                    <div>
                      <p className='text-foreground text-sm font-semibold'>
                        套餐首购 {Number(firstPurchaseDiscount.toFixed(1))} 折
                      </p>
                      <p className='text-muted-foreground mt-0.5 text-xs leading-5'>
                        优惠已自动应用于你的首次月卡购买，本订单不会同时消耗盲盒套餐折扣卡。
                      </p>
                    </div>
                  </div>
                ) : null}
                {isCollectivePurchase ? (
                  <div className='border-primary/20 bg-primary/5 mb-4 flex items-start gap-3 rounded-lg border px-4 py-3'>
                    <Layers3
                      className='text-primary mt-0.5 size-4 shrink-0'
                      aria-hidden='true'
                    />
                    <div>
                      <p className='text-foreground text-sm font-semibold'>
                        本订单将参与集享计划
                      </p>
                      <p className='text-muted-foreground mt-0.5 text-xs leading-5'>
                        支付后基础额度立即生效。本期达到满额档或持续 48
                        小时后，系统会按照最终参与档位自动补发额度差额。
                      </p>
                    </div>
                  </div>
                ) : null}
                <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-3'>
                  {summaryItems.map((item) => (
                    <SummaryItem
                      key={item.label}
                      label={item.label}
                      value={item.value}
                    />
                  ))}
                </div>
              </div>
            </div>

            {limitReached ? (
              <Alert variant='destructive'>
                <AlertDescription>
                  已达到该套餐购买上限（{props.purchaseCount}/
                  {props.purchaseLimit}）。
                </AlertDescription>
              </Alert>
            ) : null}

            {blockedByRule ? (
              <Alert variant='destructive'>
                <AlertDescription>{blockedMessage}</AlertDescription>
              </Alert>
            ) : null}

            {renderPaymentStatus()}

            {paymentTracker.stage === 'idle' ? (
              hasStripe || hasCreem || hasEpay ? (
                <div className='app-page-shell p-4'>
                  <div className='text-foreground mb-3 flex items-center gap-2 text-sm font-medium'>
                    <QrCode className='text-primary h-4 w-4' />
                    选择支付方式
                  </div>

                  <div className='space-y-3'>
                    {hasStripe || hasCreem ? (
                      <div className='grid grid-cols-2 gap-2'>
                        {hasStripe ? (
                          <Button
                            variant='outline'
                            className='w-full'
                            onClick={() => void handlePayStripe()}
                            disabled={disablePurchase}
                          >
                            Stripe
                          </Button>
                        ) : null}
                        {hasCreem ? (
                          <Button
                            variant='outline'
                            className='w-full'
                            onClick={() => void handlePayCreem()}
                            disabled={disablePurchase}
                          >
                            Creem
                          </Button>
                        ) : null}
                      </div>
                    ) : null}

                    {hasEpay ? (
                      <div className='grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto]'>
                        <Select
                          value={selectedEpayMethod}
                          onValueChange={(value) =>
                            value !== null && setSelectedEpayMethod(value)
                          }
                          disabled={disablePurchase}
                        >
                          <SelectTrigger className='w-full'>
                            <SelectValue>{selectedEpayMethodLabel}</SelectValue>
                          </SelectTrigger>
                          <SelectContent alignItemWithTrigger={false}>
                            <SelectGroup>
                              {paymentMethods.map((item) => (
                                <SelectItem key={item.type} value={item.type}>
                                  {getMethodLabel(item.type, paymentMethods, t)}
                                </SelectItem>
                              ))}
                            </SelectGroup>
                          </SelectContent>
                        </Select>
                        <Button
                          className='w-full sm:w-auto'
                          onClick={() => void handlePayEpay()}
                          disabled={disablePurchase || !selectedEpayMethod}
                        >
                          {purchaseModeLabel}
                        </Button>
                      </div>
                    ) : null}
                  </div>
                </div>
              ) : (
                <Alert>
                  <AlertDescription>
                    当前套餐暂未配置可用支付方式。
                  </AlertDescription>
                </Alert>
              )
            ) : null}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
