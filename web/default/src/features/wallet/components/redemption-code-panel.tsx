import { ExternalLink, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

interface RedemptionCodePanelProps {
  title?: string
  description?: string
  topupLink?: string
  redemptionCode: string
  onRedemptionCodeChange: (code: string) => void
  onRedeem: () => void
  redeeming: boolean
  className?: string
  compact?: boolean
}

export function RedemptionCodePanel(props: RedemptionCodePanelProps) {
  const { t } = useTranslation()

  return (
    <div className={cn('app-page-shell p-4', props.className)}>
      <div className='flex flex-wrap items-start justify-between gap-3'>
        <div className='flex min-w-0 items-center gap-2.5'>
          <span aria-hidden className='block h-3 w-[3px] shrink-0 bg-primary' />
          <div className='text-foreground text-[13px] font-semibold'>
            {props.title || t('Redemption code')}
          </div>
        </div>
        {props.topupLink ? (
          <Button
            variant='outline'
            size='sm'
            render={
              <a
                href={props.topupLink}
                target='_blank'
                rel='noopener noreferrer'
              />
            }
          >
            {t('Get a redemption code')}
            <ExternalLink data-icon='inline-end' />
          </Button>
        ) : null}
      </div>

      <div
        className={cn(
          'mt-4 grid gap-2',
          props.compact
            ? 'grid-cols-[minmax(0,1fr)_auto]'
            : 'sm:grid-cols-[minmax(0,1fr)_auto]'
        )}
      >
        <Input
          value={props.redemptionCode}
          onChange={(event) => props.onRedemptionCodeChange(event.target.value)}
          placeholder={t('Enter redemption code')}
          className='h-10 min-w-0'
        />
        <Button
          onClick={props.onRedeem}
          disabled={props.redeeming}
          className='h-10 px-5'
        >
          {props.redeeming ? (
            <Loader2 className='h-4 w-4 animate-spin' />
          ) : (
            t('Redeem now')
          )}
        </Button>
      </div>
    </div>
  )
}
