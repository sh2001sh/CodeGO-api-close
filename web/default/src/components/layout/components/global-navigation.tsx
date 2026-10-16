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
import { Link, useNavigate, useRouterState } from '@tanstack/react-router'
import { LayoutGroup, motion, useReducedMotion } from 'motion/react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { cn } from '@/lib/utils'
import type { TopNavLink } from '../types'

// Public routes and the console replace their header on layout changes.
let departingSegmentIndex: number | undefined

function rememberDepartingSegment(index: number): void {
  departingSegmentIndex = index >= 0 ? index : undefined
}

type GlobalNavigationProps = {
  links: TopNavLink[]
  mobile?: boolean
  className?: string
  onNavigate?: () => void
}

export function GlobalNavigation(props: GlobalNavigationProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const user = useAuthStore((state) => state.auth.user)
  const reduceMotion = useReducedMotion()
  const location = useRouterState({
    select: (state) => ({
      pathname: state.location.pathname,
      inConsole: state.matches.some((match) =>
        match.routeId.startsWith('/_authenticated')
      ),
    }),
  })

  function isActive(link: TopNavLink): boolean {
    if (link.external) return false
    if (link.isActive !== undefined) return link.isActive
    if (link.href === '/dashboard/overview') return location.inConsole
    return (
      location.pathname === link.href ||
      (link.href !== '/' && location.pathname.startsWith(`${link.href}/`))
    )
  }

  const activeIndex = props.links.findIndex(isActive)

  function handleClick(
    event: React.MouseEvent<HTMLAnchorElement>,
    link: TopNavLink
  ): void {
    if (link.disabled) {
      event.preventDefault()
      return
    }
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
    if (!props.mobile) rememberDepartingSegment(activeIndex)
    props.onNavigate?.()
    if (link.requiresAuth && !user) {
      event.preventDefault()
      void navigate({ to: '/sign-in', search: { redirect: link.href } })
    }
  }

  return (
    <LayoutGroup id='solaris-global-navigation'>
      <div
        className={cn(
          props.mobile
            ? 'codego-public-mobile-links'
            : 'codego-public-segments',
          props.className
        )}
        style={
          props.mobile
            ? undefined
            : {
                gridTemplateColumns: `repeat(${props.links.length}, minmax(0, 1fr))`,
              }
        }
      >
        {!props.mobile && activeIndex >= 0 && (
          <motion.span
            layoutId='active-capsule'
            className='codego-public-segment-indicator'
            initial={{
              left: `calc(4px + (100% - 8px) * ${departingSegmentIndex ?? activeIndex} / ${props.links.length})`,
            }}
            animate={{
              left: `calc(4px + (100% - 8px) * ${activeIndex} / ${props.links.length})`,
            }}
            transition={
              reduceMotion
                ? { duration: 0 }
                : { type: 'spring', stiffness: 420, damping: 34, mass: 0.8 }
            }
            style={{ width: `calc((100% - 8px) / ${props.links.length})` }}
            aria-hidden='true'
          />
        )}
        {props.links.map((link) => {
          const active = isActive(link)
          const linkProps = {
            className: cn(
              'codego-public-link',
              !props.mobile && 'codego-public-segment',
              active && 'active'
            ),
            'aria-current': active ? ('page' as const) : undefined,
            'aria-disabled': link.disabled,
            tabIndex: link.disabled ? -1 : undefined,
            onClick: (event: React.MouseEvent<HTMLAnchorElement>) =>
              handleClick(event, link),
          }

          if (link.external) {
            return (
              <a
                key={link.href}
                href={link.href}
                target='_blank'
                rel='noopener noreferrer'
                {...linkProps}
              >
                {t(link.title)}
              </a>
            )
          }
          return (
            <Link key={link.href} to={link.href} {...linkProps}>
              {t(link.title)}
            </Link>
          )
        })}
      </div>
    </LayoutGroup>
  )
}
