import { useEffect, useState } from 'react'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { ArrowRight, Search, Server, ShieldCheck, Star } from 'lucide-react'
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions, keysOptions, sessionOptions } from '../lib/queries'
import {
  groupPageOptions,
  groupDetailOptions,
  groupFavoritesOptions,
  type MarketBrowseFilters,
  type PublicCatalogModel,
} from '../lib/public-catalog'
import { useTranslation } from '../lib/i18n'
import { DataTable } from '../components/data-table'
import {
  Button,
  CopyButton,
  CopyField,
  EmptyState,
  ErrorMessage,
  Field,
  Loading,
  PageHeader,
  Status,
  Tabs,
} from '../components/ui'
import { marketTags, marketTagLabel, relatedGroups } from '../features/channelmarket/tags'
import { MarketForm, text, integer, factor } from '../features/channelmarket/form'
import { AddToRoutePool, MarketRoutePools } from '../features/channelmarket/route-pools'
import { RecentRequests } from '../features/channelmarket/recent-requests'
import { ModelGroupPrice } from '../features/public/model-group-price'
import { MarketQuality } from '../features/channelmarket/market-quality'
import { MarketShops } from '../features/channelmarket/shops'
import { GroupRatings, RatingSummary } from '../features/channelmarket/ratings'
import { MarketComparison } from '../features/channelmarket/market-comparison'
import { ModelInsights } from '../features/channelmarket/model-insights'
import {
  comparablePrice,
  effectiveGroupQuote,
  type MarketPriceBasis,
} from '../features/channelmarket/market-pricing'
import '../styles/market-workspace.css'
import '../styles/market-browse.css'

export const noticeOptions = () =>
  resourceOptions('market-notices', (signal) =>
    api.GET('/api/marketplace/multiplier-notices', { signal }).then(unwrap),
  )

function MarketQuote(props: {
  group: PublicCatalogModel['groups'][number]
  basis: MarketPriceBasis
}) {
  const { t } = useTranslation()
  const price = props.group.price
  if (price?.mode === 'expression' || price?.mode === 'tiered_expr')
    return <span className="subtle">{t('按动态规则计价')}</span>
  if (price?.mode !== 'token' && price?.mode !== 'per_token')
    return <ModelGroupPrice group={props.group} />
  // Token quotes retain an explicitly labelled input price in a per-request comparison.
  const basis = props.basis === 'request' ? 'input' : props.basis
  const comparison = comparablePrice(price, basis)
  const label = {
    input: '输入 credits / 百万 tokens',
    output: '输出 credits / 百万 tokens',
    'cache-read': '缓存读取 credits / 百万 tokens',
    'cache-write': '缓存写入 credits / 百万 tokens',
  }[basis]
  return (
    <dl className="model-price-lines">
      <div>
        <dt className="subtle">{t(label)}</dt>
        <dd className="tabular" dir="ltr">
          {comparison?.value ?? t('暂无数据')}
        </dd>
      </div>
    </dl>
  )
}

