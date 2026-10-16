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
import { useState } from 'react'
import { Menu } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import type { TopNavLink } from '../types'
import { GlobalNavigation } from './global-navigation'

type TopNavProps = React.HTMLAttributes<HTMLElement> & {
  links: TopNavLink[]
}

export function TopNav({ className, links, ...props }: TopNavProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  return (
    <>
      <Button
        variant='ghost'
        size='icon'
        className='codego-public-menu'
        aria-label={t('Open menu')}
        aria-expanded={open}
        onClick={() => setOpen(true)}
      >
        <Menu aria-hidden='true' />
      </Button>
      <nav
        className={cn('codego-public-desktop-nav', className)}
        aria-label={t('gateway.navigation')}
        {...props}
      >
        <GlobalNavigation links={links.filter((link) => !link.secondary)} />
      </nav>
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent
          side='right'
          className='codego-public-mobile-sheet'
          aria-describedby={undefined}
        >
          <SheetHeader>
            <SheetTitle>{t('Navigation')}</SheetTitle>
          </SheetHeader>
          <nav
            className='codego-public-mobile-nav'
            aria-label={t('gateway.navigation')}
          >
            <GlobalNavigation
              mobile
              links={links}
              onNavigate={() => setOpen(false)}
            />
          </nav>
        </SheetContent>
      </Sheet>
    </>
  )
}
