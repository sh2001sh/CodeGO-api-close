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

export interface SurfaceCardProps extends React.HTMLAttributes<HTMLDivElement> {
  hoverEffect?: boolean
  padding?: 'none' | 'sm' | 'md' | 'lg'
}

export function SurfaceCard({
  className,
  hoverEffect = false,
  padding = 'md',
  children,
  ...props
}: SurfaceCardProps) {
  const paddingMap = {
    none: 'p-0',
    sm: 'p-4',
    md: 'p-6',
    lg: 'p-8',
  }

  return (
    <div
      data-slot='surface-card'
      className={cn(
        'bg-card text-card-foreground rounded-2xl border border-[var(--hairline-light)]',
        'shadow-[var(--shadow-card),inset_0_1px_0_rgba(255,255,255,0.85)] dark:shadow-[var(--shadow-card),inset_0_1px_0_rgba(255,255,255,0.06)]',
        hoverEffect &&
          'transition-[box-shadow,transform,border-color] duration-300 ease-[var(--ease-spring)] hover:-translate-y-1 hover:border-[var(--hairline-strong)] hover:shadow-[var(--shadow-floating)]',
        paddingMap[padding],
        className
      )}
      {...props}
    >
      {children}
    </div>
  )
}
