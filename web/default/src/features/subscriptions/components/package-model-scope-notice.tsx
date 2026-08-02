import { BadgeInfo } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

export function PackageModelScopeNotice(props: { className?: string }) {
  const { t } = useTranslation()
  return (
    <div
      role='note'
      className={cn(
        'border-border bg-muted/35 flex items-start gap-3 rounded-lg border px-4 py-3',
        props.className
      )}
    >
      <BadgeInfo
        className='text-primary mt-0.5 size-4 shrink-0'
        aria-hidden='true'
      />
      <div>
        <p className='text-foreground text-sm font-semibold'>
          {t('Package purchase rules')}
        </p>
        <ul className='text-muted-foreground mt-1 list-disc space-y-1 pl-4 text-xs leading-5'>
          <li>
            {t(
              'Plan quota cannot be used with Claude models. Use Claude quota for Claude requests.'
            )}
          </li>
          <li>
            {t(
              'First-purchase discounts apply only to the first monthly plan. Starter, daily, and weekly plans neither receive nor consume this eligibility.'
            )}
          </li>
          <li>
            {t(
              'Renewal is available after at least 30% of the current plan quota is used. The price follows the used percentage with a 30% minimum; renewal restarts the term and unused quota does not roll over.'
            )}
          </li>
        </ul>
      </div>
    </div>
  )
}
