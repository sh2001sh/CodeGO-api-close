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
    <section className='overview-glass-card flex flex-col gap-4 p-5 sm:p-6'>
      <div className='flex items-start justify-between gap-3'>
        <div>
          <div className='text-muted-foreground text-[11px] font-medium tracking-[0.16em] uppercase'>
            Code Go Desktop
          </div>
          <div className='text-foreground mt-1 text-xl font-semibold tracking-tight'>
            从控制台进入桌面端
          </div>
          <div className='text-muted-foreground mt-2 text-sm leading-6'>
            下载桌面应用、导入 Token，并继续查看设备绑定与本地配置状态。
          </div>
        </div>
        <div className='border-primary/25 bg-primary/10 text-primary rounded-full border px-2.5 py-1 text-xs font-medium'>
          已联动
        </div>
      </div>

      <div className='grid gap-2.5'>
        {desktopHighlights.map((item) => {
          const Icon = item.icon
          return (
            <div
              key={item.label}
              className='overview-soft-card flex items-start gap-3 px-3 py-3'
            >
              <span className='border border-primary/30 text-primary bg-primary/[0.04] flex size-8 shrink-0 items-center justify-center rounded-xl'>
                <Icon className='size-3.5' aria-hidden='true' />
              </span>
              <div className='min-w-0'>
                <div className='text-foreground text-sm font-medium'>
                  {item.label}
                </div>
                <div className='text-muted-foreground mt-1 text-xs leading-5'>
                  {item.value}
                </div>
              </div>
            </div>
          )
        })}
      </div>

      <div className='grid gap-2.5'>
        {actions.map((action) => (
          <Button
            key={action.href}
            variant={action.variant}
            className='justify-between rounded-2xl'
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
