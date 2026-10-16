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
import { PublicLayout } from '@/components/layout'
import './auth.css'

type AuthLayoutProps = { children: React.ReactNode }

export function AuthLayout(props: AuthLayoutProps) {
  return (
    <PublicLayout showMainContainer={false}>
      <div className='market-auth'>
        {/* Atmospheric Organic Aurora Canvas (OpenDesign & Liquid Retina) */}
        <div className='market-auth-backdrop' aria-hidden='true'>
          <div className='market-auth-aurora-orb orb-emerald' />
          <div className='market-auth-aurora-orb orb-amber' />
          <div className='market-auth-aurora-orb orb-cyan' />
          <div className='market-auth-aurora-orb orb-violet' />
          <div className='market-auth-grid-overlay' />
        </div>

        {/* Centered Frosted Glass Stage */}
        <main id='content' tabIndex={-1} className='market-auth-centered-stage'>
          <section className='market-auth-frosted-card'>
            {props.children}
          </section>
        </main>
      </div>
    </PublicLayout>
  )
}
