import {
  useEffect,
  useRef,
  useState,
  type PointerEvent,
  type ReactNode,
} from 'react'
import { Link } from '@tanstack/react-router'
import {
  ArrowUpRight,
  Download,
  LayoutDashboard,
  Shapes,
} from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { Button } from '@/components/ui/button'

type DawnHeroProps = { modelRail: ReactNode }

export function DawnHero({ modelRail }: DawnHeroProps) {
  const rootRef = useRef<HTMLElement>(null)
  const [phase, setPhase] = useState(0)
  const [pulse, setPulse] = useState(0)
  const reduceMotion = Boolean(useReducedMotion())

  useEffect(() => {
    let frame = 0
    const update = () => {
      frame = 0
      const progress = Math.max(
        0,
        window.scrollY / Math.max(window.innerHeight, 1)
      )
      setPhase(progress < 0.72 ? 0 : progress < 1.72 ? 1 : 2)
      rootRef.current?.style.setProperty(
        '--hero-scroll',
        `${Math.min(progress * 90, 180)}px`
      )
    }
    const onScroll = () => {
      if (!frame) frame = requestAnimationFrame(update)
    }
    update()
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => {
      window.removeEventListener('scroll', onScroll)
      if (frame) cancelAnimationFrame(frame)
    }
  }, [])

  const handlePointerMove = (event: PointerEvent<HTMLElement>) => {
    if (reduceMotion || !rootRef.current) return
    const bounds = rootRef.current.getBoundingClientRect()
    const x = (event.clientX - bounds.left) / bounds.width - 0.5
    const y = (event.clientY - bounds.top) / bounds.height - 0.5
    rootRef.current.style.setProperty('--pointer-x', `${x * 30}px`)
    rootRef.current.style.setProperty('--pointer-y', `${y * 20}px`)
  }

  return (
    <section
      ref={rootRef}
      id='dawn'
      className='eclipse-hero'
      onPointerMove={handlePointerMove}
      onPointerLeave={() => {
        rootRef.current?.style.setProperty('--pointer-x', '0px')
        rootRef.current?.style.setProperty('--pointer-y', '0px')
      }}
      onPointerDown={() => setPulse((value) => value + 1)}
    >
      <div className='eclipse-hero-art' aria-hidden>
        <span className='eclipse-first-light' />
        <span className='eclipse-horizon' />
        <span className='eclipse-light-line eclipse-light-line-left' />
        <span className='eclipse-light-line eclipse-light-line-right' />
      </div>
      <div className='eclipse-hero-vignette' aria-hidden />
      <div className='eclipse-technical-orbit' aria-hidden />
      <span key={pulse} className='eclipse-click-wave' aria-hidden />

      <nav className='eclipse-phase-nav' aria-label='首页章节'>
        <a
          href='#dawn'
          className={phase === 0 ? 'is-active' : undefined}
          aria-label='日出'
        >
          <span aria-hidden />
        </a>
        <span />
        <a
          href='#offers'
          className={phase === 1 ? 'is-active' : undefined}
          aria-label='月牙'
        >
          <span aria-hidden />
        </a>
        <span />
        <a
          href='#about'
          className={phase === 2 ? 'is-active' : undefined}
          aria-label='满月'
        >
          <span aria-hidden />
        </a>
      </nav>

      <div className='relative z-10 mx-auto flex min-h-[100svh] max-w-[1500px] flex-col px-5 pt-24 pb-8 md:px-10 md:pt-28'>
        <div className='eclipse-display' aria-hidden>
          <span>CODE</span>
          <span>GO</span>
        </div>

        <motion.div
          className='eclipse-copy'
          initial={reduceMotion ? false : { opacity: 0, y: 18 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.75, ease: [0.22, 1, 0.36, 1] }}
        >
          <p>AI CODING GATEWAY · SHU26.CFD</p>
          <h1>让每一次调用，都抵达更远的地方</h1>
          <div className='eclipse-actions'>
            <Button
              className='eclipse-action-primary'
              render={<Link to='/dashboard' />}
            >
              <LayoutDashboard className='size-4' />
              控制台
              <ArrowUpRight className='size-4' />
            </Button>
            <Button
              variant='outline'
              className='eclipse-action-secondary'
              render={<Link to='/pricing' />}
            >
              <Shapes className='size-4' />
              查看模型
            </Button>
            <Button
              variant='outline'
              className='eclipse-action-secondary'
              render={<Link to='/download' />}
            >
              <Download className='size-4' />
              下载桌面端
            </Button>
          </div>
        </motion.div>

        <div className='mt-auto'>{modelRail}</div>
      </div>
    </section>
  )
}
