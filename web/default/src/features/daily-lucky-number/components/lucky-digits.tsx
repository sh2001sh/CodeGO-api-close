import { cn } from '@/lib/utils'
import { normalizeLuckyNumber } from '../lib'

export function LuckyDigits(props: {
  value?: string | number
  placeholder?: string
  size?: 'sm' | 'lg'
  className?: string
}) {
  const value = normalizeLuckyNumber(props.value)
  const placeholder = props.placeholder ?? '----'
  const digits = (value || placeholder).slice(-4).split('')
  return (
    <div
      className={cn(
        'flex items-center gap-1.5 font-mono font-semibold tabular-nums',
        props.size === 'lg' ? 'text-2xl sm:text-3xl' : 'text-base',
        props.className
      )}
      aria-label={value || placeholder}
    >
      {digits.map((digit, index) => (
        <span
          key={`${digit}-${index}`}
          className={cn(
            'bg-muted/65 text-foreground inline-flex aspect-square w-8 items-center justify-center rounded-lg border',
            props.size === 'lg' && 'w-11 rounded-xl sm:w-12'
          )}
        >
          {digit}
        </span>
      ))}
    </div>
  )
}
