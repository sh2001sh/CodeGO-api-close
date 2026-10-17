import { useEffect, useMemo, useRef, useState } from 'react'
import { ChevronLeft, ChevronRight, Pause, Play } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Input } from '@/components/ui/input'
import { createRouteScene } from './lib/route-renderer'

function getModelIcon(id: string): string {
  const lower = id.toLowerCase()
  if (lower.includes('deepseek')) return '/deepseek.svg'
  if (
    lower.includes('gpt') ||
    lower.includes('openai') ||
    lower.includes('o1') ||
    lower.includes('o3')
  )
    return '/openai.svg'
  if (
    lower.includes('claude') ||
    lower.includes('sonnet') ||
    lower.includes('anthropic')
  )
    return '/claude.svg'
  return '/logo.svg'
}

type RouteFieldProps = {
  models: { id: string; name: string; sample?: boolean }[]
  selectedId: string
  onSelect: (id: string) => void
}

const MODEL_PAGE_SIZE = 50

export function RouteField(props: RouteFieldProps) {
  const { t } = useTranslation()
  const [paused, setPaused] = useState(false)
  const [modelSearch, setModelSearch] = useState('')
  const [modelPage, setModelPage] = useState(1)
  const filteredModels = useMemo(() => {
    const query = modelSearch.trim().toLocaleLowerCase()
    return props.models.filter((model) =>
      `${model.id} ${model.name}`.toLocaleLowerCase().includes(query)
    )
  }, [props.models, modelSearch])
  const modelPageCount = Math.max(
    1,
    Math.ceil(filteredModels.length / MODEL_PAGE_SIZE)
  )
  const currentModelPage = Math.min(modelPage, modelPageCount)
  if (modelPage !== currentModelPage) setModelPage(currentModelPage)
  const pageModels = filteredModels.slice(
    (currentModelPage - 1) * MODEL_PAGE_SIZE,
    currentModelPage * MODEL_PAGE_SIZE
  )
  const selectedModel = props.models.find(
    (model) => model.id === props.selectedId
  )
  const [reducedMotion, setReducedMotion] = useState(
    () => matchMedia('(prefers-reduced-motion: reduce)').matches
  )
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const hostRef = useRef<HTMLDivElement>(null)
  const controlsRef = useRef<{
    select: (index: number) => void
    pause: (value: boolean) => void
  } | null>(null)
  const selectedIndex = Math.max(
    0,
    props.models.findIndex((model) => model.id === props.selectedId)
  )

  useEffect(() => {
    const canvas = canvasRef.current
    const host = hostRef.current
    if (!canvas || !host || !canvas.getContext('2d')) return
    const scene = createRouteScene(canvas, host)
    const reduced = matchMedia('(prefers-reduced-motion: reduce)')
    const fine = matchMedia('(pointer: fine)')
    let frame = 0
    let visible = false
    let hasSize = false
    let stopped = false
    let selected = 0
    let lastTime = 0
    let elapsed = 1.2
    let pointerX = 0
    let pointerY = 0
    let targetX = 0
    let targetY = 0

    function draw(time: number): void {
      const active = visible && !document.hidden && !stopped && !reduced.matches
      const dt = Math.min((time - (lastTime || time)) / 1000, 0.04)
      lastTime = time
      if (active) {
        elapsed += dt
        const follow = 1 - Math.exp(-dt * 7)
        pointerX += (targetX - pointerX) * follow
        pointerY += (targetY - pointerY) * follow
      }
      if (hasSize) {
        scene.render(elapsed, selected, pointerX, pointerY)
        canvas!.dataset.ready = 'true'
      }
      if (active && hasSize) frame = requestAnimationFrame(draw)
    }

    function restart(): void {
      cancelAnimationFrame(frame)
      lastTime = 0
      draw(performance.now())
    }

    controlsRef.current = {
      select(index) {
        selected = index
        if (!visible || document.hidden || stopped || reduced.matches) restart()
      },
      pause(value) {
        stopped = value
        restart()
      },
    }
    const resize = new ResizeObserver(([entry]) => {
      const { width, height } = entry.contentRect
      hasSize = width > 0 && height > 0
      if (hasSize) {
        scene.resize(width, height, Math.min(devicePixelRatio || 1, 2))
      }
      restart()
    })
    const intersection = new IntersectionObserver(([entry]) => {
      visible = entry.isIntersecting
      restart()
    })
    const theme = new MutationObserver(() => {
      scene.refreshColors()
      restart()
    })
    function move(event: PointerEvent): void {
      if (
        !fine.matches ||
        reduced.matches ||
        stopped ||
        event.pointerType === 'touch'
      )
        return
      const rect = host!.getBoundingClientRect()
      if (!rect.width || !rect.height) return
      targetX = Math.max(
        -1,
        Math.min(1, ((event.clientX - rect.left) / rect.width - 0.5) * 2)
      )
      targetY = Math.max(
        -1,
        Math.min(1, ((event.clientY - rect.top) / rect.height - 0.5) * 2)
      )
    }
    function leave(): void {
      targetX = 0
      targetY = 0
    }
    function preference(): void {
      setReducedMotion(reduced.matches)
      if (reduced.matches || !fine.matches) {
        pointerX = 0
        pointerY = 0
        leave()
      }
      restart()
    }
    resize.observe(host)
    intersection.observe(host)
    theme.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['class'],
    })
    host.addEventListener('pointermove', move)
    host.addEventListener('pointerleave', leave)
    document.addEventListener('visibilitychange', restart)
    reduced.addEventListener('change', preference)
    fine.addEventListener('change', preference)
    return () => {
      cancelAnimationFrame(frame)
      controlsRef.current = null
      delete canvas.dataset.ready
      resize.disconnect()
      intersection.disconnect()
      theme.disconnect()
      scene.dispose()
      host.removeEventListener('pointermove', move)
      host.removeEventListener('pointerleave', leave)
      document.removeEventListener('visibilitychange', restart)
      reduced.removeEventListener('change', preference)
      fine.removeEventListener('change', preference)
    }
  }, [])

  useEffect(() => {
    controlsRef.current?.select(selectedIndex)
  }, [selectedIndex])

  useEffect(() => {
    controlsRef.current?.pause(paused)
  }, [paused])

  return (
    <div className='mh-routing' id='routing'>
      <div ref={hostRef} className='mh-route-canvas'>
        <svg
          className='mh-route-fallback'
          viewBox='0 0 1440 660'
          fill='none'
          aria-hidden='true'
        >
          {Array.from({ length: 36 }, (_, index) => (
            <path
              key={index}
              d={`M-80 ${460 + index * 5} C450 ${710 - index * 12} 650 ${40 + index * 8} 1015 300 S1300 ${140 + index * 10} 1510 ${260 + index * 5}`}
              stroke='currentColor'
              strokeWidth='1.2'
              opacity={0.2 + (index / 36) * 0.45}
            />
          ))}
          <path
            d='M893 185 1076 152 1120 430 937 460Z M961 278 1046 265 1061 358 976 371Z'
            fill='#202724'
            fillRule='evenodd'
          />
        </svg>
        <canvas ref={canvasRef} aria-hidden='true' />
      </div>
      <div className='mh-route-input' aria-hidden='true'>
        <span className='mh-optical-mark' />
        <span>{t('应用')}</span>
      </div>
      <div className='mh-route-output' aria-hidden='true'>
        <span className='mh-optical-mark' />
        <span>
          CodeGo<small>{t('路由')}</small>
        </span>
      </div>
      <div className='mh-routing-dock'>
        <div className='mh-route-head'>
          <h2>
            <span className='mh-dock-light' aria-hidden='true' />
            {t('路由')}
          </h2>
          <div className='mh-head-actions'>
            <button
              className='mh-motion-button'
              type='button'
              disabled={reducedMotion}
              aria-pressed={paused || reducedMotion}
              onClick={() => setPaused(!paused)}
              title={paused ? t('播放动效') : t('暂停动效')}
            >
              {paused || reducedMotion ? (
                <Play aria-hidden='true' />
              ) : (
                <Pause aria-hidden='true' />
              )}
            </button>
          </div>
        </div>

        {/* Dynamic Optical Fiber Path */}
        <div className='mh-optical-path' aria-label={t('路由')}>
          <div className='mh-path-node'>
            <span className='mh-node-badge'>APP</span>
            <span className='mh-node-desc'>{t('客户端')}</span>
          </div>
          <div className='mh-fiber' aria-hidden='true'>
            <i />
          </div>
          <div className='mh-path-node mh-path-core'>
            <img src='/codego-mark.svg' width='16' height='16' alt='' />
            <span>CodeGo</span>
          </div>
          <div className='mh-fiber mh-fiber-lag' aria-hidden='true'>
            <i />
          </div>
          <div className='mh-path-node mh-path-dest'>
            <span className='mh-dest-dot' />
            <span className='mh-dest-name'>
              {selectedModel?.name ?? 'Target'}
            </span>
          </div>
        </div>

        {/* Model Presets Selector Grid */}
        <div
          className='mh-model-presets-grid'
          role='group'
          aria-label={t('目标模型')}
        >
          {props.models.slice(0, 3).map((model) => {
            const isSelected = model.id === props.selectedId
            return (
              <button
                key={model.id}
                type='button'
                className={cn('mh-preset-card', isSelected && 'is-selected')}
                aria-pressed={isSelected}
                onClick={() => props.onSelect(model.id)}
              >
                <div className='mh-preset-icon'>
                  <img src={getModelIcon(model.id)} alt='' />
                </div>
                <div className='mh-preset-info'>
                  <span className='mh-preset-name'>{model.name}</span>
                </div>
              </button>
            )
          })}
        </div>

        {props.models.length > 3 && (
          <div className='mt-3 grid min-w-0 gap-2'>
            <label htmlFor='home-route-search' className='sr-only'>
              {t('Search models')}
            </label>
            <Input
              id='home-route-search'
              type='search'
              value={modelSearch}
              placeholder={t('Search models')}
              onChange={(event) => {
                setModelSearch(event.target.value)
                setModelPage(1)
              }}
            />
            {filteredModels.length > 0 ? (
              <div className='mh-route-more-select'>
                <label htmlFor='home-route-model'>{t('全部模型')}:</label>
                <select
                  id='home-route-model'
                  className='min-w-0'
                  value={props.selectedId}
                  onChange={(event) => props.onSelect(event.target.value)}
                >
                  {selectedModel &&
                    !pageModels.some(
                      (model) => model.id === selectedModel.id
                    ) && (
                      <option value={selectedModel.id}>
                        {selectedModel.name}
                        {selectedModel.sample ? ` · ${t('示例')}` : ''}
                      </option>
                    )}
                  {pageModels.map((model) => (
                    <option key={model.id} value={model.id}>
                      {model.name}
                      {model.sample ? ` · ${t('示例')}` : ''}
                    </option>
                  ))}
                </select>
              </div>
            ) : (
              <span role='status'>{t('无匹配模型')}</span>
            )}
            {modelPageCount > 1 && (
              <nav
                className='mh-route-pagination flex flex-wrap items-center justify-between gap-2 text-xs'
                aria-label={t('pagination')}
              >
                <button
                  type='button'
                  className='inline-flex size-11 items-center justify-center rounded-lg'
                  aria-label={t('上一页')}
                  disabled={currentModelPage === 1}
                  onClick={() => setModelPage(currentModelPage - 1)}
                >
                  <ChevronLeft size={16} aria-hidden='true' />
                </button>
                <label className='flex items-center gap-2'>
                  <span className='sr-only'>{t('Page')}</span>
                  <input
                    type='number'
                    className='h-11 w-16 rounded-lg border border-[var(--hairline-light)] bg-[var(--surface-sunken)] px-2 text-center tabular-nums'
                    min={1}
                    max={modelPageCount}
                    value={currentModelPage}
                    aria-label={t('Page')}
                    onChange={(event) => {
                      const value = Number(event.target.value)
                      if (!Number.isFinite(value)) return
                      setModelPage(
                        Math.max(1, Math.min(modelPageCount, Math.trunc(value)))
                      )
                    }}
                  />
                  <span>/ {modelPageCount.toLocaleString()}</span>
                </label>
                <button
                  type='button'
                  className='inline-flex size-11 items-center justify-center rounded-lg'
                  aria-label={t('下一页')}
                  disabled={currentModelPage === modelPageCount}
                  onClick={() => setModelPage(currentModelPage + 1)}
                >
                  <ChevronRight size={16} aria-hidden='true' />
                </button>
              </nav>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
