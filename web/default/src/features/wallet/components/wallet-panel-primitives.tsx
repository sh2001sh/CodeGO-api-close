import type { ReactNode } from 'react'

export function WalletStatItem(props: {
  label: string
  value: string
  icon?: ReactNode
}) {
  return (
    <div className='app-subtle-panel flex items-center justify-between gap-3 px-3 py-3'>
      <div className='text-muted-foreground flex items-center gap-2 text-sm'>
        {props.icon}
        <span>{props.label}</span>
      </div>
      <div className='text-foreground font-mono text-sm font-semibold tabular-nums'>
        {props.value}
      </div>
    </div>
  )
}
