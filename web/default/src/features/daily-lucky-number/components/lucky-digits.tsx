import { motion, useReducedMotion } from 'motion/react'
import { cn } from '@/lib/utils'
import { normalizeLuckyNumber } from '../lib'
import { DIGIT_ITEM, DIGIT_STACK, EASE_OUT_QUINT } from '../motion'

type DigitSize = 'sm' | 'md' | 'lg'
type DigitTone = 'activity' | 'default' | 'stage'

const SIZE_BOX: Record<DigitSize, string> = {
  sm: 'w-8 rounded-lg text-base',
  md: 'w-10 rounded-lg text-xl sm:w-11 sm:text-2xl',
  lg: 'w-12 rounded-xl text-2xl sm:w-16 sm:text-4xl',
}

/**
 * Matches are counted right-to-left, so matchedDigits lights up the trailing
 * N boxes rather than the leading ones.
 */
function isMatched(index: number, total: number, matchedDigits: number) {
  return matchedDigits > 0 && index >= total - matchedDigits
}

export function LuckyDigits(props: {
  value?: string | number
  placeholder?: string
  size?: DigitSize
  matchedDigits?: number
  dimUnmatched?: boolean
  pending?: boolean
  rolling?: boolean
  animateReveal?: boolean
  tone?: DigitTone
  className?: string
}) {
  const reduced = Boolean(useReducedMotion())
  const size = props.size ?? 'sm'
  const value = normalizeLuckyNumber(props.value)
  const placeholder = props.placeholder ?? '----'
  const digits = (value || placeholder).slice(-4).split('')
  const matchedDigits = props.matchedDigits ?? 0
  const revealing = Boolean(props.animateReveal && value && !reduced)

  return (
    <motion.div
      className={cn(
        'flex items-center gap-1.5 font-mono font-semibold tabular-nums',
        size === 'lg' && 'gap-2 sm:gap-2.5',
        props.className
      )}
      aria-label={value || placeholder}
      variants={revealing ? DIGIT_STACK : undefined}
      initial={revealing ? 'initial' : false}
      animate={revealing ? 'animate' : undefined}
      style={revealing ? { perspective: 600 } : undefined}
    >
      {digits.map((digit, index) => (
        <DigitBox
          key={`${index}-${digit}`}
          digit={digit}
          size={size}
          revealing={revealing}
          matched={isMatched(index, digits.length, matchedDigits)}
          dimmed={
            Boolean(props.dimUnmatched) &&
            !isMatched(index, digits.length, matchedDigits)
          }
          pending={Boolean(props.pending) && !value}
          rolling={Boolean(props.rolling) && !reduced}
          reduced={reduced}
          tone={props.tone ?? 'default'}
          delayIndex={digits.length - 1 - index}
        />
      ))}
    </motion.div>
  )
}

function DigitBox(props: {
  digit: string
  size: DigitSize
  revealing: boolean
  matched: boolean
  dimmed: boolean
  pending: boolean
  rolling: boolean
  reduced: boolean
  tone: DigitTone
  delayIndex: number
}) {
  const base = cn(
    'relative inline-flex aspect-square items-center justify-center overflow-hidden border',
    SIZE_BOX[props.size],
    props.tone === 'stage'
      ? 'border-white/15 bg-white/10 text-white shadow-[inset_0_1px_0_rgba(255,255,255,0.08)]'
      : props.tone === 'activity'
        ? 'border-primary/30 bg-primary/10 text-primary shadow-[inset_0_1px_0_color-mix(in_oklch,var(--primary)_12%,transparent)]'
        : props.matched
          ? 'border-primary/60 bg-primary/12 text-primary shadow-[0_0_18px_-6px_color-mix(in_oklch,var(--primary)_65%,transparent)]'
          : props.dimmed
            ? 'border-border/60 bg-muted/40 text-muted-foreground/60'
            : 'border-border bg-muted/65 text-foreground'
  )

  if (props.rolling) {
    return <RollingDigit className={base} delayIndex={props.delayIndex} />
  }

  if (props.pending) {
    return (
      <PendingDigit
        className={base}
        delayIndex={props.delayIndex}
        reduced={props.reduced}
      />
    )
  }

  if (props.revealing) {
    return (
      <motion.span className={base} variants={DIGIT_ITEM}>
        {props.digit}
      </motion.span>
    )
  }

  if (props.matched) {
    return (
      <motion.span
        className={base}
        initial={{ scale: 0.88 }}
        animate={{ scale: 1 }}
        transition={{ duration: 0.34, ease: EASE_OUT_QUINT }}
      >
        {props.digit}
      </motion.span>
    )
  }

  return <span className={base}>{props.digit}</span>
}

function RollingDigit(props: { className: string; delayIndex: number }) {
  return (
    <span className={props.className}>
      <motion.span
        className='absolute inset-0 flex flex-col'
        animate={{ y: ['0%', '-900%'] }}
        transition={{
          duration: 1.1 + props.delayIndex * 0.12,
          repeat: Infinity,
          ease: 'linear',
        }}
      >
        {[0, 1, 2, 3, 4, 5, 6, 7, 8, 9].map((number) => (
          <span
            key={number}
            className='flex h-full w-full shrink-0 items-center justify-center'
          >
            {number}
          </span>
        ))}
      </motion.span>
    </span>
  )
}

function PendingDigit(props: {
  className: string
  delayIndex: number
  reduced: boolean
}) {
  if (props.reduced) {
    return (
      <span className={props.className}>
        <span className='size-1.5 rounded-full bg-current opacity-60' />
      </span>
    )
  }

  return (
    <motion.span
      className={props.className}
      animate={{ opacity: [0.45, 0.95, 0.45] }}
      transition={{
        duration: 1.8,
        repeat: Infinity,
        ease: 'easeInOut',
        delay: props.delayIndex * 0.16,
      }}
    >
      <span className='size-1.5 rounded-full bg-current opacity-60' />
    </motion.span>
  )
}
