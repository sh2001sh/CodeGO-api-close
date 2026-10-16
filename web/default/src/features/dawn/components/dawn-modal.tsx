/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com.
*/
import { useEffect, useRef, type ReactNode } from 'react'
import i18n from '@/i18n/config'
import { X } from 'lucide-react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'

export function DawnModal(props: {
  open: boolean
  onClose: () => void
  children: ReactNode
  variant?: 'rail' | 'narrow' | 'plain'
  label?: string
}) {
  const { open, onClose, children, variant = 'rail', label } = props
  const dialogRef = useRef<HTMLDivElement>(null)
  const closeRef = useRef(onClose)
  useEffect(() => {
    closeRef.current = onClose
  }, [onClose])

  useEffect(() => {
    if (!open) return
    const previousFocus = document.activeElement
    const dialog = dialogRef.current
    const controls = () =>
      Array.from(
        dialog?.querySelectorAll<HTMLElement>(
          'button:not(:disabled), a[href], input:not(:disabled), select:not(:disabled), [tabindex="0"]'
        ) ?? []
      ).filter((element) => element.getClientRects().length > 0)
    ;(controls()[0] ?? dialog)?.focus()
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        closeRef.current()
      }
      if (event.key === 'Tab') {
        const items = controls()
        const first = items[0]
        const last = items[items.length - 1]
        if (!first || !last) {
          event.preventDefault()
          dialog?.focus()
          return
        }
        if (
          event.shiftKey &&
          (document.activeElement === first ||
            document.activeElement === dialog)
        ) {
          event.preventDefault()
          last.focus()
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault()
          first.focus()
        }
      }
    }
    document.addEventListener('keydown', onKey)
    const { overflow } = document.body.style
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = overflow
      if (previousFocus instanceof HTMLElement) previousFocus.focus()
    }
  }, [open])

  if (!open) return null

  return createPortal(
    <div className='dawn dawn-portal'>
      <div className='mask' onClick={onClose} aria-hidden />
      <div
        className='modal'
        role='dialog'
        aria-modal='true'
        aria-label={label ?? 'dialog'}
        ref={dialogRef}
        tabIndex={-1}
        onClick={(event) => {
          if (event.target === event.currentTarget) onClose()
        }}
      >
        <div
          className={`box ${variant === 'narrow' ? 'narrow' : ''} ${variant === 'plain' ? 'plain' : ''}`}
        >
          {children}
        </div>
      </div>
    </div>,
    document.body
  )
}

export function ModalHead(props: { title: string; onClose: () => void }) {
  useTranslation()
  return (
    <div className='m-head'>
      <h3>{props.title}</h3>
      <button className='x' onClick={props.onClose} aria-label={i18n.t('关闭')}>
        <X size={18} />
      </button>
    </div>
  )
}
