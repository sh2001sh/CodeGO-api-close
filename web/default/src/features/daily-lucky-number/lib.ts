import type { TFunction } from 'i18next'
import type { MembershipTier } from './types'

export const MEMBERSHIP_TIER_META: Record<
  MembershipTier,
  { labelKey: string; color: string; softColor: string; borderColor: string }
> = {
  none: {
    labelKey: '普通',
    color: '#64748B',
    softColor: '#F1F5F9',
    borderColor: '#CBD5E1',
  },
  lite: {
    labelKey: '轻享',
    color: '#64748B',
    softColor: '#F1F5F9',
    borderColor: '#CBD5E1',
  },
  standard: {
    labelKey: '标准',
    color: '#2563EB',
    softColor: '#EFF6FF',
    borderColor: '#BFDBFE',
  },
  pro: {
    labelKey: '专业',
    color: '#7C3AED',
    softColor: '#F5F3FF',
    borderColor: '#DDD6FE',
  },
  ultra: {
    labelKey: '旗舰',
    color: '#B7791F',
    softColor: '#FFFBEB',
    borderColor: '#FDE68A',
  },
}

export function normalizeMembershipTier(value?: string | null): MembershipTier {
  if (value === 'lite' || value === 'standard' || value === 'pro' || value === 'ultra') {
    return value
  }
  return 'none'
}

export function getMembershipTierLabel(
  tier: string | undefined,
  t: TFunction
): string {
  return t(MEMBERSHIP_TIER_META[normalizeMembershipTier(tier)].labelKey)
}

export function getMembershipTierMultiplier(tier?: string | null): number {
  switch (normalizeMembershipTier(tier)) {
    case 'standard':
      return 1.1
    case 'pro':
      return 1.2
    case 'ultra':
      return 1.3
    default:
      return 1
  }
}

export function getMembershipTierRank(tier?: string | null): number {
  switch (normalizeMembershipTier(tier)) {
    case 'ultra':
      return 4
    case 'pro':
      return 3
    case 'standard':
      return 2
    case 'lite':
      return 1
    default:
      return 0
  }
}

export function normalizeLuckyNumber(value?: string | number | null): string {
  const normalized = String(value ?? '').replace(/\D/g, '')
  return normalized ? normalized.padStart(4, '0').slice(-4) : ''
}

export function getMatchedDigits(
  luckySuffix?: string | null,
  winningNumber?: string | null
): number {
  const suffix = normalizeLuckyNumber(luckySuffix)
  const winning = normalizeLuckyNumber(winningNumber)
  if (!suffix || !winning) return 0
  for (let digits = 4; digits > 0; digits -= 1) {
    if (suffix.slice(-digits) === winning.slice(-digits)) return digits
  }
  return 0
}

export function formatLuckyUsd(amount?: number | null): string {
  const value = Number(amount || 0)
  return `$${value.toFixed(2)}`
}

export function formatLuckyDate(
  timestampOrDate: number | string | undefined,
  timezone?: string,
  locale?: string
): string {
  if (!timestampOrDate) return '--'
  const date =
    typeof timestampOrDate === 'number'
      ? new Date(timestampOrDate * 1000)
      : new Date(`${timestampOrDate}T12:00:00`)
  if (Number.isNaN(date.getTime())) return '--'
  try {
    return new Intl.DateTimeFormat(locale, {
      timeZone: timezone,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    }).format(date)
  } catch {
    return date.toLocaleDateString(locale)
  }
}

export function formatLuckyDateTime(
  timestamp: number | undefined,
  timezone?: string,
  locale?: string
): string {
  if (!timestamp) return '--'
  const date = new Date(timestamp * 1000)
  if (Number.isNaN(date.getTime())) return '--'
  try {
    return new Intl.DateTimeFormat(locale, {
      timeZone: timezone,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    }).format(date)
  } catch {
    return date.toLocaleString(locale)
  }
}

export function formatCountdown(totalSeconds: number): string {
  const safe = Math.max(0, Math.floor(totalSeconds))
  const hours = Math.floor(safe / 3600)
  const minutes = Math.floor((safe % 3600) / 60)
  const seconds = safe % 60
  return [hours, minutes, seconds].map((value) => String(value).padStart(2, '0')).join(':')
}

export function getRemainingDays(endTime?: number): number {
  if (!endTime) return 0
  return Math.max(0, Math.ceil((endTime - Date.now() / 1000) / 86400))
}

export function getDrawTimeLabel(
  hour: number,
  minute: number,
  timezone: string,
  t: TFunction
): string {
  return t('{{time}} daily · {{timezone}}', {
    time: `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`,
    timezone,
  })
}
