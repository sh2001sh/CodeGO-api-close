import { lazy, Suspense, useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { getIntlLocale } from '@/i18n/format'
import {
  ChartNoAxesCombined,
  Users,
  ScrollText,
  ShieldAlert,
  Store,
  ArrowLeft,
  ChevronLeft,
  ChevronRight,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SiteSeo } from '@/components/seo'
import '@/features/channels/admin-workspace.css'
import { ChannelCreateDialog } from '@/features/marketplace/components/channel-create-dialog'
import { OwnerChannels } from '@/features/marketplace/components/owner-channels'
import { OwnerIncomeOverview } from '@/features/marketplace/components/owner-income-overview'
import { ShopProfile } from '@/features/marketplace/components/shop-profile'
import { useMyMarketplaceChannels } from '@/features/marketplace/hooks'
import { formatMultiplier } from '@/features/marketplace/lib/format'
import './marketplace-admin.css'

const OwnerOperationsPanel = lazy(() =>
  import('@/features/marketplace/components/owner-operations-panel').then(
    (module) => ({ default: module.OwnerOperationsPanel })
  )
)
const OwnerChannelUsageLogs = lazy(() =>
  import('@/features/usage-logs/components/owner-channel-usage-logs').then(
    (module) => ({ default: module.OwnerChannelUsageLogs })
  )
)
const SecurityAuditPanel = lazy(() =>
  import('@/features/security-audit/security-audit-panel').then((module) => ({
    default: module.SecurityAuditPanel,
  }))
)

export function ChannelWorkbench() {
  const { t } = useTranslation()
  const [tab, setTab] = useState('channels')
  const [showCreate, setShowCreate] = useState(false)
  const [incomeSearch, setIncomeSearch] = useState('')
  const [incomePagination, setIncomePagination] = useState({
    scope: '',
    page: 1,
  })
  const [userFocus, setUserFocus] = useState<{
    channelId: string
    userId: number
    nonce: number
  }>()
  const [logFocus, setLogFocus] = useState<{
    channelId: string
    requestId: string
    userId: number
    nonce: number
  }>()
  const channels = useMyMarketplaceChannels()
  const owned = useMemo(() => channels.data ?? [], [channels.data])
  const active = owned.filter(
    (item) => !item.deleted_at && item.lifecycle_status === 'active'
  ).length
  const needsAttention = owned.filter(
    (item) => !item.deleted_at && item.needs_attention
  ).length
  const income = owned.reduce((sum, item) => sum + (item.total_income || 0), 0)
  const rankedChannels = useMemo(
    () =>
      [...owned].sort((a, b) => (b.total_income || 0) - (a.total_income || 0)),
    [owned]
  )
  const maxIncome = Math.max(1, rankedChannels[0]?.total_income || 0)
  const topChannels = useMemo(() => {
    const keyword = incomeSearch.trim().toLowerCase()
    if (!keyword) return rankedChannels
    return rankedChannels.filter((channel) =>
      channel.search_text.includes(keyword)
    )
  }, [rankedChannels, incomeSearch])
  const incomePageCount = Math.max(1, Math.ceil(topChannels.length / 50))
  const incomePage = Math.min(
    incomePagination.scope === incomeSearch ? incomePagination.page : 1,
    incomePageCount
  )
  if (
    incomePagination.scope !== incomeSearch ||
    incomePagination.page !== incomePage
  ) {
    setIncomePagination({ scope: incomeSearch, page: incomePage })
  }
  const incomePageChannels = topChannels.slice(
    (incomePage - 1) * 50,
    incomePage * 50
  )
  const setIncomePage = (value: number) => {
    if (!Number.isFinite(value)) return
    setIncomePagination({
      scope: incomeSearch,
      page: Math.max(1, Math.min(incomePageCount, Math.trunc(value))),
    })
  }
  return (
    <main
      className='admin-workspace owner-workspace w-full'
      aria-label={t('渠道主管理')}
    >
      <SiteSeo
        title={t('渠道主工作台')}
        description={t('先看渠道状态与收益，再处理用户和调用问题。')}
        canonicalPath='/channel-workbench'
        robots='noindex,follow'
      />
      <Tabs value={tab} onValueChange={setTab} className='gap-4'>
        <header className='owner-workspace-header'>
          <div>
            <h1>{t('渠道主工作台')}</h1>
          </div>
          <div className='flex flex-wrap items-center gap-3'>
            <Link
              to='/market'
              className='inline-flex min-h-11 items-center gap-2 px-3 text-sm'
            >
              <ArrowLeft className='size-4' aria-hidden='true' />
              {t('返回市场')}
            </Link>
            <Button onClick={() => setShowCreate(true)}>{t('添加渠道')}</Button>
          </div>
        </header>
        <dl className='owner-workspace-summary'>
          {[
            [t('我的渠道'), owned.length.toLocaleString(getIntlLocale())],
            [t('在售渠道'), active.toLocaleString(getIntlLocale())],
            [t('待处理渠道'), needsAttention.toLocaleString(getIntlLocale())],
            [t('累计渠道收入'), formatQuota(income)],
          ].map(([label, value]) => (
            <div key={label}>
              <dt>{label}</dt>
              <dd>{channels.isLoading || channels.isError ? '—' : value}</dd>
            </div>
          ))}
        </dl>
        {channels.isError && (
          <div role='alert' className='admin-query-error'>
            <span>{t('渠道数据加载失败，请重试。')}</span>
            <Button variant='outline' onClick={() => void channels.refetch()}>
              {t('重试')}
            </Button>
          </div>
        )}
        <TabsList
          variant='line'
          className='owner-workspace-tabs'
          aria-label={t('渠道主管理导航')}
        >
          <TabsTrigger value='channels'>
            <ChartNoAxesCombined aria-hidden='true' />
            {t('我的渠道')}
          </TabsTrigger>
          <TabsTrigger value='income'>{t('收益')}</TabsTrigger>
          <TabsTrigger value='users'>
            <Users aria-hidden='true' />
            {t('用户与倍率')}
          </TabsTrigger>
          <TabsTrigger value='logs'>
            <ScrollText aria-hidden='true' />
            {t('调用日志')}
          </TabsTrigger>
          <TabsTrigger value='security-audit'>
            <ShieldAlert aria-hidden='true' />
            {t('安全审计')}
          </TabsTrigger>
          <TabsTrigger value='shop'>
            <Store aria-hidden='true' />
            {t('店铺设置')}
          </TabsTrigger>
        </TabsList>
        <Suspense
          fallback={<Skeleton className='h-64 w-full rounded-[20px]' />}
        >
          <TabsContent value='channels'>
            <OwnerChannels onAdd={() => setShowCreate(true)} />
          </TabsContent>
          <TabsContent value='income'>
            <div className='owner-income-panel'>
              <OwnerIncomeOverview channels={owned} />
            </div>
            <section className='owner-income-rank'>
              <div className='flex flex-wrap items-center justify-between gap-3'>
                <h2>{t('各渠道累计收入')}</h2>
                <Input
                  className='w-full sm:w-80'
                  value={incomeSearch}
                  onChange={(event) => setIncomeSearch(event.target.value)}
                  placeholder={t('搜索我的渠道')}
                  aria-label={t('搜索我的渠道')}
                />
              </div>
              {!channels.isLoading &&
                !channels.isError &&
                incomePageCount > 1 && (
                  <nav
                    className='owner-income-pagination my-4 flex flex-wrap items-center justify-between gap-3'
                    aria-label={t('pagination')}
                  >
                    <span className='text-muted-foreground text-sm'>
                      {t('Total')}{' '}
                      {topChannels.length.toLocaleString(getIntlLocale())}
                    </span>
                    <div className='flex flex-wrap items-center gap-2'>
                      <Button
                        variant='outline'
                        disabled={incomePage === 1}
                        onClick={() => setIncomePage(incomePage - 1)}
                      >
                        <ChevronLeft aria-hidden='true' />
                        {t('上一页')}
                      </Button>
                      <label className='flex items-center gap-2 text-sm'>
                        <span className='sr-only'>{t('Page')}</span>
                        <Input
                          type='number'
                          className='w-[68px] text-center tabular-nums'
                          min={1}
                          max={incomePageCount}
                          value={incomePage}
                          aria-label={t('Page')}
                          onChange={(event) =>
                            setIncomePage(Number(event.target.value))
                          }
                        />
                        <span>
                          / {incomePageCount.toLocaleString(getIntlLocale())}
                        </span>
                      </label>
                      <Button
                        variant='outline'
                        disabled={incomePage === incomePageCount}
                        onClick={() => setIncomePage(incomePage + 1)}
                      >
                        {t('下一页')}
                        <ChevronRight aria-hidden='true' />
                      </Button>
                    </div>
                  </nav>
                )}
              {channels.isLoading && <Skeleton className='h-40 w-full' />}
              {!channels.isLoading &&
                !channels.isError &&
                topChannels.length === 0 && (
                  <p className='py-8'>
                    {owned.length === 0
                      ? t('还没有渠道')
                      : t('当前筛选条件下没有渠道')}
                  </p>
                )}
              {!channels.isError &&
                incomePageChannels.map((item) => {
                  const name = item.custom_name
                    ? `${item.id}-${item.custom_name}`
                    : item.system_display_name
                  return (
                    <article key={item.id}>
                      <span className='min-w-0 font-medium break-words'>
                        {name}
                      </span>
                      <span className='text-muted-foreground'>
                        {t('倍率')} {formatMultiplier(item.multiplier)}×
                      </span>
                      <span className='font-medium'>
                        {formatQuota(item.total_income || 0)}
                      </span>
                      <div
                        className='income-track'
                        role='img'
                        aria-label={`${name}: ${formatQuota(item.total_income || 0)}`}
                      >
                        <div
                          style={{
                            width: `${Math.max(0, ((item.total_income || 0) / maxIncome) * 100)}%`,
                          }}
                        />
                      </div>
                    </article>
                  )
                })}
            </section>
          </TabsContent>
          <TabsContent value='users'>
            <OwnerOperationsPanel focus={userFocus} />
          </TabsContent>
          <TabsContent value='logs'>
            <OwnerChannelUsageLogs
              channels={channels.data ?? []}
              focus={logFocus}
            />
          </TabsContent>
          <TabsContent value='security-audit'>
            <SecurityAuditPanel
              channels={channels.data ?? []}
              onInspectUser={(event) => {
                setUserFocus({
                  channelId: event.marketplace_channel_id,
                  userId: event.user_id,
                  nonce: Date.now(),
                })
                setTab('users')
              }}
              onInspectLogs={(event) => {
                setLogFocus({
                  channelId: event.marketplace_channel_id,
                  requestId: event.request_id,
                  userId: event.user_id,
                  nonce: Date.now(),
                })
                setTab('logs')
              }}
            />
          </TabsContent>
          <TabsContent value='shop'>
            <ShopProfile />
          </TabsContent>
        </Suspense>
        <ChannelCreateDialog open={showCreate} onOpenChange={setShowCreate} />
      </Tabs>
    </main>
  )
}
