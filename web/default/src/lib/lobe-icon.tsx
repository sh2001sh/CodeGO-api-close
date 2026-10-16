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
import { Component, Suspense, type ReactNode } from 'react'
import LobeIconRenderer from './lobe-icon-renderer'

class IconErrorBoundary extends Component<
  { children: ReactNode; fallback: ReactNode },
  { failed: boolean }
> {
  state = { failed: false }

  static getDerivedStateFromError() {
    return { failed: true }
  }

  render() {
    return this.state.failed ? this.props.fallback : this.props.children
  }
}

/**
 * Render any LobeHub icon, variant and chained properties on demand.
 * Examples: "OpenAI.Color", "Claude.Avatar.type={'platform'}".
 */
export function getLobeIcon(
  iconName: string | undefined | null,
  size: number = 20
): ReactNode {
  const name = typeof iconName === 'string' ? iconName.trim() : ''
  const fallback = (
    <div
      className='bg-muted text-muted-foreground flex items-center justify-center rounded-full text-xs font-medium'
      style={{ width: size, height: size }}
    >
      {name ? name.charAt(0).toUpperCase() : '?'}
    </div>
  )

  if (!name) return fallback

  return (
    <IconErrorBoundary key={name} fallback={fallback}>
      <Suspense fallback={fallback}>
        <LobeIconRenderer iconName={name} size={size} />
      </Suspense>
    </IconErrorBoundary>
  )
}
