import type { LucideIcon } from 'lucide-react'
import { Label } from '@/components/ui/label'

export function FormSection(props: {
  icon: LucideIcon
  title: string
  description: string
  children: React.ReactNode
}) {
  const Icon = props.icon
  return (
    <section className='border-border grid gap-5 border-b px-4 py-6 sm:px-5 lg:grid-cols-[190px_minmax(0,1fr)]'>
      <div className='flex items-start gap-3'>
        <span className='bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center rounded-md'>
          <Icon className='size-4' />
        </span>
        <div>
          <h4 className='text-sm font-semibold'>{props.title}</h4>
          <p className='text-muted-foreground mt-1 text-xs leading-5'>
            {props.description}
          </p>
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
  return (
    <div className='space-y-2'>
      <Label>{props.label}</Label>
      {props.children}
      {props.error && (
        <p className='text-destructive text-xs' role='alert'>
          {props.error}
        </p>
      )}
    </div>
  )
}
