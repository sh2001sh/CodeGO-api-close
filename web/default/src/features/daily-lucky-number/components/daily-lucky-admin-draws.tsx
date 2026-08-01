import { ArrowLeft, ArrowRight, RefreshCw, RotateCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { StatusBadge } from '@/components/status-badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatLuckyDateTime, formatLuckyUsd } from '../lib'
import type { LuckyBackfillResult, LuckyDrawAdminView } from '../types'
import { LuckyDigits } from './lucky-digits'

export function DailyLuckyAdminDraws(props: {
  draws: LuckyDrawAdminView[]
  page: number
  pageSize: number
  total: number
  loading: boolean
  retryingId?: number
  backfillPending: boolean
  backfillResult?: LuckyBackfillResult
  onRetry: (drawId: number) => void
  onBackfill: () => void
  onPageChange: (page: number) => void
}) {
  const { t } = useTranslation()
  const totalPages = Math.max(1, Math.ceil(props.total / props.pageSize))

  return (
    <section className='app-page-shell overflow-hidden'>
      <div className='border-border/70 flex flex-wrap items-start justify-between gap-3 border-b px-4 py-4 sm:px-5'>
        <div>
          <h2 className='text-foreground text-base font-semibold'>
            {t('Draw history and settlement')}
          </h2>
          <p className='text-muted-foreground mt-1 text-sm leading-5'>
            {t('Retry settles existing rewards with the original winning number; it never creates a new draw.')}
          </p>
        </div>
        <Button
          variant='outline'
          size='sm'
          onClick={props.onBackfill}
          disabled={props.backfillPending}
        >
          <RefreshCw
            data-icon='inline-start'
            className={props.backfillPending ? 'animate-spin' : undefined}
          />
          {props.backfillPending ? t('Backfilling...') : t('Backfill missing numbers')}
        </Button>
      </div>

      {props.backfillResult ? (
        <div className='border-border/70 bg-muted/20 border-b px-4 py-3 text-xs leading-5 sm:px-5'>
          {t('Backfill result: scanned {{scanned}}, existing {{existing}}, created {{created}}, failed {{failed}}.', {
            scanned: props.backfillResult.scanned,
            existing: props.backfillResult.already_exists,
            created: props.backfillResult.created,
            failed: props.backfillResult.failed,
          })}
          {props.backfillResult.failed_ids.length > 0 ? (
            <span className='text-destructive ml-1'>
              {t('Failed subscription IDs: {{ids}}', {
                ids: props.backfillResult.failed_ids.join(', '),
              })}
            </span>
          ) : null}
        </div>
      ) : null}

      {props.loading ? (
        <div className='space-y-3 p-4 sm:p-5'>
          <div className='bg-muted h-9 animate-pulse rounded-lg' />
          <div className='bg-muted h-44 animate-pulse rounded-lg' />
        </div>
      ) : props.draws.length === 0 ? (
        <Empty className='min-h-56 border-0'>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              <RotateCw aria-hidden='true' />
            </EmptyMedia>
            <EmptyTitle>{t('No draws have been created yet')}</EmptyTitle>
            <EmptyDescription>
              {t('The ledger-worker creates one immutable draw after the configured daily time.')}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Draw date')}</TableHead>
                <TableHead>{t('Winning number')}</TableHead>
                <TableHead>{t('Participants')}</TableHead>
                <TableHead>{t('Rewards')}</TableHead>
                <TableHead>{t('Nominal / cost')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
                <TableHead className='text-right'>{t('Action')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {props.draws.map((item) => {
                const draw = item.draw
                const canRetry = draw.status !== 'completed'
                return (
                  <TableRow key={draw.id}>
                    <TableCell>
                      <div className='text-foreground text-xs font-medium'>{draw.draw_date}</div>
                      <div className='text-muted-foreground mt-1 text-[11px]'>
                        {formatLuckyDateTime(draw.drawn_at, draw.timezone)}
                      </div>
                    </TableCell>
                    <TableCell><LuckyDigits value={draw.winning_number} /></TableCell>
                    <TableCell className='font-mono text-xs tabular-nums'>
                      {item.participant_count}
                    </TableCell>
                    <TableCell className='text-xs tabular-nums'>
                      <div>{item.credited_count}/{item.reward_count} {t('credited')}</div>
                      <div className='text-muted-foreground mt-1'>{draw.full_match_count} {t('full matches')}</div>
                    </TableCell>
                    <TableCell className='text-xs tabular-nums'>
                      <div className='font-medium'>{formatLuckyUsd(item.nominal_reward_usd)}</div>
                      <div className='text-muted-foreground mt-1'>¥{item.actual_cost_cny.toFixed(2)}</div>
                    </TableCell>
                    <TableCell>
                      <StatusBadge
                        label={getDrawStatusLabel(draw.status, t)}
                        variant={getDrawStatusVariant(draw.status)}
                        copyable={false}
                      />
                      {draw.error_message ? (
                        <div className='text-destructive mt-1 max-w-48 truncate text-[11px]' title={draw.error_message}>
                          {draw.error_message}
                        </div>
                      ) : null}
                    </TableCell>
                    <TableCell className='text-right'>
                      <Button
                        variant='outline'
                        size='sm'
                        onClick={() => props.onRetry(draw.id)}
                        disabled={!canRetry || props.retryingId === draw.id}
                      >
                        <RotateCw
                          data-icon='inline-start'
                          className={props.retryingId === draw.id ? 'animate-spin' : undefined}
                        />
                        {props.retryingId === draw.id ? t('Retrying...') : t('Retry settlement')}
                      </Button>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
          <div className='border-border/70 flex items-center justify-between border-t px-4 py-3 sm:px-5'>
            <span className='text-muted-foreground text-xs tabular-nums'>
              {t('Page {{page}} of {{total}}', { page: props.page, total: totalPages })}
            </span>
            <div className='flex items-center gap-2'>
              <Button
                variant='outline'
                size='icon-sm'
                aria-label={t('Previous page')}
                onClick={() => props.onPageChange(props.page - 1)}
                disabled={props.page <= 1}
              >
                <ArrowLeft aria-hidden='true' />
              </Button>
              <Button
                variant='outline'
                size='icon-sm'
                aria-label={t('Next page')}
                onClick={() => props.onPageChange(props.page + 1)}
                disabled={props.page >= totalPages}
              >
                <ArrowRight aria-hidden='true' />
              </Button>
            </div>
          </div>
        </>
      )}
    </section>
  )
}

function getDrawStatusLabel(status: string, t: (key: string) => string): string {
  switch (status) {
    case 'completed':
      return t('Completed')
    case 'settling':
      return t('Settling')
    case 'failed':
      return t('Failed')
    default:
      return t('Pending')
  }
}

function getDrawStatusVariant(status: string): 'success' | 'warning' | 'danger' | 'neutral' {
  switch (status) {
    case 'completed':
      return 'success'
    case 'failed':
      return 'danger'
    case 'settling':
      return 'warning'
    default:
      return 'neutral'
  }
}
