import type { LucideIcon } from 'lucide-react'

export function IncomeMetric(props: {
  icon: LucideIcon
  label: string
  value: string
}) {
  const Icon = props.icon
  return (
    <span className='bg-muted/35 flex min-w-0 items-center gap-2 rounded-md px-2.5 py-2'>
      <Icon className='text-muted-foreground size-3.5 shrink-0' />
      <span className='text-muted-foreground truncate'>{props.label}</span>
      <span className='ml-auto font-medium tabular-nums'>{props.value}</span>
    </span>
  )
}
