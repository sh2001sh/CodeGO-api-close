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

For commercial licensing, please contact support@quantumnous.com.
*/
import { useEffect, useRef, useState, type ReactNode } from 'react'

/** 是否偏好减少动效（SSR 安全）。 */
function prefersReducedMotion(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches
  )
}

/** 进入视口时上浮显现。 */
export function Reveal(props: { children: ReactNode; className?: string }) {
  const ref = useRef<HTMLDivElement>(null)
  const [shown, setShown] = useState(prefersReducedMotion)

  useEffect(() => {
    const element = ref.current
    if (!element || prefersReducedMotion()) return
    const observer = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (entry.isIntersecting) {
            setShown(true)
            observer.disconnect()
          }
        })
      },
      { threshold: 0.18 }
    )
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  return (
    <div ref={ref} className={`reveal${shown ? ' in' : ''}${props.className ? ` ${props.className}` : ''}`}>
      {props.children}
    </div>
  )
}

/** 进入视口时数字滚动。 */
export function CountUp(props: {
  to: number
  div?: number
  digits?: number
  durationMs?: number
}) {
  const ref = useRef<HTMLSpanElement>(null)
  const [value, setValue] = useState(0)
  const startedRef = useRef(false)

  useEffect(() => {
    const element = ref.current
    if (!element) return
    const reduce =
      typeof window.matchMedia === 'function' &&
      window.matchMedia('(prefers-reduced-motion: reduce)').matches
    const observer = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (!entry.isIntersecting || startedRef.current) return
          startedRef.current = true
          observer.disconnect()
          if (reduce) {
            setValue(props.to)
            return
          }
          const start = performance.now()
          const duration = props.durationMs ?? 1400
          const tick = (now: number) => {
            const progress = Math.min((now - start) / duration, 1)
            const eased = 1 - Math.pow(1 - progress, 3)
            setValue(props.to * eased)
            if (progress < 1) requestAnimationFrame(tick)
          }
          requestAnimationFrame(tick)
        })
      },
      { threshold: 0.3 }
    )
    observer.observe(element)
    return () => observer.disconnect()
  }, [props.to, props.durationMs])

  const div = props.div ?? 1
  const digits = props.digits ?? (div > 1 ? 1 : 0)
  const text =
    div > 1
      ? (value / div).toFixed(digits)
      : Math.round(value).toLocaleString()

  return <span ref={ref}>{text}</span>
}
