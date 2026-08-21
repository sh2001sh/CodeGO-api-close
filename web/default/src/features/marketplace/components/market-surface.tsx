import { RefreshCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { useMarketplaceGroups } from '../hooks'
import { MARKETPLACE_SOURCE_OPTIONS } from '../lib/channel-form'
import type { GroupFilters } from '../types'
import { MarketplaceGroupList } from './group-list'
import { MarketplaceFilters } from './marketplace-filters'
import { MarketplaceHighlights } from './marketplace-highlights'

export function MarketSurface(props: {
  filters: GroupFilters
  updateFilters: (patch: Partial<GroupFilters>) => void
  query: ReturnType<typeof useMarketplaceGroups>
  summary: string
  ranking?: boolean
}) {
  const { t } = useTranslation()
  const totalPages = Math.max(
    1,
    Math.ceil((props.query.data?.total ?? 0) / props.filters.page_size)
  )
  const updatedAt = props.query.dataUpdatedAt
    ? new Date(props.query.dataUpdatedAt).toLocaleTimeString()
    : '--'
  return (
    <section className='border-border bg-card overflow-hidden rounded-lg border'>
      <div className='flex flex-wrap items-center justify-between gap-3 px-4 py-3 sm:px-5'>
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
      {props.ranking && (
        <div className='border-border border-y bg-sky-500/[0.06] px-4 py-3 text-xs leading-5 text-sky-800 dark:text-sky-200'>
          {t(
            '榜单使用 Wilson 可靠性修正，并综合 TTFT、总延迟、TPS 与倍率；小样本渠道会继续观测，不参与正式名次。'
          )}
        </div>
      )}
      <div className='border-border border-b px-4 sm:px-5'>
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
            className='h-9 max-w-full justify-start overflow-x-auto'
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
      <MarketplaceHighlights highlights={props.query.data?.highlights} />
      <MarketplaceGroupList
        groups={props.query.data?.items ?? []}
        loading={props.query.isLoading}
        error={props.query.isError}
        routePoolEnabled={!props.ranking}
        onRetry={() => void props.query.refetch()}
      />
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
