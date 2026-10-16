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
import { type TopNavLink } from '../types'

/** The same destinations and order are used by every public and console header. */
export const defaultTopNavLinks: TopNavLink[] = [
  { title: 'Home', href: '/' },
  { title: 'Models', href: '/pricing' },
  { title: 'Market', href: '/market' },
  { title: 'Docs', href: '/docs' },
  {
    title: 'Community',
    href: 'https://community.codegoai.com',
    external: true,
  },
  { title: 'Rankings', href: '/rankings' },
  { title: 'Console', href: '/dashboard/overview', requiresAuth: true },
  { title: 'Status', href: '/group-status', secondary: true },
  { title: 'Chat', href: '/playground', secondary: true },
]
