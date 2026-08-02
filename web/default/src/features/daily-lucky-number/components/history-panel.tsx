import {
  AlertCircle,
  ArrowLeft,
  ArrowRight,
  History,
  Trophy,
} from 'lucide-react'
import { cn } from '@/lib/utils'
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatLuckyDate, formatLuckyUsd } from '../lib'
import type {
  LuckyDrawView,
  LuckyPublicWinPage,
  LuckyRewardPage,
} from '../types'
import { LuckyDigits } from './lucky-digits'
import { TierBadge } from './tier-badge'

export function HistoryPanel(props: {
  tab: 'mine' | 'public'
  onTabChange: (tab: 'mine' | 'public') => void
  history?: LuckyRewardPage
  publicWins?: LuckyPublicWinPage
  historyPage: number
  publicPage: number
  onPageChange: (page: number) => void
  timezone: string
  previousDraw?: LuckyDrawView
  historyLoading?: boolean
  publicWinsLoading?: boolean
  historyError?: boolean
  publicWinsError?: boolean
  onRetry: () => void
}) {
  const page = props.tab === 'mine' ? props.history : props.publicWins
  const currentPage =
    props.tab === 'mine' ? props.historyPage : props.publicPage
  const loading =
    props.tab === 'mine' ? props.historyLoading : props.publicWinsLoading
  const error =
    props.tab === 'mine' ? props.historyError : props.publicWinsError
  const totalPages = page
    ? Math.max(1, Math.ceil(page.total / page.page_size))
    : 1

  return (
    <section className='app-page-shell overflow-hidden'>
      <div className='border-border/70 flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3 sm:px-5'>
        <div className='flex items-center gap-2'>
          <History className='text-primary size-4' aria-hidden='true' />
          <h2 className='text-foreground text-base font-semibold'>中奖记录</h2>
        </div>
        <div
          className='bg-muted inline-flex rounded-lg p-1'
          role='tablist'
          aria-label='中奖记录视图'
        >
          <button
            type='button'
            role='tab'
            aria-selected={props.tab === 'mine'}
            aria-controls='daily-lucky-history-panel'
            className={cn(
              'rounded-md px-3 py-1.5 text-xs font-medium transition-colors',
              props.tab === 'mine'
                ? 'bg-background text-foreground shadow-sm'
                : 'text-muted-foreground hover:text-foreground'
            )}
            onClick={() => props.onTabChange('mine')}
          >
            我的中奖记录
          </button>
          <button
            type='button'
            role='tab'
            aria-selected={props.tab === 'public'}
            aria-controls='daily-lucky-history-panel'
            className={cn(
              'rounded-md px-3 py-1.5 text-xs font-medium transition-colors',
              props.tab === 'public'
                ? 'bg-background text-foreground shadow-sm'
                : 'text-muted-foreground hover:text-foreground'
            )}
            onClick={() => props.onTabChange('public')}
          >
            历史中奖名单
          </button>
        </div>
      </div>

      {props.previousDraw ? (
        <div className='border-border/70 bg-muted/20 flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3 sm:px-5'>
          <div className='flex items-center gap-3'>
            <span className='text-muted-foreground text-xs'>上期开奖</span>
            <LuckyDigits value={props.previousDraw.winning_number} />
            <span className='text-muted-foreground text-xs'>
              {formatLuckyDate(
                props.previousDraw.draw_date,
                props.timezone,
                'zh-CN'
              )}
            </span>
          </div>
          <span className='text-muted-foreground text-xs tabular-nums'>
            四位全中{' '}
            <strong className='text-foreground font-mono'>
              {props.previousDraw.full_match_count}
            </strong>{' '}
            份
          </span>
        </div>
      ) : null}

      <div
        id='daily-lucky-history-panel'
        role='tabpanel'
        aria-label={props.tab === 'mine' ? '我的中奖记录' : '历史中奖名单'}
      >
        {error ? (
          <Alert variant='destructive' className='m-4 sm:m-5'>
            <AlertCircle aria-hidden='true' />
            <AlertDescription className='flex flex-wrap items-center justify-between gap-3'>
              <span>
                {props.tab === 'mine'
                  ? '个人中奖记录加载失败。'
                  : '历史中奖名单加载失败。'}
              </span>
              <Button variant='outline' size='sm' onClick={props.onRetry}>
                重试
              </Button>
            </AlertDescription>
          </Alert>
        ) : loading ? (
          <div className='space-y-3 p-4 sm:p-5'>
            <Skeleton className='h-9 w-full' />
            <Skeleton className='h-40 w-full' />
          </div>
        ) : !page || page.records.length === 0 ? (
          <Empty className='min-h-56 border-0'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                {props.tab === 'mine' ? (
                  <History aria-hidden='true' />
                ) : (
                  <Trophy aria-hidden='true' />
                )}
              </EmptyMedia>
              <EmptyTitle>
                {props.tab === 'mine'
                  ? '暂时没有中奖记录'
                  : '暂时没有公开中奖名单'}
              </EmptyTitle>
              <EmptyDescription>
                {props.tab === 'mine'
                  ? '命中的开奖记录会显示在这里，参与和额度结算均由系统自动完成。'
                  : '中奖记录完成额度结算后会在这里公开展示。'}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>日期</TableHead>
                  <TableHead>中奖号码</TableHead>
                  <TableHead>
                    {props.tab === 'mine' ? '我的尾号' : '中奖尾号'}
                  </TableHead>
                  <TableHead>结果</TableHead>
                  <TableHead className='text-right'>奖励</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {props.tab === 'mine'
                  ? props.history?.records.map((item) => (
                      <TableRow key={item.reward.id}>
                        <TableCell className='text-muted-foreground text-xs'>
                          {formatLuckyDate(
                            item.draw_date,
                            props.timezone,
                            'zh-CN'
                          )}
                        </TableCell>
                        <TableCell>
                          <LuckyDigits value={item.winning_number} />
                        </TableCell>
                        <TableCell className='font-mono text-sm tabular-nums'>
                          {item.reward.lucky_number}
                        </TableCell>
                        <TableCell>
                          <span className='text-success text-xs font-medium'>
                            命中 {item.reward.matched_digits} 位
                          </span>
                        </TableCell>
                        <TableCell className='text-right font-mono text-sm font-semibold tabular-nums'>
                          {formatLuckyUsd(item.reward_usd)}
                        </TableCell>
                      </TableRow>
                    ))
                  : props.publicWins?.records.map((item, index) => (
                      <TableRow
                        key={`${item.draw_date}-${item.lucky_suffix}-${index}`}
                      >
                        <TableCell className='text-muted-foreground text-xs'>
                          {formatLuckyDate(
                            item.draw_date,
                            props.timezone,
                            'zh-CN'
                          )}
                        </TableCell>
                        <TableCell>
                          <LuckyDigits value={item.winning_number} />
                        </TableCell>
                        <TableCell className='font-mono text-sm tabular-nums'>
                          {item.lucky_suffix}
                        </TableCell>
                        <TableCell className='text-xs'>
                          <div className='flex items-center gap-2'>
                            <TierBadge tier={item.membership_tier} compact />
                            <span>命中 {item.matched_digits} 位</span>
                          </div>
                        </TableCell>
                        <TableCell className='text-right font-mono text-sm font-semibold tabular-nums'>
                          {formatLuckyUsd(item.reward_usd)}
                        </TableCell>
                      </TableRow>
                    ))}
              </TableBody>
            </Table>
            <div className='border-border/70 flex items-center justify-between border-t px-4 py-3 sm:px-5'>
              <span className='text-muted-foreground text-xs tabular-nums'>
                第 {currentPage} / {totalPages} 页
              </span>
              <div className='flex items-center gap-2'>
                <Button
                  variant='outline'
                  size='icon-sm'
                  onClick={() => props.onPageChange(currentPage - 1)}
                  disabled={currentPage <= 1}
                  aria-label='上一页'
                >
                  <ArrowLeft aria-hidden='true' />
                </Button>
                <Button
                  variant='outline'
                  size='icon-sm'
                  onClick={() => props.onPageChange(currentPage + 1)}
                  disabled={currentPage >= totalPages}
                  aria-label='下一页'
                >
                  <ArrowRight aria-hidden='true' />
                </Button>
              </div>
            </div>
          </>
        )}
      </div>
    </section>
  )
}
