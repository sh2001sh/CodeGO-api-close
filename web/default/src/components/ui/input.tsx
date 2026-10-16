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
import * as React from 'react'
import { Input as InputPrimitive } from '@base-ui/react/input'
import { cn } from '@/lib/utils'

function Input({ className, type, ...props }: React.ComponentProps<'input'>) {
  return (
    <InputPrimitive
      type={type}
      data-slot='input'
      className={cn(
        'file:text-foreground disabled:bg-muted/40 aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40 h-11 w-full min-w-0 rounded-xl border border-[var(--hairline-strong)] bg-[var(--surface-sunken)]/50 px-3.5 py-2 text-base shadow-xs transition-[background-color,border-color,box-shadow] duration-200 ease-[var(--ease-optical)] outline-none file:inline-flex file:h-6 file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-[var(--ink-muted)] hover:bg-white focus:bg-white focus-visible:border-[var(--brand-orange)] focus-visible:ring-4 focus-visible:ring-[var(--brand-orange)]/15 disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm dark:bg-white/[0.04] dark:hover:bg-white/[0.07] dark:focus:bg-white/[0.07]',
        className
      )}
      {...props}
    />
  )
}

export { Input }
