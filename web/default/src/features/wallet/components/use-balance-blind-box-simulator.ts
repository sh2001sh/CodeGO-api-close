import { useMemo, useState } from 'react'
import { toast } from 'sonner'
import { parseQuotaFromDollars } from '@/lib/format'
import { isApiSuccess, simulateBalanceBlindBoxes } from '../api'
import type { BalanceBlindBoxSimulationDraw } from '../types'

export interface SimulationHistoryItem extends BalanceBlindBoxSimulationDraw {
  id: number
}

interface SimulationSession {
  initialQuota: number
  balanceQuota: number
  spentQuota: number
  rewardQuota: number
  drawCount: number
  history: SimulationHistoryItem[]
  smallPityProgress: number
  pityProgress: number
  firstDrawEligible: boolean
}

export function useBalanceBlindBoxSimulator(priceUSD: number) {
  const [initialAmount, setInitialAmount] = useState('100')
  const [count, setCount] = useState(1)
  const [busy, setBusy] = useState(false)
  const [session, setSession] = useState<SimulationSession | null>(null)
  const priceQuota = parseQuotaFromDollars(priceUSD)
  const initialQuota = parseQuotaFromDollars(Number(initialAmount || 0))
  const maxCount = session
    ? Math.min(100, Math.floor(session.balanceQuota / priceQuota))
    : 1
  const safeCount = Math.max(1, Math.min(count, Math.max(1, maxCount)))
  const canStart =
    Number(initialAmount) >= priceUSD && Number(initialAmount) <= 1_000_000
  const canDraw = Boolean(session && maxCount > 0 && !busy)

  const stats = useMemo(() => {
    if (!session) return null
    return {
      ...session,
      netQuota: session.balanceQuota - session.initialQuota,
      returnRate:
        session.spentQuota > 0
          ? (session.rewardQuota / session.spentQuota) * 100
          : 0,
    }
  }, [session])

  const start = () => {
    if (!canStart) {
      toast.error(`模拟初始额度需在 ${priceUSD.toFixed(2)} 至 1,000,000 之间`)
      return
    }
    setCount(1)
    setSession({
      initialQuota,
      balanceQuota: initialQuota,
      spentQuota: 0,
      rewardQuota: 0,
      drawCount: 0,
      history: [],
      smallPityProgress: 0,
      pityProgress: 0,
      firstDrawEligible: true,
    })
  }

  const draw = async () => {
    if (!session || !canDraw) return
    setBusy(true)
    try {
      const response = await simulateBalanceBlindBoxes(
        session.balanceQuota,
        safeCount,
        {
          smallPityProgress: session.smallPityProgress,
          pityProgress: session.pityProgress,
          firstDrawEligible: session.firstDrawEligible,
        }
      )
      if (!isApiSuccess(response) || !response.data) {
        throw new Error(response.message || '模拟抽盒失败')
      }
      const baseID = session.drawCount
      setSession({
        ...session,
        balanceQuota: response.data.balance_after,
        spentQuota: session.spentQuota + response.data.cost_quota,
        rewardQuota: session.rewardQuota + response.data.reward_quota,
        drawCount: session.drawCount + response.data.draws.length,
        smallPityProgress: response.data.small_pity_progress,
        pityProgress: response.data.pity_progress,
        firstDrawEligible: response.data.first_draw_eligible,
        history: [
          ...response.data.draws.map((item, index) => ({
            ...item,
            id: baseID + index + 1,
          })),
          ...session.history,
        ].slice(0, 40),
      })
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : '模拟抽盒失败')
    } finally {
      setBusy(false)
    }
  }

  return {
    initialAmount,
    setInitialAmount,
    count: safeCount,
    setCount,
    busy,
    session,
    stats,
    maxCount,
    canStart,
    canDraw,
    start,
    draw,
    reset: () => setSession(null),
  }
}
