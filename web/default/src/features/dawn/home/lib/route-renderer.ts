/* Copyright (C) 2023-2026 QuantumNous. AGPL-3.0-or-later. */

export type RouteScene = {
  render: (
    time: number,
    selected: number,
    pointerX: number,
    pointerY: number
  ) => void
  resize: (width: number, height: number, dpr: number) => void
  refreshColors: () => void
  dispose: () => void
}

type Point = [number, number]
type OpticalProfile = {
  frequency: number
  speed: number
  amplitude: number
  brightness: number
}
type Core = { x: number; y: number; size: number; mobile: boolean }

// These values describe the artwork, never model latency or throughput.
const PROFILES: OpticalProfile[] = [
  { frequency: 1.05, speed: 0.75, amplitude: 1, brightness: 0.78 },
  { frequency: 1.55, speed: 1.35, amplitude: 0.74, brightness: 1 },
  { frequency: 0.75, speed: 0.55, amplitude: 1.3, brightness: 0.62 },
]
const STRANDS = 36

/** Optical ribbon and extruded aperture. RouteField owns its clock and lifecycle. */
export function createRouteScene(
  canvas: HTMLCanvasElement,
  host: HTMLElement
): RouteScene {
  const context = canvas.getContext('2d', { alpha: true })
  if (!context) throw new Error('Route sculpture requires a 2D canvas context')
  const ctx = context
  let width = 1
  let height = 1
  let disposed = false
  let dark = false
  let lastTime = -1
  let lastSelection = -1
  const field = { ...PROFILES[0] }
  let waveGradient: CanvasGradient
  let paleGradient: CanvasGradient
  const positions = Array.from(
    { length: STRANDS * 3 },
    () => new Float32Array(282)
  )

  function refreshColors(): void {
    if (disposed) return
    dark = getComputedStyle(host).colorScheme === 'dark'
    waveGradient = ctx.createLinearGradient(0, 0, width, 0)
    waveGradient.addColorStop(0, 'rgba(255,107,28,.08)')
    waveGradient.addColorStop(0.25, 'rgba(255,113,24,.42)')
    waveGradient.addColorStop(0.56, 'rgba(255,85,0,.82)')
    waveGradient.addColorStop(0.78, 'rgba(255,133,42,.72)')
    waveGradient.addColorStop(1, 'rgba(255,161,75,.12)')
    paleGradient = ctx.createLinearGradient(0, 0, width, 0)
    paleGradient.addColorStop(0, 'rgba(146,153,158,.06)')
    paleGradient.addColorStop(
      0.5,
      dark ? 'rgba(199,195,184,.4)' : 'rgba(142,145,139,.28)'
    )
    paleGradient.addColorStop(1, 'rgba(167,158,141,.04)')
  }

  function resize(nextWidth: number, nextHeight: number, dpr: number): void {
    if (disposed) return
    width = Math.max(1, Number.isFinite(nextWidth) ? nextWidth : 1)
    height = Math.max(1, Number.isFinite(nextHeight) ? nextHeight : 1)
    const ratio = Math.max(1, Math.min(Number.isFinite(dpr) ? dpr : 1, 2))
    canvas.width = Math.round(width * ratio)
    canvas.height = Math.round(height * ratio)
    ctx.setTransform(ratio, 0, 0, ratio, 0, 0)
    refreshColors()
  }

  function polygon(
    points: Point[],
    fill: string | CanvasGradient | null,
    stroke?: string,
    lineWidth = 1
  ): void {
    ctx.beginPath()
    ctx.moveTo(points[0][0], points[0][1])
    for (let i = 1; i < points.length; i++)
      ctx.lineTo(points[i][0], points[i][1])
    ctx.closePath()
    if (fill) {
      ctx.fillStyle = fill
      ctx.fill()
    }
    if (stroke) {
      ctx.strokeStyle = stroke
      ctx.lineWidth = lineWidth
      ctx.stroke()
    }
  }

  function drawWaves(core: Core, time: number, pointerY: number): void {
    const steps = core.mobile ? 90 : 140
    const scale = core.mobile ? 0.65 : Math.min(width / 1440, 1.2)
    for (let group = 0; group < 3; group++) {
      const pale = group === 2
      ctx.strokeStyle = pale ? paleGradient : waveGradient
      ctx.lineWidth = pale ? 0.6 : 0.75
      ctx.globalAlpha = pale ? 1 : field.brightness
      for (let strand = 0; strand < STRANDS; strand++) {
        const points = positions[group * STRANDS + strand]
        const spread = (strand / (STRANDS - 1) - 0.5) * 2
        ctx.beginPath()
        for (let sample = 0; sample <= steps; sample++) {
          const t = sample / steps
          const x = -width * 0.1 + t * width * 1.2
          const normalized = (x - core.x) / width
          const distance = Math.abs(normalized)
          const neck = 1 - Math.exp(-distance * 8)
          const envelope =
            neck * (74 + distance * 155) * scale * field.amplitude
          const phase =
            normalized * (x < core.x ? 5.8 : 5) * field.frequency + time * 0.17
          let y = core.y + Math.sin(phase + group * 1.7) * envelope
          // Keep the entire left copy area clear. The input enters below it.
          if (x < core.x && !core.mobile)
            y += Math.pow(Math.min(1, distance * 2.6), 0.7) * 180
          const band =
            Math.cos(phase * 0.9 + group * 2.1 + strand * 0.012) *
            envelope *
            0.65
          y +=
            spread * band +
            Math.sin(normalized * 12 + strand * 0.05 + time * 0.23) * neck * 5
          y += pointerY * Math.sin(t * Math.PI) * 10 * neck
          points[sample * 2] = x
          points[sample * 2 + 1] = y
          if (sample === 0) ctx.moveTo(x, y)
          else ctx.lineTo(x, y)
        }
        ctx.stroke()
        if (!pale && strand % 7 === 0) {
          const progress =
            (time * 0.045 * field.speed + strand * 0.047 + group * 0.31) % 1
          const end = Math.min(steps, Math.floor(progress * steps))
          const start = Math.max(0, end - 2)
          ctx.beginPath()
          ctx.moveTo(points[start * 2], points[start * 2 + 1])
          for (let i = start + 1; i <= end; i++)
            ctx.lineTo(points[i * 2], points[i * 2 + 1])
          ctx.strokeStyle = '#ffb26b'
          ctx.lineWidth = 1.5
          ctx.stroke()
          ctx.strokeStyle = waveGradient
          ctx.lineWidth = 0.75
        }
      }
    }
    ctx.globalAlpha = 1
  }

  function drawCore(core: Core, pointerX: number): void {
    const size = core.size
    ctx.save()
    ctx.translate(core.x, core.y)
    ctx.rotate(-0.17 + pointerX * 0.025)
    ctx.save()
    ctx.translate(3, size * 1.38)
    ctx.scale(1, 0.12)
    const shadow = ctx.createRadialGradient(0, 0, 0, 0, 0, size * 1.1)
    shadow.addColorStop(0, dark ? 'rgba(0,0,0,.5)' : 'rgba(83,61,39,.18)')
    shadow.addColorStop(1, 'rgba(83,61,39,0)')
    ctx.fillStyle = shadow
    ctx.fillRect(-size * 1.2, -size * 1.2, size * 2.4, size * 2.4)
    ctx.restore()
    const glow = ctx.createRadialGradient(0, 0, size * 0.1, 0, 0, size * 1.6)
    glow.addColorStop(0, `rgba(255,125,24,${0.24 * field.brightness})`)
    glow.addColorStop(0.4, 'rgba(255,121,24,.065)')
    glow.addColorStop(1, 'rgba(255,131,30,0)')
    ctx.fillStyle = glow
    ctx.fillRect(-size * 1.6, -size * 1.6, size * 3.2, size * 3.2)
    ctx.scale(size / 150, size / 150)
    const a: Point = [-111, -113]
    const b: Point = [66, -133]
    const c: Point = [88, 116]
    const d: Point = [-90, 135]
    const back = (p: Point): Point => [p[0] + 36, p[1] - 22]
    const top = ctx.createLinearGradient(-100, -160, 90, -95)
    top.addColorStop(0, '#a9aba7')
    top.addColorStop(0.12, '#6b706f')
    top.addColorStop(0.46, '#414746')
    top.addColorStop(0.8, '#71746f')
    top.addColorStop(1, '#373d3b')
    polygon([a, back(a), back(b), b], top, '#b6b3a280', 0.7)
    const side = ctx.createLinearGradient(60, 0, 130, 0)
    side.addColorStop(0, '#414644')
    side.addColorStop(0.7, '#232b29')
    side.addColorStop(1, '#171f1e')
    polygon([b, back(b), back(c), c], side, '#737a6d70', 0.6)
    const surface = ctx.createLinearGradient(-90, -125, 90, 140)
    surface.addColorStop(0, '#404745')
    surface.addColorStop(0.2, '#2b3331')
    surface.addColorStop(0.5, '#151d1c')
    surface.addColorStop(0.8, '#18201e')
    surface.addColorStop(1, '#323732')
    const hole: Point[] = [
      [-58, -36],
      [22, -45],
      [30, 47],
      [-50, 56],
    ]
    ctx.beginPath()
    for (const ring of [[a, b, c, d], hole]) {
      ctx.moveTo(ring[0][0], ring[0][1])
      for (let i = 1; i < ring.length; i++) ctx.lineTo(ring[i][0], ring[i][1])
      ctx.closePath()
    }
    ctx.fillStyle = surface
    ctx.fill('evenodd')
    polygon([a, b, c, d], null, 'rgba(208,208,183,.5)', 0.75)
    const bevel = ctx.createLinearGradient(-55, 0, 33, 0)
    bevel.addColorStop(0, '#815b3e')
    bevel.addColorStop(0.2, '#b77e4b')
    bevel.addColorStop(0.55, '#563b26')
    bevel.addColorStop(1, '#1d2824')
    polygon(
      [hole[0], hole[1], [33, -52], [-47, -43]],
      '#ca986a',
      '#ffc08790',
      0.5
    )
    polygon([hole[0], hole[3], [-38, 48], [-46, -43]], bevel, '#e6b17d80', 0.5)
    polygon(
      [hole[3], hole[2], [41, 39], [-38, 48]],
      '#ad673b',
      '#eab07860',
      0.5
    )
    polygon(hole, null, '#ffbf7b', 0.9)
    ctx.save()
    polygon(hole, null)
    ctx.clip()
    const inner = ctx.createLinearGradient(-55, -30, 35, 55)
    inner.addColorStop(0, 'rgba(255,177,99,.2)')
    inner.addColorStop(0.35, 'rgba(255,234,204,.6)')
    inner.addColorStop(0.52, 'rgba(255,147,34,.9)')
    inner.addColorStop(0.61, 'rgba(255,243,210,.95)')
    inner.addColorStop(1, 'rgba(255,133,27,.1)')
    ctx.fillStyle = inner
    ctx.fillRect(-65, -55, 110, 115)
    ctx.globalCompositeOperation = 'screen'
    for (let i = 0; i < 13; i++) {
      ctx.beginPath()
      ctx.moveTo(-80, 34 + i * 1.4)
      ctx.bezierCurveTo(-30, 20 + i * 0.5, 0, 1 + i * 0.6, 55, -13 + i * 1.8)
      ctx.strokeStyle = i % 4 === 0 ? '#fff9db' : 'rgba(255,196,96,.65)'
      ctx.lineWidth = 0.8
      ctx.stroke()
    }
    ctx.restore()
    ctx.beginPath()
    ctx.moveTo(-95, -96)
    ctx.lineTo(51, -112)
    ctx.strokeStyle = 'rgba(204,211,198,.25)'
    ctx.lineWidth = 0.7
    ctx.stroke()
    for (let i = 0; i < 3; i++) {
      ctx.fillStyle = i === Math.abs(lastSelection) % 3 ? '#ff9d54' : '#646f5e'
      ctx.fillRect(-59 + i * 18, 100, 8, 1.5)
    }
    ctx.restore()
  }

  function render(
    timeSeconds: number,
    selectedIndex: number,
    pointerX: number,
    pointerY: number
  ): void {
    if (disposed) return
    const time = Number.isFinite(timeSeconds) ? Math.max(0, timeSeconds) : 0
    const selected = Number.isFinite(selectedIndex)
      ? Math.abs(Math.floor(selectedIndex))
      : 0
    const base = PROFILES[selected % PROFILES.length]
    const variation = Math.floor(selected / PROFILES.length)
    const target = {
      frequency: base.frequency + Math.sin(variation * 1.7) * 0.16,
      speed: base.speed + Math.sin(variation * 2.1) * 0.12,
      amplitude: base.amplitude + Math.sin(variation * 0.8) * 0.12,
      brightness: base.brightness + Math.sin(variation * 1.3) * 0.08,
    }
    const still = time <= lastTime
    const delta =
      lastTime < 0 ? 1 : Math.max(0, Math.min(0.05, time - lastTime))
    const blend =
      lastTime < 0 || (still && lastSelection !== selected)
        ? 1
        : 1 - Math.exp(-delta * 3.2)
    for (const key of [
      'frequency',
      'speed',
      'amplitude',
      'brightness',
    ] as const)
      field[key] += (target[key] - field[key]) * blend
    lastTime = time
    lastSelection = selected
    const x = Math.max(-1, Math.min(1, pointerX || 0))
    const y = Math.max(-1, Math.min(1, pointerY || 0))
    const mobile = width <= 760
    const core: Core = {
      x: width * (mobile ? 0.55 : 0.705) + x * 7,
      y: height * (mobile ? 0.58 : 0.45) + y * 5,
      size: mobile
        ? Math.min(width * 0.24, 120)
        : Math.min(width * 0.117, 175, height * 0.25),
      mobile,
    }
    ctx.clearRect(0, 0, width, height)
    drawWaves(core, time, y)
    drawCore(core, x)
  }

  refreshColors()
  return {
    render,
    resize,
    refreshColors,
    dispose(): void {
      disposed = true
      ctx.clearRect(0, 0, width, height)
    },
  }
}
