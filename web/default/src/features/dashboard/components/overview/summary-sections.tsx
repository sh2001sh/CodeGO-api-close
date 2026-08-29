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
import type { ReactNode } from 'react'
import { Link } from '@tanstack/react-router'
import { ArrowRight, ArrowUpRight, WalletCards } from 'lucide-react'
import { formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { CountUp } from '@/components/count-up'
import { formatSubscriptionQuotaAmount } from '@/features/subscriptions/lib'

export type MetricDef = {
  label: string
  value: string
  numeric?: number
  format?: (value: number) => string
  hint?: string
}

export type BalanceSegment = {
  label: string
  display: string
  value: number
  dot: string
  bar: string
}

export function BalanceWorkspace(props: {
  available: string
  availableValue?: number
  currencyLabel: string
  segments: BalanceSegment[]
  metrics: MetricDef[]
}) {
  return (
    <section className='codego-panel relative overflow-hidden p-5 sm:p-6 xl:p-7'>
      <span
        aria-hidden
        className='border-primary/[0.08] pointer-events-none absolute -top-32 -right-24 size-72 rounded-full border'
      />
      <div className='grid gap-8 xl:grid-cols-[minmax(0,1.05fr)_minmax(0,0.95fr)] xl:items-stretch'>
        <div className='flex min-w-0 flex-col'>
          <div className='flex items-center gap-2.5'>
            <WalletCards className='text-primary size-3.5' />
            <span className='codego-stat-label'>可用总额度</span>
            <span className='codego-stat-label border-primary/30 text-primary ml-auto border px-2 py-0.5'>
              {props.currencyLabel}
            </span>
          </div>

          <div className='text-foreground mt-4 text-5xl leading-none font-semibold tabular-nums xl:text-6xl'>
            {props.availableValue != null ? (
              <CountUp value={props.availableValue} format={formatQuota} />
            ) : (
              props.available
            )}
          </div>

          <div className='codego-hairline mt-7' />

          <div className='mt-5 flex flex-wrap gap-2'>
            <Button
              variant='outline'
              className='justify-between'
              render={<Link to='/wallet' />}
            >
              <span>钱包</span>
              <ArrowUpRight data-icon='inline-end' />
            </Button>
            <Button
              variant='outline'
              className='justify-between'
              render={<Link to='/packages' />}
            >
              <span>套餐</span>
              <ArrowUpRight data-icon='inline-end' />
            </Button>
            <Button
              className='justify-between'
              render={<Link to='/blind-box' />}
            >
              <span>盲盒</span>
              <ArrowUpRight data-icon='inline-end' />
            </Button>
          </div>
        </div>

        <div className='codego-fact-row grid grid-cols-1 content-center sm:grid-cols-3'>
          {props.metrics.map((metric) => (
            <DataMetricCell
              key={metric.label}
              label={metric.label}
              value={metric.value}
              numeric={metric.numeric}
              format={metric.format}
            />
          ))}
        </div>
      </div>
    </section>
  )
}

export function DataMetricCell(props: {
  label: string
  value: string
  numeric?: number
  format?: (value: number) => string
}) {
  return (
    <div className='min-w-0'>
      <div className='codego-stat-label'>{props.label}</div>
      <div className='text-foreground mt-2.5 text-2xl leading-none font-semibold tabular-nums'>
        {props.numeric != null && props.format ? (
          <CountUp value={props.numeric} format={props.format} />
        ) : (
          props.value
        )}
      </div>
    </div>
  )
}

export function PackageStatusCard(props: {
  hasSubscription: boolean
  title: string
  subtitle: string
  remainingDays: number
  totalUsed: number
  totalAmount: number
  totalHint: string
  periodUsed?: number
  periodAmount?: number
  periodHint?: string
  children?: ReactNode
}) {
  const totalPercent =
    props.totalAmount > 0
      ? Math.min(100, Math.round((props.totalUsed / props.totalAmount) * 100))
      : 0
  const periodPercent =
    props.periodAmount && props.periodAmount > 0
      ? Math.min(
          100,
          Math.round(((props.periodUsed ?? 0) / props.periodAmount) * 100)
        )
      : 0

  return (
    <section className='codego-panel flex h-full flex-col p-5 sm:p-6'>
      <div className='flex items-start justify-between gap-3'>
        <div className='min-w-0'>
          <div className='codego-stat-label'>套餐</div>
          <div className='text-foreground mt-2 truncate text-xl font-semibold'>
            {props.hasSubscription ? props.title : '未订阅'}
          </div>
        </div>
        {props.hasSubscription ? (
          <div className='codego-stat-label border-primary/30 text-primary shrink-0 border px-2 py-1'>
            剩余 {props.remainingDays} 天
          </div>
        ) : null}
      </div>

      {props.hasSubscription ? (
        <div className='mt-6 flex flex-1 flex-col gap-5'>
          <QuotaRule
            label='总额度'
            used={props.totalUsed}
            total={props.totalAmount}
            percent={totalPercent}
            footnote={props.totalHint}
          />
          {props.periodAmount != null && props.periodAmount > 0 ? (
            <QuotaRule
              label='周期额度'
              used={props.periodUsed ?? 0}
              total={props.periodAmount}
              percent={periodPercent}
              footnote={props.periodHint}
            />
          ) : null}
        </div>
      ) : (
        <div className='codego-empty mt-8 mb-8 flex-1 justify-center'>
          <span aria-hidden className='bg-border mb-1 block h-8 w-px' />
          NO ACTIVE PLAN
          <Button
            size='sm'
            variant='outline'
            className='mt-2'
            render={<Link to='/packages' />}
          >
            查看套餐
          </Button>
        </div>
      )}

      <Button
        className='mt-6 justify-between'
        variant={props.hasSubscription ? 'default' : 'outline'}
        render={<Link to={props.hasSubscription ? '/wallet' : '/packages'} />}
      >
        <span>{props.hasSubscription ? '扣费与排序' : '查看可用套餐'}</span>
        <ArrowRight data-icon='inline-end' />
      </Button>
    </section>
  )
}

function QuotaRule(props: {
  label: string
  used: number
  total: number
  percent: number
  footnote?: string
}) {
  return (
    <div>
      <div className='flex items-baseline justify-between gap-3'>
        <span className='codego-stat-label'>{props.label}</span>
        <span className='text-foreground text-sm font-semibold tabular-nums'>
          {formatSubscriptionQuotaAmount(Math.max(0, props.total - props.used))}
          <span className='text-muted-foreground font-normal'>
            {' '}
            / {formatSubscriptionQuotaAmount(props.total)}
          </span>
        </span>
      </div>
      <div className='bg-border/60 mt-3 h-[3px] w-full'>
        <div
          className='bg-primary h-full transition-[width] duration-500'
          style={{ width: `${props.percent}%` }}
        />
      </div>
      {props.footnote ? (
        <div className='text-muted-foreground mt-2 text-xs tabular-nums'>
          {props.footnote}
        </div>
      ) : null}
    </div>
  )
}

export function StatusInfoCard(props: {
  label: string
  value: string
  hint: string
}) {
  return (
    <div className='min-w-0'>
      <div className='codego-stat-label'>{props.label}</div>
      <div className='text-foreground mt-1 text-base font-semibold'>
        {props.value}
      </div>
    </div>
  )
}

export function EditorialStepRow(props: {
  index: number
  title: string
  completed: boolean
}) {
  return (
    <div className='border-border/70 flex items-start gap-4 border-t py-4 first:border-t-0 first:pt-0'>
      <span
        className={cn(
          'font-mono text-[11px] tabular-nums',
          props.completed ? 'text-primary' : 'text-muted-foreground/60'
        )}
      >
        {String(props.index + 1).padStart(2, '0')}
      </span>
      <div className='min-w-0 flex-1'>
        <div className='text-foreground flex items-center gap-2 text-sm font-semibold'>
          {props.title}
          {props.completed ? (
            <span className='codego-stat-label text-primary'>DONE</span>
          ) : null}
        </div>
      </div>
    </div>
  )
}
