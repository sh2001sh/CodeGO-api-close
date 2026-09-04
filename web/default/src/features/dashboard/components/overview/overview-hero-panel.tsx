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
import { Link } from '@tanstack/react-router'
import {
  ArrowUpRight,
  Check,
  Copy,
  KeyRound,
  MessageSquare,
  WalletCards,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { CopyButton } from '@/components/copy-button'
import type { SetupGuideState } from './setup-guide/use-setup-guide'

function EndpointRow(props: {
  label: string
  value: string
  copyLabel: string
}) {
  return (
    <div className='border-border/70 flex items-center gap-3 border-t py-3 first:border-t-0 first:pt-0'>
      <span className='codego-stat-label w-16 shrink-0'>{props.label}</span>
      <code
        className='text-foreground min-w-0 flex-1 truncate font-mono text-[12px]'
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
        <Copy className='size-3.5' />
      </CopyButton>
    </div>
  )
}

export function OverviewHeroPanel(props: { guide: SetupGuideState }) {
  const { guide } = props

  return (
    <section className='overview-hero-card p-5 sm:p-6 xl:p-7'>
      <div className='grid gap-8 xl:grid-cols-[minmax(0,1fr)_minmax(360px,400px)] xl:items-start'>
        <div className='flex min-w-0 flex-col gap-6'>
          <div>
            <h2 className='codego-kicker'>WORKSPACE</h2>
          </div>

          <div className='flex flex-wrap items-center gap-2'>
            <Button render={<Link to='/keys' />}>
              <KeyRound data-icon='inline-start' />
              创建 API 密钥
            </Button>
            <Button variant='outline' render={<Link to='/packages' />}>
              <WalletCards data-icon='inline-start' />
              查看可用套餐
            </Button>
            <Button variant='outline' render={<Link to='/playground' />}>
              <MessageSquare data-icon='inline-start' />
              打开 AI 聊天
              <ArrowUpRight data-icon='inline-end' />
            </Button>
          </div>

        </div>

        <div className='flex min-w-0 flex-col gap-5'>
          <div className='flex items-center justify-between gap-3'>
            <span className='codego-kicker'>API ENDPOINT</span>
            {guide.requestExample.ready ? (
              <span className='codego-stat-label text-primary'>
                <Check className='mr-1 inline size-3' />
                {guide.requestExample.keyName}
              </span>
            ) : null}
          </div>

          <div>
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
            <Button
              variant='outline'
              className='justify-between'
              render={<Link to='/keys' />}
            >
              <span>创建 API 密钥</span>
              <ArrowUpRight data-icon='inline-end' />
            </Button>
          )}
        </div>
      </div>
    </section>
  )
}
