import { RefreshCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import type { useMarketplaceGroups } from '../hooks'
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
  return (
    <section className='border-border bg-card overflow-hidden rounded-xl border'>
      <div className='flex flex-wrap items-start justify-between gap-3 px-4 py-4 sm:px-5'>
        <div>
          <h3 className='font-semibold'>
            {props.ranking ? t('质量排行榜') : t('渠道目录')}
          </h3>
          <p className='text-muted-foreground mt-1 text-xs leading-5'>
            {props.summary}
          </p>
        </div>
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
      {props.ranking && (
        <div className='border-border border-y bg-sky-500/[0.06] px-4 py-3 text-xs leading-5 text-sky-800 dark:text-sky-200'>
          {t(
            '榜单使用 Wilson 可靠性修正，并综合 TTFT、总延迟、TPS 与倍率；小样本渠道会继续观测，不参与正式名次。'
          )}
        </div>
      )}
      <MarketplaceFilters
        filters={props.filters}
        onChange={props.updateFilters}
      />
      <MarketplaceHighlights groups={props.query.data?.items ?? []} />
      <MarketplaceGroupList
        groups={props.query.data?.items ?? []}
        loading={props.query.isLoading}
        error={props.query.isError}
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
