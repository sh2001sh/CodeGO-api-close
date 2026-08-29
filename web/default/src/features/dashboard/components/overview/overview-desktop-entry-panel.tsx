/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or (at your
option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Affero
General Public License for more details.

You should have received a copy of the GNU Affero General Public License along
with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { Link } from '@tanstack/react-router'
import { ArrowRight, Laptop, Link2, ShieldCheck } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { buildCodeGoDesktopQuickActions } from './codego-desktop-entry'

const desktopHighlights = [
  {
    label: '账号联动',
    value: '网页登录后可一键导入桌面身份',
    icon: Link2,
  },
  {
    label: '本地配置',
    value: '在桌面端统一分发工具接入与诊断信息',
    icon: Laptop,
  },
  {
    label: '安全写入',
    value: '写入前校验本地配置，避免覆盖损坏文件',
    icon: ShieldCheck,
  },
]

export function CodeGoDesktopEntryPanel() {
  const actions = buildCodeGoDesktopQuickActions()

  return (
    <section className='codego-panel flex flex-col gap-4 p-5 sm:p-6'>
      <div className='flex items-start justify-between gap-3'>
        <div className='min-w-0'>
          <span className='codego-kicker'>DESKTOP</span>
          <div className='text-foreground mt-1.5 text-lg font-semibold'>
            Code Go Desktop
          </div>
        </div>
        <div className='codego-stat-label shrink-0 border border-primary/30 px-2 py-1 text-primary'>
          已联动
        </div>
      </div>

      <div className='grid gap-2.5'>
        {desktopHighlights.map((item) => {
          const Icon = item.icon
          return (
            <div
              key={item.label}
              className='flex items-center justify-between gap-3 border-t border-border/70 py-3 first:border-t-0 first:pt-0'
            >
              <div className='text-foreground text-sm font-medium'>
                {item.label}
              </div>
              <Icon
                className='text-primary size-4 shrink-0'
                aria-hidden='true'
              />
            </div>
          )
        })}
      </div>

      <div className='grid gap-2.5'>
        {actions.map((action) => (
          <Button
            key={action.href}
            variant={action.variant}
            className='justify-between'
            render={<Link to={action.href} />}
          >
            <span>{action.label}</span>
            <ArrowRight data-icon='inline-end' />
          </Button>
        ))}
      </div>
    </section>
  )
}
