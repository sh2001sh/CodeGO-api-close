import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { AlertCircle, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { SectionPageLayout } from '@/components/layout'
import {
  getDailyLuckyNumberHistory,
  getDailyLuckyNumberPublicWins,
} from './api'
import { DrawStage } from './components/draw-stage'
import { HistoryPanel } from './components/history-panel'
import { LuckyMatchBoard } from './components/lucky-match-board'
import { RewardLadder } from './components/reward-ladder'
import { DailyLuckyRulesDialog } from './components/rules-dialog'
import { TodayWinnersPanel } from './components/today-winners-panel'
import { useDailyLuckyNumberSelf } from './hooks/use-daily-lucky-number'
import { useDailyLuckyRulesDialog } from './hooks/use-daily-lucky-rules-dialog'
import {
  getMatchedDigits,
  getMembershipTierRank,
  normalizeLuckyNumber,
  normalizeMembershipTier,
} from './lib'
import type {
  LuckyNumberSelfPayload,
  LuckyPublicWinPage,
  LuckyRewardPage,
  MembershipTier,
} from './types'

/** Ladder amounts are shown for the user's strongest active card. */
function resolveTopTier(payload?: LuckyNumberSelfPayload): MembershipTier {
  if (!payload) return 'none'
  return payload.subscriptions.reduce<MembershipTier>((best, entry) => {
    const tier = normalizeMembershipTier(
      entry.subscription.membership_tier || entry.plan?.membership_tier
    )
    return getMembershipTierRank(tier) > getMembershipTierRank(best)
      ? tier
      : best
  }, 'none')
}

function resolveBestMatch(payload?: LuckyNumberSelfPayload): number {
  const winning = payload?.today_draw?.winning_number
  if (!payload || !winning) return 0
  const subscriptionBest = payload.subscriptions.reduce((best, entry) => {
    const suffix = normalizeLuckyNumber(
      entry.subscription.lucky_number?.lucky_suffix ??
        entry.number?.lucky_suffix
    )
    return Math.max(best, getMatchedDigits(suffix, winning))
  }, 0)
  return (payload.today_blind_box_numbers || []).reduce((best, entry) => {
    if (
      payload.today_draw?.drawn_at &&
      entry.created_at > payload.today_draw.drawn_at
    ) {
      return best
    }
    return Math.max(
      best,
      getMatchedDigits(normalizeLuckyNumber(entry.lucky_suffix), winning)
    )
  }, subscriptionBest)
}

export function DailyLuckyNumberPage() {
  const { t } = useTranslation()
  const selfQuery = useDailyLuckyNumberSelf()
  const [historyTab, setHistoryTab] = useState<'mine' | 'public'>('mine')
  const [historyPage, setHistoryPage] = useState(1)
  const [publicPage, setPublicPage] = useState(1)
  const [now, setNow] = useState(() => Date.now())
  const {
    open: rulesOpen,
    onOpenChange: setRulesOpen,
    openRules,
  } = useDailyLuckyRulesDialog()

  const historyQuery = useQuery({
    queryKey: ['daily-lucky-number', 'history', historyPage],
    queryFn: async (): Promise<LuckyRewardPage> => {
      const response = await getDailyLuckyNumberHistory(historyPage)
      if (!response.success || !response.data) {
        throw new Error(response.message || 'Unable to load winning history.')
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
        throw new Error(
          response.message || 'Unable to load the public winners list.'
        )
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
    () =>
      Math.max(
        0,
        Math.floor(((payload?.next_draw_at ?? 0) * 1000 - now) / 1000)
      ),
    [now, payload?.next_draw_at]
  )

  const refresh = () => {
    void selfQuery.refetch()
    void historyQuery.refetch()
    void publicWinsQuery.refetch()
  }

  const refreshing =
    selfQuery.isFetching ||
    historyQuery.isFetching ||
    publicWinsQuery.isFetching

  const topTier = useMemo(() => resolveTopTier(payload), [payload])
  const bestMatch = useMemo(() => resolveBestMatch(payload), [payload])

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Daily Lucky Number')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          size='icon-sm'
          onClick={refresh}
          disabled={refreshing}
          aria-label={t('Refresh')}
          title={t('Refresh')}
        >
          <RefreshCw
            className={
              refreshing ? 'animate-spin motion-reduce:animate-none' : undefined
            }
            aria-hidden='true'
          />
        </Button>
      </SectionPageLayout.Actions>
      <DailyLuckyRulesDialog
        open={rulesOpen}
        onOpenChange={setRulesOpen}
        rules={payload?.rules}
        timezone={payload?.timezone}
        drawHour={payload?.draw_hour}
        drawMinute={payload?.draw_minute}
      />
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
                <span>
                  {t('Unable to load the Daily Lucky Number activity.')}
                </span>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => void selfQuery.refetch()}
                >
                  {t('Retry')}
                </Button>
              </AlertDescription>
            </Alert>
          ) : payload ? (
            <>
              <DrawStage
                payload={payload}
                countdownSeconds={countdownSeconds}
                onOpenRules={openRules}
              />
              <LuckyMatchBoard
                subscriptions={payload.subscriptions}
                blindBoxNumbers={payload.today_blind_box_numbers || []}
                draw={payload.today_draw}
                rewards={payload.recent_rewards}
                rules={payload.rules}
              />
              <RewardLadder
                rules={payload.rules}
                tier={topTier}
                matchedDigits={bestMatch}
                onOpenRules={openRules}
              />
              <TodayWinnersPanel
                records={publicWinsQuery.data?.records}
                drawDate={payload.today_draw?.draw_date}
                timezone={payload.timezone}
                loading={publicWinsQuery.isLoading}
                error={publicWinsQuery.isError}
                onRetry={() => void publicWinsQuery.refetch()}
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
                previousDraw={payload.previous_draw}
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
                <span>
                  {t(
                    'No extra draw entries are sold. Rewards cannot be withdrawn, traded, or transferred.'
                  )}
                </span>
                <Button
                  variant='link'
                  size='sm'
                  render={<Link to='/packages' />}
                >
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
