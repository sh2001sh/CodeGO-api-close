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
import { Loader2, PackageOpen } from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { cn } from '@/lib/utils'

type EditorialBoxGlyphProps = {
  pending: boolean
  opening: boolean
  busy: boolean
  onOpen: () => void
}

function resolveIdleMotion(reduced: boolean, opening: boolean) {
  if (reduced) return undefined
  if (opening) return { rotate: [0, -4, 4, -3, 3, 0], y: 0 }
  return { y: [0, -6, 0] }
}

export function EditorialBoxGlyph(props: EditorialBoxGlyphProps) {
  const reduced = Boolean(useReducedMotion())
  const interactive = props.pending && !props.busy

  return (
    <motion.button
      type='button'
      onClick={interactive ? props.onOpen : undefined}
      disabled={!interactive}
      aria-label={interactive ? '开启 1 个盲盒' : '盲盒库存为空'}
      className='focus-visible:ring-ring relative block size-24 cursor-pointer outline-none focus-visible:ring-2 focus-visible:ring-offset-2 disabled:cursor-default sm:size-28'
      animate={resolveIdleMotion(reduced, props.opening)}
      transition={{
        duration: props.opening ? 0.5 : 3.8,
        repeat: Infinity,
        ease: 'easeInOut',
      }}
      whileHover={interactive && !reduced ? { y: -4 } : undefined}
      whileTap={interactive && !reduced ? { scale: 0.95 } : undefined}
    >
      <BoxHalo pending={props.pending} reduced={reduced} />
      <BoxBody />
      <BoxLid pending={props.pending} reduced={reduced} />
      <BoxCountBadge pending={props.pending} opening={props.opening} />
    </motion.button>
  )
}

function BoxHalo(props: { pending: boolean; reduced: boolean }) {
  return (
    <motion.span
      aria-hidden
      className={cn(
        'bg-primary/25 pointer-events-none absolute -inset-5 rounded-full blur-2xl',
        props.pending ? 'opacity-100' : 'opacity-0'
      )}
      animate={
        props.reduced || !props.pending
          ? undefined
          : { scale: [1, 1.14, 1], opacity: [0.45, 0.85, 0.45] }
      }
      transition={{ duration: 3.2, repeat: Infinity, ease: 'easeInOut' }}
    />
  )
}

function BoxBody() {
  return (
    <span
      aria-hidden
      className='bg-card border-primary/45 absolute inset-x-1 top-[30%] bottom-0 border'
    >
      <span className='bg-primary/30 absolute inset-y-0 left-1/2 w-3 -translate-x-1/2' />
    </span>
  )
}

function BoxLid(props: { pending: boolean; reduced: boolean }) {
  return (
    <motion.span
      aria-hidden
      className={cn(
        'bg-primary/10 border-primary/55 absolute inset-x-0 top-[10%] flex h-[26%] items-center justify-center border',
        props.pending ? 'text-primary' : 'text-muted-foreground'
      )}
      animate={
        props.reduced || !props.pending
          ? undefined
          : { rotate: [-2, 2, -2], y: [0, -3, 0] }
      }
      transition={{ duration: 2.6, repeat: Infinity, ease: 'easeInOut' }}
    >
      <span className='bg-primary/35 absolute inset-y-0 left-1/2 w-3 -translate-x-1/2' />
      <PackageOpen className='relative size-5' />
    </motion.span>
  )
}

function BoxCountBadge(props: { pending: boolean; opening: boolean }) {
  if (!props.pending) return null
  return (
    <span className='border-primary/50 bg-primary text-primary-foreground absolute -top-2 -right-2 z-10 flex size-7 items-center justify-center border font-mono text-xs font-bold tabular-nums'>
      {props.opening ? <Loader2 className='size-3.5 animate-spin' /> : '×1'}
    </span>
  )
}
