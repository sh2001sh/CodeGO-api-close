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
import { useEffect, useMemo, useRef, useState } from 'react'
import { motion } from 'motion/react'
import { cn } from '@/lib/utils'
import type { ReelItem } from './blind-box-reel-data'

/**
 * Horizontal case-opening reel (CS:GO pattern, dawn editorial skin):
 * cells scroll fast then decelerate on a cubic ease-out until the winning
 * cell lands under the fixed copper center marker.
 */

const REEL_EASE = [0.12, 0.78, 0.06, 1] as const
const CELL_WIDTH = 148
const START_INDEX = 3
const WINNER_INDEX = 32
const TRAIL_CELLS = 5
const REEL_DURATION = 2.9

function useReelAnimation(props: {
  reduced: boolean
  onComplete?: () => void
}) {
  const [settled, setSettled] = useState(false)
  const completedRef = useRef(false)
  const completeTimerRef = useRef<number | null>(null)

  useEffect(() => {
    return () => {
      if (completeTimerRef.current != null) {
        window.clearTimeout(completeTimerRef.current)
      }
    }
  }, [])

  const handleAnimationComplete = () => {
    if (completedRef.current) return
    setSettled(true)
    if (props.reduced) {
      completedRef.current = true
      props.onComplete?.()
      return
    }
    completeTimerRef.current = window.setTimeout(() => {
      completedRef.current = true
      props.onComplete?.()
    }, 620)
  }

  return { settled, handleAnimationComplete }
}

function weightRandomIndex(length: number) {
  // front-loaded: low tiers dominate the strip like a real pool
  const roll = Math.random()
  const shaped = Math.floor(Math.pow(roll, 1.8) * length)
  return Math.min(length - 1, shaped)
}

export function BlindBoxReel(props: {
  pool: ReelItem[]
  winner: ReelItem
  reduced?: boolean
  onComplete?: () => void
}) {
  const reduced = Boolean(props.reduced)
  const animation = useReelAnimation({
    reduced,
    onComplete: props.onComplete,
  })

  const cells = useMemo(() => {
    const pool = props.pool.length > 0 ? props.pool : [props.winner]
    const list: ReelItem[] = []
    for (let i = 0; i < WINNER_INDEX; i++) {
      list.push(pool[weightRandomIndex(pool.length)])
    }
    list.push(props.winner)
    for (let i = 0; i < TRAIL_CELLS; i++) {
      list.push(pool[weightRandomIndex(pool.length)])
    }
    return list
  }, [props.pool, props.winner])

  const startOffset = START_INDEX * CELL_WIDTH + CELL_WIDTH / 2
  const winnerOffset = WINNER_INDEX * CELL_WIDTH + CELL_WIDTH / 2

  return (
    <div className='relative overflow-hidden' role='img' aria-label='开盒动画'>
      <ReelMarker />
      <ReelEdgeFade />

      <motion.div
        className='relative left-1/2 flex'
        initial={reduced ? false : { x: -startOffset }}
        animate={{ x: -winnerOffset }}
        transition={
          reduced
            ? { duration: 0 }
            : { duration: REEL_DURATION, ease: [...REEL_EASE] }
        }
        onAnimationComplete={animation.handleAnimationComplete}
      >
        {cells.map((cell, index) => (
          <ReelCell
            key={`${cell.key}-${index}`}
            cell={cell}
            highlight={index === WINNER_INDEX && animation.settled}
          />
        ))}
      </motion.div>
    </div>
  )
}

function ReelMarker() {
  return (
    <div
      aria-hidden
      className='bg-primary pointer-events-none absolute top-0 bottom-0 left-1/2 z-20 w-[2px] -translate-x-1/2'
    >
      <span className='bg-primary absolute -top-px left-1/2 size-1.5 -translate-x-1/2 rotate-45' />
      <span className='bg-primary absolute -bottom-px left-1/2 size-1.5 -translate-x-1/2 rotate-45' />
    </div>
  )
}

function ReelEdgeFade() {
  return (
    <>
      <div
        aria-hidden
        className='from-card pointer-events-none absolute inset-y-0 left-0 z-10 w-16 bg-gradient-to-r to-transparent'
      />
      <div
        aria-hidden
        className='from-card pointer-events-none absolute inset-y-0 right-0 z-10 w-16 bg-gradient-to-l to-transparent'
      />
    </>
  )
}

function ReelCell(props: { cell: ReelItem; highlight: boolean }) {
  return (
    <div style={{ width: CELL_WIDTH }} className='shrink-0 px-2'>
      <div
        className={cn(
          'relative flex h-32 flex-col items-center justify-center gap-2 border px-3 text-center transition-colors duration-200',
          props.highlight
            ? 'border-primary bg-primary/[0.06]'
            : 'border-border/60 bg-background/40'
        )}
      >
        <span
          aria-hidden
          className={cn('bg-primary block', resolveRarityMarkClass(props.cell))}
        />
        <span
          className={cn(
            'text-foreground line-clamp-2 text-[11px] leading-4 font-medium',
            props.highlight && 'text-primary'
          )}
        >
          {props.cell.label}
        </span>
        {props.cell.tag ? <ReelTag item={props.cell} /> : null}
        {props.highlight ? (
          <motion.span
            aria-hidden
            className='border-primary absolute inset-0'
            initial={{ opacity: 0.9, scale: 1 }}
            animate={{ opacity: 0, scale: 1.35 }}
            transition={{ duration: 0.55, ease: 'easeOut' }}
          />
        ) : null}
      </div>
    </div>
  )
}

function resolveRarityMarkClass(item: ReelItem) {
  if (item.strong) return 'h-[3px] w-9'
  if (item.tag) return 'h-[2px] w-5 opacity-70'
  return 'h-[2px] w-2.5 opacity-35'
}

function ReelTag(props: { item: ReelItem }) {
  return (
    <span
      className={cn(
        'border px-1 py-px font-mono text-[9px] uppercase',
        props.item.strong
          ? 'border-primary/50 bg-primary text-primary-foreground'
          : 'border-primary/40 text-primary'
      )}
    >
      {props.item.tag}
    </span>
  )
}
