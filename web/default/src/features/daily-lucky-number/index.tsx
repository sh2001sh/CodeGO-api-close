import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { AlertCircle, BookOpen, Info, RefreshCw } from 'lucide-react'
import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { getDailyLuckyNumberHistory, getDailyLuckyNumberPublicWins } from './api'
import { DailyLuckyOverview } from './components/daily-lucky-overview'
import { HistoryPanel } from './components/history-panel'
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
      <SectionPageLayout.Title>每日幸运号码</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        月卡用户自动参与每日开奖，号码永久保留，中奖奖励直接计入对应套餐额度。
      </SectionPageLayout.Description>
      <SectionPageLayout.Actions>
        <Button variant='outline' size='sm' onClick={() => setRulesOpen(true)}>
          <Info data-icon='inline-start' />
          查看规则
        </Button>
        <Button variant='outline' size='sm' onClick={refresh} disabled={selfQuery.isFetching}>
          <RefreshCw className={selfQuery.isFetching ? 'animate-spin' : undefined} data-icon='inline-start' />
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
                <Button variant='outline' size='sm' onClick={() => void selfQuery.refetch()}>
                  重试
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
              <TodayWinnersPanel
                records={publicWinsQuery.data?.records}
                drawDate={payload.today_draw?.draw_date}
                timezone={payload.timezone}
                loading={publicWinsQuery.isLoading}
                error={publicWinsQuery.isError}
                onRetry={() => void publicWinsQuery.refetch()}
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
                <span>无需额外购买抽奖次数，奖励不可提现、交易或转让。</span>
                <Button variant='link' size='sm' render={<Link to='/packages' />}>
                  查看套餐
                </Button>
              </div>
            </>
          ) : null}
        </div>
      </SectionPageLayout.Content>
      <Dialog open={rulesOpen} onOpenChange={setRulesOpen}>
        <DialogContent className='max-h-[min(700px,calc(100vh-2rem))] max-w-lg overflow-y-auto'>
          <DialogHeader className='text-left'>
            <DialogTitle className='flex items-center gap-2 text-lg'>
              <BookOpen className='text-primary size-5' aria-hidden='true' />
              每日幸运号码规则
            </DialogTitle>
            <DialogDescription>请以页面显示的活动状态和开奖时间为准。</DialogDescription>
          </DialogHeader>
          <div className='space-y-4 text-sm leading-6'>
            <RuleItem title='1. 自动参与'>月卡套餐会自动获得一个专属号码，无需额外购买抽奖次数。只有处于有效期内的月卡参与后续开奖。</RuleItem>
            <RuleItem title='2. 每日开奖'>系统按照页面显示的时区和时间每日开奖。开奖完成并结算后，中奖号码和当日名单会公开展示。</RuleItem>
            <RuleItem title='3. 命中方式'>从号码最右侧开始连续匹配，匹配位数越多，奖励档位越高；同一号码每期只按最高命中档位结算一次。</RuleItem>
            <RuleItem title='4. 奖励发放'>奖励直接增加到命中的月卡套餐额度，不进入钱包余额，不能提现、交易或转让。</RuleItem>
            <RuleItem title='5. 号码与记录'>号码一经生成会永久保留；套餐过期后仍可查看历史记录，但不会继续参与新的开奖。</RuleItem>
          </div>
          <DialogFooter showCloseButton />
        </DialogContent>
      </Dialog>
    </SectionPageLayout>
  )
}

function RuleItem(props: { title: string; children: ReactNode }) {
  return (
    <div className='bg-muted/35 rounded-lg border px-3.5 py-3'>
      <div className='text-foreground font-semibold'>{props.title}</div>
      <p className='text-muted-foreground mt-1'>{props.children}</p>
    </div>
  )
}
