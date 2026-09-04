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
import { useEffect, type ReactNode } from 'react'
import { X } from 'lucide-react'
import { createPortal } from 'react-dom'

export function DawnModal(props: {
  open: boolean
  onClose: () => void
  children: ReactNode
  variant?: 'rail' | 'narrow' | 'plain'
  label?: string
}) {
  const { open, onClose, children, variant = 'rail', label } = props

  useEffect(() => {
    if (!open) return
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    const { overflow } = document.body.style
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = overflow
    }
  }, [open, onClose])

  if (!open) return null

  return createPortal(
    <div className='dawn dawn-portal'>
      <div className='mask' onClick={onClose} aria-hidden />
      <div
        className='modal'
        role='dialog'
        aria-modal='true'
        aria-label={label ?? 'dialog'}
      >
        <div
          className={`box${variant === 'narrow' ? ' narrow' : ''}${variant === 'plain' ? ' plain' : ''}`}
        >
          {children}
        </div>
      </div>
    </div>,
    document.body
  )
}

export function ModalHead(props: { title: string; onClose: () => void }) {
  return (
    <div className='m-head'>
      <h3>{props.title}</h3>
      <button className='x' onClick={props.onClose} aria-label='关闭'>
        <X size={18} />
      </button>
    </div>
  )
}
