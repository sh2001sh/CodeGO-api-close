import type { ReactNode } from 'react'
import { AlertCircle, CheckCircle2, Info, Inbox, TriangleAlert } from 'lucide-react'
import { useTranslation } from '../../lib/i18n'

export function ErrorMessage(props: { error?: Error | null }) {
  const { t } = useTranslation()
  if (!props.error) return null
  return (
    <div role="alert" className="error-message">
      <AlertCircle size={16} aria-hidden />
      <span>{t(props.error.message)}</span>
    </div>
  )
}

type Tone = 'info' | 'success' | 'warning' | 'danger'
const toneIcon = { info: Info, success: CheckCircle2, warning: TriangleAlert, danger: AlertCircle }

/** Inline message block for guidance, warnings and confirmations inside a page. */
export function Callout(props: { tone?: Tone; title?: string; children?: ReactNode }) {
  const { t } = useTranslation()
  const tone = props.tone ?? 'info'
  const Icon = toneIcon[tone]
  return (
    <div
      className={tone === 'danger' ? 'error-message' : 'callout'}
      data-tone={tone}
      role={tone === 'danger' || tone === 'warning' ? 'alert' : 'note'}
    >
      <Icon size={16} aria-hidden />
      <div className="callout-body">
        {props.title && <span className="callout-title">{t(props.title)}</span>}
        {props.children && <div>{props.children}</div>}
      </div>
    </div>
  )
}

export function Loading(props: { rows?: number }) {
  const { t } = useTranslation()
  const rows = Math.max(2, props.rows ?? 3)
  return (
    <div aria-label={t('正在加载')} role="status" className="loading">
      {Array.from({ length: rows }, (_, index) => (
        <span key={index} />
      ))}
    </div>
  )
}

export function EmptyState(props: {
  title: string
  description?: string
  icon?: ReactNode
  action?: ReactNode
}) {
  const { t } = useTranslation()
  return (
    <div className="empty-state">
      <div className="empty-state-icon" aria-hidden>
        {props.icon ?? <Inbox size={20} />}
      </div>
      <div className="empty-state-title">{t(props.title)}</div>
      {props.description && <p>{t(props.description)}</p>}
      {props.action}
    </div>
  )
}

const successStates = new Set([
  'active',
  'enabled',
  'paid',
  'settled',
  'completed',
  'Completed',
  'verified',
  'approved',
  'issued',
  'resolved',
  'available',
  'succeeded',
  'success',
  'online',
  'healthy',
  'credited',
])
const dangerStates = new Set([
  'disabled',
  'auto_disabled',
  'failed',
  'canceled',
  'cancelled',
  'rejected',
  'revoked',
  'banned',
  'error',
  'offline',
  'UpstreamErrorBeforeOutput',
  'UpstreamErrorAfterOutput',
  'EmptyStream',
  'Timeout',
])
const warningStates = new Set([
  'pending',
  'created',
  'verifying',
  'draft',
  'forming',
  'open',
  'paused',
  'expired',
  'refunded',
  'degraded',
  'CompletedNoUsage',
  'ClientCanceled',
  'processing',
])

export function statusTone(value: string): Tone | 'neutral' {
  if (successStates.has(value)) return 'success'
  if (dangerStates.has(value)) return 'danger'
  if (warningStates.has(value)) return 'warning'
  return 'neutral'
}

export function Status(props: { value: string }) {
  const { t } = useTranslation()
  const value = String(props.value ?? '')
  return (
    <span className="status" data-state={value} data-tone={statusTone(value)}>
      {t(value) || '—'}
    </span>
  )
}

export function Badge(props: {
  tone?: Tone | 'accent' | 'neutral'
  children: ReactNode
  className?: string
}) {
  return (
    <span className={`badge ${props.className ?? ''}`.trim()} data-tone={props.tone ?? 'neutral'}>
      {props.children}
    </span>
  )
}
