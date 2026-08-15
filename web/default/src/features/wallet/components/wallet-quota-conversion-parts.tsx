import { Clock3 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { WalletQuotaConversionOverview } from '../types'

const STANDARD_TO_CLAUDE = 'standard_to_claude'

export function DirectionButton(props: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type='button'
      className={cn(
        'focus-visible:ring-ring min-h-9 rounded-md px-3 text-sm font-medium transition focus-visible:ring-2 focus-visible:outline-none',
        props.active
          ? 'bg-background text-foreground shadow-sm'
          : 'text-muted-foreground hover:text-foreground'
      )}
      onClick={props.onClick}
    >
      {props.children}
    </button>
  )
}

export function AmountPanel(props: {
  label: string
  balance: number
  input: string
  max: number
  onInput: (value: string) => void
  onMax: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className='border-border/70 rounded-lg border p-3.5'>
      <div className='flex items-center justify-between gap-2 text-xs'>
        <span className='text-foreground font-medium'>{props.label}</span>
        <span className='text-muted-foreground'>
          {t('Available')} {formatUSD(props.balance)}
        </span>
      </div>
      <div className='mt-3 flex items-center gap-2'>
        <Input
          type='number'
          min='0.01'
          max={props.max}
          step='0.01'
          inputMode='decimal'
          value={props.input}
          onChange={(event) => props.onInput(event.target.value)}
          placeholder='0.00'
          className='h-11 text-base tabular-nums'
        />
        <Button type='button' variant='outline' onClick={props.onMax}>
          {t('All')}
        </Button>
      </div>
    </div>
  )
}

export function ReceivePanel(props: {
  label: string
  amount: number
  rate: string
}) {
  const { t } = useTranslation()
  return (
    <div className='border-border/70 bg-muted/25 rounded-lg border p-3.5'>
      <div className='text-foreground text-xs font-medium'>{props.label}</div>
      <div className='text-foreground mt-3 text-xl font-semibold tabular-nums'>
        {formatUSD(props.amount)}
      </div>
      <div className='text-muted-foreground mt-2 text-xs'>
        {t('Fixed rate')}: {props.rate}
      </div>
    </div>
  )
}

export function ConversionHistory(props: {
  overview: WalletQuotaConversionOverview | null
  standalone?: boolean
}) {
  const { t } = useTranslation()
  const records = props.overview?.recent_conversions || []
  if (records.length === 0) return null
  const quotaPerUSD = props.overview?.quota_per_usd || 500_000
  return (
    <div
      className={cn(
        'border-border/70',
        props.standalone ? '' : 'mt-4 border-t pt-4'
      )}
    >
      {!props.standalone ? (
        <div className='text-foreground flex items-center gap-2 text-sm font-semibold'>
          <Clock3 className='text-muted-foreground size-4' />
          {t('Recent wallet conversions')}
        </div>
      ) : null}
      <div className='mt-2 divide-y'>
        {records.slice(0, 5).map((record) => (
          <div
            key={record.id}
            className='flex flex-wrap items-center justify-between gap-x-4 gap-y-1 py-2.5 text-xs'
          >
            <span className='text-muted-foreground'>
              {new Date(record.created_at * 1000).toLocaleString()}
            </span>
            <span className='text-foreground font-medium tabular-nums'>
              {formatUSD(record.source_quota / quotaPerUSD)}{' '}
              {record.direction === STANDARD_TO_CLAUDE
                ? t('Standard balance')
                : t('Universal quota')}{' '}
              → {formatUSD(record.target_quota / quotaPerUSD)}{' '}
              {record.direction === STANDARD_TO_CLAUDE
                ? t('Universal quota')
                : t('Standard balance')}
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}

export function formatInputAmount(value: number) {
  if (!Number.isFinite(value)) return ''
  return value.toFixed(6).replace(/\.?0+$/, '')
}

export function formatUSD(value: number) {
  if (!Number.isFinite(value)) return '$0'
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: 'USD',
    currencyDisplay: 'narrowSymbol',
    minimumFractionDigits: 0,
    maximumFractionDigits: 4,
  }).format(value)
}
