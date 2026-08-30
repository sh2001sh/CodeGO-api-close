import { RefreshCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { useMarketplaceGroups } from '../hooks'
import { MARKETPLACE_SOURCE_OPTIONS } from '../lib/channel-form'
import type { GroupFilters } from '../types'
import { MarketplaceGroupList } from './group-list'
import { MarketplaceFilters } from './marketplace-filters'

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
        <div className='border-border bg-primary/[0.05] border-y px-4 py-3 text-xs leading-5'>
          <span className='text-foreground'>
            {t(
              '榜单使用 Wilson 可靠性修正，并综合 TTFT、总延迟、TPS 与倍率；小样本渠道会继续观测，不参与正式名次。'
            )}
          </span>
        </div>
      )}
      <div className='border-border bg-muted/25 border-b px-4 py-2 sm:px-5'>
        <p className='text-muted-foreground/75 text-[11px] leading-5'>
          <span className='text-muted-foreground font-medium'>
            {t('第三方市场分组免责声明')}
          </span>
          {t(
            '市场中的分组由用户提交并由平台提供路由管理，不代表官方服务或官方授权。平台不保证第三方分组的安全性、合法性、可用性、稳定性、模型能力或数据处理方式，请自行评估渠道来源、凭据风险和使用结果；因第三方分组导致的服务中断、数据或隐私风险、额度损失由使用者自行承担。'
          )}
        </p>
      </div>
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
      <MarketplaceGroupList
        groups={props.query.data?.items ?? []}
        loading={props.query.isLoading}
        error={props.query.isError && !props.query.data}
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
