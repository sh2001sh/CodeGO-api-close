import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { AlertCircle, BookOpen, RefreshCw } from 'lucide-react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { SectionPageLayout } from '@/components/layout'
import {
  getDailyLuckyNumberHistory,
  getDailyLuckyNumberPublicWins,
} from './api'
import { DailyLuckyOverview } from './components/daily-lucky-overview'
import { HistoryPanel } from './components/history-panel'
import { DailyLuckyRulesPanel } from './components/rules-panel'
import { LuckySubscriptionList } from './components/subscription-list'
import { TodayWinnersPanel } from './components/today-winners-panel'
import { useDailyLuckyNumberSelf } from './hooks/use-daily-lucky-number'
import type { LuckyPublicWinPage, LuckyRewardPage } from './types'

export function DailyLuckyNumberPage() {
  const selfQuery = useDailyLuckyNumberSelf()
  const [historyTab, setHistoryTab] = useState<'mine' | 'public'>('mine')
  const [historyPage, setHistoryPage] = useState(1)
  const [publicPage, setPublicPage] = useState(1)
  const [now, setNow] = useState(() => Date.now())
  const [rulesOpen, setRulesOpen] = useState(false)

  const historyQuery = useQuery({
    queryKey: ['daily-lucky-number', 'history', historyPage],
    queryFn: async (): Promise<LuckyRewardPage> => {
      const response = await getDailyLuckyNumberHistory(historyPage)
      if (!response.success || !response.data) {
        throw new Error(response.message || '无法加载中奖记录。')
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
        throw new Error(response.message || '无法加载公开中奖名单。')
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

  const viewRules = () => {
    setRulesOpen(true)
    window.requestAnimationFrame(() => {
      const rules = document.getElementById('daily-lucky-rules')
      if (!rules) return
      const reducedMotion = window.matchMedia(
        '(prefers-reduced-motion: reduce)'
      ).matches
      rules.scrollIntoView({
        behavior: reducedMotion ? 'auto' : 'smooth',
        block: 'start',
      })
    })
  }

  const toggleRules = () => {
    if (rulesOpen) {
      setRulesOpen(false)
      return
    }

    viewRules()
  }

  const refreshing =
    selfQuery.isFetching ||
    historyQuery.isFetching ||
    publicWinsQuery.isFetching
  const activityTimezone = payload?.timezone ?? 'Asia/Shanghai'
  const showRulesFallback = selfQuery.isError && !payload

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>每日幸运号</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        有效月卡每天自动参与一次全站开奖；号码永久保留，中奖奖励直接计入对应月卡额度。
      </SectionPageLayout.Description>
      <SectionPageLayout.Actions>
        <Button
          size='sm'
          onClick={toggleRules}
          aria-controls='daily-lucky-rules-content'
          aria-expanded={rulesOpen}
        >
          <BookOpen data-icon='inline-start' />
          {rulesOpen ? '收起完整规则' : '查看完整规则'}
        </Button>
        <Button
          variant='outline'
          size='sm'
          onClick={refresh}
          disabled={refreshing}
        >
          <RefreshCw
            className={refreshing ? 'animate-spin' : undefined}
            data-icon='inline-start'
          />
          刷新
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
                <span>每日幸运号码活动加载失败。</span>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => void selfQuery.refetch()}
                >
                  重试
                </Button>
              </AlertDescription>
            </Alert>
          ) : payload ? (
            <>
              <DailyLuckyOverview
                payload={payload}
                countdownSeconds={countdownSeconds}
              />
              <DailyLuckyRulesPanel
                open={rulesOpen}
                onOpenChange={setRulesOpen}
                rules={payload.rules}
                timezone={payload.timezone}
                drawHour={payload.draw_hour}
                drawMinute={payload.draw_minute}
              />
              <div className='grid gap-5 xl:grid-cols-[minmax(0,1.1fr)_minmax(320px,0.9fr)]'>
                <LuckySubscriptionList
                  subscriptions={payload.subscriptions}
                  draw={payload.today_draw}
                  rewards={payload.recent_rewards}
                  rules={payload.rules}
                />
                <TodayWinnersPanel
                  records={publicWinsQuery.data?.records}
                  drawDate={payload.today_draw?.draw_date}
                  timezone={payload.timezone}
                  loading={publicWinsQuery.isLoading}
                  error={publicWinsQuery.isError}
                  onRetry={() => void publicWinsQuery.refetch()}
                />
              </div>
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
                <span>无需额外购买抽奖次数，奖励不可提现、交易或转让。</span>
                <Button
                  variant='link'
                  size='sm'
                  render={<Link to='/packages' />}
                >
                  查看套餐
                </Button>
              </div>
            </>
          ) : null}
          {showRulesFallback ? (
            <DailyLuckyRulesPanel
              open={rulesOpen}
              onOpenChange={setRulesOpen}
              timezone={activityTimezone}
            />
          ) : null}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