export default function MarketPage() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const navigate = useNavigate({ from: '/channel-market' })
  const routeSearch = useSearch({ from: '/_market/channel-market' })
  const session = useQuery(sessionOptions())
  const signedIn = Boolean(session.data)
  const [search, setSearch] = useState('')
  const [tab, setTab] = useState(
    routeSearch.shop || routeSearch.view === 'shops' ? 'shops' : 'browse',
  )
  const [scope, setScope] = useState<NonNullable<MarketBrowseFilters['scope']>>('all')
  const [tag, setTag] = useState('')
  const [advancedFilters, setAdvancedFilters] = useState(false)
  const [sort, setSort] = useState<NonNullable<MarketBrowseFilters['sort']>>(
    routeSearch.model ? 'price' : 'recommended',
  )
  const [priceBasis, setPriceBasis] = useState<MarketPriceBasis>('input')
  const model = routeSearch.model ?? ''
  const [comparison, setComparison] = useState<{ model: string; ids: string[] }>({
    model,
    ids: [],
  })
  const [page, setPage] = useState(1)
  const [debouncedSearch, setDebouncedSearch] = useState(search)
  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedSearch(search), 250)
    return () => window.clearTimeout(timer)
  }, [search])
  const filters = { search: debouncedSearch, model, tag, scope, sort, price_basis: priceBasis }
  const filterKey = JSON.stringify(filters)
  const [pageFilter, setPageFilter] = useState(filterKey)
  const activePage = pageFilter === filterKey ? page : 1
  useEffect(() => {
    if (pageFilter !== filterKey) {
      setPageFilter(filterKey)
      setPage(1)
    }
  }, [filterKey, pageFilter])
  const groupsQuery = useQuery({
    ...groupPageOptions({ ...filters, page: activePage, page_size: 24 }, signedIn),
    enabled: tab === 'browse',
    refetchInterval: 60_000,
    refetchIntervalInBackground: false,
  })
  const selectedQuery = useQuery({
    ...groupDetailOptions(routeSearch.group ?? ''),
    enabled: Boolean(routeSearch.group),
    refetchInterval: 60_000,
    refetchIntervalInBackground: false,
  })
  const keysQuery = useQuery({ ...keysOptions(), enabled: signedIn })
  const noticesQuery = useQuery({ ...noticeOptions(), enabled: signedIn })
  const groups = groupsQuery.data?.groups ?? []
  const visibleFavoriteIDs = [
    ...new Set([
      ...groups.map((group) => group.group_id),
      ...(selectedQuery.data ? [selectedQuery.data.group_id] : []),
    ]),
  ].sort()
  const favoritesQuery = useQuery({
    ...groupFavoritesOptions(1, 100, visibleFavoriteIDs),
    enabled: signedIn && visibleFavoriteIDs.length > 0,
  })
  const favoriteIDs = new Set(favoritesQuery.data?.items.map((group) => group.group_id))
  const favorite = useMutation({
    mutationFn: (body: { group_id: string; favorite: boolean }) =>
      api.PUT('/api/marketplace/group-favorites', { body }).then(unwrap),
    onSuccess: () => client.invalidateQueries({ queryKey: ['group-favorites'] }),
  })
  const keys = keysQuery.data ?? []
  const notices = noticesQuery.data ?? []
  const selected = selectedQuery.error ? undefined : selectedQuery.data
  const comparisonIds = comparison.model === model ? comparison.ids : []
  const comparisonQueries = useQueries({
    queries: comparisonIds.map((id) => ({
      ...groupDetailOptions(id),
      refetchInterval: 60_000,
      refetchIntervalInBackground: false,
    })),
  })
  const comparisonGroups = comparisonQueries.flatMap((query) =>
    !query.error && query.data?.declared_models?.includes(model) ? [query.data] : [],
  )
  const toggleComparison = (id: string) => {
    setComparison((previous) => {
      const ids = previous.model === model ? previous.ids : []
      return {
        model,
        ids: ids.includes(id)
          ? ids.filter((value) => value !== id)
          : ids.length < 4
            ? [...ids, id]
            : ids,
      }
    })
  }
  const modelNames = groupsQuery.data?.models ?? []
  const returnTo = `/channel-market${routeSearch.group || model ? `?${new URLSearchParams({ ...(routeSearch.group ? { group: routeSearch.group } : {}), ...(model ? { model } : {}) })}` : ''}`
  const setModel = (value: string) => {
    setSort(value ? 'price' : 'recommended')
    void navigate({ search: { ...routeSearch, model: value || undefined } })
  }
  useEffect(() => {
    setTab(routeSearch.shop || routeSearch.view === 'shops' ? 'shops' : 'browse')
  }, [routeSearch.shop, routeSearch.view, routeSearch.group])
  useEffect(() => {
    if (selected)
      document
        .getElementById(`market-group-${selected.group_id}`)
        ?.scrollIntoView({ block: 'nearest' })
  }, [selected?.group_id])
  const [acceptedGroup, setAcceptedGroup] = useState('')
  const [message, setMessage] = useState('')
  const [newSecret, setNewSecret] = useState('')
  const [boundGroup, setBoundGroup] = useState('')
  const activeKeys = keys.filter((key) => key.status === 'active')
  const rows = groups.map((group) => (selected?.group_id === group.group_id ? selected : group))
  if (selected && !rows.some((group) => group.group_id === selected.group_id))
    rows.unshift(selected)
  const pagination = groupsQuery.data?.pagination
  const setResultPage = (value: number) => {
    setPageFilter(filterKey)
    setPage(value)
    document.getElementById('market-results')?.scrollIntoView({ block: 'start' })
  }
  useEffect(() => {
    if (
      pagination &&
      activePage > Math.max(1, Math.ceil(pagination.total / pagination.page_size))
    ) {
      void client.invalidateQueries({
        queryKey: [signedIn ? 'market-groups' : 'public-group-page'],
        refetchType: 'none',
      })
      setPageFilter(filterKey)
      setPage(Math.max(1, Math.ceil(pagination.total / pagination.page_size)))
    }
  }, [pagination, activePage, filterKey, client, signedIn])
  const refresh = () => {
    setMessage('已保存')
    void client.invalidateQueries({ queryKey: ['market-groups'] })
    void client.invalidateQueries({ queryKey: ['keys'] })
    void client.invalidateQueries({ queryKey: ['public-groups'] })
    void client.invalidateQueries({ queryKey: ['conversation-groups'] })
  }
  const bind = useMutation({
    onMutate: () => {
      setBoundGroup('')
      setMessage('')
    },
    mutationFn: (body: { id: string; token: bigint }) =>
      api.POST('/api/marketplace/groups/{id}/bind-token', {
        params: { path: { id: body.id } },
        body: { token_id: body.token },
      }),
    onSuccess: (_result, body) => {
      setBoundGroup(body.id)
      setMessage('分组已绑定，可前往对话测试。')
      void client.invalidateQueries({ queryKey: ['keys'] })
      void client.invalidateQueries({ queryKey: ['conversation-groups'] })
    },
  })
  const createKey = useMutation({
    mutationFn: (group: string) =>
      api
        .POST('/api/marketplace/groups/{id}/bind-token', {
          params: { path: { id: group } },
          body: { token_id: 0 },
        })
        .then(unwrap),
    onSuccess: (key, group) => {
      setNewSecret(key.api_key ?? '')
      setBoundGroup(group)
      setMessage('分组已绑定，可前往对话测试。')
      void client.invalidateQueries({ queryKey: ['keys'] })
      void client.invalidateQueries({ queryKey: ['conversation-groups'] })
    },
  })
  const invite = useMutation({
    mutationFn: (token: string) =>
      api.POST('/api/marketplace/invites/accept', { body: { token } }).then(unwrap),
    onSuccess: (result) => {
      setAcceptedGroup(result.group_id)
      refresh()
    },
  })
  const bargain = useMutation({
    mutationFn: (body: { id: string; multiplier: number; reason: string }) =>
      api.POST('/api/marketplace/groups/{id}/bargain-requests', {
        params: { path: { id: body.id } },
        body: { proposed_multiplier: body.multiplier, reason: body.reason },
      }),
    onSuccess: () => setMessage('议价申请已提交'),
  })
  const feedback = useMutation({
    mutationFn: (id: string) =>
      api.POST('/api/marketplace/groups/{id}/feedback', { params: { path: { id } } }),
    onSuccess: () => setMessage('反馈已提交'),
  })
  const read = useMutation({
    mutationFn: (id: string) =>
      api.POST('/api/marketplace/multiplier-notices/{id}/read', { params: { path: { id } } }),
    onSuccess: () => client.invalidateQueries({ queryKey: ['market-notices'] }),
  })
  const keySelect = (name: string) => (
    <label className="field" htmlFor={name}>
      <span>API Key</span>
      <select id={name} name={name} required defaultValue="">
        <option value="" disabled>
          {t('选择 Key')}
        </option>
        {activeKeys.map((key) => (
          <option key={String(key.id)} value={String(key.id)}>
            {key.name}
          </option>
        ))}
      </select>
    </label>
  )
  return (
    <div
      className={signedIn ? 'market-workspace' : 'market-workspace site-container market-public'}
    >
      <PageHeader
        title="渠道市场"
        description="按模型比较实际报价与服务质量，再选择分组。倍率仅是计费系数。"
        action={
          <Link
            to={signedIn ? '/my-channels' : '/sign-in'}
            search={signedIn ? {} : { returnTo: '/my-channels' }}
            className="button button-secondary"
          >
            {t('渠道工作台')}
            <ArrowRight size={16} aria-hidden />
          </Link>
        }
      />
      <ErrorMessage
        error={
          groupsQuery.error ??
          selectedQuery.error ??
          comparisonQueries.find((query) => query.error)?.error ??
          (signedIn ? (keysQuery.error ?? noticesQuery.error) : null) ??
          createKey.error ??
          bind.error ??
          invite.error ??
          bargain.error ??
          feedback.error ??
          read.error ??
          (signedIn ? favoritesQuery.error : null) ??
          favorite.error
        }
      />
      {newSecret && (
        <section className="secret-panel">
          <span>{t('新 API Key')}</span>
          <CopyField value={newSecret} label="复制 Key" />
          <Button variant="quiet" onClick={() => setNewSecret('')}>
            {t('关闭')}
          </Button>
        </section>
      )}
      {message && (
        <p role="status" className="notice">
          {t(message)}
        </p>
      )}
      <Tabs
        label="市场分区"
        value={tab}
        onValueChange={(value) => {
          setTab(value)
          if (value === 'browse' || value === 'shops')
            void navigate({
              search: {
                ...routeSearch,
                view: value === 'shops' ? 'shops' : 'groups',
                shop: value === 'shops' ? routeSearch.shop : undefined,
                group: value === 'shops' ? undefined : routeSearch.group,
              },
            })
        }}
        items={[
          {
            value: 'browse',
            label: '分组',
            content: (
              <>
                <div className="market-browser-tools market-primary-filters">
                  <label className="field market-search" htmlFor="market-search">
                    <span>{t('搜索渠道或模型')}</span>
                    <div className="market-search-input">
                      <Search size={18} aria-hidden />
                      <input
                        id="market-search"
                        value={search}
                        onChange={(event) => setSearch(event.target.value)}
                        placeholder={t('渠道名称、模型或厂商')}
                      />
                    </div>
                  </label>
                  <label className="field" htmlFor="market-model">
                    <span>{t('选择比较的模型')}</span>
                    <select
                      id="market-model"
                      value={model}
                      onChange={(event) => setModel(event.target.value)}
                    >
                      <option value="">{t('全部模型')}</option>
                      {model && !modelNames.includes(model) && (
                        <option value={model}>{model}</option>
                      )}
                      {modelNames.map((name) => (
                        <option key={name} value={name}>
                          {name}
                        </option>
                      ))}
                    </select>
                  </label>
                  <Button
                    variant="quiet"
                    className="market-filter-toggle"
                    aria-expanded={advancedFilters}
                    aria-controls="market-filter-panel"
                    onClick={() => setAdvancedFilters(!advancedFilters)}
                  >
                    {t('筛选与排序')}
                  </Button>
                  <div
                    id="market-filter-panel"
                    className="market-filter-panel"
                    data-expanded={advancedFilters}
                  >
                    <label className="field" htmlFor="market-scope">
                      <span>{t('渠道范围')}</span>
                      <select
                        id="market-scope"
                        value={scope}
                        onChange={(event) =>
                          setScope(event.target.value as NonNullable<MarketBrowseFilters['scope']>)
                        }
                      >
                        <option value="all">{t('全部可访问分组')}</option>
                        <option value="public">{t('公开渠道')}</option>
                        {signedIn && <option value="private">{t('私有渠道')}</option>}
                      </select>
                    </label>
                    <label className="field" htmlFor="market-sort">
                      <span>{t('排序')}</span>
                      <select
                        id="market-sort"
                        value={sort}
                        onChange={(event) =>
                          setSort(event.target.value as NonNullable<MarketBrowseFilters['sort']>)
                        }
                      >
                        <option value="recommended">{t('推荐：可靠性优先')}</option>
                        <option value="multiplier">{t('倍率从低到高')}</option>
                        <option value="price" disabled={!model}>
                          {t('所选模型报价从低到高')}
                        </option>
                        <option value="models">{t('模型数从多到少')}</option>
                        <option value="success">{t('调用成功率从高到低')}</option>
                        <option value="name">{t('名称')}</option>
                      </select>
                    </label>
                    <label className="field" htmlFor="market-tag">
                      <span>{t('厂商标签')}</span>
                      <select
                        id="market-tag"
                        value={tag}
                        onChange={(event) => setTag(event.target.value)}
                      >
                        <option value="">{t('全部厂商')}</option>
                        {marketTags.map((item) => (
                          <option key={item.value} value={item.value}>
                            {t(item.label)}
                          </option>
                        ))}
                      </select>
                    </label>
                    {model && (
                      <label className="field" htmlFor="market-price-basis">
                        <span>{t('比较价格口径')}</span>
                        <select
                          id="market-price-basis"
                          value={priceBasis}
                          onChange={(event) =>
                            setPriceBasis(event.target.value as MarketPriceBasis)
                          }
                        >
                          <option value="input">{t('输入')}</option>
                          <option value="output">{t('输出')}</option>
                          <option value="cache-read">{t('缓存读取')}</option>
                          <option value="cache-write">{t('缓存写入')}</option>
                          <option value="request">{t('按次计价')}</option>
                        </select>
                      </label>
                    )}
                  </div>
                </div>
                <div className="market-result-bar">
                  <span>{t('可访问分组')}</span>
                  <strong>{pagination?.total ?? groups.length}</strong>
                </div>
                <p className="subtle market-quote-note">
                  {t('报价已包含分组倍率，单位为 credits；动态计价与不同计费单位分开展示。')}{' '}
                  {t('厂商标签由渠道主选择，仅供筛选参考。')}
                </p>
                <p className="market-comparison-hint" id="market-compare-hint">
                  {t(
                    model
                      ? '选择二至四个分组加入比较。列表概览覆盖全部模型，比较表与详情使用所选模型数据。'
                      : '先选择一个模型，再勾选分组比较报价、预估费用与真实服务质量。',
                  )}
                </p>
                {model && comparisonGroups.length > 0 && (
                  <MarketComparison
                    key={model}
                    groups={comparisonGroups}
                    model={model}
                    onRemove={toggleComparison}
                    onClear={() => setComparison({ model, ids: [] })}
                    onInspect={(group) => {
                      setSearch('')
                      setScope('all')
                      setTag('')
                      void navigate({ search: { ...routeSearch, group: group.id } })
                    }}
                  />
                )}
                {model && comparisonIds.length > 0 && comparisonGroups.length === 0 && (
                  <Button variant="quiet" onClick={() => setComparison({ model, ids: [] })}>
                    {t('清空比较')}
                  </Button>
                )}
                {sort === 'recommended' && (
                  <p className="subtle market-quote-note">
                    {t(
                      '优先展示有足够近期请求样本的分组，再按成功率置信下界排序；同分比较所选模型报价。',
                    )}
                  </p>
                )}
                {routeSearch.group && !selectedQuery.isPending && selectedQuery.error && (
                  <p className="notice" role="status">
                    {t('所选分组不存在、已下架或无访问权限。')}
                  </p>
                )}
                {(groupsQuery.isPending || (routeSearch.group && selectedQuery.isPending)) && (
                  <Loading />
                )}
                <div className="market-list" id="market-results" aria-busy={groupsQuery.isFetching}>
                  {rows.map((group) => (
                    <article
                      className="market-listing"
                      key={group.group_id}
                      id={`market-group-${group.group_id}`}
                    >
                      <div className="market-listing-main">
                        <div className="market-provider-mark">
                          <Server size={22} aria-hidden />
                        </div>
                        <div className="market-listing-content">
                          <div className="market-listing-name">
                            <h2>{group.system_display_name}</h2>
                            <Status
                              value={group.visibility === 'private' ? '私有渠道' : '公开渠道'}
                            />
                          </div>
                          <p className="market-listing-source">
                            {group.approved_source_label || group.provider_type}
                            <span aria-hidden> · </span>
                            {group.declared_models?.length ?? 0} {t('个模型')}
                          </p>
                          <div className="market-stable-id">
                            <span>{t('分组 ID')}</span> <code dir="ltr">{group.id}</code>
                            <CopyButton value={group.id} label="复制分组 ID" />
                          </div>
                          <label className="market-compare-choice">
                            <input
                              type="checkbox"
                              checked={comparisonIds.includes(group.group_id)}
                              disabled={
                                !model ||
                                (comparisonIds.length >= 4 &&
                                  !comparisonIds.includes(group.group_id))
                              }
                              onChange={() => toggleComparison(group.group_id)}
                              aria-describedby="market-compare-hint"
                              aria-label={`${t('加入比较')} ${group.system_display_name}`}
                            />
                            {t('加入比较')}
                          </label>
                          {group.shop && (
                            <button
                              type="button"
                              className="market-shop-link"
                              onClick={() => {
                                setTab('shops')
                                void navigate({
                                  search: {
                                    model: model || undefined,
                                    shop: group.shop!.id,
                                    view: 'shops',
                                  },
                                })
                              }}
                            >
                              {group.shop.name} · {t('进入店铺')}
                            </button>
                          )}
                          {!!group.tags?.length && (
                            <ul className="market-purpose-tags" aria-label={t('厂商标签')}>
                              {group.tags.map((value) => (
                                <li key={value}>
                                  <button type="button" onClick={() => setTag(value)}>
                                    {t(marketTagLabel(value))}
                                  </button>
                                </li>
                              ))}
                            </ul>
                          )}
                          {group.remark && <p className="market-listing-remark">{group.remark}</p>}
                          <ul className="market-model-preview" aria-label={t('支持的模型')}>
                            {group.declared_models?.slice(0, 4).map((model) => (
                              <li key={model}>
                                <code>{model}</code>
                              </li>
                            ))}
                            {(group.declared_models?.length ?? 0) > 4 && (
                              <li>+{group.declared_models!.length - 4}</li>
                            )}
                          </ul>
                        </div>
                        <div className="market-listing-quality">
                          {group.verification_status === 'passed' ? (
                            <span className="market-verified">
                              <ShieldCheck size={15} aria-hidden />
                              {t('连通验证通过')}
                            </span>
                          ) : (
                            <Status value={group.verification_status} />
                          )}
                        </div>
                        <div className="market-listing-price">
                          <span>{t('计费倍率')}</span>
                          <strong>{String(group.multiplier)}×</strong>
                        </div>
                        <div className="market-listing-actions">
                          {signedIn && (
                            <Button
                              variant="quiet"
                              aria-pressed={favoriteIDs.has(group.group_id)}
                              disabled={
                                favorite.isPending ||
                                favoritesQuery.isPending ||
                                favoritesQuery.isError
                              }
                              onClick={() =>
                                favorite.mutate({
                                  group_id: group.group_id,
                                  favorite: !favoriteIDs.has(group.group_id),
                                })
                              }
                            >
                              <Star
                                size={16}
                                aria-hidden
                                fill={favoriteIDs.has(group.group_id) ? 'currentColor' : 'none'}
                              />
                              {t(favoriteIDs.has(group.group_id) ? '已收藏' : '收藏分组')}
                            </Button>
                          )}
                          <Button
                            variant={
                              selected?.group_id === group.group_id ? 'secondary' : 'primary'
                            }
                            aria-expanded={selected?.group_id === group.group_id}
                            aria-controls={`market-detail-${group.group_id}`}
                            onClick={() => {
                              void navigate({
                                search: {
                                  ...routeSearch,
                                  group:
                                    selected?.group_id === group.group_id ? undefined : group.id,
                                },
                              })
                              setMessage('')
                            }}
                          >
                            {t(selected?.group_id === group.group_id ? '收起' : '选择渠道')}
                          </Button>
                        </div>
                      </div>
                      <MarketQuality
                        group={group}
                        expanded={selected?.group_id === group.group_id}
                      />
                      <RecentRequests group={group} />
                      {group.rating && (
                        <div className="market-group-rating">
                          <RatingSummary {...group.rating} />
                        </div>
                      )}
                      {model && (
                        <div className="market-row-quote">
                          <strong>{model}</strong>
                          {effectiveGroupQuote(group, model) ? (
                            <MarketQuote
                              group={effectiveGroupQuote(group, model)!}
                              basis={priceBasis}
                            />
                          ) : (
                            <span className="subtle">{t('暂未提供价格')}</span>
                          )}
                        </div>
                      )}
                      {selected?.group_id === group.group_id && (
                        <section
                          id={`market-detail-${group.group_id}`}
                          className="market-listing-detail"
                          aria-label={`${group.system_display_name} ${t('渠道详情')}`}
                        >
                          <h3>{t('支持的模型')}</h3>
                          <ul className="market-model-preview">
                            {group.declared_models?.map((model) => (
                              <li key={model}>
                                <code>{model}</code>
                              </li>
                            ))}
                          </ul>
                          <details className="market-price-details">
                            <summary>{t('模型价格')}</summary>
                            <div className="market-model-quotes">
                              {(model ? [model] : (group.declared_models ?? [])).map((name) => {
                                const quote = effectiveGroupQuote(group, name)
                                return (
                                  <section key={name}>
                                    <h4>
                                      <code>{name}</code>
                                    </h4>
                                    {quote ? (
                                      <ModelGroupPrice group={quote} />
                                    ) : (
                                      <span className="subtle">{t('暂未提供价格')}</span>
                                    )}
                                  </section>
                                )
                              })}
                            </div>
                          </details>
                          {model ? (
                            <ModelInsights group={group.group_id} model={model} />
                          ) : (
                            <label className="field" htmlFor={`market-insight-model-${group.id}`}>
                              <span>{t('查看模型服务质量')}</span>
                              <select
                                id={`market-insight-model-${group.id}`}
                                value=""
                                onChange={(event) => setModel(event.target.value)}
                              >
                                <option value="">{t('选择比较的模型')}</option>
                                {group.declared_models?.map((name) => (
                                  <option key={name} value={name}>
                                    {name}
                                  </option>
                                ))}
                              </select>
                            </label>
                          )}
                          <GroupRatings
                            id={group.group_id}
                            signedIn={signedIn}
                            returnTo={returnTo}
                          />
                          {!signedIn ? (
                            <Link
                              to="/sign-in"
                              search={{ returnTo }}
                              className="button button-primary"
                            >
                              {t('登录后绑定此分组')}
                            </Link>
                          ) : activeKeys.length ? (
                            <MarketForm
                              pending={bind.isPending}
                              submit="绑定 Key"
                              onSubmit={(fields) =>
                                bind.mutate({
                                  id: group.group_id,
                                  token: integer(fields, 'market-key'),
                                })
                              }
                            >
                              {keySelect('market-key')}
                            </MarketForm>
                          ) : (
                            <EmptyState
                              title="暂无可绑定的 API Key"
                              action={
                                <Button
                                  variant="secondary"
                                  loading={createKey.isPending}
                                  disabled={createKey.isPending}
                                  onClick={() => createKey.mutate(group.group_id)}
                                >
                                  {t('创建 API Key')}
                                </Button>
                              }
                            />
                          )}
                          {boundGroup === group.group_id && (
                            <Link
                              to="/playground"
                              search={{
                                group: group.routing_group,
                                model: model || group.declared_models?.[0],
                              }}
                              className="button button-secondary"
                            >
                              {t('使用此分组对话')}
                            </Link>
                          )}
                          {signedIn && (
                            <AddToRoutePool group={group} onOpenPools={() => setTab('routes')} />
                          )}
                          {signedIn && (
                            <details className="market-price-details">
                              <summary>{t('议价与反馈')}</summary>
                              <MarketForm
                                pending={bargain.isPending}
                                submit="申请专属倍率"
                                onSubmit={(fields) =>
                                  bargain.mutate({
                                    id: group.group_id,
                                    multiplier: factor(fields),
                                    reason: text(fields, 'reason'),
                                  })
                                }
                              >
                                <Field
                                  name="multiplier"
                                  label="期望倍率"
                                  required
                                  defaultValue={String(group.multiplier)}
                                />
                                <Field name="reason" label="议价理由" required maxLength={1000} />
                              </MarketForm>
                              <Button
                                variant="quiet"
                                disabled={feedback.isPending}
                                onClick={() => feedback.mutate(group.group_id)}
                              >
                                {t('提交渠道服务反馈')}
                              </Button>
                            </details>
                          )}
                          {relatedGroups(groups, group, model).length > 0 && (
                            <div className="market-related">
                              <h3>{t('相关分组')}</h3>
                              <p className="subtle">{t('按共同厂商标签与支持的模型推荐。')}</p>
                              <ul>
                                {relatedGroups(groups, group, model).map((related) => (
                                  <li key={related.group_id}>
                                    <Link
                                      to="/channel-market"
                                      search={{
                                        group: related.id,
                                        model: model || undefined,
                                      }}
                                    >
                                      {related.system_display_name}
                                    </Link>
                                    <span className="subtle">
                                      {' '}
                                      ·{' '}
                                      {related.tags
                                        ?.map((tag) => t(marketTagLabel(tag)))
                                        .join(' · ')}
                                    </span>
                                  </li>
                                ))}
                              </ul>
                            </div>
                          )}
                        </section>
                      )}
                    </article>
                  ))}
                  {!groupsQuery.isPending && !groupsQuery.error && !rows.length && (
                    <EmptyState
                      title={
                        search || scope !== 'all' || model || tag
                          ? '没有匹配的渠道'
                          : '暂无可用渠道'
                      }
                      action={
                        search || scope !== 'all' || model || tag ? (
                          <Button
                            variant="secondary"
                            onClick={() => {
                              setSearch('')
                              setScope('all')
                              setTag('')
                              setModel('')
                            }}
                          >
                            {t('清除筛选')}
                          </Button>
                        ) : undefined
                      }
                    />
                  )}
                </div>
                {pagination && pagination.total > pagination.page_size && (
                  <nav className="market-pagination" aria-label={t('分页')}>
                    <span className="subtle" aria-live="polite">
                      {pagination.page} / {Math.ceil(pagination.total / pagination.page_size)} ·{' '}
                      {pagination.total} {t('可访问分组')}
                    </span>
                    <div className="market-pagination-actions">
                      <Button
                        variant="secondary"
                        disabled={activePage <= 1 || groupsQuery.isFetching}
                        onClick={() => setResultPage(activePage - 1)}
                      >
                        {t('上一页')}
                      </Button>
                      <Button
                        variant="secondary"
                        disabled={
                          activePage * pagination.page_size >= pagination.total ||
                          groupsQuery.isFetching
                        }
                        onClick={() => setResultPage(activePage + 1)}
                      >
                        {t('下一页')}
                      </Button>
                    </div>
                  </nav>
                )}
              </>
            ),
          },
          {
            value: 'shops',
            label: '店铺',
            content: (
              <MarketShops
                search={search}
                onSearch={setSearch}
                tag={tag}
                onTag={setTag}
                onModel={setModel}
                selected={routeSearch.shop}
                model={model}
                onSelect={(shop) => {
                  void navigate({ search: { model: model || undefined, shop, view: 'shops' } })
                }}
                onGroup={(group) => {
                  setTab('browse')
                  void navigate({ search: { group, model: model || undefined, view: 'groups' } })
                }}
              />
            ),
          },
          {
            value: 'routes',
            label: '路由池',
            content: signedIn ? (
              <MarketRoutePools />
            ) : (
              <EmptyState
                title="登录后配置你的路由池"
                action={
                  <Link to="/sign-in" search={{ returnTo }} className="button button-primary">
                    {t('登录')}
                  </Link>
                }
              />
            ),
          },
          ...(signedIn
            ? [
                {
                  value: 'invites',
                  label: '邀请与通知',
                  content: (
                    <>
                      <section className="section">
                        <h2>{t('接受私有组邀请')}</h2>
                        <MarketForm
                          pending={invite.isPending}
                          submit="接受邀请"
                          onSubmit={(fields) => invite.mutate(text(fields, 'invite-token'))}
                        >
                          <Field name="invite-token" label="邀请令牌" required />
                        </MarketForm>
                        {acceptedGroup && (
                          <>
                            <p className="notice" role="status">
                              {t('已获得分组访问权限')} <code>{acceptedGroup}</code>
                            </p>
                            {activeKeys.length ? (
                              <MarketForm
                                pending={bind.isPending}
                                submit="绑定私有组"
                                onSubmit={(fields) =>
                                  bind.mutate({
                                    id: acceptedGroup,
                                    token: integer(fields, 'private-key'),
                                  })
                                }
                              >
                                {keySelect('private-key')}
                              </MarketForm>
                            ) : (
                              <Link to="/keys" className="button button-secondary">
                                {t('创建 API Key')}
                              </Link>
                            )}
                          </>
                        )}
                      </section>
                      <section className="section">
                        <h2>{t('倍率变更通知')}</h2>
                        <DataTable
                          rows={notices}
                          rowKey={(row) => String(row.id)}
                          columns={[
                            { label: '渠道', render: (row) => String(row.channel_id) },
                            {
                              label: '原倍率',
                              render: (row) => Number(row.previous_multiplier_ppm) / 1_000_000,
                              numeric: true,
                            },
                            {
                              label: '新倍率',
                              render: (row) => Number(row.multiplier_ppm) / 1_000_000,
                              numeric: true,
                            },
                            {
                              label: '操作',
                              render: (row) => (
                                <Button
                                  variant="quiet"
                                  disabled={read.isPending}
                                  onClick={() => read.mutate(String(row.id))}
                                >
                                  {t('标为已读')}
                                </Button>
                              ),
                            },
                          ]}
                        />
                      </section>
                    </>
                  ),
                },
              ]
            : []),
        ]}
      />
    </div>
  )
}
