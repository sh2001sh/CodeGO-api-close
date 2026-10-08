import { useEffect, useState } from 'react'
import { Store, ArrowLeft, ArrowRight } from 'lucide-react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'
import { useTranslation } from '../../lib/i18n'
import { Button, EmptyState, ErrorMessage, Loading } from '../../components/ui'
import { MarketQuality } from './market-quality'
import { RecentRequests } from './recent-requests'
import { ModelGroupPrice } from '../public/model-group-price'
import { effectiveGroupQuote } from './market-pricing'
import { RatingSummary } from './ratings'
import type { MarketBrowseFilters } from '../../lib/public-catalog'
import { marketTags } from './tags'

export const shopsOptions = (filters: MarketBrowseFilters) =>
  resourceOptions(
    'market-shops',
    async (signal) => {
      const { page, page_size, search, model, tag } = filters
      const result = await api.GET('/api/marketplace/shops', {
        params: { query: { page, page_size, search, model, tag } },
        signal,
      })
      const shops = unwrap(result)
      return {
        shops,
        pagination: result.data?.pagination ?? {
          page: filters.page,
          page_size: filters.page_size,
          total: shops.length,
        },
        models: result.data?.models ?? [
          ...new Set(shops.flatMap((shop) => shop.declared_models ?? [])),
        ],
      }
    },
    [JSON.stringify(filters)],
  )
export const shopOptions = (id: string, filters: MarketBrowseFilters) =>
  resourceOptions(
    'market-shop',
    (signal) =>
      api
        .GET('/api/marketplace/shops/{id}', { params: { path: { id }, query: filters }, signal })
        .then(unwrap),
    [id, JSON.stringify(filters)],
  )
