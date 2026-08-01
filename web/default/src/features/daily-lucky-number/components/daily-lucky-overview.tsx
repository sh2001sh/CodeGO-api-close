import { Check, Clock3, Gift, Info, RefreshCw, Sparkles, TicketCheck, Trophy } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { LuckyDigits } from './lucky-digits'
import { formatCountdown, formatLuckyDate } from '../lib'
import type { LuckyDrawView, LuckyNumberSelfPayload } from '../types'

export function DailyLuckyOverview(props: {
  payload: LuckyNumberSelfPayload
  countdownSeconds: number
  onRefresh: () => void
  refreshing?: boolean
}) {
  const today = props.payload.today_draw
  const previous = props.payload.previous_draw
  const drawCompleted = today?.status === 'completed'
  const drawTime = `${String(props.payload.draw_hour).padStart(2, '0')}:${String(props.payload.draw_minute).padStart(2, '0')}`

  return (
    <section className='app-page-shell overflow-hidden'>
      <div className='border-border/70 flex flex-wrap items-center justify-between gap-4 border-b px-4 py-4 sm:px-6'>
        <div className='flex min-w-0 items-center gap-3'>
          <span className='bg-primary text-primary-foreground flex size-10 shrink-0 items-center justify-center rounded-xl shadow-sm'>
            <Sparkles className='size-5' aria-hidden='true' />
          </span>
          <div className='min-w-0'>
            <div className='text-primary text-[11px] font-semibold'>CODE GO LUCKY DRAW</div>
            <h2 className='text-foreground mt-0.5 text-base font-semibold tracking-tight sm:text-lg'>今天，看看幸运号码会落在哪里</h2>
          </div>
        </div>
        <div className='flex items-center gap-2'>
          <span className={cn('inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium', props.payload.enabled ? 'border-success/20 bg-success/10 text-success' : 'border-muted-foreground/20 bg-muted text-muted-foreground')}>
            <span className={cn('size-1.5 rounded-full', props.payload.enabled ? 'bg-success' : 'bg-muted-foreground')} />
            {props.payload.enabled ? '活动进行中' : '活动暂不可用'}
          </span>
          <Button variant='ghost' size='icon-sm' onClick={props.onRefresh} disabled={props.refreshing} aria-label='刷新活动数据'>
            <RefreshCw className={cn(props.refreshing && 'animate-spin')} aria-hidden='true' />
          </Button>
        </div>
      </div>

      {!props.payload.enabled ? (
        <Alert className='mx-4 mt-4 sm:mx-6'>
          <Info aria-hidden='true' />
          <AlertDescription>活动暂时不可用，现有套餐额度和历史记录不受影响。</AlertDescription>
        </Alert>
      ) : null}

      <div className='grid xl:grid-cols-[minmax(0,1.35fr)_minmax(300px,0.65fr)]'>
        <div className='p-4 sm:p-6'>
          <div className='border-primary/25 bg-primary/[0.045] relative overflow-hidden rounded-xl border'>
            <div className='border-primary/20 flex flex-wrap items-center justify-between gap-3 border-b border-dashed px-4 py-3 sm:px-5'>
              <div className='flex items-center gap-2 text-xs font-medium'>
                <TicketCheck className='text-primary size-4' aria-hidden='true' />
                <span>今日幸运票</span>
                {today ? <span className='text-muted-foreground'>· {formatLuckyDate(today.draw_date, props.payload.timezone, 'zh-CN')}</span> : null}
              </div>
              <span className='text-muted-foreground font-mono text-xs tabular-nums'>{drawTime} · {props.payload.timezone}</span>
            </div>

            <div className='grid gap-5 px-4 py-5 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end sm:px-5 sm:py-6'>
              <div>
                <div className='text-muted-foreground text-xs'>{drawCompleted ? '今日中奖号码' : '等待今日开奖结果'}</div>
                <div className='mt-3'>
                  <LuckyDigits value={today?.winning_number} placeholder='????' size='lg' />
                </div>
                <p className='text-muted-foreground mt-3 text-xs leading-5'>从号码最右侧开始连续匹配，每期只按最高命中档位结算一次。</p>
              </div>
              <div className='border-primary/20 bg-background/70 min-w-36 rounded-lg border px-4 py-3 sm:text-right'>
                <div className='text-muted-foreground text-xs'>{drawCompleted ? '完全命中' : '距离开奖'}</div>
                <div className='text-foreground mt-1 font-mono text-2xl font-semibold tabular-nums'>
                  {drawCompleted ? today.full_match_count : formatCountdown(props.countdownSeconds)}
                </div>
                <div className='text-muted-foreground mt-1 text-[11px]'>{drawCompleted ? '份幸运大奖' : '系统自动开奖'}</div>
              </div>
            </div>

            <div className='border-primary/20 grid border-t border-dashed sm:grid-cols-3'>
              <DrawStep label='月卡自动入场' detail='号码永久保留' completed />
              <DrawStep label='每日定时开奖' detail={`${drawTime} ${props.payload.timezone}`} completed={Boolean(today)} active={!today} />
              <DrawStep label='奖励自动到账' detail='直接计入套餐额度' completed={drawCompleted} />
            </div>
          </div>

          <div className='mt-5 grid gap-3 sm:grid-cols-2'>
            <DrawSnapshot label='上期开奖' draw={previous} timezone={props.payload.timezone} emptyText='暂时还没有完成的开奖记录。' />
            <div className='border-border bg-muted/25 rounded-xl border px-4 py-4'>
              <div className='flex items-center gap-2'>
                <Gift className='text-primary size-4' aria-hidden='true' />
                <span className='text-foreground text-sm font-semibold'>奖励去向</span>
              </div>
              <p className='text-muted-foreground mt-2 text-sm leading-6'>中奖奖励不会进入钱包，而是直接增加到命中的月卡套餐额度。</p>
            </div>
          </div>
        </div>

        <aside className='border-border/70 bg-muted/20 border-t px-4 py-5 sm:px-6 xl:border-t-0 xl:border-l'>
          <div className='flex items-center gap-2'>
            <span className='bg-warning/15 text-warning flex size-8 items-center justify-center rounded-lg'>
              <Trophy className='size-4' aria-hidden='true' />
            </span>
            <div>
              <div className='text-foreground text-sm font-semibold'>本期累计奖池</div>
              <div className='text-muted-foreground text-xs'>完全命中时按规则分配</div>
            </div>
          </div>
          <div className='mt-6 font-mono text-4xl font-semibold tracking-tight tabular-nums'>${Number(props.payload.jackpot_usd || 0).toFixed(2)}</div>
          <div className='text-muted-foreground mt-2 text-sm'>奖池上限 ${Number(props.payload.jackpot_cap_usd || 0).toFixed(2)}</div>
          <div className='border-border mt-6 border-t pt-4'>
            <div className='text-muted-foreground text-xs'>开奖倒计时</div>
            <div className='text-foreground mt-1 font-mono text-2xl font-semibold tabular-nums'>{formatCountdown(props.countdownSeconds)}</div>
          </div>
          <div className='border-border mt-5 border-t pt-4 text-xs leading-5'>
            <div className='text-foreground font-medium'>今天能获得什么</div>
            <p className='text-muted-foreground mt-1.5'>有效月卡自动参与，不需要额外购买次数。中奖额度可随套餐继续使用。</p>
          </div>
        </aside>
      </div>
    </section>
  )
}

