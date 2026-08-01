import { Clock3, Gift, Info, ShieldCheck, Sparkles, TicketCheck, Trophy } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { LuckyDigits } from './lucky-digits'
import { formatCountdown, formatLuckyDate, formatLuckyUsd, normalizeLuckyNumberRules } from '../lib'
import type { LuckyDrawView, LuckyNumberSelfPayload } from '../types'

export function DailyLuckyOverview(props: {
  payload: LuckyNumberSelfPayload
  countdownSeconds: number
}) {
  const today = props.payload.today_draw
  const previous = props.payload.previous_draw
  const rules = normalizeLuckyNumberRules(props.payload.rules)
  const drawTime = `${String(props.payload.draw_hour).padStart(2, '0')}:${String(props.payload.draw_minute).padStart(2, '0')}`
  const numberPublished = Boolean(today?.winning_number)
  const drawCompleted = today?.status === 'completed'
  const drawFailed = today?.status === 'failed'
  const drawSettling = numberPublished && !drawCompleted && !drawFailed
  const statusLabel = !props.payload.enabled
    ? '活动暂不可用'
    : drawCompleted
      ? '今日已结算'
      : drawFailed
        ? '开奖待处理'
        : drawSettling
          ? '结算处理中'
          : '等待今日开奖'
  const statusTone = !props.payload.enabled
    ? 'border-muted-foreground/20 bg-muted text-muted-foreground'
    : drawFailed
      ? 'border-warning/25 bg-warning/10 text-warning'
      : drawCompleted
        ? 'border-success/20 bg-success/10 text-success'
        : 'border-primary/20 bg-primary/10 text-primary'

  return (
    <section className='app-page-shell overflow-hidden'>
      <div className='border-border/70 flex flex-wrap items-start justify-between gap-4 border-b px-4 py-4 sm:px-6'>
        <div className='flex min-w-0 items-start gap-3'>
          <span className='bg-primary text-primary-foreground flex size-10 shrink-0 items-center justify-center rounded-xl shadow-sm'>
            <Sparkles className='size-5' aria-hidden='true' />
          </span>
          <div className='min-w-0'>
            <div className='text-primary text-[11px] font-semibold'>每日一次 · 自动参与</div>
            <h2 className='text-foreground mt-0.5 text-base font-semibold tracking-tight sm:text-lg'>今日开奖</h2>
            <p className='text-muted-foreground mt-1 text-xs leading-5'>全站统一四位开奖号码，中奖奖励自动进入对应月卡额度。</p>
          </div>
        </div>
        <span className={cn('inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium', statusTone)}>
          <span className={cn('size-1.5 rounded-full', drawFailed ? 'bg-warning' : drawCompleted ? 'bg-success' : 'bg-primary')} />
          {statusLabel}
        </span>
      </div>

      {!props.payload.enabled ? (
        <Alert className='mx-4 mt-4 sm:mx-6'>
          <Info aria-hidden='true' />
          <AlertDescription>活动暂时不可用，现有套餐额度和历史记录不受影响。</AlertDescription>
        </Alert>
      ) : null}

      <div className='grid xl:grid-cols-[minmax(0,1.35fr)_minmax(300px,0.65fr)]'>
        <div className='p-4 sm:p-6'>
          <div className='border-primary/25 bg-primary/[0.045] rounded-xl border'>
            <div className='border-primary/20 flex flex-wrap items-center justify-between gap-3 border-b border-dashed px-4 py-3 sm:px-5'>
              <div className='flex items-center gap-2 text-xs font-medium'>
                <TicketCheck className='text-primary size-4' aria-hidden='true' />
                <span>{today ? '今日开奖号码' : '本期开奖周期'}</span>
                {today ? <span className='text-muted-foreground'>· {formatLuckyDate(today.draw_date, props.payload.timezone, 'zh-CN')}</span> : null}
              </div>
              <span className='text-muted-foreground inline-flex items-center gap-1.5 font-mono text-xs tabular-nums'>
                <Clock3 className='size-3.5' aria-hidden='true' />
                {drawTime} · {props.payload.timezone}
              </span>
            </div>

            <div className='grid gap-5 px-4 py-5 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end sm:px-5 sm:py-6'>
              <div className='min-w-0'>
                <div className='text-muted-foreground text-xs'>
                  {drawCompleted ? '已公布并完成结算' : drawSettling ? '开奖号码已生成，正在结算' : drawFailed ? '本期结算出现异常' : '距离下次开奖'}
                </div>
                <div className='mt-3'>
                  <LuckyDigits value={numberPublished ? today?.winning_number : undefined} placeholder='????' size='lg' />
                </div>
                <p className='text-muted-foreground mt-3 text-xs leading-5'>从号码最右侧开始连续匹配，只领取最高命中档位；未命中不扣除任何月卡额度。</p>
              </div>
              <div className='border-primary/20 bg-background/70 min-w-36 rounded-lg border px-4 py-3 sm:text-right'>
                <div className='text-muted-foreground text-xs'>{drawCompleted ? '四位全中' : drawSettling ? '当前状态' : drawFailed ? '处理方式' : '距离开奖'}</div>
                <div className='text-foreground mt-1 font-mono text-2xl font-semibold tabular-nums'>
                  {drawCompleted ? `${today?.full_match_count ?? 0} 份` : drawSettling ? '处理中' : drawFailed ? '自动重试' : formatCountdown(props.countdownSeconds)}
                </div>
                <div className='text-muted-foreground mt-1 text-[11px]'>{drawCompleted ? '月卡获得全中奖励' : drawFailed ? '不会重新生成号码' : '系统自动开奖'}</div>
              </div>
            </div>
          </div>

          <div className='border-border mt-4 grid overflow-hidden rounded-xl border sm:grid-cols-3'>
            <OverviewMetric label='开奖时间' value={drawTime} detail={props.payload.timezone} />
            <OverviewMetric label='参与方式' value='月卡自动参与' detail='无需签到或购买次数' />
            <OverviewMetric label='奖励去向' value='月卡额度' detail='结算后自动入账' />
          </div>

          <div className='mt-4 grid gap-3 sm:grid-cols-2'>
            <DrawSnapshot label='上期开奖' draw={previous} timezone={props.payload.timezone} emptyText='暂时还没有完成的开奖记录。' />
            <div className='border-border bg-muted/25 rounded-xl border px-4 py-4'>
              <div className='flex items-center gap-2'>
                <Gift className='text-primary size-4' aria-hidden='true' />
                <span className='text-foreground text-sm font-semibold'>累计奖池说明</span>
              </div>
              <p className='text-muted-foreground mt-2 text-sm leading-6'>奖池初始为 {formatLuckyUsd(rules.jackpot_initial_usd)}，无人四位全中时增加 {formatLuckyUsd(rules.jackpot_increment_usd)}；有人全中后按规则分配并重置。</p>
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
              <div className='text-muted-foreground text-xs'>四位全中时由中奖月卡平分</div>
            </div>
          </div>
          <div className='mt-6 font-mono text-4xl font-semibold tracking-tight tabular-nums'>${Number(props.payload.jackpot_usd || 0).toFixed(2)}</div>
          <div className='text-muted-foreground mt-2 text-sm'>奖池上限 ${Number(props.payload.jackpot_cap_usd || 0).toFixed(2)}</div>
          <div className='border-border mt-6 border-t pt-4'>
            <div className='text-muted-foreground text-xs'>当前开奖状态</div>
            <div className='text-foreground mt-1 text-sm font-semibold'>{statusLabel}</div>
            <p className='text-muted-foreground mt-1.5 text-xs leading-5'>只有开奖快照时有效且开启权益的月卡参与本期。</p>
          </div>
          <div className='border-border mt-5 border-t pt-4 text-xs leading-5'>
            <div className='text-foreground flex items-center gap-2 font-medium'>
              <ShieldCheck className='text-primary size-3.5' aria-hidden='true' />
              奖励安全边界
            </div>
            <p className='text-muted-foreground mt-1.5'>奖励不会进入普通钱包，不可提现、交易或转让；号码和历史记录会长期保留。</p>
          </div>
        </aside>
      </div>
    </section>
  )
}

function OverviewMetric(props: { label: string; value: string; detail: string }) {
  return (
    <div className='border-border px-4 py-3.5 first:border-t-0 sm:border-l sm:first:border-l-0'>
      <div className='text-muted-foreground text-[11px]'>{props.label}</div>
      <div className='text-foreground mt-1 text-sm font-semibold'>{props.value}</div>
      <div className='text-muted-foreground mt-0.5 text-[11px]'>{props.detail}</div>
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
