import { useCallback, useEffect, useMemo, useState } from 'react'
import { useAuthStore } from '@/stores/auth-store'
import { getSelf } from '@/lib/api'
import { useStatus } from '@/hooks/use-status'
import { useSystemConfig } from '@/hooks/use-system-config'
import {
  getPublicPlans,
  getSelfSubscriptionFull,
} from '@/features/subscriptions/api'
import type {
  PlanRecord,
  SelfSubscriptionData,
} from '@/features/subscriptions/types'
import { DEFAULT_DISCOUNT_RATE } from '../constants'
import { getDefaultPaymentType, isWaffoPancakePayment } from '../lib'
import type {
  CreemProduct,
  PaymentMethod,
  PresetAmount,
  UserWalletData,
} from '../types'
import { useCreemPayment } from './use-creem-payment'
import { usePayment } from './use-payment'
import { useRedemption } from './use-redemption'
import { useTopupInfo } from './use-topup-info'
import { useWaffoPancakePayment } from './use-waffo-pancake-payment'
import { useWaffoPayment } from './use-waffo-payment'

export function useWalletWorkspace() {
  const [user, setUser] = useState<UserWalletData | null>(null)
  const [userLoading, setUserLoading] = useState(true)
  const [subscriptionData, setSubscriptionData] =
    useState<SelfSubscriptionData | null>(null)
  const [subscriptionLoading, setSubscriptionLoading] = useState(true)
  const [publicPlans, setPublicPlans] = useState<PlanRecord[]>([])
  const [publicPlansLoading, setPublicPlansLoading] = useState(true)
  const [topupAmount, setTopupAmount] = useState(0)
  const [selectedPreset, setSelectedPreset] = useState<number | null>(null)
  const [selectedPaymentMethod, setSelectedPaymentMethod] =
    useState<PaymentMethod>()
  const [paymentLoading, setPaymentLoading] = useState<string | null>(null)
  const [confirmDialogOpen, setConfirmDialogOpen] = useState(false)
  const [billingDialogOpen, setBillingDialogOpen] = useState(false)
  const [redemptionCode, setRedemptionCode] = useState('')
  const [creemDialogOpen, setCreemDialogOpen] = useState(false)
  const [selectedCreemProduct, setSelectedCreemProduct] =
    useState<CreemProduct | null>(null)
  const setAuthUser = useAuthStore((state) => state.auth.setUser)

  const { status } = useStatus()
  const { currency } = useSystemConfig()
  const { topupInfo, presetAmounts, loading: topupLoading } = useTopupInfo()

  const effectiveUsdExchangeRate = useMemo(() => {
    return currency?.quotaDisplayType === 'USD'
      ? 1
      : currency?.usdExchangeRate || 1
  }, [currency?.quotaDisplayType, currency?.usdExchangeRate])

  const {
    amount: paymentAmount,
    calculating,
    processing,
    calculatePaymentAmount,
    processPayment,
  } = usePayment()
  const { redeeming, redeemCode } = useRedemption()
  const { processing: creemProcessing, processCreemPayment } = useCreemPayment()
  const { processWaffoPayment } = useWaffoPayment()
  const { processing: pancakeProcessing, processWaffoPancakePayment } =
    useWaffoPancakePayment()

  const fetchUser = useCallback(async () => {
    try {
      setUserLoading(true)
      const response = await getSelf()
      if (response.success && response.data) {
        const userData = response.data as UserWalletData
        setUser(userData)
        setAuthUser(response.data)
      }
    } catch (_error) {
      // no-op
    } finally {
      setUserLoading(false)
    }
  }, [setAuthUser])

  const fetchSubscriptionData = useCallback(async () => {
    try {
      setSubscriptionLoading(true)
      const response = await getSelfSubscriptionFull()
      if (response.success && response.data) {
        setSubscriptionData(response.data)
      } else {
        setSubscriptionData(null)
      }
    } catch (_error) {
      setSubscriptionData(null)
    } finally {
      setSubscriptionLoading(false)
    }
  }, [])

  const fetchPublicPlans = useCallback(async () => {
    try {
      setPublicPlansLoading(true)
      const response = await getPublicPlans()
      setPublicPlans(response.success ? response.data || [] : [])
    } catch (_error) {
      setPublicPlans([])
    } finally {
      setPublicPlansLoading(false)
    }
  }, [])

  useEffect(() => {
    void fetchUser()
    void fetchSubscriptionData()
    void fetchPublicPlans()
  }, [fetchPublicPlans, fetchSubscriptionData, fetchUser])

  useEffect(() => {
    if (typeof window === 'undefined') return

    const handleSubscriptionChanged = () => {
      void fetchSubscriptionData()
      void fetchUser()
      void fetchPublicPlans()
    }

    window.addEventListener('subscription:changed', handleSubscriptionChanged)
    return () => {
      window.removeEventListener(
        'subscription:changed',
        handleSubscriptionChanged
      )
    }
  }, [fetchPublicPlans, fetchSubscriptionData, fetchUser])

  useEffect(() => {
    if (topupInfo && topupAmount === 0) {
      const minTopup = 1
      setTopupAmount(minTopup)

      const defaultPaymentType = getDefaultPaymentType(topupInfo)
      calculatePaymentAmount(minTopup, defaultPaymentType)
    }
  }, [topupInfo, topupAmount, calculatePaymentAmount])

  const getCurrentPaymentType = useCallback(() => {
    return selectedPaymentMethod?.type || getDefaultPaymentType(topupInfo)
  }, [selectedPaymentMethod, topupInfo])

  const handleSelectPreset = useCallback(
    (preset: PresetAmount) => {
      setTopupAmount(preset.value)
      setSelectedPreset(preset.value)
      calculatePaymentAmount(preset.value, getCurrentPaymentType())
    },
    [calculatePaymentAmount, getCurrentPaymentType]
  )

  const handleTopupAmountChange = useCallback(
    (amount: number) => {
      setTopupAmount(amount)
      setSelectedPreset(null)
      calculatePaymentAmount(amount, getCurrentPaymentType())
    },
    [calculatePaymentAmount, getCurrentPaymentType]
  )

  const handlePaymentMethodSelect = useCallback(
    async (method: PaymentMethod) => {
      setSelectedPaymentMethod(method)
      setPaymentLoading(method.type)

      try {
        const minTopup = 1
        if (topupAmount < minTopup) {
          return
        }

        await calculatePaymentAmount(topupAmount, method.type)
        setConfirmDialogOpen(true)
      } finally {
        setPaymentLoading(null)
      }
    },
    [calculatePaymentAmount, topupAmount]
  )

  const handlePaymentConfirm = useCallback(async () => {
    if (!selectedPaymentMethod) return

    const isPancake = isWaffoPancakePayment(selectedPaymentMethod.type)
    const success = isPancake
      ? await processWaffoPancakePayment(topupAmount)
      : await processPayment(topupAmount, selectedPaymentMethod.type)

    if (success) {
      setConfirmDialogOpen(false)
      await fetchUser()
    }
  }, [
    fetchUser,
    processPayment,
    processWaffoPancakePayment,
    selectedPaymentMethod,
    topupAmount,
  ])

  const handleRedeem = useCallback(async () => {
    if (!redemptionCode) return

    const success = await redeemCode(redemptionCode)
    if (success) {
      setRedemptionCode('')
      await fetchUser()
    }
  }, [fetchUser, redeemCode, redemptionCode])

  const handleCreemProductSelect = useCallback((product: CreemProduct) => {
    setSelectedCreemProduct(product)
    setCreemDialogOpen(true)
  }, [])

  const handleCreemConfirm = useCallback(async () => {
    if (!selectedCreemProduct) return

    const success = await processCreemPayment(selectedCreemProduct.productId)
    if (success) {
      setCreemDialogOpen(false)
      setSelectedCreemProduct(null)
      await fetchUser()
    }
  }, [fetchUser, processCreemPayment, selectedCreemProduct])

  const handleWaffoMethodSelect = useCallback(
    async (_method: unknown, index: number) => {
      const loadingKey = `waffo-${index}`
      setPaymentLoading(loadingKey)

      try {
        await processWaffoPayment(topupAmount, index)
      } finally {
        setPaymentLoading(null)
      }
    },
    [processWaffoPayment, topupAmount]
  )

  const getDiscountRate = useCallback(() => {
    return DEFAULT_DISCOUNT_RATE
  }, [])

  return {
    user,
    userLoading,
    subscriptionData,
    subscriptionLoading,
    publicPlans,
    publicPlansLoading,
    topupInfo,
    presetAmounts,
    topupLoading,
    topupAmount,
    selectedPreset,
    selectedPaymentMethod,
    paymentAmount,
    calculating,
    paymentLoading,
    redemptionCode,
    redeeming,
    status,
    effectiveUsdExchangeRate,
    confirmDialogOpen,
    billingDialogOpen,
    creemDialogOpen,
    selectedCreemProduct,
    processing,
    pancakeProcessing,
    creemProcessing,
    fetchUser,
    fetchSubscriptionData,
    fetchPublicPlans,
    handleSelectPreset,
    handleTopupAmountChange,
    handlePaymentMethodSelect,
    handlePaymentConfirm,
    handleRedeem,
    handleCreemProductSelect,
    handleCreemConfirm,
    handleWaffoMethodSelect,
    getDiscountRate,
    setConfirmDialogOpen,
    setBillingDialogOpen,
    setCreemDialogOpen,
    setRedemptionCode,
  }
}
