import type { Variants } from 'motion/react'

export const EASE_OUT_QUINT = [0.22, 1, 0.36, 1] as const

export const STAGE_STACK: Variants = {
  initial: {},
  animate: { transition: { staggerChildren: 0.09, delayChildren: 0.04 } },
}

export const STAGE_ITEM: Variants = {
  initial: { opacity: 0, y: 14 },
  animate: {
    opacity: 1,
    y: 0,
    transition: { duration: 0.42, ease: EASE_OUT_QUINT },
  },
}

export const REDUCED_STACK: Variants = { initial: {}, animate: {} }

export const REDUCED_ITEM: Variants = {
  initial: { opacity: 0 },
  animate: { opacity: 1, transition: { duration: 0.18 } },
}

/** Digits reveal right-to-left, mirroring how matches are counted. */
export const DIGIT_STACK: Variants = {
  initial: {},
  animate: {
    transition: { staggerChildren: 0.08, staggerDirection: -1 },
  },
}

export const DIGIT_ITEM: Variants = {
  initial: { opacity: 0, rotateX: -75, y: -8 },
  animate: {
    opacity: 1,
    rotateX: 0,
    y: 0,
    transition: { duration: 0.46, ease: EASE_OUT_QUINT },
  },
}

export function stackVariants(reduced: boolean) {
  return {
    container: reduced ? REDUCED_STACK : STAGE_STACK,
    item: reduced ? REDUCED_ITEM : STAGE_ITEM,
  }
}
