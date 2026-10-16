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

For commercial licensing, please contact support@quantumnous.com
*/
import { cn } from '@/lib/utils'

export interface IOSSwitchProps {
  checked: boolean
  onChange: (checked: boolean) => void
  disabled?: boolean
  label?: string
  className?: string
}

export function IOSSwitch({
  checked,
  onChange,
  disabled = false,
  label,
  className,
}: IOSSwitchProps) {
  return (
    <label
      className={cn(
        'inline-flex cursor-pointer items-center gap-2.5 select-none',
        disabled && 'cursor-not-allowed opacity-50',
        className
      )}
    >
      <div
        className={cn(
          'relative inline-flex h-6 w-11 flex-shrink-0 rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out',
          checked ? 'bg-[#E05D26]' : 'bg-[#E8E2D9]'
        )}
        onClick={(e) => {
          e.preventDefault()
          if (!disabled) onChange(!checked)
        }}
      >
        <span
          className={cn(
            'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow-sm ring-0 transition duration-200 ease-in-out',
            checked ? 'translate-x-5' : 'translate-x-0'
          )}
        />
      </div>
      {label && (
        <span className='text-[13px] font-medium text-[#1D1917]'>{label}</span>
      )}
    </label>
  )
}
