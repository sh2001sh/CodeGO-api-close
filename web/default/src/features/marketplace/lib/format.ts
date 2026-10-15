import type { MarketplaceStatus } from '../types'

export const statusLabels: Record<MarketplaceStatus, string> = {
  draft: '草稿',
  verifying: '检测中',
  pending_review: '待审核',
  active: '可用',
  degraded: '质量下降',
  suspended: '已暂停',
  disabled: '已下架',
}

export function formatDuration(milliseconds: number) {
  if (!milliseconds) return '--'
  if (milliseconds < 1000) return `${Math.round(milliseconds)} ms`
  return `${(milliseconds / 1000).toFixed(2)} s`
}

export function formatPercent(value: number) {
  return value ? `${value.toFixed(2)}%` : '--'
}

export function formatNumber(value: number) {
  return new Intl.NumberFormat('zh-CN').format(value)
}

export function formatMultiplier(value: number) {
  const maximumFractionDigits = value < 0.01 ? 6 : value < 1 ? 4 : 2
  return new Intl.NumberFormat('zh-CN', {
    minimumFractionDigits: 0,
    maximumFractionDigits,
  }).format(value)
}
