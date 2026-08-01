import { AlertCircle, CalendarDays, ShieldCheck, Trophy } from 'lucide-react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'
import { formatLuckyDate, formatLuckyUsd } from '../lib'
import type { LuckyPublicWin } from '../types'
import { LuckyDigits } from './lucky-digits'
import { TierBadge } from './tier-badge'

export function TodayWinnersPanel(props: {
  records?: LuckyPublicWin[]
  drawDate?: string
  timezone: string
  loading?: boolean
  error?: boolean
  onRetry: () => void
}) {
  const records = props.drawDate
    ? (props.records ?? []).filter((item) => item.draw_date === props.drawDate)
    : []

  return (
    <section className='app-page-shell overflow-hidden'>
      <div className='border-border/70 flex flex-wrap items-center justify-between gap-3 border-b px-4 py-4 sm:px-5'>
        <div className='flex items-center gap-3'>
          <span className='bg-primary/10 text-primary flex size-9 items-center justify-center rounded-lg'>
            <Trophy className='size-4' aria-hidden='true' />
          </span>
          <div>
            <h2 className='text-foreground text-base font-semibold'>今日中奖名单</h2>
            <p className='text-muted-foreground mt-0.5 text-xs'>公开展示已完成结算的中奖尾号</p>
          </div>
        </div>
        {props.drawDate ? (
          <span className='text-muted-foreground inline-flex items-center gap-1.5 text-xs'>
            <CalendarDays className='size-3.5' aria-hidden='true' />
            {formatLuckyDate(props.drawDate, props.timezone, 'zh-CN')}
          </span>
        ) : null}
      </div>

      {props.error ? (
        <Alert variant='destructive' className='m-4 sm:m-5'>
          <AlertCircle aria-hidden='true' />
          <AlertDescription className='flex flex-wrap items-center justify-between gap-3'>
            <span>中奖名单暂时加载失败，请稍后重试。</span>
            <Button variant='outline' size='sm' onClick={props.onRetry}>重试</Button>
          </AlertDescription>
        </Alert>
      ) : props.loading ? (
        <div className='space-y-3 p-4 sm:p-5'>
          <Skeleton className='h-12 w-full' />
          <Skeleton className='h-12 w-full' />
          <Skeleton className='h-12 w-full' />
        </div>
      ) : records.length === 0 ? (
        <Empty className='min-h-48 border-0'>
          <EmptyHeader>
            <EmptyMedia variant='icon'><Trophy aria-hidden='true' /></EmptyMedia>
            <EmptyTitle>{props.drawDate ? '今天暂时没有中奖记录' : '今日尚未开奖'}</EmptyTitle>
            <EmptyDescription>
              {props.drawDate ? '中奖记录将在结算完成后展示。' : '开奖后将会在这里展示当日中奖名单。'}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className='divide-border divide-y'>
          {records.map((item, index) => (
            <div key={`${item.draw_date}-${item.lucky_suffix}-${index}`} className='hover:bg-primary/[0.025] flex flex-wrap items-center gap-3 px-4 py-3.5 transition-colors sm:px-5'>
              <span className='text-primary bg-primary/10 flex size-6 items-center justify-center rounded-full font-mono text-[11px] font-semibold tabular-nums'>
                {String(index + 1).padStart(2, '0')}
              </span>
              <div className='min-w-28 flex-1'>
                <div className='text-foreground flex items-center gap-2 text-sm font-medium'>
                  <LuckyDigits value={item.lucky_suffix} />
                  <span className='text-muted-foreground text-xs'>幸运尾号</span>
                </div>
              </div>
              <div className='flex items-center gap-2'>
                <TierBadge tier={item.membership_tier} compact />
                <span className='text-muted-foreground text-xs tabular-nums'>命中 {item.matched_digits} 位</span>
              </div>
              <span className={cn('text-success ml-auto font-mono text-sm font-semibold tabular-nums')}>
                +{formatLuckyUsd(item.reward_usd)}
              </span>
            </div>
          ))}
          <div className='text-muted-foreground flex items-center gap-2 px-4 py-3 text-xs sm:px-5'>
            <ShieldCheck className='size-3.5' aria-hidden='true' />
            为保护隐私，仅展示中奖尾号和套餐档位。
          </div>
        </div>
      )}
    </section>
  )
}
