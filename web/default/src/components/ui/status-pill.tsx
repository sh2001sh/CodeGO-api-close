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
import { cn } from '@/lib/utils'

export type StatusType = 'success' | 'warning' | 'danger' | 'neutral' | 'brand'

export interface StatusPillProps extends React.HTMLAttributes<HTMLSpanElement> {
  status: StatusType
  dot?: boolean
}

export function StatusPill({
  status,
  dot = true,
  children,
  className,
  ...props
}: StatusPillProps) {
  const styles: Record<
    StatusType,
    { bg: string; text: string; dotColor: string }
  > = {
    success: {
      bg: 'bg-[var(--telemetry-emerald-bg)]',
      text: 'text-[var(--telemetry-emerald)]',
      dotColor: 'bg-[var(--telemetry-emerald)]',
    },
    warning: {
      bg: 'bg-[var(--telemetry-amber-bg)]',
      text: 'text-[var(--telemetry-amber)]',
      dotColor: 'bg-[var(--telemetry-amber)]',
    },
    danger: {
      bg: 'bg-[var(--telemetry-rose-bg)]',
      text: 'text-[var(--telemetry-rose)]',
      dotColor: 'bg-[var(--telemetry-rose)]',
    },
    neutral: {
      bg: 'bg-muted',
      text: 'text-muted-foreground',
      dotColor: 'bg-muted-foreground',
    },
    brand: {
      bg: 'bg-accent',
      text: 'text-accent-foreground',
      dotColor: 'bg-[var(--brand-orange)]',
    },
  }

  const current = styles[status]

  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs leading-none font-medium tabular-nums',
        current.bg,
        current.text,
        className
      )}
      {...props}
    >
      {dot && (
        <span
          className={cn(
            'h-1.5 w-1.5 flex-shrink-0 rounded-full',
            current.dotColor
          )}
          aria-hidden='true'
        />
      )}
      <span>{children}</span>
    </span>
  )
}
