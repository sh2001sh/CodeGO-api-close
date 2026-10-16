/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useDeferredValue, useEffect, useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import i18n from '@/i18n/config'
import {
  Activity,
  Boxes,
  CircleHelp,
  ShieldCheck,
  TriangleAlert,
  RefreshCcw,
  Search,
} from 'lucide-react'
import { useTranslation, Trans } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import { BoundedSelect } from '@/components/bounded-select'
import { SectionPageLayout } from '@/components/layout'
import {
  useMarketplaceAutoRoutePool,
  useMarketplaceAutoRoutePoolUpdate,
  useMarketplaceRoutePool,
  useMarketplaceRoutePools,
  useMarketplaceRoutePoolUpdate,
} from '@/features/marketplace/hooks'
import {
  persistActiveRoutePoolID,
  readActiveRoutePoolID,
} from '@/features/marketplace/lib/active-route-pool'
import { GroupStatusSection } from './group-status-section'
import {
  collectModelOptions,
  filterGroupStatusItems,
  sortItems,
  summarizeGroups,
} from './presentation'
import './status.css'
import { useSidebarGroupStatus } from './use-sidebar-group-status'

export function SidebarGroupStatusPage() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const authed = !!user
  const query = useSidebarGroupStatus()
  const [source, setSource] = useState<'all' | 'official' | 'marketplace_user'>(
    'all'
  )
  const [search, setSearch] = useState('')
  const [modelFilter, setModelFilter] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [collapsedGroups, setCollapsedGroups] = useState<Set<string>>(
    () => new Set()
  )
  const [visibleGroupCount, setVisibleGroupCount] = useState(20)
  const [activePoolID, setActivePoolID] = useState(readActiveRoutePoolID)
  const pools = useMarketplaceRoutePools(authed)
  const autoPool = useMarketplaceAutoRoutePool(authed)
  const isAutoPool = activePoolID === 'auto'
  const poolDetail = useMarketplaceRoutePool(
    authed && activePoolID && !isAutoPool ? activePoolID : ''
  )
  const updatePool = useMarketplaceRoutePoolUpdate()
  const updateAutoPool = useMarketplaceAutoRoutePoolUpdate()
  const poolOptions = useMemo(() => {
    const auto = autoPool.data ? [{ id: 'auto', name: 'AUTO 池' }] : []
    const custom = (pools.data ?? []).map((pool) => ({
      id: pool.id,
      name: pool.name,
    }))
    return [...auto, ...custom]
  }, [autoPool.data, pools.data])
  const poolMemberIDs = useMemo(
    () =>
      new Set(
        ((isAutoPool ? autoPool.data?.items : poolDetail.data?.items) ?? [])
          .filter((item) => item.selected)
          .map((item) => item.group_id)
      ),
    [autoPool.data?.items, isAutoPool, poolDetail.data?.items]
  )
  const activePoolName = poolOptions.find(
    (pool) => pool.id === activePoolID
  )?.name

  useEffect(() => {
    if (!authed || pools.isLoading || autoPool.isLoading) return
    if (activePoolID && poolOptions.some((pool) => pool.id === activePoolID))
      return
    setActivePoolID(poolOptions[0]?.id ?? '')
  }, [activePoolID, authed, autoPool.isLoading, poolOptions, pools.isLoading])

  useEffect(() => {
    persistActiveRoutePoolID(activePoolID)
  }, [activePoolID])
  const deferredSearch = useDeferredValue(search)
  const allItems = useMemo(
    () => sortItems(query.data?.data ?? []),
    [query.data?.data]
  )
  const items = useMemo(
    () =>
      filterGroupStatusItems(allItems, {
        source,
        model: modelFilter,
        status: statusFilter,
        search: deferredSearch,
      }),
    [allItems, deferredSearch, modelFilter, source, statusFilter]
  )
  useEffect(() => {
    setVisibleGroupCount(20)
  }, [deferredSearch, modelFilter, source, statusFilter])
  const visibleItems = useMemo(
    () => items.slice(0, visibleGroupCount),
    [items, visibleGroupCount]
  )
  const modelOptions = useMemo(() => collectModelOptions(allItems), [allItems])
  const modelFilterOptions = useMemo(
    () => [
      { value: '', label: t('全部模型') },
      ...modelOptions.map((model) => ({ value: model, label: model })),
    ],
    [modelOptions, t]
  )
  const summary = useMemo(() => summarizeGroups(allItems), [allItems])
  const toggleGroup = (group: string) => {
    setCollapsedGroups((current) => {
      const next = new Set(current)
      if (next.has(group)) next.delete(group)
      else next.add(group)
      return next
    })
  }
  const joinCurrentPool = async (groupID: string) => {
    if (!authed) {
      toast.error(i18n.t('登录后才能添加到路由池'))
      return
    }
    if (!activePoolID) {
      toast.error(i18n.t('请先在分组市场创建路由池'))
      return
    }
    if (isAutoPool && !autoPool.data) {
      toast.error(i18n.t('当前路由池仍在加载，请稍后重试'))
      return
    }
    if (!isAutoPool && !poolDetail.data) {
      toast.error(i18n.t('当前路由池仍在加载，请稍后重试'))
      return
    }
    if (!groupID || poolMemberIDs.has(groupID)) return
    const nextIDs = [...poolMemberIDs, groupID]
    try {
      if (isAutoPool) {
        await updateAutoPool.mutateAsync({
          groupIds: nextIDs,
          config: autoPool.data?.config,
        })
      } else {
        await updatePool.mutateAsync({
          id: activePoolID,
          groupIds: nextIDs,
          config: poolDetail.data?.config,
        })
      }
      toast.success(
        i18n.t('已加入路由池（{{param0}}）', {
          param0: activePoolName ?? i18n.t('当前池'),
        })
      )
    } catch (error) {
      toast.error(error instanceof Error ? error.message : i18n.t('加入失败'))
    }
  }

  return (
    <div className='demo-status-page exchange-status'>
      <SectionPageLayout>
        <SectionPageLayout.Title>
          <span className='demo-status-title'>
            <Trans i18nKey={'Status'} />
            <span aria-hidden='true'>.</span>
          </span>
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button variant='outline' size='sm' render={<Link to='/market' />}>
            <Trans i18nKey={'前往分组市场'} />
          </Button>
          <Button
            variant='outline'
            size='sm'
            onClick={() => void query.refetch()}
            disabled={query.isFetching}
          >
            <RefreshCcw
              className={cn('size-3.5', query.isFetching && 'animate-spin')}
            />
            <Trans i18nKey={'刷新'} />
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='mx-auto flex w-full max-w-[1700px] flex-col gap-5'>
            <OverviewPanel summary={summary} loading={query.isLoading} />

            {query.dataUpdatedAt > 0 && (
              <div className='status-refresh-time'>
                <span>{i18n.t('刷新时间')}</span>
                <time dateTime={new Date(query.dataUpdatedAt).toISOString()}>
                  {new Date(query.dataUpdatedAt).toLocaleString(i18n.language)}
                </time>
              </div>
            )}
            {query.isError && query.data && (
              <div role='alert' className='status-stale-error'>
                {i18n.t('模型状态暂时不可用')}
                <Button variant='outline' onClick={() => void query.refetch()}>
                  {i18n.t('重试')}
                </Button>
              </div>
            )}

            <div className='status-filter-workbench'>
              <div
                className='status-source-switch flex max-w-full items-center gap-1'
                aria-label={i18n.t('分组来源筛选')}
              >
                {(
                  [
                    ['all', i18n.t('全部')],
                    ['official', i18n.t('官方渠道')],
                    ['marketplace_user', i18n.t('第三方渠道')],
                  ] as const
                ).map(([value, label]) => (
                  <button
                    key={value}
                    type='button'
                    onClick={() => setSource(value)}
                    aria-pressed={source === value}
                    className={`min-h-11 rounded-lg px-3 text-[13px] transition-colors ${
                      source === value
                        ? 'bg-card text-foreground font-semibold shadow-sm'
                        : 'text-muted-foreground hover:text-foreground'
                    }`}
                  >
                    {i18n.t(label)}
                  </button>
                ))}
              </div>
              <div className='grid grid-cols-2 gap-2 xl:flex xl:items-center'>
                <label className='relative col-span-2 min-w-0 flex-1 xl:max-w-xl'>
                  <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2' />
                  <Input
                    value={search}
                    onChange={(event) => setSearch(event.target.value)}
                    placeholder={i18n.t('搜索分组名称、内部 ID 或模型')}
                    aria-label={i18n.t('搜索分组名称、内部 ID 或模型')}
                    className='bg-background pl-9'
                  />
                </label>
                <BoundedSelect
                  options={modelFilterOptions}
                  value={modelFilter}
                  onValueChange={setModelFilter}
                  aria-label={i18n.t('按模型筛选')}
                  placeholder={i18n.t('全部模型')}
                  className='min-h-11 w-full xl:w-52'
                />
                {authed && (
                  <NativeSelect
                    value={activePoolID}
                    onChange={(event) => setActivePoolID(event.target.value)}
                    aria-label={i18n.t('当前路由池')}
                    className='min-h-11 w-full xl:w-48'
                  >
                    {poolOptions.map((pool) => (
                      <option key={pool.id} value={pool.id}>
                        {pool.name}
                      </option>
                    ))}
                  </NativeSelect>
                )}
                <NativeSelect
                  value={statusFilter}
                  onChange={(event) => setStatusFilter(event.target.value)}
                  aria-label={i18n.t('按状态筛选')}
                  className='min-h-11 w-full xl:w-40'
                >
                  <option value=''>
                    <Trans i18nKey={'全部状态'} />
                  </option>
                  <option value='healthy'>
                    <Trans i18nKey={'稳定'} />
                  </option>
                  <option value='unstable'>
                    <Trans i18nKey={'波动'} />
                  </option>
                  <option value='failed'>
                    <Trans i18nKey={'异常'} />
                  </option>
                  <option value='unknown'>
                    <Trans i18nKey={'暂无近期请求'} />
                  </option>
                </NativeSelect>
              </div>
            </div>

            {query.isLoading ? (
              <BoardSkeleton />
            ) : query.isError && !query.data ? (
              <ErrorPanel onRetry={() => void query.refetch()} />
            ) : items.length === 0 ? (
              <EmptyPanel
                onClear={() => {
                  setSearch('')
                  setModelFilter('')
                  setStatusFilter('')
                  setSource('all')
                }}
              />
            ) : (
              <div className='flex flex-col gap-5'>
                {visibleItems.map((group) => (
                  <GroupStatusSection
                    key={group.group}
                    group={group}
                    expanded={!collapsedGroups.has(group.group)}
                    onToggle={() => toggleGroup(group.group)}
                    poolName={activePoolName}
                    inCurrentPool={poolMemberIDs.has(group.group_id)}
                    joining={updatePool.isPending || updateAutoPool.isPending}
                    onJoinCurrentPool={() =>
                      void joinCurrentPool(group.group_id)
                    }
                  />
                ))}
                {visibleItems.length < items.length && (
                  <div className='flex justify-center pt-1'>
                    <Button
                      variant='outline'
                      onClick={() =>
                        setVisibleGroupCount((count) => count + 20)
                      }
                    >
                      <Trans i18nKey={'加载更多（剩余'} />{' '}
                      {items.length - visibleItems.length}{' '}
                      <Trans i18nKey={'个）'} />
                    </Button>
                  </div>
                )}
              </div>
            )}
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    </div>
  )
}

