import { RefreshCcw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { useMarketplaceGroups } from '../hooks'
import { MARKETPLACE_SOURCE_OPTIONS } from '../lib/channel-form'
import type { GroupFilters } from '../types'
import { MarketplaceGroupList } from './group-list'
import { MarketplaceFilters } from './marketplace-filters'
import { RoutePoolWorkspace } from './route-pool-workspace'

export function MarketSurface(props: {
  filters: GroupFilters
  updateFilters: (patch: Partial<GroupFilters>) => void
  query: ReturnType<typeof useMarketplaceGroups>
  summary: string
  ranking?: boolean
}) {
  const { t } = useTranslation()
  const [activePoolID, setActivePoolID] = useState('')
  const totalPages = Math.max(
    1,
    Math.ceil((props.query.data?.total ?? 0) / props.filters.page_size)
  )
  const updatedAt = props.query.dataUpdatedAt
    ? new Date(props.query.dataUpdatedAt).toLocaleTimeString()
    : '--'
  return (
    <section className='demo-market-surface'>
      <div className='market-headline'>
        <div>
          <h3 className='text-sm font-semibold'>
            {props.ranking ? t('质量排行榜') : t('渠道目录')}
          </h3>
          <p className='text-muted-foreground mt-0.5 text-xs leading-5'>
            {props.summary}
          </p>
        </div>
        <div className='flex flex-wrap items-center justify-end gap-3'>
          <span
            className='text-muted-foreground text-xs tabular-nums'
            aria-live='polite'
          >
            {t('更新于 {{time}}', { time: updatedAt })}
          </span>
          <Button
            variant='outline'
            size='sm'
            onClick={() => void props.query.refetch()}
            disabled={props.query.isFetching}
          >
            <RefreshCcw
              className={props.query.isFetching ? 'animate-spin' : ''}
            />
            {t('刷新数据')}
          </Button>
        </div>
      </div>
      <div className='market-source-tabs'>
        <Tabs
          value={props.filters.source || 'all'}
          onValueChange={(value) =>
            props.updateFilters({
              source: value === 'all' ? '' : value,
              page: 1,
            })
          }
        >
          <TabsList
            variant='line'
            className='h-auto max-w-full flex-wrap justify-start gap-x-1'
          >
            <TabsTrigger value='all' className='shrink-0 px-3'>
              {t('全部来源')}
            </TabsTrigger>
            {MARKETPLACE_SOURCE_OPTIONS.map((source) => (
              <TabsTrigger
                key={source}
                value={source}
                className='shrink-0 px-3'
              >
                {source}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>
      <MarketplaceFilters
        filters={props.filters}
        onChange={props.updateFilters}
      />
      <div className='cols'>
        <MarketplaceGroupList
          groups={props.query.data?.items ?? []}
          loading={props.query.isLoading}
          error={props.query.isError && !props.query.data}
          routePoolEnabled={!props.ranking}
          routePoolID={activePoolID}
          onRetry={() => void props.query.refetch()}
        />
        {!props.ranking && (
          <aside className='poolpanel'>
            <RoutePoolWorkspace
              compact
              activePoolID={activePoolID}
              onActivePoolChange={setActivePoolID}
            />
          </aside>
        )}
      </div>
      <div className='border-border bg-muted/15 flex items-center justify-between border-t px-4 py-3'>
        <span className='text-muted-foreground text-xs'>
          {t('第 {{page}} / {{pages}} 页', {
            page: props.filters.page,
            pages: totalPages,
          })}
        </span>
        <div className='flex gap-2'>
          <Button
            variant='outline'
            size='sm'
            disabled={props.filters.page <= 1}
            onClick={() =>
              props.updateFilters({ page: props.filters.page - 1 })
            }
          >
            {t('上一页')}
          </Button>
          <Button
            variant='outline'
            size='sm'
            disabled={props.filters.page >= totalPages}
            onClick={() =>
              props.updateFilters({ page: props.filters.page + 1 })
            }
          >
            {t('下一页')}
          </Button>
        </div>
      </div>
    </section>
  )
}
