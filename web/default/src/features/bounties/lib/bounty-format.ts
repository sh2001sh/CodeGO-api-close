import { toIntlLocale } from '@/i18n/languages'
import i18next, { type TFunction } from 'i18next'
import { getCurrencyDisplay } from '@/lib/currency'
import { formatUsdAmount, quotaUnitsToUsd } from '@/lib/format'
import type { BountyTaskStatus, BountyWalletType } from '../types'

export function formatBountyAmount(value: number) {
  return formatUsdAmount(quotaUnitsToUsd(value))
}

export function bountyUsdToQuota(value: number) {
  if (!Number.isFinite(value) || value <= 0) return 0
  return Math.max(
    1,
    Math.round(value * getCurrencyDisplay().config.quotaPerUnit)
  )
}

export function formatBountyDate(value?: string) {
  if (!value) return '—'
  return new Intl.DateTimeFormat(toIntlLocale(i18next.language), {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(value))
}

export function formatBountyRelativeTime(value: string, t: TFunction) {
  const remaining = new Date(value).getTime() - Date.now()
  const hours = Math.floor(remaining / 3_600_000)
  if (hours < 0) return t('Deadline passed')
  if (hours < 24) {
    return t('Due in {{count}} hours', { count: Math.max(hours, 1) })
  }
  return t('Due in {{count}} days', { count: Math.floor(hours / 24) })
}

export function walletLabel(walletType: BountyWalletType, t: TFunction) {
  return walletType === 'claude_wallet' ? t('Claude quota') : t('Normal quota')
}

export function taskTypeLabel(taskType: string, t: TFunction) {
  const labels: Record<string, string> = {
    general: 'General coding',
    ui: 'UI / interaction',
    frontend: 'Frontend',
    backend: 'Backend',
  }
  return t(labels[taskType] ?? 'Other task type')
}

export function taskStatusLabel(
  status: BountyTaskStatus | string,
  t: TFunction
) {
  const labels: Record<string, string> = {
    draft: 'Draft',
    published: 'Available to apply',
    selecting: 'Selecting executor',
    assigned: 'Ready to start',
    in_progress: 'In progress',
    waiting_for_publisher: 'Waiting for reply',
    publisher_replied: 'Awaiting confirmation',
    submitted: 'Submitted',
    reviewing: 'Awaiting review',
    changes_requested: 'Changes requested',
    completed: 'Completed',
    expired: 'Expired',
    cancelled: 'Cancelled',
    disputed: 'Disputed',
    resolved: 'Resolved',
    suspended: 'Suspended',
  }
  return t(labels[status] ?? 'Unknown task status')
}

export function applicationStatusLabel(status: string, t: TFunction) {
  const labels: Record<string, string> = {
    pending: 'Application pending',
    accepted: 'Application accepted',
    rejected: 'Application not selected',
  }
  return t(labels[status] ?? 'Unknown application status')
}

export function materialStatusLabel(status: string, t: TFunction) {
  const labels: Record<string, string> = {
    open: 'Awaiting publisher reply',
    replied: 'Reply received',
    awaiting_confirmation: 'Awaiting executor confirmation',
    closed: 'Resolved',
  }
  return t(labels[status] ?? 'Unknown material status')
}

export function taskStatusTone(status: BountyTaskStatus) {
  if (status === 'completed' || status === 'resolved') return 'success'
  if (status === 'waiting_for_publisher' || status === 'changes_requested')
    return 'warning'
  if (status === 'disputed') return 'danger'
  if (status === 'published' || status === 'selecting') return 'info'
  if (status === 'expired' || status === 'cancelled' || status === 'suspended')
    return 'muted'
  return 'default'
}

export function bountyRequiresEffectImages(taskType: string, tags: string[]) {
  if (taskType === 'ui' || taskType === 'frontend') return true
  return tags.some((tag) =>
    [
      'ui',
      'frontend',
      'interface',
      'interaction',
      'design',
      '前端',
      '界面',
      '交互',
      '设计',
    ].includes(tag.trim().toLowerCase())
  )
}
