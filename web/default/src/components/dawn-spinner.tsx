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

/**
 * 破晓加载环：铜色弧线沿轨道缓缓升起，
 * 与主页的地平线光同源。reduced-motion 时静态显示。
 */
export function DawnSpinner(props: { className?: string }) {
  return (
    <span
      className={cn(
        'dawn-spinner bg-border/50 relative block size-6 rounded-full',
        props.className
      )}
      role='status'
      aria-label='加载中'
    >
      <span className='dawn-spinner-arc absolute inset-0 rounded-full' />
    </span>
  )
}
