import { useEffect, useMemo, useState } from 'react'
import { useDebounce } from '@/hooks'
import {
  BarChart3,
  LineChart,
  Plus,
  ShieldCheck,
  Store,
  UploadCloud,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SectionPageLayout } from '@/components/layout'
import { acceptMarketplaceGroupInvite } from './api'
import { AdminGovernance } from './components/admin-governance'
import { ChannelWorkspace } from './components/channel-workspace'
import { MarketSurface } from './components/market-surface'
import { MarketplaceMultiplierTrend } from './components/marketplace-multiplier-trend'
import { MarketplaceOverview } from './components/marketplace-overview'
import { TokenBindPanel } from './components/token-bind-panel'
import { useMarketplaceGroups } from './hooks'
import type { GroupFilters } from './types'

const defaultFilters: GroupFilters = {
  search: '',
  model: '',
  source: '',
  provider: '',
  status: '',
  verification: '',
  sort: 'score',
  direction: 'desc',
  window_hours: 24,
  page: 1,
  page_size: 20,
}

type MarketplaceTab = 'market' | 'ranking' | 'trend' | 'mine' | 'admin'

export function MarketplacePage() {
  const { t } = useTranslation()
  const role = useAuthStore((state) => state.auth.user?.role ?? 0)
  const isAdmin = role >= 10
  const [tab, setTab] = useState<MarketplaceTab>('market')
  const [showChannelForm, setShowChannelForm] = useState(false)
  const [filters, setFilters] = useState<GroupFilters>(defaultFilters)
  const [inviteHandled, setInviteHandled] = useState(false)
  const [acceptedInvite, setAcceptedInvite] = useState<{
    groupId: string
    groupName: string
  } | null>(null)
  const debouncedSearch = useDebounce(filters.search, 300)
  const debouncedModel = useDebounce(filters.model, 300)
  const effectiveFilters = useMemo(
    () => ({
      ...filters,
      search: debouncedSearch,
      model: debouncedModel,
    }),
    [debouncedModel, debouncedSearch, filters]
  )
  const groups = useMarketplaceGroups(effectiveFilters)
  useEffect(() => {
    if (inviteHandled) return
    const token = new URLSearchParams(window.location.search).get('invite')
    if (!token) {
      setInviteHandled(true)
      return
    }
    const currentUrl = new URL(window.location.href)
    currentUrl.searchParams.delete('invite')
    window.history.replaceState(
      {},
      '',
      `${currentUrl.pathname}${currentUrl.search}${currentUrl.hash}`
    )
    setInviteHandled(true)
    void acceptMarketplaceGroupInvite(token)
      .then((result) => {
        setAcceptedInvite({
          groupId: result.group_id,
          groupName: result.group_name,
        })
        toast.success(
          t('已获得分组访问权限：{{name}}', { name: result.group_name })
        )
      })
      .catch((error) => {
        toast.error(error instanceof Error ? error.message : t('邀请链接无效'))
      })
  }, [inviteHandled, t])
  const updateFilters = (patch: Partial<GroupFilters>) =>
    setFilters((current) => ({ ...current, ...patch }))

  const openChannelForm = () => {
    setTab('mine')
    setShowChannelForm(true)
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('分组市场')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {isAdmin && (
          <Button variant='outline' size='sm' onClick={() => setTab('admin')}>
            <ShieldCheck />
            {t('渠道治理')}
          </Button>
        )}
        <Button size='sm' onClick={openChannelForm}>
          <Plus />
          {t('添加渠道')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='mx-auto w-full max-w-[1800px] space-y-3'>
          {acceptedInvite && (
            <section className='border-border bg-card space-y-3 rounded-lg border p-4'>
              <div>
                <h3 className='text-sm font-semibold'>{t('邀请分组已加入')}</h3>
                <p className='text-muted-foreground mt-1 text-xs'>
                  {acceptedInvite.groupName} ·{' '}
                  {t(
                    '该分组不会出现在公开市场，可直接绑定 Key 或加入 Auto 路由池。'
                  )}
                </p>
              </div>
              <TokenBindPanel groupId={acceptedInvite.groupId} compact />
            </section>
          )}
          <MarketplaceOverview
            total={groups.data?.total ?? 0}
            ranked={groups.data?.ranked_count ?? 0}
            multiplier={groups.data?.highlights.cheapest?.multiplier}
            updating={groups.isFetching}
          />
          <Tabs
            value={tab}
            onValueChange={(value) => {
              const nextTab = value as MarketplaceTab
              setTab(nextTab)
              if (nextTab !== 'mine') setShowChannelForm(false)
            }}
          >
            <TabsList
              variant='line'
              className='border-border h-10 w-full justify-start gap-1 overflow-x-auto border-b px-1 sm:gap-2'
            >
              <TabsTrigger
                value='market'
                className='min-w-20 px-2 sm:min-w-24 sm:px-3'
              >
                <Store />
                {t('市场分组')}
              </TabsTrigger>
              <TabsTrigger
                value='ranking'
                className='min-w-20 px-2 sm:min-w-24 sm:px-3'
              >
                <BarChart3 />
                {t('质量排行')}
              </TabsTrigger>
              <TabsTrigger
                value='trend'
                className='min-w-20 px-2 sm:min-w-24 sm:px-3'
              >
                <LineChart />
                {t('价格走势')}
              </TabsTrigger>
              <TabsTrigger
                value='mine'
                className='min-w-20 px-2 sm:min-w-24 sm:px-3'
              >
                <UploadCloud />
                {t('我的渠道')}
              </TabsTrigger>
              {isAdmin && (
                <TabsTrigger
                  value='admin'
                  className='min-w-20 px-2 sm:min-w-24 sm:px-3'
                >
                  <ShieldCheck />
                  {t('渠道治理')}
                </TabsTrigger>
              )}
            </TabsList>
            <TabsContent value='market'>
              <MarketSurface
                filters={filters}
                updateFilters={updateFilters}
                query={groups}
                summary={`${t('共 {{total}} 个公开分组', { total: groups.data?.total ?? 0 })} · ${t('{{count}} 个达到正式排名门槛', { count: groups.data?.ranked_count ?? 0 })}`}
              />
            </TabsContent>
            <TabsContent value='ranking'>
              <MarketSurface
                ranking
                filters={filters}
                updateFilters={updateFilters}
                query={groups}
                summary={t('用统一口径比较可靠性、响应性能、吞吐与调用成本。')}
              />
            </TabsContent>
            <TabsContent value='trend'>
              <MarketplaceMultiplierTrend
                model={filters.model}
                onModelChange={(model) => updateFilters({ model, page: 1 })}
              />
            </TabsContent>
            <TabsContent value='mine'>
              <ChannelWorkspace
                showForm={showChannelForm}
                onShowForm={() => setShowChannelForm(true)}
                onHideForm={() => setShowChannelForm(false)}
              />
            </TabsContent>
            {isAdmin && (
              <TabsContent value='admin'>
                <AdminGovernance />
              </TabsContent>
            )}
          </Tabs>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
