/* Copyright (C) 2023-2026 QuantumNous. AGPL-3.0-or-later. */
import { useEffect, useRef } from 'react'

/** A projected torus: one continuous surface, responding to the pointer. */
export function BrandOrbit(props: { paused: boolean }) {
  const canvasRef = useRef<HTMLCanvasElement>(null)

  useEffect(() => {
    const canvas = canvasRef.current
    const host = canvas?.parentElement
    if (!canvas || !host) return
    const context = canvas.getContext('2d')
    if (!context) return
    const motion = window.matchMedia('(prefers-reduced-motion: reduce)')
    let frame = 0
    let width = 0
    let height = 0
    let rotation = 0.35
    let pointerX = 0
    let pointerY = 0
    let visible = true
    let lastTime = 0

    const draw = (time: number) => {
      const animated =
        !props.paused && !motion.matches && visible && !document.hidden
      if (time - lastTime >= 32 || !animated) {
        if (animated) rotation += 0.003
        lastTime = time
        context.clearRect(0, 0, width, height)
        const scale = Math.min(width, height) / 520
        const angle = rotation + pointerX * 0.35
        const tilt = 0.65 + pointerY * 0.3
        const copper =
          getComputedStyle(host).getPropertyValue('--brand-orange').trim() ||
          '#d16436'
        context.strokeStyle = copper
        context.lineWidth = 0.7

        const project = (u: number, v: number): [number, number, number] => {
          const radius = 152 + 48 * Math.cos(v)
          const x = radius * Math.cos(u)
          const y = radius * Math.sin(u)
          const z = 48 * Math.sin(v)
          const turnedX = x * Math.cos(angle) + z * Math.sin(angle)
          const turnedZ = -x * Math.sin(angle) + z * Math.cos(angle)
          const turnedY = y * Math.cos(tilt) - turnedZ * Math.sin(tilt)
          const depth = y * Math.sin(tilt) + turnedZ * Math.cos(tilt)
          const perspective = 650 / (650 - depth)
          return [
            width / 2 + turnedX * scale * perspective,
            height / 2 + turnedY * scale * perspective,
            depth,
          ]
        }

        for (let ring = 0; ring < 64; ring++) {
          const u = (ring / 64) * Math.PI * 2
          context.beginPath()
          for (let point = 0; point <= 80; point++) {
            const [x, y] = project(u, (point / 80) * Math.PI * 2)
            if (point === 0) context.moveTo(x, y)
            else context.lineTo(x, y)
          }
          context.globalAlpha = 0.6
          context.stroke()
        }
        for (let ring = 0; ring < 22; ring++) {
          const v = (ring / 22) * Math.PI * 2
          context.beginPath()
          for (let point = 0; point <= 160; point++) {
            const [x, y] = project((point / 160) * Math.PI * 2, v)
            if (point === 0) context.moveTo(x, y)
            else context.lineTo(x, y)
          }
          context.globalAlpha = 0.28
          context.stroke()
        }
        context.globalAlpha = 1
      }
      if (animated) frame = requestAnimationFrame(draw)
    }
    const restart = () => {
      cancelAnimationFrame(frame)
      lastTime = 0
      frame = requestAnimationFrame(draw)
    }
    const resize = new ResizeObserver(([entry]) => {
      width = entry.contentRect.width
      height = entry.contentRect.height
      const density = Math.min(window.devicePixelRatio || 1, 2)
      canvas.width = Math.round(width * density)
      canvas.height = Math.round(height * density)
      context.setTransform(density, 0, 0, density, 0, 0)
      restart()
    })
    const intersection = new IntersectionObserver(([entry]) => {
      visible = entry.isIntersecting
      restart()
    })
    const move = (event: PointerEvent) => {
      const rect = host.getBoundingClientRect()
      pointerX = (event.clientX - rect.left) / rect.width - 0.5
      pointerY = (event.clientY - rect.top) / rect.height - 0.5
      if (props.paused || motion.matches) restart()
    }
    const leave = () => {
      pointerX = 0
      pointerY = 0
      if (props.paused || motion.matches) restart()
    }
    resize.observe(host)
    intersection.observe(host)
    host.addEventListener('pointermove', move)
    host.addEventListener('pointerleave', leave)
    document.addEventListener('visibilitychange', restart)
    motion.addEventListener('change', restart)
    return () => {
      cancelAnimationFrame(frame)
      resize.disconnect()
      intersection.disconnect()
      host.removeEventListener('pointermove', move)
      host.removeEventListener('pointerleave', leave)
      document.removeEventListener('visibilitychange', restart)
      motion.removeEventListener('change', restart)
    }
  }, [props.paused])

  return (
    <canvas ref={canvasRef} className='brand-orbit-canvas' aria-hidden='true' />
  )
}
