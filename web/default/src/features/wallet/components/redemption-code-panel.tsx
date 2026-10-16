import { ExternalLink, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

interface RedemptionCodePanelProps {
  title?: string
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
    <section
      className={cn('commerce-redemption', props.className)}
      aria-labelledby='redemption-code-title'
    >
      <div className='commerce-redemption-heading'>
        <h2 id='redemption-code-title' className='commerce-redemption-title'>
          {props.title || t('Redemption code')}
        </h2>
      </div>
      <div className='commerce-redemption-entry'>
        <Input
          id='wallet-redemption-code'
          aria-labelledby='redemption-code-title'
          value={props.redemptionCode}
          onChange={(event) => props.onRedemptionCodeChange(event.target.value)}
          autoComplete='off'
          className='min-w-0 font-mono'
          onKeyDown={(event) => {
            if (
              event.key === 'Enter' &&
              props.redemptionCode.trim() &&
              !props.redeeming
            )
              props.onRedeem()
          }}
        />
        <Button
          onClick={props.onRedeem}
          disabled={props.redeeming || !props.redemptionCode.trim()}
          className='commerce-redemption-submit'
        >
          {props.redeeming ? (
            <Loader2 className='h-4 w-4 animate-spin' />
          ) : null}
          {t('Redeem now')}
        </Button>
      </div>
      {props.topupLink ? (
        <a
          className='commerce-redemption-link'
          href={props.topupLink}
          target='_blank'
          rel='noopener noreferrer'
        >
          {t('Get a redemption code')}
          <ExternalLink className='size-3.5' aria-hidden='true' />
        </a>
      ) : null}
    </section>
  )
}
