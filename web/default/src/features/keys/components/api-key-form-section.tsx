import type { ReactNode } from 'react'
import type { LucideIcon } from 'lucide-react'

export function ApiKeyFormSection(props: {
  title: string
  description: string
  icon: LucideIcon
  children: ReactNode
}) {
  const Icon = props.icon
  return (
    <section className='api-key-form-section'>
      <div className='api-key-form-section-heading'>
        <div className='api-key-form-section-icon'>
          <Icon className='size-4' aria-hidden='true' />
        </div>
        <div className='min-w-0'>
          <h3>{props.title}</h3>
          <p>{props.description}</p>
        </div>
      </div>
      <div className='space-y-6'>{props.children}</div>
    </section>
  )
}
