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
import {
  ArrowRight,
  Check,
  Circle,
  KeyRound,
  LinkIcon,
  Package,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { CopyButton } from '@/components/copy-button'
import type { SetupGuideState } from './setup-guide/use-setup-guide'

function EndpointRow(props: {
  label: string
  value: string
  copyLabel: string
}) {
  return (
    <div className='bg-background/72 flex min-w-0 items-center gap-2 rounded-xl border border-white/60 px-3 py-2 dark:border-white/10'>
      <span className='text-muted-foreground shrink-0 text-[11px] font-medium'>
        {props.label}
      </span>
      <code
        className='text-foreground min-w-0 flex-1 truncate font-mono text-[11px]'
        title={props.value}
      >
        {props.value}
      </code>
      <CopyButton
        value={props.value}
        variant='ghost'
        size='sm'
        className='h-6 px-2 text-[11px]'
        tooltip={props.copyLabel}
        successTooltip='已复制'
        aria-label={props.copyLabel}
      >
        复制
      </CopyButton>
    </div>
  )
}

export function OverviewHeroPanel(props: { guide: SetupGuideState }) {
  const { guide } = props

  return (
    <section className='overview-hero-card p-5 sm:p-6 xl:p-7'>
      <div className='grid gap-5 xl:grid-cols-[minmax(0,1fr)_minmax(380px,420px)] xl:items-start'>
        <div className='flex min-w-0 flex-col gap-5'>
          <div className='space-y-3'>
            <div className='text-primary text-xs font-semibold tracking-[0.16em] uppercase'>
              快速开始
            </div>
            <h2 className='max-w-xl text-3xl font-semibold tracking-tight text-balance sm:text-4xl xl:text-5xl'>
              先看钱包、套餐，再去盲盒
            </h2>
            <p className='text-muted-foreground max-w-xl text-sm leading-7 sm:text-[15px]'>
              从这里直接进入钱包、套餐和盲盒，先确认余额和订阅状态，再处理购买或抽取。
            </p>
          </div>

          <div className='flex flex-wrap items-center gap-2'>
            <Button variant='outline' render={<Link to='/wallet' />}>
              <KeyRound data-icon='inline-start' />
              查看钱包
            </Button>
            <Button variant='outline' render={<Link to='/packages' />}>
              <Package data-icon='inline-start' />
              查看套餐
            </Button>
            <Button render={<Link to='/blind-box' />}>
              <ArrowRight data-icon='inline-end' />
              进入盲盒
            </Button>
          </div>

          <div className='grid gap-2.5 sm:grid-cols-3'>
            {guide.startSteps.map((step, index) => {
              const StatusIcon = step.completed ? Check : Circle
              return (
                <div
                  key={step.title}
                  className='overview-soft-card flex min-w-0 gap-3 p-3.5'
                >
                  <span
                    className={cn(
                      'flex size-9 shrink-0 items-center justify-center rounded-xl border',
                      step.completed
                        ? 'border-success/30 bg-success/10 text-success'
                        : 'border-border/70 bg-background/70 text-muted-foreground'
                    )}
                  >
                    <StatusIcon className='size-4' aria-hidden='true' />
                  </span>
                  <div className='min-w-0'>
                    <div className='flex items-center gap-2 text-sm font-medium'>
                      <span className='text-muted-foreground font-mono text-xs tabular-nums'>
                        {index + 1}.
                      </span>
                      <span className='truncate'>{step.title}</span>
                    </div>
                    <p className='text-muted-foreground mt-1 line-clamp-2 text-xs leading-5'>
                      {step.description}
                    </p>
                  </div>
                </div>
              )
            })}
          </div>
        </div>

        <div className='overview-soft-card flex min-w-0 flex-col gap-4 p-5'>
          <div className='flex items-center gap-2.5'>
            <span className='bg-primary/10 text-primary flex size-10 shrink-0 items-center justify-center rounded-xl'>
              <LinkIcon className='size-5' aria-hidden='true' />
            </span>
            <div className='min-w-0'>
              <div className='text-base font-semibold'>API 请求地址</div>
              <div className='text-muted-foreground text-xs'>
                {guide.requestExample.ready
                  ? `使用密钥：${guide.requestExample.keyName}`
                  : '创建 API 密钥后即可开始请求'}
              </div>
            </div>
          </div>

          <div className='space-y-3'>
            <EndpointRow
              label='OpenAI'
              value='https://shu26.cfd/v1'
              copyLabel='复制 OpenAI 格式地址'
            />
            <EndpointRow
              label='Anthropic'
              value='https://shu26.cfd'
              copyLabel='复制 Anthropic 格式地址'
            />
          </div>

          {!guide.requestExample.ready && (
            <Button variant='outline' render={<Link to='/keys' />}>
              <KeyRound data-icon='inline-start' />
              创建 API 密钥
            </Button>
          )}
        </div>
      </div>
    </section>
  )
}
