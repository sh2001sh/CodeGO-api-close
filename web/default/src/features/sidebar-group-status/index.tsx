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
import { useDeferredValue, useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { RefreshCcw, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import { SectionPageLayout } from '@/components/layout'
import { GroupStatusMonitorCard } from './group-status-monitor-card'
import {
  collectModelOptions,
  filterGroupStatusItems,
  sortItems,
  summarizeGroups,
} from './presentation'
import { useSidebarGroupStatus } from './use-sidebar-group-status'

export function SidebarGroupStatusPage() {
  const { t } = useTranslation()
  const query = useSidebarGroupStatus()
  const [source, setSource] = useState<'all' | 'official' | 'marketplace_user'>(
    'all'
  )
  const [search, setSearch] = useState('')
  const [modelFilter, setModelFilter] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
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
  const modelOptions = useMemo(() => collectModelOptions(allItems), [allItems])
  const summary = useMemo(() => summarizeGroups(allItems), [allItems])

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Group status')}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        查看各分组下模型的可用状态、最近请求成功率和对应时间段表现。
      </SectionPageLayout.Description>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          size='sm'
          render={
            <Link to='/dashboard/$section' params={{ section: 'overview' }} />
          }
        >
          概览
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
          刷新
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='mx-auto flex w-full max-w-[1700px] flex-col gap-5'>
          <OverviewPanel summary={summary} loading={query.isLoading} />

          <div className='border-border flex flex-col gap-3 border-b pb-3'>
            <div
              className='border-border/70 flex w-fit items-center gap-4 border-b'
              aria-label='分组来源筛选'
            >
              {(
                [
                  ['all', '全部'],
                  ['official', '官方渠道'],
                  ['marketplace_user', '第三方渠道'],
                ] as const
              ).map(([value, label]) => (
                <button
                  key={value}
                  type='button'
                  onClick={() => setSource(value)}
                  className={`-mb-px border-b-2 pb-2.5 text-[13px] transition-colors ${
                    source === value
                      ? 'border-primary text-foreground font-semibold'
                      : 'text-muted-foreground hover:text-foreground border-transparent'
                  }`}
                >
                  {label}
                </button>
              ))}
            </div>
            <div className='flex flex-col gap-2 xl:flex-row xl:items-center'>
              <label className='relative min-w-0 flex-1 xl:max-w-xl'>
                <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2' />
                <Input
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  placeholder='搜索分组名称、内部 ID 或模型'
                  aria-label='搜索分组名称、内部 ID 或模型'
                  className='bg-background pl-9'
                />
              </label>
              <NativeSelect
                value={modelFilter}
                onChange={(event) => setModelFilter(event.target.value)}
                aria-label='按模型筛选'
                className='bg-background xl:w-52'
              >
                <option value=''>全部模型</option>
                {modelOptions.map((model) => (
                  <option key={model} value={model}>
                    {model}
                  </option>
                ))}
              </NativeSelect>
              <NativeSelect
                value={statusFilter}
                onChange={(event) => setStatusFilter(event.target.value)}
                aria-label='按状态筛选'
                className='bg-background xl:w-40'
              >
                <option value=''>全部状态</option>
                <option value='healthy'>稳定</option>
                <option value='unstable'>波动</option>
                <option value='failed'>异常</option>
                <option value='unknown'>暂无近期请求</option>
              </NativeSelect>
            </div>
          </div>

          {query.isLoading ? (
            <BoardSkeleton />
          ) : query.isError ? (
            <ErrorPanel onRetry={() => void query.refetch()} />
          ) : items.length === 0 ? (
            <EmptyPanel />
          ) : (
            <div className='flex flex-col gap-5'>
              {items.map((group) => (
                <section
                  key={group.group}
                  className='group-status-render-section app-page-shell p-4'
                >
                  <div className='mb-4 flex items-end justify-between gap-3'>
                    <div className='space-y-1'>
                      <h3 className='text-foreground text-xl font-semibold tracking-tight'>
                        {group.display_name || group.group}
                      </h3>
                      <p className='text-muted-foreground text-sm'>
                        {(group.source_type ?? 'official') ===
                        'marketplace_user'
                          ? '第三方渠道 · 套餐与余额'
                          : '官方渠道'}{' '}
                        · {group.models.length} 个模型
                      </p>
                    </div>
                    <div className='shrink-0 text-right'>
                      <div className='text-muted-foreground text-xs'>
                        缓存命中率
                      </div>
                      <div className='mt-0.5 text-lg font-semibold tabular-nums'>
                        {group.cache_hit_rate == null
                          ? '--'
                          : `${group.cache_hit_rate.toFixed(1)}%`}
                      </div>
                    </div>
                  </div>

                  <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5'>
                    {group.models.length === 0 ? (
                      <div className='codego-empty px-4 py-6'>
                        <span
                          aria-hidden
                          className='bg-border block h-6 w-px'
                        />
                        NO MODELS
                      </div>
                    ) : (
                      group.models.map((model) => (
                        <GroupStatusMonitorCard
                          key={`${group.group}-${model.model}`}
                          item={model}
                        />
                      ))
                    )}
                  </div>
                </section>
              ))}
            </div>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function OverviewPanel(props: {
  summary: ReturnType<typeof summarizeGroups>
  loading: boolean
}) {
  const metrics = [
    {
      label: '分组',
      value: String(props.summary.groups),
      tone: 'text-foreground',
    },
    {
      label: '稳定',
      value: String(props.summary.healthyModels),
      tone: 'text-success',
    },
    {
      label: '波动',
      value: String(props.summary.unstableModels),
      tone: 'text-warning',
    },
    {
      label: '异常',
      value: String(props.summary.failedModels),
      tone: 'text-destructive',
    },
    {
      label: '无请求',
      value: String(props.summary.unknownModels),
      tone: 'text-muted-foreground',
    },
  ]

  return (
    <Card className='border-border py-0'>
      <CardHeader className='border-border border-b py-4'>
        <div className='flex items-center justify-between gap-3'>
          <div className='flex items-center gap-2.5'>
            <span aria-hidden className='bg-primary block h-3 w-[3px]' />
            <CardTitle className='text-[13px] font-semibold'>
              分组模型状态
            </CardTitle>
          </div>
          <span className='codego-stat-label'>6H WINDOW</span>
        </div>
      </CardHeader>
      <CardContent className='py-0'>
        <div className='codego-fact-row grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5'>
          {metrics.map((metric) => (
            <div
              key={metric.label}
              className='min-w-0 px-4 py-4 sm:px-5 sm:py-5'
            >
              <div className='codego-stat-label'>{metric.label}</div>
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
          <div className='text-base font-semibold'>模型状态暂时不可用</div>
          <div className='text-muted-foreground text-sm leading-6'>
            当前无法获取分组下模型状态数据，请稍后刷新重试。
          </div>
        </div>
        <Button variant='outline' size='sm' onClick={props.onRetry}>
          <RefreshCcw className='size-3.5' />
          重新获取
        </Button>
      </CardContent>
    </Card>
  )
}

function EmptyPanel() {
  return (
    <Card>
      <CardContent className='py-8'>
        <div className='space-y-1'>
          <div className='text-base font-semibold'>暂无可展示的模型状态</div>
          <div className='text-muted-foreground text-sm leading-6'>
            当前用户还没有可用的业务分组模型，或暂未产生用于监测的请求样本。
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
