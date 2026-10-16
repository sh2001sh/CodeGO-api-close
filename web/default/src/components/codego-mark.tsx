import type { SVGProps } from 'react'

export function CodeGoMark(props: SVGProps<SVGSVGElement>) {
  return (
    <svg {...props} viewBox='0 0 32 32' fill='none' aria-hidden='true'>
      <path
        fill='currentColor'
        d='M26 4H12C6.48 4 2 8.48 2 14v4c0 5.52 4.48 10 10 10h14a2 2 0 0 0 2-2v-2a2 2 0 0 0-2-2H12a4 4 0 0 1-4-4v-4a4 4 0 0 1 4-4h14a2 2 0 0 0 2-2V6a2 2 0 0 0-2-2Z'
      />
      <path fill='currentColor' d='M17 13h6v6h-6a3 3 0 0 1 0-6Z' />
      <rect
        x='25'
        y='13'
        width='6'
        height='6'
        rx='1.5'
        fill='var(--dawn-copper, #a94825)'
      />
    </svg>
  )
}
