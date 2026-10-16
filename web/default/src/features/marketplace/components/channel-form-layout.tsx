import {
  Children,
  cloneElement,
  isValidElement,
  useId,
  type ReactElement,
} from 'react'
import type { LucideIcon } from 'lucide-react'
import { Label } from '@/components/ui/label'

export function FormSection(props: {
  icon: LucideIcon
  title: string
  children: React.ReactNode
}) {
  const Icon = props.icon
  return (
    <section className='marketplace-editor-section'>
      <div className='flex items-start gap-3'>
        <span className='marketplace-editor-section-icon'>
          <Icon className='size-4' />
        </span>
        <div>
          <h4 className='text-base font-semibold'>{props.title}</h4>
        </div>
      </div>
      <div className='min-w-0 space-y-4'>{props.children}</div>
    </section>
  )
}

export function FormField(props: {
  label: string
  error?: string
  children: React.ReactNode
}) {
  const generatedID = useId()
  const children = Children.toArray(props.children)
  const control = isValidElement(children[0])
    ? (children[0] as ReactElement<{ id?: string }>)
    : null
  const fieldID = control?.props.id ?? generatedID
  return (
    <div className='space-y-2'>
      <Label htmlFor={fieldID}>{props.label}</Label>
      {children.map((child, index) =>
        index === 0 && control ? cloneElement(control, { id: fieldID }) : child
      )}
      {props.error && (
        <p className='text-destructive text-base' role='alert'>
          {props.error}
        </p>
      )}
    </div>
  )
}
