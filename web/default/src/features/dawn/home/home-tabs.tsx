/* Copyright (C) 2023-2026 QuantumNous. AGPL-3.0-or-later. */
import type { KeyboardEvent } from 'react'
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useMediaQuery } from '@/hooks/use-media-query'

type HomeTabsProps<T extends string> = {
  id: string
  labelKey: string
  value: T
  options: readonly { value: T; labelKey: string }[]
  onChange: (value: T) => void
  variant: 'view' | 'role' | 'code'
}

export function HomeTabs<T extends string>(props: HomeTabsProps<T>) {
  const { t } = useTranslation()
  const desktop = useMediaQuery('(min-width: 768px)')
  const vertical = props.variant === 'view' && desktop

  function handleKey(event: KeyboardEvent<HTMLDivElement>): void {
    const buttons = Array.from(
      event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="tab"]')
    )
    const current = buttons.indexOf(document.activeElement as HTMLButtonElement)
    if (current < 0) return
    let next: number
    if (event.key === 'Home') next = 0
    else if (event.key === 'End') next = buttons.length - 1
    else if (event.key === (vertical ? 'ArrowDown' : 'ArrowRight'))
      next = (current + 1) % buttons.length
    else if (event.key === (vertical ? 'ArrowUp' : 'ArrowLeft'))
      next = (current - 1 + buttons.length) % buttons.length
    else return
    event.preventDefault()
    props.onChange(props.options[next].value)
    buttons[next].focus()
  }

  return (
    <div
      className={`${props.variant}-tabs`}
      role='tablist'
      aria-label={t(props.labelKey)}
      aria-orientation={vertical ? 'vertical' : 'horizontal'}
      onKeyDown={handleKey}
    >
      {props.options.map((option) => (
        <button
          key={option.value}
          id={`${props.id}-${option.value}`}
          className={`${props.variant}-tab`}
          type='button'
          role='tab'
          aria-selected={props.value === option.value}
          aria-controls={`${props.id}-panel`}
          tabIndex={props.value === option.value ? 0 : -1}
          onClick={() => props.onChange(option.value)}
        >
          <span className='od-field'>
            <strong>{t(option.labelKey)}</strong>
          </span>
          {props.variant === 'view' && (
            <ArrowRight className='icon' aria-hidden='true' />
          )}
        </button>
      ))}
    </div>
  )
}
