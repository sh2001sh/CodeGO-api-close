import {
  CircleHelp,
  CreditCard,
  Layers3,
  Package,
  WalletCards,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import type { LogOtherData } from '../types'

type BillingQuotaCategory =
  | 'universal'
  | 'gpt'
  | 'subscription'
  | 'credit_pack'
  | 'unknown'

function resolveBillingQuotaCategory(
  other: LogOtherData | null | undefined
): BillingQuotaCategory | null {
  if (!other) return null
  // Audit metadata is persisted JSON: runtime values can outlive this UI's types.
  switch (other.billing_quota_category) {
    case 'universal':
    case 'gpt':
    case 'subscription':
    case 'credit_pack':
      return other.billing_quota_category
    default:
      if (other.billing_quota_category) return 'unknown'
  }

  switch (other.billing_source) {
    case 'claude_wallet':
      return 'universal'
    case 'wallet':
      return 'gpt'
    case 'subscription':
      if (other.subscription_billing_version === 'credit_pack')
        return 'credit_pack'
      return 'subscription'
    default:
      return null
  }
}

export function BillingQuotaSourceBadge(props: {
  other: LogOtherData | null | undefined
  className?: string
}) {
  const { t } = useTranslation()
  const category = resolveBillingQuotaCategory(props.other)
  if (!category) return null

  const config = {
    universal: {
      label: t('Universal quota'),
      icon: WalletCards,
      className:
        'border-sky-500/25 bg-sky-500/10 text-sky-700 dark:text-sky-300',
    },
    gpt: {
      label: t('Official GPT quota'),
      icon: CreditCard,
      className:
        'border-amber-500/25 bg-amber-500/10 text-amber-700 dark:text-amber-300',
    },
    subscription: {
      label: t('GPT plan quota'),
      icon: Layers3,
      className: 'border-success/30 bg-success/10 text-success',
    },
    credit_pack: {
      label: t('Permanent plan quota'),
      icon: Package,
      className: 'border-success/30 bg-success/10 text-success',
    },
    unknown: {
      label: t('Unknown'),
      icon: CircleHelp,
      className: 'border-border bg-muted text-muted-foreground',
    },
  }[category]
  const Icon = config.icon

  return (
    <span
      className={cn(
        'inline-flex w-fit items-center gap-1 rounded-md border px-1.5 py-0.5 text-[11px] font-medium whitespace-nowrap',
        config.className,
        props.className
      )}
    >
      <Icon className='size-3' aria-hidden='true' />
      {config.label}
    </span>
  )
}
