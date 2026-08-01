import { AlertCircle, ArrowLeft, ArrowRight, History, Trophy } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatLuckyDate, formatLuckyUsd } from '../lib'
import type { LuckyPublicWinPage, LuckyRewardPage } from '../types'
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
  historyLoading?: boolean
  publicWinsLoading?: boolean
  historyError?: boolean
  publicWinsError?: boolean
  onRetry: () => void
}) {
  const { t, i18n } = useTranslation()
  const page = props.tab === 'mine' ? props.history : props.publicWins
  const currentPage = props.tab === 'mine' ? props.historyPage : props.publicPage
  const loading = props.tab === 'mine' ? props.historyLoading : props.publicWinsLoading
  const error = props.tab === 'mine' ? props.historyError : props.publicWinsError
  const totalPages = page ? Math.max(1, Math.ceil(page.total / page.page_size)) : 1

  return (
    <section className='app-page-shell overflow-hidden'>
      <div className='border-border/70 flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3 sm:px-5'>
        <div className='flex items-center gap-2'>
          <History className='text-primary size-4' aria-hidden='true' />
          <h2 className='text-foreground text-base font-semibold'>{t('Draw history')}</h2>
        </div>
        <div className='bg-muted inline-flex rounded-lg p-1' role='tablist' aria-label={t('Draw history views')}>
          <button
            type='button'
            role='tab'
            aria-selected={props.tab === 'mine'}
            aria-controls='daily-lucky-history-panel'
            className={cn('rounded-md px-3 py-1.5 text-xs font-medium transition-colors', props.tab === 'mine' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground')}
            onClick={() => props.onTabChange('mine')}
          >
            {t('My records')}
          </button>
          <button
            type='button'
            role='tab'
            aria-selected={props.tab === 'public'}
            aria-controls='daily-lucky-history-panel'
            className={cn('rounded-md px-3 py-1.5 text-xs font-medium transition-colors', props.tab === 'public' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground')}
            onClick={() => props.onTabChange('public')}
          >
            {t('Public wins')}
          </button>
        </div>
      </div>

      <div id='daily-lucky-history-panel' role='tabpanel' aria-label={props.tab === 'mine' ? t('My records') : t('Public wins')}>
      {error ? (
        <Alert variant='destructive' className='m-4 sm:m-5'>
          <AlertCircle aria-hidden='true' />
          <AlertDescription className='flex flex-wrap items-center justify-between gap-3'>
            <span>{props.tab === 'mine' ? t('Unable to load your draw history.') : t('Unable to load public wins.')}</span>
            <Button variant='outline' size='sm' onClick={props.onRetry}>
              {t('Try again')}
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
              {props.tab === 'mine' ? <History aria-hidden='true' /> : <Trophy aria-hidden='true' />}
            </EmptyMedia>
            <EmptyTitle>{props.tab === 'mine' ? t('No winning records yet') : t('No public wins yet')}</EmptyTitle>
            <EmptyDescription>
              {props.tab === 'mine'
                ? t('Only matching draws are listed here. Participation and quota settlement happen automatically.')
                : t('Large wins appear here after their quota settlement is complete.')}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Date')}</TableHead>
                <TableHead>{t('Winning number')}</TableHead>
                <TableHead>{props.tab === 'mine' ? t('Your suffix') : t('Card suffix')}</TableHead>
                <TableHead>{t('Result')}</TableHead>
                <TableHead className='text-right'>{t('Reward')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {props.tab === 'mine'
                ? props.history?.records.map((item) => (
                    <TableRow key={item.reward.id}>
                      <TableCell className='text-muted-foreground text-xs'>
                        {formatLuckyDate(item.draw_date, props.timezone, i18n.language)}
                      </TableCell>
                      <TableCell><LuckyDigits value={item.winning_number} /></TableCell>
                      <TableCell className='font-mono text-sm tabular-nums'>{item.reward.lucky_number}</TableCell>
                      <TableCell>
                        <span className='text-success text-xs font-medium'>
                          {t('{{count}} digits', { count: item.reward.matched_digits })}
                        </span>
                      </TableCell>
                      <TableCell className='text-right font-mono text-sm font-semibold tabular-nums'>
                        {formatLuckyUsd(item.reward_usd)}
                      </TableCell>
                    </TableRow>
                  ))
                : props.publicWins?.records.map((item, index) => (
                    <TableRow key={`${item.draw_date}-${item.lucky_suffix}-${index}`}>
                      <TableCell className='text-muted-foreground text-xs'>
                        {formatLuckyDate(item.draw_date, props.timezone, i18n.language)}
                      </TableCell>
                      <TableCell><LuckyDigits value={item.winning_number} /></TableCell>
                      <TableCell className='font-mono text-sm tabular-nums'>{item.lucky_suffix}</TableCell>
                      <TableCell className='text-xs'>
                        <div className='flex items-center gap-2'>
                          <TierBadge tier={item.membership_tier} compact />
                          <span>{t('{{count}} digits', { count: item.matched_digits })}</span>
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
              {t('Page {{page}} of {{total}}', { page: currentPage, total: totalPages })}
            </span>
            <div className='flex items-center gap-2'>
              <Button
                variant='outline'
                size='icon-sm'
                onClick={() => props.onPageChange(currentPage - 1)}
                disabled={currentPage <= 1}
                aria-label={t('Previous page')}
              >
                <ArrowLeft aria-hidden='true' />
              </Button>
              <Button
                variant='outline'
                size='icon-sm'
                onClick={() => props.onPageChange(currentPage + 1)}
                disabled={currentPage >= totalPages}
                aria-label={t('Next page')}
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
