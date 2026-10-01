import { Button as BaseButton } from '@base-ui/react/button'
import type { ComponentProps, ReactNode } from 'react'
import { useTranslation } from '../lib/i18n'

export function Button(
  props: ComponentProps<typeof BaseButton> & { variant?: 'primary' | 'quiet' | 'danger' },
) {
  const { variant = 'primary', className = '', ...rest } = props
  return <BaseButton {...rest} className={`button button-${variant} ${className}`} />
}

export function PageHeader(props: { title: string; action?: ReactNode }) {
  const { t } = useTranslation()
  return (
    <header className="page-header">
      <h1>{t(props.title)}</h1>
      <div>{props.action}</div>
    </header>
  )
}

export function ErrorMessage(props: { error?: Error | null }) {
  const { t } = useTranslation()
  if (!props.error) return null
  return (
    <div role="alert" className="error-message">
      {t(props.error.message)}
    </div>
  )
}

export function Field(props: {
  label: string
  name: string
  type?: string
  required?: boolean
  defaultValue?: string | number
  placeholder?: string
  min?: number
  maxLength?: number
}) {
  const { t } = useTranslation()
  return (
    <label className="field" htmlFor={props.name}>
      <span>{t(props.label)}</span>
      <input
        id={props.name}
        name={props.name}
        type={props.type ?? 'text'}
        required={props.required}
        defaultValue={props.defaultValue}
        placeholder={props.placeholder}
        min={props.min}
        maxLength={props.maxLength}
      />
    </label>
  )
}

export function Loading() {
  const { t } = useTranslation()
  return (
    <div aria-label={t('正在加载')} role="status" className="loading">
      <span />
      <span />
      <span />
    </div>
  )
}

export function Status(props: { value: string }) {
  const { t } = useTranslation()
  return (
    <span className="status" data-state={props.value}>
      {t(props.value)}
    </span>
  )
}
