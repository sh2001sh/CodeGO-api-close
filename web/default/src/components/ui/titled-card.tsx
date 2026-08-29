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
import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from './card'

type TitledCardProps = {
  title: ReactNode
  description?: ReactNode
  icon?: ReactNode
  action?: ReactNode
  children?: ReactNode
  className?: string
  headerClassName?: string
  contentClassName?: string
  iconClassName?: string
  titleClassName?: string
  descriptionClassName?: string
}

export function TitledCard({
  title,
  description,
  icon,
  action,
  children,
  className,
  headerClassName,
  contentClassName,
  iconClassName,
  titleClassName,
  descriptionClassName,
}: TitledCardProps) {
  // Kept in the prop contract for call-site compatibility; visuals are fixed.
  void icon
  void iconClassName
  void descriptionClassName
  return (
    <Card className={cn('gap-0 overflow-hidden py-0', className)}>
      <CardHeader
        className={cn('border-b p-3 !pb-3 sm:p-5 sm:!pb-5', headerClassName)}
      >
        <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
          <div className='flex min-w-0 items-center gap-2.5'>
            <span
              aria-hidden
              className='bg-primary block h-3 w-[3px] shrink-0'
            />
            <div className='min-w-0'>
              <CardTitle
                className={cn(
                  'text-[13px] sm:text-sm sm:font-semibold',
                  titleClassName
                )}
              >
                {title}
              </CardTitle>
              {description != null && (
                <CardDescription className='sr-only'>
                  {description}
                </CardDescription>
              )}
            </div>
          </div>
          {action != null && (
            <div className='w-full shrink-0 sm:w-auto'>{action}</div>
          )}
        </div>
      </CardHeader>
      <CardContent className={cn('p-3 sm:p-5', contentClassName)}>
        {children}
      </CardContent>
    </Card>
  )
}
