import { cn } from '@/lib/utils'
import { formatLuckyUsd } from '../lib'
import type { LuckyPublicWin } from '../types'
import { LuckyDigits } from './lucky-digits'
import { TierBadge } from './tier-badge'

export type WinnerFilter = 'all' | 1 | 2 | 3 | 4

const FILTERS: Array<{ value: WinnerFilter; label: string }> = [
  { value: 'all', label: '全部' },
  { value: 1, label: '命中 1 位' },
  { value: 2, label: '命中 2 位' },
  { value: 3, label: '命中 3 位' },
  { value: 4, label: '命中 4 位' },
]

export function WinnerFilters(props: {
  filter: WinnerFilter
  counts: number[]
  total: number
  onChange: (filter: WinnerFilter) => void
}) {
  return (
    <div className='border-border/70 bg-muted/20 border-b p-3 sm:p-4'>
      <div
        className='grid grid-cols-2 gap-2 sm:grid-cols-5'
        role='group'
        aria-label='按命中位数筛选'
      >
        {FILTERS.map((item) => {
          const count =
            item.value === 'all' ? props.total : props.counts[item.value - 1]
          return (
            <button
              key={item.value}
              type='button'
              aria-pressed={props.filter === item.value}
              className={cn(
                'border-border bg-background hover:border-primary/40 focus-visible:ring-ring flex min-h-14 items-center justify-between gap-2 rounded-lg border px-3 text-left transition-colors focus-visible:ring-2 focus-visible:outline-none',
                props.filter === item.value &&
                  'border-primary bg-primary/[0.06]'
              )}
              onClick={() => props.onChange(item.value)}
            >
              <span
                className={cn(
                  'text-xs font-medium',
                  props.filter === item.value
                    ? 'text-primary'
                    : 'text-muted-foreground'
                )}
              >
                {item.label}
              </span>
              <span className='text-foreground font-mono text-lg font-semibold tabular-nums'>
                {count}
              </span>
            </button>
          )
        })}
      </div>
    </div>
  )
}

export function WinnerList(props: { records: LuckyPublicWin[] }) {
  return (
    <div className='divide-border/70 @container grid divide-y @2xl:grid-cols-2 @2xl:divide-y-0'>
      {props.records.map((item, index) => (
        <article
          key={`${item.draw_date}-${item.lucky_suffix}-${item.matched_digits}-${index}`}
          className='border-border/70 hover:bg-muted/20 flex min-w-0 items-center gap-3 px-4 py-3.5 transition-colors sm:px-5 lg:border-b lg:odd:border-r'
        >
          <MatchLevel digits={item.matched_digits} />
          <div className='min-w-0 flex-1'>
            <div className='flex flex-wrap items-center gap-2'>
              <LuckyDigits value={item.lucky_suffix} />
              <TierBadge tier={item.membership_tier} compact />
            </div>
            <div className='text-muted-foreground mt-1 text-[11px]'>
              幸运尾号
            </div>
          </div>
          <div className='shrink-0 text-right'>
            <div className='text-success font-mono text-sm font-semibold tabular-nums'>
              +{formatLuckyUsd(item.reward_usd)}
            </div>
            <div className='text-muted-foreground mt-1 text-[11px]'>已到账</div>
          </div>
        </article>
      ))}
    </div>
  )
}

function MatchLevel(props: { digits: number }) {
  const tone = {
    1: 'bg-muted text-muted-foreground',
    2: 'bg-info/10 text-info',
    3: 'bg-primary/10 text-primary',
    4: 'bg-success/10 text-success',
  }[props.digits]

  return (
    <span
      className={cn(
        'flex size-10 shrink-0 flex-col items-center justify-center rounded-lg',
        tone
      )}
    >
      <strong className='font-mono text-base leading-none tabular-nums'>
        {props.digits}
      </strong>
      <span className='mt-0.5 text-[9px] font-medium'>位</span>
    </span>
  )
}
