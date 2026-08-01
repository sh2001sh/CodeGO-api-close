import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { AlertCircle, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { getDailyLuckyNumberHistory, getDailyLuckyNumberPublicWins } from './api'
import { DailyLuckyOverview } from './components/daily-lucky-overview'
import { HistoryPanel } from './components/history-panel'
import { LuckySubscriptionList } from './components/subscription-list'
import { useDailyLuckyNumberSelf } from './hooks/use-daily-lucky-number'
import type { LuckyPublicWinPage, LuckyRewardPage } from './types'

export function DailyLuckyNumberPage() {
  const { t } = useTranslation()
  const selfQuery = useDailyLuckyNumberSelf()
  const [historyTab, setHistoryTab] = useState<'mine' | 'public'>('mine')
  const [historyPage, setHistoryPage] = useState(1)
  const [publicPage, setPublicPage] = useState(1)
  const [now, setNow] = useState(() => Date.now())

  const historyQuery = useQuery({
    queryKey: ['daily-lucky-number', 'history', historyPage],
    queryFn: async (): Promise<LuckyRewardPage> => {
      const response = await getDailyLuckyNumberHistory(historyPage)
      if (!response.success || !response.data) {
        throw new Error(response.message || 'Unable to load draw history.')
      }
      return response.data
    },
    staleTime: 60 * 1000,
  })

  const publicWinsQuery = useQuery({
    queryKey: ['daily-lucky-number', 'public-wins', publicPage],
    queryFn: async (): Promise<LuckyPublicWinPage> => {
      const response = await getDailyLuckyNumberPublicWins(publicPage)
      if (!response.success || !response.data) {
        throw new Error(response.message || 'Unable to load public wins.')
      }
      return response.data
    },
    staleTime: 60 * 1000,
  })

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [])

  const payload = selfQuery.data
  const countdownSeconds = useMemo(
    () => Math.max(0, Math.floor(((payload?.next_draw_at ?? 0) * 1000 - now) / 1000)),
    [now, payload?.next_draw_at]
  )

  const refresh = () => {
    void selfQuery.refetch()
    void historyQuery.refetch()
    void publicWinsQuery.refetch()
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Daily Lucky Number')}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t('A quiet daily benefit for eligible monthly subscriptions: one shared draw, automatic settlement, and a permanent card number.')}
      </SectionPageLayout.Description>
      <SectionPageLayout.Actions>
        <Button variant='outline' size='sm' onClick={refresh} disabled={selfQuery.isFetching}>
          <RefreshCw className={selfQuery.isFetching ? 'animate-spin' : undefined} data-icon='inline-start' />
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='mx-auto flex w-full max-w-7xl flex-col gap-5'>
          {selfQuery.isLoading && !payload ? (
            <div className='space-y-4'>
              <Skeleton className='h-64 w-full rounded-2xl' />
              <Skeleton className='h-40 w-full rounded-2xl' />
            </div>
          ) : selfQuery.isError ? (
            <Alert variant='destructive'>
              <AlertCircle aria-hidden='true' />
              <AlertDescription className='flex flex-wrap items-center justify-between gap-3'>
                <span>{t('Unable to load the daily lucky number activity.')}</span>
                <Button variant='outline' size='sm' onClick={() => void selfQuery.refetch()}>
                  {t('Try again')}
                </Button>
              </AlertDescription>
            </Alert>
          ) : payload ? (
            <>
              <DailyLuckyOverview
                payload={payload}
                countdownSeconds={countdownSeconds}
                onRefresh={refresh}
                refreshing={selfQuery.isFetching}
              />
              <LuckySubscriptionList
                subscriptions={payload.subscriptions}
                draw={payload.today_draw}
                rewards={payload.recent_rewards}
              />
              <HistoryPanel
                tab={historyTab}
                onTabChange={setHistoryTab}
                history={historyQuery.data}
                publicWins={publicWinsQuery.data}
                historyPage={historyPage}
                publicPage={publicPage}
                historyLoading={historyQuery.isLoading}
                publicWinsLoading={publicWinsQuery.isLoading}
                historyError={historyQuery.isError}
                publicWinsError={publicWinsQuery.isError}
                onRetry={() => {
                  if (historyTab === 'mine') void historyQuery.refetch()
                  else void publicWinsQuery.refetch()
                }}
                onPageChange={(page) => {
                  if (historyTab === 'mine') setHistoryPage(page)
                  else setPublicPage(page)
                }}
                timezone={payload.timezone}
              />
              <div className='text-muted-foreground flex flex-wrap items-center justify-between gap-2 text-xs leading-5'>
                <span>{t('No purchase of extra draw entries is available. Rewards cannot be withdrawn, traded, or transferred.')}</span>
                <Button variant='link' size='sm' render={<Link to='/packages' />}>
                  {t('View plans')}
                </Button>
              </div>
            </>
          ) : null}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
