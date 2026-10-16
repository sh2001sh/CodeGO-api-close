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

export interface NumericStatProps {
  title: string
  value: string | number
  unit?: string
  trend?: {
    value: string
    isUp: boolean
  }
  subText?: string
  className?: string
}

export function NumericStat({
  title,
  value,
  unit,
  trend,
  subText,
  className,
}: NumericStatProps) {
  return (
    <div className={cn('flex flex-col', className)}>
      <span className='mb-1.5 text-[12px] font-medium text-[#78716C]'>
        {title}
      </span>
      <div className='flex items-baseline gap-1.5'>
        <span className='font-mono text-3xl font-semibold tracking-tight text-[#1D1917]'>
          {value}
        </span>
        {unit && (
          <span className='text-[13px] font-medium text-[#A8A29E]'>{unit}</span>
        )}
      </div>
      {(trend || subText) && (
        <div className='mt-2 flex items-center gap-2'>
          {trend && (
            <span
              className={cn(
                'inline-flex items-center rounded-full px-2 py-0.5 font-mono text-[11px] font-semibold',
                trend.isUp
                  ? 'bg-[#EDF7F0] text-[#16A34A]'
                  : 'bg-[#FEECEC] text-[#DC2626]'
              )}
            >
              {trend.isUp ? '↑' : '↓'} {trend.value}
            </span>
          )}
          {subText && (
            <span className='text-[11px] text-[#A8A29E]'>{subText}</span>
          )}
        </div>
      )}
    </div>
  )
}
