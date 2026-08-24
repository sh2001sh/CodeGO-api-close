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
import { useEffect, useRef, useState } from 'react'

function easeOutQuart(t: number) {
  return 1 - Math.pow(1 - t, 4)
}

/**
 * Animates a numeric metric from 0 (or the previous value) to `value`,
 * rendering `format(current)`. Falls back to the final value instantly
 * when the user prefers reduced motion or on first paint failures.
 */
export function CountUp(props: {
  value: number
  format: (value: number) => string
  durationMs?: number
  className?: string
}) {
  const { value, format, durationMs = 900, className } = props
  const [display, setDisplay] = useState(() => format(value))
  const fromRef = useRef(0)
  const frameRef = useRef<number | null>(null)

  useEffect(() => {
    const reduce = window.matchMedia('(prefers-reduced-motion: reduce)')
    if (reduce.matches) {
      fromRef.current = value
      frameRef.current = requestAnimationFrame(() => setDisplay(format(value)))
      return () => {
        if (frameRef.current !== null) cancelAnimationFrame(frameRef.current)
      }
    }

    const from = fromRef.current
    if (from === value) return
    const start = performance.now()

    const tick = (now: number) => {
      const t = Math.min(1, (now - start) / durationMs)
      const current = from + (value - from) * easeOutQuart(t)
      setDisplay(format(current))
      if (t < 1) {
        frameRef.current = requestAnimationFrame(tick)
      } else {
        fromRef.current = value
      }
    }
    frameRef.current = requestAnimationFrame(tick)
    return () => {
      if (frameRef.current !== null) cancelAnimationFrame(frameRef.current)
      fromRef.current = value
    }
  }, [value, durationMs, format])

  return <span className={className}>{display}</span>
}
