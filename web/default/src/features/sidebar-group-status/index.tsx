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
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import { SectionPageLayout } from '@/components/layout'
import { GroupStatusSection } from './group-status-section'
import {
  collectModelOptions,
  filterGroupStatusItems,
  sortItems,
  summarizeGroups,
} from './presentation'
import { useSidebarGroupStatus } from './use-sidebar-group-status'

export function SidebarGroupStatusPage() {
  const query = useSidebarGroupStatus()
  const [source, setSource] = useState<'all' | 'official' | 'marketplace_user'>(
    'all'
  )
  const [search, setSearch] = useState('')
  const [modelFilter, setModelFilter] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  // groups render expanded by default; this set only tracks explicit collapses
  const [collapsedGroups, setCollapsedGroups] = useState<Set<string>>(
    () => new Set()
  )
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
  const toggleGroup = (group: string) => {
    setCollapsedGroups((current) => {
      const next = new Set(current)
      if (next.has(group)) next.delete(group)
      else next.add(group)
      return next
    })
  }

  return (
    <div className='demo-status-page'>
    <SectionPageLayout>
      <SectionPageLayout.Title>
        <span className='demo-status-title'>分组状态，<em>实时</em>。</span>
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          size='sm'
          render={
            <Link to='/marketplace' />
          }
        >
          前往分组市场
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
          ) : query.isError && !query.data ? (
            <ErrorPanel onRetry={() => void query.refetch()} />
          ) : items.length === 0 ? (
            <EmptyPanel />
          ) : (
            <div className='flex flex-col gap-5'>
              {items.map((group) => (
                <GroupStatusSection
                  key={group.group}
                  group={group}
                  expanded={!collapsedGroups.has(group.group)}
                  onToggle={() => toggleGroup(group.group)}
                />
              ))}
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
