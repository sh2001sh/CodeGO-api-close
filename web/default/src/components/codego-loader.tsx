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
 * CodeGo 文字线条加载：铜色线条沿字形描画一圈，收笔淡出，循环往复。
 * 与主页的手绘轨道线条同语言；reduced-motion 时静态展示描边字形。
 */
export function CodeGoLoader(props: { className?: string }) {
  return (
    <svg
      viewBox='0 0 118 26'
      className={cn('codego-loader', props.className)}
      role='status'
      aria-label='CodeGo 加载中'
    >
      <text
        x='1'
        y='20'
        className='codego-loader-text'
        textLength='116'
      >
        CodeGo
      </text>
    </svg>
  )
}
