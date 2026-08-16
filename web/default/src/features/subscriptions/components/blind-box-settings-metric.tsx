import type { LucideIcon } from 'lucide-react'

export function BlindBoxSettingsMetric(props: {
  icon: LucideIcon
  label: string
  value: string
}) {
  const Icon = props.icon
  return (
    <div className='bg-muted/35 rounded-lg border px-3 py-2.5'>
      <div className='text-muted-foreground flex items-center gap-1.5 text-xs'>
        <Icon className='size-3.5' aria-hidden='true' />
        {props.label}
      </div>
      <div className='mt-1 text-sm font-semibold tabular-nums'>
        {props.value}
      </div>
    </div>
  )
}
