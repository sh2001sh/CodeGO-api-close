import { Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  getMembershipTierLabel,
  MEMBERSHIP_TIER_META,
  normalizeMembershipTier,
} from '../lib'

export function TierBadge(props: {
  tier?: string
  compact?: boolean
  className?: string
}) {
  const { t } = useTranslation()
  const tier = normalizeMembershipTier(props.tier)
  const meta = MEMBERSHIP_TIER_META[tier]
  return (
    <span
      className={cn(
        'inline-flex w-fit items-center gap-1.5 rounded-full border px-2 py-1 text-[11px] font-semibold',
        props.compact && 'px-1.5 py-0.5 text-[10px]',
        props.className
      )}
      style={{
        color: meta.color,
        backgroundColor: meta.softColor,
        borderColor: meta.borderColor,
      }}
    >
      <Sparkles className={cn(props.compact ? 'size-2.5' : 'size-3')} aria-hidden='true' />
      <span>{getMembershipTierLabel(tier, t)}</span>
    </span>
  )
}
