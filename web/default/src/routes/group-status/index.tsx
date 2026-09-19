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

For commercial licensing, please contact support@quantumnous.com.
*/
import { createFileRoute } from '@tanstack/react-router'
import { SiteSeo } from '@/components/seo'
import { DawnNav } from '@/features/dawn/components/dawn-nav'
import { SidebarGroupStatusPage } from '@/features/sidebar-group-status'

export const Route = createFileRoute('/group-status/')({
  component: GroupStatusPage,
})

function GroupStatusPage() {
  return (
    <div className='bg-background text-foreground relative min-h-svh overflow-x-clip'>
      <SiteSeo
        title='分组状态 | Code Go'
        description='分组状态 · 近 6 小时真实请求可用率'
        canonicalPath='/group-status'
      />
      <div className='dawn !min-h-0 !bg-transparent'>
        <DawnNav />
      </div>
      <div className='min-h-svh pt-32 pb-[env(safe-area-inset-bottom)] md:pt-20'>
        <SidebarGroupStatusPage />
      </div>
    </div>
  )
}
