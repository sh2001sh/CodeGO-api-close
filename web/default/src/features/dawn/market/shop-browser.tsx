import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Star, Store } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { MarketReportButton } from '@/features/marketplace/components/market-report-button'
import {
  getMarketplaceShops,
  rateShop,
  type MarketplaceShop,
} from '@/features/marketplace/shop-api'
import type { GroupFilters } from '@/features/marketplace/types'
import { DawnQueryError } from '../components/query-error'

export function ShopBrowser(props: {
  filters: GroupFilters
  authed: boolean
  onVisit: (shop: MarketplaceShop) => void
  onTag: (tag: string) => void
  onPage: (page: number) => void
}) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['marketplace-shops', props.filters],
    queryFn: () => getMarketplaceShops(props.filters),
  })
  if (query.isPending)
    return (
      <div className='space-y-4' aria-label={t('店铺加载中')}>
        <Skeleton className='h-52 w-full' />
        <Skeleton className='h-52 w-full' />
      </div>
    )
  if (query.isError)
    return (
      <DawnQueryError
        title={t('店铺加载失败')}
        description={query.error.message}
        onRetry={() => void query.refetch()}
        retrying={query.isFetching}
      />
    )
  const pages = Math.max(
    1,
    Math.ceil(query.data.total / props.filters.page_size)
  )
  return (
    <section aria-label={t('店铺列表')} className='space-y-4'>
      <div className='flex items-center justify-between gap-2 text-sm'>
        <h2 className='font-semibold'>{t('逛店铺')}</h2>
        <span className='text-muted-foreground'>
          {t('{{count}} 家店铺', { count: query.data.total })}
        </span>
      </div>
      <div className='market-shop-shelf'>
        {query.data.items.map((shop) => (
          <ShopCard
            key={shop.id}
            shop={shop}
            authed={props.authed}
            onVisit={() => props.onVisit(shop)}
            onTag={props.onTag}
          />
        ))}
      </div>
      {!query.data.items.length && (
        <div className='empty'>
          <Store size={24} aria-hidden='true' />
          <b>{t('没有找到符合条件的店铺')}</b>
          <span>{t('请尝试其他关键词或清除搜索条件')}</span>
        </div>
      )}
      {pages > 1 && (
        <nav
          aria-label={t('店铺分页')}
          className='flex flex-wrap items-center justify-between gap-3 py-3 text-sm'
        >
          <span>
            {props.filters.page} / {pages}
          </span>
          <div className='flex gap-2'>
            <Button
              variant='outline'
              disabled={props.filters.page <= 1}
              onClick={() => props.onPage(props.filters.page - 1)}
            >
              {t('上一页')}
            </Button>
            <Button
              variant='outline'
              disabled={props.filters.page >= pages}
              onClick={() => props.onPage(props.filters.page + 1)}
            >
              {t('下一页')}
            </Button>
          </div>
        </nav>
      )}
    </section>
  )
}

function ShopCard(props: {
  shop: MarketplaceShop
  authed: boolean
  onVisit: () => void
  onTag: (tag: string) => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const mutation = useMutation({
    mutationFn: rateShop,
    onSuccess: () => {
      toast.success(t('店铺评分已更新'))
      void client.invalidateQueries({ queryKey: ['marketplace-shops'] })
    },
    onError: (error) => toast.error(error.message),
  })
  const shop = props.shop
  let permission = t('付费使用该店铺渠道后可评分')
  if (shop.rating_permission === 'login_required')
    permission = t('登录并付费使用后可评分')
  if (shop.rating_permission === 'owner') permission = t('不能评价自己的店铺')
  if (shop.can_rate) permission = t('我的评分')
  return (
    <article className='market-shop-card flex min-w-0 flex-col'>
      <div className='flex items-start justify-between gap-3'>
        <div className='flex min-w-0 items-center gap-3'>
          <Store
            className='text-muted-foreground size-6 shrink-0'
            aria-hidden='true'
          />
          <div className='min-w-0'>
            <h3 className='truncate text-lg font-semibold' title={shop.name}>
              {shop.name}
            </h3>
            <p className='text-muted-foreground mt-1 text-xs tabular-nums'>
              ID {shop.id}
            </p>
          </div>
        </div>
        <MarketReportButton
          type='shop'
          id={String(shop.id)}
          name={shop.name}
          authed={props.authed}
        />
      </div>
      <div className='mt-4 flex flex-wrap items-center gap-x-4 gap-y-2 text-sm'>
        <span>{t('{{count}} 个在售商品', { count: shop.channel_count })}</span>
        <span>
          {t('{{multiplier}} 倍起', { multiplier: shop.min_multiplier })}
        </span>
        <span>{t('{{count}} 个模型', { count: shop.models.length })}</span>
      </div>
      {shop.tags.length > 0 && (
        <div className='mt-3 flex flex-wrap gap-2'>
          {shop.tags.map((tag) => (
            <button
              type='button'
              key={tag}
              className='bg-muted text-muted-foreground hover:text-foreground focus-visible:ring-ring rounded-md px-2 py-1 text-xs focus-visible:ring-2'
              onClick={() => props.onTag(tag)}
            >
              #{tag}
            </button>
          ))}
        </div>
      )}
      <div className='mt-auto pt-5'>
        <div className='border-border flex flex-wrap items-center justify-between gap-2 border-t pt-4'>
          <span className='inline-flex items-center gap-1.5 text-sm'>
            <Star
              className='size-4 fill-amber-500 text-amber-500'
              aria-hidden='true'
            />
            {shop.rating_count ? shop.rating.toFixed(1) : t('暂无评分')}
            <span className='text-muted-foreground'>
              {t('{{count}} 人评分', { count: shop.rating_count })}
            </span>
          </span>
          <Button size='sm' onClick={props.onVisit}>
            {t('进店看商品')}
          </Button>
        </div>
        <fieldset
          className='mt-3'
          disabled={!shop.can_rate || mutation.isPending}
        >
          <legend className='text-muted-foreground mb-1 text-xs'>
            {permission}
          </legend>
          <div className='flex gap-1' role='group' aria-label={t('店铺评分')}>
            {[1, 2, 3, 4, 5].map((stars) => (
              <button
                key={stars}
                type='button'
                className='focus-visible:ring-ring enabled:hover:bg-muted inline-flex size-9 items-center justify-center rounded-md focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-50'
                aria-label={t('{{count}} 星', { count: stars })}
                aria-pressed={shop.viewer_stars === stars}
                onClick={() => mutation.mutate({ id: shop.id, stars })}
              >
                <Star
                  className={
                    stars <= shop.viewer_stars
                      ? 'size-5 fill-amber-500 text-amber-500'
                      : 'text-muted-foreground size-5'
                  }
                  aria-hidden='true'
                />
              </button>
            ))}
          </div>
        </fieldset>
      </div>
    </article>
  )
}
