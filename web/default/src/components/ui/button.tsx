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
import { isValidElement } from 'react'
import { Button as ButtonPrimitive } from '@base-ui/react/button'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const buttonVariants = cva(
  "group/button inline-flex shrink-0 items-center justify-center rounded-xl border border-transparent bg-clip-padding text-sm font-medium whitespace-nowrap transition-[background-color,color,box-shadow,border-color,transform] duration-200 ease-[var(--ease-optical)] outline-none select-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/40 active:scale-[0.975] active:transition-transform active:duration-100 active:ease-[var(--ease-spring)] disabled:pointer-events-none disabled:opacity-50 disabled:active:scale-100 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default:
          'bg-primary text-primary-foreground shadow-sm hover:bg-[var(--primary-hover)] active:bg-[var(--primary-active)] border border-black/10 dark:border-white/10 shadow-[inset_0_1px_0_rgba(255,255,255,0.15)] font-medium',
        brand:
          'bg-[var(--brand-orange)] text-white font-medium shadow-[inset_0_1px_0_rgba(255,255,255,0.25),var(--shadow-orange)] hover:bg-[var(--brand-orange-hover)] active:bg-[var(--brand-orange-active)] border border-[var(--brand-orange-active)]/40',
        outline:
          'border-[var(--hairline-light)] bg-white/75 dark:bg-white/5 backdrop-blur-md text-foreground shadow-[var(--shadow-card)] hover:bg-white dark:hover:bg-white/10 hover:border-[var(--hairline-strong)] hover:shadow-[var(--shadow-floating)] aria-expanded:bg-muted aria-expanded:text-foreground',
        secondary:
          'bg-secondary text-secondary-foreground shadow-[inset_0_1px_0_rgb(255_255_255_/_12%),0_1px_2px_rgba(0,0,0,0.06)] hover:bg-secondary/90 aria-expanded:bg-secondary aria-expanded:text-secondary-foreground',
        ghost:
          'hover:bg-[var(--bg-subtle)] text-foreground/80 hover:text-foreground aria-expanded:bg-[var(--bg-subtle)] aria-expanded:text-foreground',
        destructive:
          'bg-destructive/12 text-destructive hover:bg-destructive/20 focus-visible:border-destructive/40 focus-visible:ring-destructive/25 dark:bg-destructive/20 dark:hover:bg-destructive/30',
        link: 'text-[var(--brand-orange-ink)] underline-offset-4 hover:underline',
      },
      size: {
        default:
          'h-11 gap-2 px-4 has-data-[icon=inline-end]:pr-3 has-data-[icon=inline-start]:pl-3',
        xs: "h-6 gap-1 rounded-lg px-2 text-xs in-data-[slot=button-group]:rounded-lg has-data-[icon=inline-end]:pr-1.5 has-data-[icon=inline-start]:pl-1.5 [&_svg:not([class*='size-'])]:size-3",
        sm: "h-8 gap-1.5 rounded-lg px-3 text-[0.8rem] in-data-[slot=button-group]:rounded-lg has-data-[icon=inline-end]:pr-2 has-data-[icon=inline-start]:pl-2 [&_svg:not([class*='size-'])]:size-3.5",
        lg: 'h-12 gap-2.5 px-6 text-base has-data-[icon=inline-end]:pr-4 has-data-[icon=inline-start]:pl-4',
        icon: 'size-11 rounded-xl',
        'icon-xs':
          "size-6 rounded-lg in-data-[slot=button-group]:rounded-lg [&_svg:not([class*='size-'])]:size-3",
        'icon-sm': 'size-8 rounded-lg in-data-[slot=button-group]:rounded-lg',
        'icon-lg': 'size-12 rounded-xl',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  }
)

function isNativeButtonRender(render: ButtonPrimitive.Props['render']) {
  if (!render || !isValidElement(render)) {
    return true
  }

  return render.type === 'button'
}

function Button({
  className,
  variant = 'default',
  size = 'default',
  nativeButton,
  render,
  ...props
}: ButtonPrimitive.Props & VariantProps<typeof buttonVariants>) {
  return (
    <ButtonPrimitive
      data-slot='button'
      data-variant={variant}
      data-size={size}
      className={cn(buttonVariants({ variant, size, className }))}
      nativeButton={nativeButton ?? isNativeButtonRender(render)}
      render={render}
      {...props}
    />
  )
}

export { Button, buttonVariants }