export function MarketShops(props: {
  selected?: string
  onSelect: (id?: string) => void
  onGroup: (id: string) => void
  model: string
  search: string
  onSearch: (search: string) => void
  tag: string
  onTag: (tag: string) => void
  onModel: (model: string) => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [page, setPage] = useState(1)
  const [advancedFilters, setAdvancedFilters] = useState(false)
  const [debouncedSearch, setDebouncedSearch] = useState(props.search)
  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedSearch(props.search), 250)
    return () => window.clearTimeout(timer)
  }, [props.search])
  const filterKey = JSON.stringify([props.selected, debouncedSearch, props.model, props.tag])
  const [pageFilter, setPageFilter] = useState(filterKey)
  const activePage = pageFilter === filterKey ? page : 1
  useEffect(() => {
    if (pageFilter !== filterKey) {
      setPageFilter(filterKey)
      setPage(1)
    }
  }, [filterKey, pageFilter])
  const filters: MarketBrowseFilters = {
    page: activePage,
    page_size: 24,
    search: debouncedSearch,
    model: props.model,
    tag: props.tag,
  }
  const shops = useQuery({ ...shopsOptions(filters), enabled: !props.selected })
  const detail = useQuery({
    ...shopOptions(props.selected ?? '', filters),
    enabled: Boolean(props.selected),
  })
  const shop = detail.data?.shop
  const models = props.selected ? (shop?.declared_models ?? []) : (shops.data?.models ?? [])
  const visibleShops = shops.data?.shops
  const pagination = props.selected ? detail.data?.pagination : shops.data?.pagination
  const fetching = props.selected ? detail.isFetching : shops.isFetching
  const setResultPage = (value: number) => {
    setPageFilter(filterKey)
    setPage(value)
    document.getElementById('market-shop-results')?.scrollIntoView({ block: 'start' })
  }
  useEffect(() => {
    if (
      pagination &&
      activePage > Math.max(1, Math.ceil(pagination.total / pagination.page_size))
    ) {
      void client.invalidateQueries({
        queryKey: props.selected ? ['market-shop', props.selected] : ['market-shops'],
        refetchType: 'none',
      })
      setPageFilter(filterKey)
      setPage(Math.max(1, Math.ceil(pagination.total / pagination.page_size)))
    }
  }, [pagination, activePage, filterKey, client, props.selected])
  return (
    <section className="market-shops" id="market-shop-results" aria-label={t('渠道主店铺')}>
      <ErrorMessage error={props.selected ? detail.error : shops.error} />
      <label className="field" htmlFor="shop-search">
        <span>{t('搜索店铺或模型')}</span>
        <input
          id="shop-search"
          value={props.search}
          onChange={(event) => props.onSearch(event.target.value)}
        />
      </label>
      <div className="market-browser-tools">
        <label className="field" htmlFor="shop-model">
          <span>{t('选择比较的模型')}</span>
          <select
            id="shop-model"
            value={props.model}
            onChange={(event) => props.onModel(event.target.value)}
          >
            <option value="">{t('全部模型')}</option>
            {models.map((model) => (
              <option key={model} value={model}>
                {model}
              </option>
            ))}
          </select>
        </label>
        <Button
          variant="quiet"
          className="market-filter-toggle"
          aria-expanded={advancedFilters}
          aria-controls="shop-filter-panel"
          onClick={() => setAdvancedFilters(!advancedFilters)}
        >
          {t('筛选与排序')}
        </Button>
        <div id="shop-filter-panel" className="market-filter-panel" data-expanded={advancedFilters}>
          <label className="field" htmlFor="shop-tag">
            <span>{t('厂商标签')}</span>
            <select
              id="shop-tag"
              value={props.tag}
              onChange={(event) => props.onTag(event.target.value)}
            >
              <option value="">{t('全部厂商')}</option>
              {marketTags.map((tag) => (
                <option key={tag.value} value={tag.value}>
                  {t(tag.label)}
                </option>
              ))}
            </select>
          </label>
        </div>
      </div>

      {props.selected ? (
        <>
          <Button variant="quiet" onClick={() => props.onSelect(undefined)}>
            <ArrowLeft size={16} aria-hidden />
            {t('全部店铺')}
          </Button>
          {detail.isPending && <Loading />}
          {shop && (
            <>
              <header className="market-shop-heading">
                <Store size={28} aria-hidden />
                <div>
                  <h2>{shop.name}</h2>
                  <p className="subtle">
                    {t('店铺 ID')} <code dir="ltr">{shop.id}</code>
                  </p>
                </div>
              </header>
              {shop.description && <p className="market-shop-description">{shop.description}</p>}
              {shop.rating && <RatingSummary {...shop.rating} />}
              <p className="subtle">
                {t('按分组查看模型报价和服务质量。店铺名称不代表平台认证。')}
              </p>
              <div className="market-shop-groups">
                {detail.data?.groups.map((group) => (
                  <article key={group.id}>
                    <header>
                      <div>
                        <h3>{group.system_display_name}</h3>
                        <p className="subtle">
                          {t('分组 ID')} <code dir="ltr">{group.id}</code>
                        </p>
                      </div>
                      <Button variant="secondary" onClick={() => props.onGroup(group.id)}>
                        {t('查看分组')}
                        <ArrowRight size={16} aria-hidden />
                      </Button>
                    </header>
                    {group.remark && <p>{group.remark}</p>}
                    <MarketQuality group={group} />
                    {group.rating && <RatingSummary {...group.rating} />}
                    <RecentRequests group={group} />
                    {props.model && effectiveGroupQuote(group, props.model) && (
                      <div className="market-row-quote">
                        <strong>{props.model}</strong>
                        <ModelGroupPrice group={effectiveGroupQuote(group, props.model)!} />
                      </div>
                    )}
                  </article>
                ))}
                {!detail.data?.groups.some(
                  (group) => !props.model || group.declared_models?.includes(props.model),
                ) && <EmptyState title="这家店铺暂无符合条件的公开分组" />}
              </div>
            </>
          )}
        </>
      ) : (
        <>
          <div className="market-shop-intro">
            <h2>{t('找到适合的渠道主')}</h2>
            <p className="subtle">
              {t('店铺展示渠道主的公开服务；实际价格与质量以具体分组为准。')}
            </p>
          </div>
          {shops.isPending && <Loading />}
          <div className="market-shop-list">
            {visibleShops?.map((shop) => (
              <article key={shop.id}>
                <Store size={24} aria-hidden />
                <div>
                  <h3>{shop.name}</h3>
                  <p className="subtle">
                    {t('店铺 ID')} <code dir="ltr">{shop.id}</code> · {shop.group_count}{' '}
                    {t('个公开分组')}
                  </p>
                  {shop.description && (
                    <p className="market-shop-description">{shop.description}</p>
                  )}
                  {shop.rating && <RatingSummary {...shop.rating} />}
                  <ul className="market-model-preview" aria-label={t('支持的模型')}>
                    {shop.declared_models?.slice(0, 4).map((model) => (
                      <li key={model}>
                        <code>{model}</code>
                      </li>
                    ))}
                    {(shop.declared_models?.length ?? 0) > 4 && (
                      <li>+{shop.declared_models.length - 4}</li>
                    )}
                  </ul>
                </div>
                <Button variant="secondary" onClick={() => props.onSelect(shop.id)}>
                  {t('进入店铺')}
                  <ArrowRight size={16} aria-hidden />
                </Button>
              </article>
            ))}
          </div>
          {!shops.isPending && !shops.error && !visibleShops?.length && (
            <EmptyState
              title={props.search || props.model || props.tag ? '没有匹配的店铺' : '暂无公开店铺'}
            />
          )}
        </>
      )}
      {pagination && pagination.total > pagination.page_size && (
        <nav className="market-pagination" aria-label={t('分页')}>
          <span className="subtle" aria-live="polite">
            {pagination.page} / {Math.ceil(pagination.total / pagination.page_size)} ·{' '}
            {pagination.total}
          </span>
          <div className="market-pagination-actions">
            <Button
              variant="secondary"
              disabled={activePage <= 1 || fetching}
              onClick={() => setResultPage(activePage - 1)}
            >
              {t('上一页')}
            </Button>
            <Button
              variant="secondary"
              disabled={activePage * pagination.page_size >= pagination.total || fetching}
              onClick={() => setResultPage(activePage + 1)}
            >
              {t('下一页')}
            </Button>
          </div>
        </nav>
      )}
    </section>
  )
}
