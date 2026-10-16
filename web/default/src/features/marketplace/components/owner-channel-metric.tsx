import type { LucideIcon } from 'lucide-react'

export function IncomeMetric(props: {
  icon: LucideIcon
  label: string
  value: string
}) {
  const Icon = props.icon
  return (
    <span className='owner-income-metric flex min-w-0 items-center gap-2'>
      <Icon className='text-muted-foreground size-3.5 shrink-0' />
      <span className='text-muted-foreground truncate'>{props.label}</span>
      <span className='ml-auto font-mono font-medium tabular-nums'>
        {props.value}
      </span>
    </span>
  )
}