function DrawStep(props: { label: string; detail: string; completed?: boolean; active?: boolean }) {
  return (
    <div className='border-primary/20 flex items-center gap-3 px-4 py-3.5 sm:border-r sm:last:border-r-0'>
      <span className={cn('flex size-6 shrink-0 items-center justify-center rounded-full border text-[11px] font-semibold', props.completed ? 'border-success/30 bg-success/10 text-success' : props.active ? 'border-primary/30 bg-primary/10 text-primary' : 'border-muted-foreground/20 bg-background text-muted-foreground')}>
        {props.completed ? <Check className='size-3.5' aria-hidden='true' /> : props.active ? '2' : '3'}
      </span>
      <div className='min-w-0'>
        <div className='text-foreground text-xs font-medium'>{props.label}</div>
        <div className='text-muted-foreground mt-0.5 truncate text-[11px]'>{props.detail}</div>
      </div>
    </div>
  )
}

function DrawSnapshot(props: { label: string; draw?: LuckyDrawView; timezone: string; emptyText: string }) {
  return (
    <div className='border-border bg-background rounded-xl border px-4 py-4'>
      <div className='flex items-center justify-between gap-2'>
        <h3 className='text-foreground text-sm font-semibold'>{props.label}</h3>
        {props.draw ? <span className='text-muted-foreground text-[11px]'>{formatLuckyDate(props.draw.draw_date, props.timezone, 'zh-CN')}</span> : null}
      </div>
      {props.draw ? (
        <div className='mt-3 flex items-end justify-between gap-3'>
          <LuckyDigits value={props.draw.winning_number} />
          <span className='text-muted-foreground text-xs'>完全命中 <strong className='text-foreground font-mono tabular-nums'>{props.draw.full_match_count}</strong> 份</span>
        </div>
      ) : <p className='text-muted-foreground mt-3 text-sm'>{props.emptyText}</p>}
    </div>
  )
}