function OverviewPanel(props: {
  summary: ReturnType<typeof summarizeGroups>
  loading: boolean
}) {
  const { t } = useTranslation()
  const metrics = [
    {
      label: t('分组'),
      icon: Boxes,
      value: String(props.summary.groups),
      tone: 'text-foreground',
    },
    {
      label: t('稳定'),
      icon: ShieldCheck,
      value: String(props.summary.healthyModels),
      tone: 'text-success',
    },
    {
      label: t('波动'),
      icon: Activity,
      value: String(props.summary.unstableModels),
      tone: 'text-warning',
    },
    {
      label: t('异常'),
      icon: TriangleAlert,
      value: String(props.summary.failedModels),
      tone: 'text-destructive',
    },
    {
      label: t('无请求'),
      icon: CircleHelp,
      value: String(props.summary.unknownModels),
      tone: 'text-muted-foreground',
    },
  ]

  return (
    <Card className='status-overview-panel border-border py-0'>
      <CardHeader className='border-border border-b py-4'>
        <div className='flex items-center justify-between gap-3'>
          <div className='flex items-center gap-2.5'>
            <CardTitle className='flex items-center gap-2 text-[13px] font-semibold'>
              <Activity className='text-primary size-4' aria-hidden='true' />
              <Trans i18nKey={'分组模型状态'} />
            </CardTitle>
          </div>
          <span className='codego-stat-label'>{i18n.t('6H WINDOW')}</span>
        </div>
      </CardHeader>
      <CardContent className='py-0'>
        <div className='codego-fact-row grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5'>
          {metrics.map((metric) => (
            <div
              key={metric.label}
              className='min-w-0 px-4 py-4 sm:px-5 sm:py-5'
            >
              <div className='codego-stat-label flex items-center gap-2'>
                <metric.icon className='size-4' aria-hidden='true' />
                {metric.label}
              </div>
              {props.loading ? (
                <Skeleton className='mt-3 h-8 w-16' />
              ) : (
                <div
                  className={cn(
                    'mt-2.5 text-2xl leading-none font-semibold tabular-nums',
                    metric.tone
                  )}
                >
                  {metric.value}
                </div>
              )}
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

function BoardSkeleton() {
  return (
    <div className='flex flex-col gap-5'>
      {Array.from({ length: 4 }).map((_, groupIndex) => (
        <Card key={groupIndex} className='bg-card/50 py-0'>
          <CardContent className='space-y-4 px-4 py-4'>
            <div className='space-y-2'>
              <Skeleton className='h-6 w-36 rounded-md' />
              <Skeleton className='h-4 w-24 rounded-md' />
            </div>
            <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5'>
              {Array.from({ length: 5 }).map((__, cardIndex) => (
                <Skeleton key={cardIndex} className='h-48 w-full rounded-2xl' />
              ))}
            </div>
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

function ErrorPanel(props: { onRetry: () => void }) {
  return (
    <Card>
      <CardContent className='flex flex-col items-start gap-4 py-8'>
        <div className='space-y-1'>
          <div className='text-base font-semibold'>
            <Trans i18nKey={'模型状态暂时不可用'} />
          </div>
        </div>
        <Button variant='outline' size='sm' onClick={props.onRetry}>
          <RefreshCcw className='size-3.5' />
          <Trans i18nKey={'重新获取'} />
        </Button>
      </CardContent>
    </Card>
  )
}

function EmptyPanel(props: { onClear: () => void }) {
  return (
    <Card>
      <CardContent className='py-8'>
        <div className='space-y-1'>
          <div className='text-base font-semibold'>
            <Trans i18nKey={'暂无可展示的模型状态'} />
          </div>
          <Button variant='outline' onClick={props.onClear}>
            <Trans i18nKey={'清除筛选'} />
          </Button>
          <Button variant='outline' render={<Link to='/market' />}>
            <Trans i18nKey={'前往分组市场'} />
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
