import { Gift, type LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Input } from '@/components/ui/input'

export function BalanceBoxQuantityControl(props: {
  count: number
  max: number
  disabled: boolean
  onChange: (count: number) => void
}) {
  return (
    <div>
      <div className='flex flex-wrap items-baseline justify-between gap-2'>
        <label
          className='text-foreground text-sm font-semibold'
          htmlFor='balance-box-count'
        >
          盲盒数量
        </label>
        <span className='text-muted-foreground text-xs'>
          单次最多 {props.max}
        </span>
      </div>
      <div className='mt-2.5 flex min-w-0 flex-wrap items-center gap-2'>
        {[1, 5, 10, 20, 50, 100]
          .filter((value) => value <= props.max)
          .map((value) => (
            <button
              key={value}
              type='button'
              disabled={props.disabled}
              onClick={() => props.onChange(value)}
              className={cn(
                'rounded-full border px-3.5 py-1.5 text-sm font-medium transition-colors disabled:opacity-50',
                props.count === value
                  ? 'border-teal-600 bg-teal-600 text-white dark:border-teal-500 dark:bg-teal-500'
                  : 'border-border bg-background/80 text-foreground hover:border-teal-500/70'
              )}
            >
              x{value}
            </button>
          ))}
        <Input
          id='balance-box-count'
          type='number'
          min={1}
          max={props.max}
          value={props.count}
          disabled={props.disabled}
          aria-label='自定义盲盒数量'
          className='h-9 w-20 max-w-full'
          onChange={(event) =>
            props.onChange(
              Math.min(
                props.max,
                Math.max(1, Math.floor(Number(event.target.value) || 1))
              )
            )
          }
        />
      </div>
    </div>
  )
}

export function BalanceBoxModeButton(props: {
  active: boolean
  icon: LucideIcon
  label: string
  onClick: () => void
}) {
  const Icon = props.icon || Gift
  return (
    <button
      type='button'
      role='tab'
      aria-selected={props.active}
      onClick={props.onClick}
      className={cn(
        'focus-visible:ring-ring flex min-h-10 items-center justify-center gap-2 rounded-md px-2 text-sm font-medium transition-colors outline-none focus-visible:ring-2',
        props.active
          ? 'bg-background text-teal-700 shadow-sm dark:text-teal-300'
          : 'text-muted-foreground hover:text-foreground'
      )}
    >
      <Icon className='size-4' />
      <span>{props.label}</span>
    </button>
  )
}

export function BalanceBoxMetric(props: { label: string; value: string }) {
  return (
    <div>
      <div className='text-muted-foreground text-[11px]'>{props.label}</div>
      <div className='text-foreground mt-1 font-mono text-sm font-semibold tabular-nums'>
        {props.value}
      </div>
    </div>
  )
}

export function newBalanceBoxRequestId() {
  return typeof crypto !== 'undefined' && crypto.randomUUID
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(36).slice(2)}`
}
