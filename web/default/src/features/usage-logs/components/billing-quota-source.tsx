import { CreditCard, Layers3, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import type { LogOtherData } from '../types'

type BillingQuotaCategory = 'universal' | 'gpt' | 'subscription'

function resolveBillingQuotaCategory(
  other: LogOtherData | null | undefined
): BillingQuotaCategory | null {
  if (!other) return null
  if (other.billing_quota_category) return other.billing_quota_category

  switch (other.billing_source) {
    case 'claude_wallet':
      return 'universal'
    case 'wallet':
      return 'gpt'
    case 'subscription':
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
