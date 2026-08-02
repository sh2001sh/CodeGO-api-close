import { useMemo, useState } from 'react'
import {
  AlertCircle,
  CalendarDays,
  ChevronDown,
  ChevronUp,
  ShieldCheck,
  Trophy,
} from 'lucide-react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { formatLuckyDate } from '../lib'
import type { LuckyPublicWin } from '../types'
import {
  type WinnerFilter,
  WinnerFilters,
  WinnerList,
} from './today-winners-list'

const INITIAL_VISIBLE_COUNT = 8
export function TodayWinnersPanel(props: {
  records?: LuckyPublicWin[]
  drawDate?: string
  timezone: string
  loading?: boolean
  error?: boolean
  onRetry: () => void
}) {
  const view = useWinnerPanelView(props.records, props.drawDate)

  return (
    <section className='app-page-shell overflow-hidden'>
      <PanelHeader
        drawDate={props.drawDate}
        timezone={props.timezone}
        count={view.records.length}
      />
      {props.error ? (
        <ErrorState onRetry={props.onRetry} />
      ) : props.loading ? (
        <LoadingState />
      ) : view.records.length === 0 ? (
        <EmptyState hasDraw={Boolean(props.drawDate)} />
      ) : (
        <>
          <WinnerFilters
            filter={view.filter}
            counts={view.counts}
            total={view.records.length}
            onChange={(value) => {
              view.setFilter(value)
              view.setExpanded(false)
            }}
          />
          {view.visible.length > 0 ? (
            <WinnerList records={view.visible} />
          ) : (
            <div className='text-muted-foreground px-4 py-8 text-center text-sm'>
              今日暂无该档位的中奖记录
            </div>
          )}
          <PanelFooter
            expanded={view.expanded}
            hasMore={view.filtered.length > INITIAL_VISIBLE_COUNT}
            hiddenCount={view.filtered.length - INITIAL_VISIBLE_COUNT}
            pageMayBeTruncated={view.pageMayBeTruncated}
            onToggle={() => view.setExpanded((value) => !value)}
          />
        </>
      )}
    </section>
  )
}

function useWinnerPanelView(
  records: LuckyPublicWin[] | undefined,
  drawDate: string | undefined
) {
  const [filter, setFilter] = useState<WinnerFilter>('all')
  const [expanded, setExpanded] = useState(false)
  const todayRecords = useMemo(
    () =>
      drawDate
        ? (records ?? [])
            .filter((item) => item.draw_date === drawDate)
            .sort((a, b) => b.matched_digits - a.matched_digits)
        : [],
    [drawDate, records]
  )
  const counts = useMemo(
    () =>
      [1, 2, 3, 4].map(
        (digits) =>
          todayRecords.filter((item) => item.matched_digits === digits).length
      ),
    [todayRecords]
  )
  const filtered =
    filter === 'all'
      ? todayRecords
      : todayRecords.filter((item) => item.matched_digits === filter)

  return {
    filter,
    setFilter,
    expanded,
    setExpanded,
    records: todayRecords,
    counts,
    filtered,
    visible: expanded ? filtered : filtered.slice(0, INITIAL_VISIBLE_COUNT),
    pageMayBeTruncated:
      todayRecords.length > 0 &&
      todayRecords.length === (records?.length ?? 0) &&
      todayRecords.length >= 20,
  }
}

function PanelHeader(props: {
  drawDate?: string
  timezone: string
  count: number
}) {
  return (
    <div className='border-border/70 flex flex-wrap items-center justify-between gap-3 border-b px-4 py-4 sm:px-5'>
      <div className='flex items-center gap-3'>
        <span className='bg-primary/10 text-primary flex size-9 items-center justify-center rounded-lg'>
          <Trophy className='size-4' aria-hidden='true' />
        </span>
        <div>
          <h2 className='text-foreground text-base font-semibold'>
            今日中奖名单
          </h2>
          <p className='text-muted-foreground mt-0.5 text-xs'>
            {props.count > 0
              ? `已加载 ${props.count} 条中奖记录`
              : '按命中位数查看奖励结果'}
          </p>
        </div>
      </div>
      {props.drawDate ? (
        <span className='text-muted-foreground inline-flex items-center gap-1.5 text-xs'>
          <CalendarDays className='size-3.5' aria-hidden='true' />
          {formatLuckyDate(props.drawDate, props.timezone, 'zh-CN')}
        </span>
      ) : null}
    </div>
  )
}

function PanelFooter(props: {
  expanded: boolean
  hasMore: boolean
  hiddenCount: number
  pageMayBeTruncated: boolean
  onToggle: () => void
}) {
  return (
    <div className='border-border/70 bg-muted/15 flex flex-wrap items-center justify-between gap-2 border-t px-4 py-3 sm:px-5'>
      <span className='text-muted-foreground inline-flex items-center gap-2 text-xs'>
        <ShieldCheck className='size-3.5' aria-hidden='true' />
        {props.pageMayBeTruncated
          ? '名单较长，完整结果可在下方历史名单中分页查看'
          : '仅展示中奖尾号和套餐档位'}
      </span>
      {props.hasMore ? (
        <Button variant='ghost' size='sm' onClick={props.onToggle}>
          {props.expanded ? (
            <ChevronUp data-icon='inline-start' />
          ) : (
            <ChevronDown data-icon='inline-start' />
          )}
          {props.expanded ? '收起名单' : `再看 ${props.hiddenCount} 条`}
        </Button>
      ) : null}
    </div>
  )
}

function ErrorState(props: { onRetry: () => void }) {
  return (
    <Alert variant='destructive' className='m-4 sm:m-5'>
      <AlertCircle aria-hidden='true' />
      <AlertDescription className='flex flex-wrap items-center justify-between gap-3'>
        <span>中奖名单暂时加载失败，请稍后重试。</span>
        <Button variant='outline' size='sm' onClick={props.onRetry}>
          重试
        </Button>
      </AlertDescription>
    </Alert>
  )
}

function LoadingState() {
  return (
    <div className='space-y-3 p-4 sm:p-5'>
      <Skeleton className='h-14 w-full' />
      <Skeleton className='h-28 w-full' />
    </div>
  )
}

function EmptyState(props: { hasDraw: boolean }) {
  return (
    <Empty className='min-h-48 border-0'>
      <EmptyHeader>
        <EmptyMedia variant='icon'>
          <Trophy aria-hidden='true' />
        </EmptyMedia>
        <EmptyTitle>
          {props.hasDraw ? '今天暂时没有中奖记录' : '今日尚未开奖'}
        </EmptyTitle>
        <EmptyDescription>
          {props.hasDraw
            ? '中奖记录将在结算完成后按命中档位展示。'
            : '开奖后将会在这里展示当日中奖名单。'}
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}
