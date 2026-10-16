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

For commercial licensing, please contact support@quantumnous.com.
*/
import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useSearch, useNavigate } from '@tanstack/react-router'
import i18n from '@/i18n/config'
import { getIntlLocale } from '@/i18n/format'
import {
  ArrowRight,
  ArrowUpRight,
  AudioLines,
  Box,
  ChevronLeft,
  ChevronRight,
  Image,
  LayoutGrid,
  Search,
  SearchX,
  Store,
  Table2,
  Video,
  Waypoints,
  X,
} from 'lucide-react'
import { Trans, useTranslation } from 'react-i18next'
import { CopyButton } from '@/components/copy-button'
import { SiteSeo } from '@/components/seo'
import { SiteFooter } from '@/components/site-footer'
import { useMarketplaceGroups } from '@/features/marketplace/hooks'
import { getPerfMetricsSummary } from '@/features/performance-metrics/api'
import { CodeSamplesSection } from '@/features/pricing/components/model-details-api'
import {
  EXCLUDED_GROUPS,
  QUOTA_TYPE_VALUES,
} from '@/features/pricing/constants'
import { usePricingData } from '@/features/pricing/hooks/use-pricing-data'
import {
  formatDynamicUnitPrice,
  getDynamicPricingTiers,
  isDynamicPricingModel,
} from '@/features/pricing/lib/dynamic-price'
import { countFreeModels } from '@/features/pricing/lib/free-models'
import { availablePricingModels } from '@/features/pricing/lib/merge-pricing-models'
import { formatPrice, formatGroupPrice } from '@/features/pricing/lib/price'
import type {
  PriceType,
  PricingModel,
  TokenUnit,
} from '@/features/pricing/types'
import { DawnModal } from '../components/dawn-modal'
import { DawnNav } from '../components/dawn-nav'
import { DawnQueryError } from '../components/query-error'
import './directory.css'

const VENDOR_RULES: Array<[RegExp, string]> = [
  [/^gpt|^o[134](-mini)?|^chatgpt/, 'OpenAI'],
  [/^claude/, 'Anthropic'],
  [/^gemini/, 'Google'],
  [/^deepseek/, 'DeepSeek'],
  [/^qwen/, 'Alibaba'],
  [/^llama|^meta/, 'Meta'],
  [/^kimi|^moonshot/, 'Moonshot'],
  [/^glm|^chatglm/, 'Zhipu'],
  [/^doubao|^skylark/, 'ByteDance'],
  [/^ernie/, 'Baidu'],
  [/^hunyuan/, 'Tencent'],
  [/^minimax|^abab/, 'MiniMax'],
  [/^yi-/, '01.AI'],
  [/^mixtral|^mistral/, 'Mistral'],
  [/^grok/, 'xAI'],
  [/^command/, 'Cohere'],
  [/^flux|^black-forest/, 'BFL'],
  [/^seedream|^doubao-seed/, 'ByteDance'],
  [/^whisper|^tts|^dall-e|^gpt-image|^sora/, 'OpenAI'],
]

const MODALITIES = [
  { value: 'all', label: 'gateway.all', icon: LayoutGrid },
  { value: 'text', label: 'gateway.text', icon: Box },
  { value: 'image', label: 'gateway.image', icon: Image },
  { value: 'video', label: 'gateway.video', icon: Video },
  { value: 'audio', label: 'gateway.audio', icon: AudioLines },
]

const PAGE_SIZE = 50

function matchesModality(model: PricingModel, modality: string): boolean {
  if (modality === 'all') return true
  if (
    model.output_modalities?.includes(
      modality as 'text' | 'image' | 'video' | 'audio'
    )
  )
    return true
  const endpoints = model.supported_endpoint_types ?? []
  const types: Record<string, string[]> = {
    text: ['openai', 'openai-response', 'anthropic', 'gemini'],
    image: ['image-generation'],
    video: ['openai-video'],
    audio: ['audio-speech', 'audio-transcription', 'audio-translation'],
  }
  return endpoints.some((endpoint) => types[modality]?.includes(endpoint))
}

function guessVendor(name: string): string {
  const lower = name.toLowerCase()
  for (const [pattern, vendor] of VENDOR_RULES) {
    if (pattern.test(lower)) return vendor
  }
  return '其它'
}

function displayVendor(name: string): string {
  return name === '其它' ? i18n.t('Other') : name
}

export function DawnPlaza() {
  const { t } = useTranslation()
  const searchParams = useSearch({ from: '/pricing/' })
  const navigate = useNavigate({ from: '/pricing/' })
  const pricing = usePricingData()
  const marketplaceGroupsQuery = useMarketplaceGroups({
    search: '',
    model: '',
    source: '',
    provider: '',
    status: '',
    verification: '',
    sort: 'score',
    direction: 'desc',
    window_hours: 24,
    page: 1,
    page_size: 50,
  })
  const perfQuery = useQuery({
    queryKey: ['pricing-model-performance', 24],
    queryFn: () => getPerfMetricsSummary(24),
    staleTime: 60 * 1000,
    retry: false,
  })
  const requestCounts = useMemo(
    () =>
      new Map(
        (perfQuery.data?.data?.models ?? []).map((item) => [
          item.model_name,
          item.request_count ?? 0,
        ])
      ),
    [perfQuery.data]
  )
  const unit: TokenUnit = searchParams.tokenUnit ?? 'M'
  const view = searchParams.view === 'card' ? 'grid' : 'table'
  const vendor = searchParams.vendor ?? '全部厂家'
  const keyword = searchParams.search ?? ''
  const [searchDraft, setSearchDraft] = useState(keyword)
  const sort = searchParams.sort ?? 'hot'
  const modality = searchParams.endpointType ?? 'all'
  const updateSearch = (patch: Partial<typeof searchParams>) => {
    void navigate({
      search: (previous) => ({ ...previous, ...patch }),
      replace: true,
    })
  }
  const setUnit = (tokenUnit: TokenUnit) => updateSearch({ tokenUnit })
  const setView = (value: 'table' | 'grid') =>
    updateSearch({ view: value === 'grid' ? 'card' : 'table' })
  const setVendor = (value: string) =>
    updateSearch({ vendor: value === '全部厂家' ? undefined : value })
  const setKeyword = (value: string) =>
    updateSearch({ search: value || undefined })
  const setSort = (value: string) => updateSearch({ sort: value })
  const [detail, setDetail] = useState<string | null>(null)
  const [pagination, setPagination] = useState({ scope: '', page: 1 })

  useEffect(() => setSearchDraft(keyword), [keyword])
  useEffect(() => {
    if (searchDraft === keyword) return
    const timer = window.setTimeout(() => setKeyword(searchDraft), 180)
    return () => window.clearTimeout(timer)
  }, [searchDraft, keyword])

  const models = useMemo(
    () =>
      availablePricingModels(
        pricing.models,
        pricing.pricedModelDetails,
        pricing.availableModels
      )
        .filter((model) => Boolean(model.model_name?.trim()))
        .map((model) => ({
          ...model,
          vendorName: model.vendor_name || guessVendor(model.model_name),
        })),
    [pricing.models, pricing.pricedModelDetails, pricing.availableModels]
  )

  const vendors = useMemo(
    () => [
      '全部厂家',
      ...[...new Set(models.map((model) => model.vendorName))].sort(),
    ],
    [models]
  )

  const vendorCounts = useMemo(() => {
    const counts = new Map<string, number>()
    for (const model of models) {
      counts.set(model.vendorName, (counts.get(model.vendorName) ?? 0) + 1)
    }
    return counts
  }, [models])

  const groups = useMemo(
    () =>
      Object.entries(pricing.usableGroup)
        .filter(([name]) => !EXCLUDED_GROUPS.includes(name))
        .map(([name, meta]) => ({
          name,
          ratio: pricing.groupRatio[name] ?? 1,
          desc: typeof meta === 'string' ? meta : meta.desc,
        })),
    [pricing.usableGroup, pricing.groupRatio]
  )

  const marketplaceGroups = useMemo(
    () =>
      [
        ...(marketplaceGroupsQuery.data?.official_items ?? []),
        ...(marketplaceGroupsQuery.data?.items ?? []),
      ].map((group) => ({
        name: group.system_display_name,
        ratio: group.multiplier ?? 1,
        desc: group.source_label || group.source_type,
        sourceLabel:
          group.source_label ||
          (group.source_type === 'official'
            ? i18n.t('官方')
            : i18n.t('第三方')),
        sourceType: group.source_type,
        isSiteGroup: false,
        models: group.models,
      })),
    [marketplaceGroupsQuery.data, i18n.language]
  )

  const list = useMemo(() => {
    const query = keyword.trim().toLowerCase()
    const filtered = models.filter(
      (model) =>
        (vendor === '全部厂家' || model.vendorName === vendor) &&
        matchesModality(model, modality) &&
        (!searchParams.group ||
          model.enable_groups?.includes(searchParams.group)) &&
        (!searchParams.quotaType ||
          String(model.quota_type) === searchParams.quotaType) &&
        (!searchParams.tag ||
          model.tags?.split(/[,;|\s]+/).includes(searchParams.tag)) &&
        (!query ||
          model.model_name.toLowerCase().includes(query) ||
          model.vendorName.toLowerCase().includes(query))
    )
    if (sort === 'price') {
      filtered.sort((a, b) => {
        const price = (model: PricingModel) => {
          if (
            model.pricing_available === false ||
            model.quota_type === QUOTA_TYPE_VALUES.REQUEST
          )
            return Number.POSITIVE_INFINITY
          if (isDynamicPricingModel(model)) {
            const value = getDynamicPricingTiers(model)[0]?.inputPrice
            return typeof value === 'number' && Number.isFinite(value)
              ? value
              : Number.POSITIVE_INFINITY
          }
          return Number.isFinite(model.model_ratio)
            ? model.model_ratio * 2
            : Number.POSITIVE_INFINITY
        }
        return price(a) - price(b) || a.model_name.localeCompare(b.model_name)
      })
    } else if (sort === 'hot') {
      const coverage = (m: (typeof filtered)[number]) =>
        requestCounts.get(m.model_name) ?? 0
      filtered.sort(
        (a, b) =>
          coverage(b) - coverage(a) || a.model_name.localeCompare(b.model_name)
      )
    } else {
      filtered.sort((a, b) => a.model_name.localeCompare(b.model_name))
    }
    return filtered
  }, [
    models,
    vendor,
    keyword,
    sort,
    requestCounts,
    modality,
    searchParams.group,
    searchParams.quotaType,
    searchParams.tag,
  ])

  const free = useMemo(
    () => countFreeModels(models, pricing.groupRatio),
    [models, pricing.groupRatio]
  )
  const pageScope = JSON.stringify([
    vendor,
    keyword,
    sort,
    modality,
    searchParams.group,
    searchParams.quotaType,
    searchParams.tag,
  ])
  const pageCount = Math.max(1, Math.ceil(list.length / PAGE_SIZE))
  const page = Math.min(
    pagination.scope === pageScope ? pagination.page : 1,
    pageCount
  )
  if (pagination.scope !== pageScope || pagination.page !== page) {
    setPagination({ scope: pageScope, page })
  }
  const pageModels = list.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE)
  const setPage = (value: number) => {
    if (!Number.isFinite(value)) return
    setPagination({
      scope: pageScope,
      page: Math.max(1, Math.min(pageCount, Math.trunc(value))),
    })
  }

  const detailModel = detail
    ? models.find((model) => model.model_name === detail)
    : null
  const detailGroups = useMemo(() => {
    if (!detailModel) return []
    const enabled = Array.isArray(detailModel.enable_groups)
      ? detailModel.enable_groups
      : []
    const official = groups
      .filter((group) => enabled.includes(group.name))
      .map((group) => ({
        ...group,
        sourceLabel: i18n.t('官方'),
        sourceType: 'official' as const,
        isSiteGroup: true,
      }))
    const marketplace = marketplaceGroups.filter((group) =>
      group.models.some(
        (model) => model.toLowerCase() === detailModel.model_name.toLowerCase()
      )
    )
    const seen = new Set(official.map((group) => group.name))
    return [
      ...official,
      ...marketplace.filter((group) => !seen.has(group.name)),
    ]
  }, [detailModel, groups, marketplaceGroups, i18n.language])

  return (
    <div className='dawn gateway-plaza'>
      <SiteSeo
        title={i18n.t('模型广场 | Code Go')}
        description={i18n.t('模型广场 · 输入输出价目')}
        canonicalPath='/pricing'
      />
      <DawnNav />
      <main className='dawn-wrap' id='content' tabIndex={-1}>
        <div className='mhead'>
          <div>
            <h1>
              {t('Models')}
              <span aria-hidden='true'>.</span>
            </h1>
          </div>
          <div className='seg'>
            <button
              className={unit === 'M' ? 'on' : ''}
              aria-pressed={unit === 'M'}
              onClick={() => setUnit('M')}
            >
              /1M
            </button>
            <button
              className={unit === 'K' ? 'on' : ''}
              aria-pressed={unit === 'K'}
              onClick={() => setUnit('K')}
            >
              /1K
            </button>
          </div>
        </div>

        <div className='gateway-directory-summary'>
          <span>
            <b>{models.length}</b>
            {t('Models')}
          </span>
          <span>
            <b>{vendors.length - 1}</b>
            {t('gateway.providers')}
          </span>
          <span>
            <b>{free}</b>
            {t('Free models')}
          </span>
        </div>

        <div className='gateway-directory-layout'>
          <aside
            className='gateway-sidebar'
            aria-label={t('gateway.providers')}
          >
            <h2>{t('gateway.providers')}</h2>
            <div>
              {vendors.map((name) => (
                <button
                  key={name}
                  className={vendor === name ? 'on' : ''}
                  aria-pressed={vendor === name}
                  onClick={() => setVendor(name)}
                >
                  {name === '全部厂家'
                    ? t('gateway.allProviders')
                    : displayVendor(name)}
                  <span>
                    {name === '全部厂家'
                      ? models.length
                      : (vendorCounts.get(name) ?? 0)}
                  </span>
                </button>
              ))}
            </div>
          </aside>
          <div className='gateway-directory-content'>
            <div className='filters'>
              <div className='gateway-directory-search'>
                <Search size={17} aria-hidden='true' />
                <label className='sr-only' htmlFor='directory-search'>
                  {t('Search models')}
                </label>
                <input
                  id='directory-search'
                  placeholder={i18n.t('搜索模型 / 厂家')}
                  aria-label={t('Search models')}
                  value={searchDraft}
                  onChange={(event) => setSearchDraft(event.target.value)}
                />
                {keyword && (
                  <button
                    onClick={() => {
                      setSearchDraft('')
                      setKeyword('')
                    }}
                    aria-label={t('gateway.clearSearch')}
                  >
                    <X size={15} />
                  </button>
                )}
              </div>
              <select
                className='fsel'
                value={sort}
                aria-label={t('gateway.sort')}
                onChange={(event) =>
                  setSort(event.target.value as 'hot' | 'name' | 'price')
                }
              >
                <option value='hot'>
                  <Trans i18nKey={'按热度'} />
                </option>
                <option value='name'>
                  <Trans i18nKey={'按名称'} />
                </option>
                <option value='price'>
                  <Trans i18nKey={'按输入价'} />
                </option>
              </select>
              <div className='iconseg'>
                <button
                  className={view === 'table' ? 'on' : ''}
                  onClick={() => setView('table')}
                  title={i18n.t('表格')}
                  aria-label={i18n.t('表格')}
                  aria-pressed={view === 'table'}
                >
                  <Table2 size={15} />
                </button>
                <button
                  className={view === 'grid' ? 'on' : ''}
                  onClick={() => setView('grid')}
                  title={i18n.t('卡片')}
                  aria-label={i18n.t('卡片')}
                  aria-pressed={view === 'grid'}
                >
                  <LayoutGrid size={15} />
                </button>
              </div>
            </div>
            <div
              className='gateway-directory-tabs'
              role='group'
              aria-label={t('gateway.modalities')}
            >
              {MODALITIES.map((item) => (
                <button
                  key={item.value}
                  className={modality === item.value ? 'on' : ''}
                  aria-pressed={modality === item.value}
                  onClick={() =>
                    updateSearch({
                      endpointType:
                        item.value === 'all' ? undefined : item.value,
                    })
                  }
                >
                  <item.icon size={15} />
                  {t(item.label)}
                </button>
              ))}
            </div>
            <div className='directory-refine'>
              <label>
                <span>{t('Group')}</span>
                <select
                  value={searchParams.group ?? ''}
                  onChange={(event) =>
                    updateSearch({ group: event.target.value || undefined })
                  }
                >
                  <option value=''>{t('All')}</option>
                  {groups.map((group) => (
                    <option key={group.name} value={group.name}>
                      {group.name}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                <span>{t('计费方式')}</span>
                <select
                  value={searchParams.quotaType ?? ''}
                  onChange={(event) =>
                    updateSearch({ quotaType: event.target.value || undefined })
                  }
                >
                  <option value=''>{t('All')}</option>
                  <option value='0'>{t('Token-based')}</option>
                  <option value='1'>{t('Per Request')}</option>
                </select>
              </label>
            </div>
            <div className='gateway-result-count' aria-live='polite'>
              <span>{t('gateway.results', { count: list.length })}</span>
              {(keyword ||
                vendor !== '全部厂家' ||
                modality !== 'all' ||
                searchParams.group ||
                searchParams.quotaType ||
                searchParams.tag) && (
                <button
                  onClick={() =>
                    updateSearch({
                      search: undefined,
                      vendor: undefined,
                      endpointType: undefined,
                      group: undefined,
                      quotaType: undefined,
                      tag: undefined,
                    })
                  }
                >
                  <X size={12} />
                  {t('gateway.resetFilters')}
                </button>
              )}
            </div>

            {!pricing.isLoading && !pricing.error && pageCount > 1 && (
              <nav className='gateway-pagination' aria-label={t('pagination')}>
                <button
                  type='button'
                  disabled={page === 1}
                  onClick={() => setPage(page - 1)}
                >
                  <ChevronLeft size={16} aria-hidden='true' />
                  {t('上一页')}
                </button>
                <label>
                  <span>{t('Page')}</span>
                  <input
                    type='number'
                    min={1}
                    max={pageCount}
                    value={page}
                    aria-label={t('Page')}
                    onChange={(event) => setPage(Number(event.target.value))}
                  />
                  <span>/ {pageCount.toLocaleString(getIntlLocale())}</span>
                </label>
                <button
                  type='button'
                  disabled={page === pageCount}
                  onClick={() => setPage(page + 1)}
                >
                  {t('下一页')}
                  <ChevronRight size={16} aria-hidden='true' />
                </button>
              </nav>
            )}

            {pricing.isLoading ? (
              <div
                className='gateway-skeleton'
                aria-busy='true'
                aria-label={t('Loading')}
              />
            ) : pricing.error ? (
              <DawnQueryError
                title={i18n.t('模型价格加载失败')}
                onRetry={() => void pricing.refetch()}
              />
            ) : list.length === 0 ? (
              <div className='empty'>
                <span className='eic'>
                  <SearchX size={20} />
                </span>
                <b>
                  <Trans i18nKey={'无匹配模型'} />
                </b>
                <button
                  className='btn'
                  onClick={() => {
                    setSearchDraft('')
                    updateSearch({
                      search: undefined,
                      vendor: undefined,
                      endpointType: undefined,
                      group: undefined,
                      quotaType: undefined,
                      tag: undefined,
                    })
                  }}
                >
                  {t('gateway.resetFilters')}
                </button>
                <button className='btn' onClick={() => void pricing.refetch()}>
                  {t('刷新')}
                </button>
                <Link className='btn' to='/docs'>
                  {t('Docs')}
                </Link>
              </div>
            ) : view === 'table' ? (
              <div className='gtable cache-prices' style={{ marginTop: 16 }}>
                <div className='tr th'>
                  <span>
                    <Trans i18nKey={'模型'} />
                  </span>
                  <span>
                    <Trans i18nKey={'输入 /'} />
                    {unit}
                  </span>
                  <span>
                    <Trans i18nKey={'输出 /'} />
                    {unit}
                  </span>
                  <span>
                    {t('缓存写入')} /{unit}
                  </span>
                  <span>
                    {t('缓存读取')} /{unit}
                  </span>
                  <span>
                    <Trans i18nKey={'可用分组'} />
                  </span>
                  <span style={{ textAlign: 'right' }}>
                    <Trans i18nKey={'详情'} />
                  </span>
                </div>
                {pageModels.map((model) => (
                  <ModelRow
                    key={model.model_name}
                    model={model}
                    unit={unit}
                    onOpen={() => setDetail(model.model_name)}
                    groups={groups.map((g) => g.name)}
                  />
                ))}
              </div>
            ) : (
              <div className='mgrid'>
                {pageModels.map((model) => (
                  <button
                    className='mcard'
                    key={model.model_name}
                    onClick={() => setDetail(model.model_name)}
                  >
                    <span
                      className='mn'
                      style={{
                        fontFamily: 'var(--dawn-mono)',
                        fontSize: 12.5,
                        fontWeight: 700,
                        display: 'inline-flex',
                        alignItems: 'center',
                        gap: 9,
                        overflow: 'hidden',
                      }}
                    >
                      <span
                        style={{
                          overflow: 'hidden',
                          textOverflow: 'ellipsis',
                          whiteSpace: 'nowrap',
                        }}
                      >
                        {model.model_name}
                      </span>
                      <span className='ven'>
                        {displayVendor(model.vendorName)}
                      </span>
                      {model.quota_type === QUOTA_TYPE_VALUES.REQUEST && (
                        <span className='btag'>
                          <Trans i18nKey={'按量'} />
                        </span>
                      )}
                    </span>
                    <div className='pr'>
                      <PriceText model={model} unit={unit} />
                      <span>
                        {' '}
                        {model.quota_type === QUOTA_TYPE_VALUES.REQUEST ? (
                          t('gateway.perRequest')
                        ) : (
                          <>
                            /{unit} <Trans i18nKey={'输入'} />
                          </>
                        )}
                      </span>
                    </div>
                    <div className='sub'>
                      {model.quota_type === QUOTA_TYPE_VALUES.REQUEST
                        ? i18n.t('按量计费')
                        : i18n.t('输出 {{param0}} /{{param1}}', {
                            param0: formatPrice(model, 'output', unit),
                            param1: unit,
                          })}
                    </div>
                    {model.quota_type !== QUOTA_TYPE_VALUES.REQUEST && (
                      <div className='sub'>
                        {t('缓存写入')} {cacheText(model, unit, 'create_cache')}{' '}
                        /{unit}
                        <br />
                        {t('缓存读取')} {cacheText(model, unit, 'cache')} /
                        {unit}
                      </div>
                    )}
                    <div className='gtags' style={{ marginTop: 12 }}>
                      {groupsFor(
                        model,
                        groups.map((g) => g.name)
                      )
                        .slice(0, 3)
                        .map((name) => (
                          <span className='gtag' key={name}>
                            {name}
                          </span>
                        ))}
                    </div>
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>
      </main>

      <SiteFooter />

      <DawnModal
        open={!!detailModel}
        onClose={() => setDetail(null)}
        variant='plain'
        label={i18n.t('模型详情')}
      >
        {detailModel && (
          <div className='m-main'>
            <div className='m-head' style={{ marginBottom: 18 }}>
              <h3
                style={{
                  fontFamily: 'var(--dawn-mono)',
                  fontSize: 20,
                  display: 'flex',
                  alignItems: 'center',
                  gap: 10,
                  flexWrap: 'wrap',
                }}
              >
                {detailModel.model_name}
                <CopyButton
                  value={detailModel.model_name}
                  aria-label={t('Copy model name')}
                />
                <span className='ven' style={{ fontSize: 12 }}>
                  {displayVendor(detailModel.vendorName)}
                </span>
                {detailModel.quota_type === QUOTA_TYPE_VALUES.REQUEST && (
                  <span className='btag'>
                    <Trans i18nKey={'按量'} />
                  </span>
                )}
              </h3>
              <button
                className='x'
                onClick={() => setDetail(null)}
                aria-label={i18n.t('关闭')}
              >
                <X size={18} />
              </button>
            </div>
            <div className='pgrid'>
              {detailModel.quota_type === QUOTA_TYPE_VALUES.REQUEST ? (
                <>
                  <div className='pc'>
                    <b>{meteredPrice(detailModel)}</b>
                    <span>
                      <Trans i18nKey={'单价 / 次'} />
                    </span>
                  </div>
                  <div className='pc'>
                    <b className='c-warn' style={{ color: 'var(--dawn-warn)' }}>
                      <Trans i18nKey={'按量'} />
                    </b>
                    <span>
                      <Trans i18nKey={'计费方式'} />
                    </span>
                  </div>
                </>
              ) : (
                <>
                  <div
                    className={`pc${detailModel.model_ratio === 0 ? 'free' : ''}`}
                  >
                    <b>{baseModelPrice(detailModel, 'input', unit)}</b>
                    <span>
                      <Trans i18nKey={'输入 /'} /> {unit}
                    </span>
                  </div>
                  <div
                    className={`pc${detailModel.model_ratio === 0 ? 'free' : ''}`}
                  >
                    <b>{baseModelPrice(detailModel, 'output', unit)}</b>
                    <span>
                      <Trans i18nKey={'输出 /'} /> {unit}
                    </span>
                  </div>
                  <div className='pc'>
                    <b>{baseModelPrice(detailModel, 'create_cache', unit)}</b>
                    <span>
                      <Trans i18nKey={'缓存写入 /'} /> {unit}
                    </span>
                  </div>
                  <div className='pc free'>
                    <b>{baseModelPrice(detailModel, 'cache', unit)}</b>
                    <span>
                      <Trans i18nKey={'缓存读取 /'} /> {unit}
                    </span>
                  </div>
                </>
              )}
            </div>
            <div className='kvs'>
              <div className='kv2'>
                <b>
                  {detailModel.quota_type === QUOTA_TYPE_VALUES.REQUEST
                    ? i18n.t('每次调用')
                    : `1${unit} tokens`}
                </b>
                <Trans i18nKey={'计费粒度'} />
              </div>
              <div className='kv2'>
                <b>
                  {detailGroups.length} <Trans i18nKey={'个'} />
                </b>
                <Trans i18nKey={'覆盖分组'} />
              </div>
              {detailModel.tags && (
                <div className='kv2'>
                  <b>{detailModel.tags}</b>
                  <Trans i18nKey={'标签'} />
                </div>
              )}
            </div>
            <div className='sect'>
              <Waypoints size={12} />
              <Trans i18nKey={'各分组价格（含分组倍率）'} />
            </div>
            {isDynamicPricingModel(detailModel) && (
              <div className='gtab cache-tiers' style={{ marginBottom: 12 }}>
                <div className='gr gh'>
                  <span>
                    <Trans i18nKey={'阶梯'} />
                  </span>
                  <span>
                    <Trans i18nKey={'条件'} />
                  </span>
                  <span>
                    <Trans i18nKey={'输入 /'} />
                    {unit}
                  </span>
                  <span>
                    <Trans i18nKey={'输出 /'} />
                    {unit}
                  </span>
                  <span>
                    {t('缓存写入')} /{unit}
                  </span>
                  <span>
                    {t('缓存读取')} /{unit}
                  </span>
                </div>
                {getDynamicPricingTiers(detailModel).map((tier, index) => (
                  <div className='gr' key={`tier-${index}`}>
                    <span className='gn'>
                      <Trans i18nKey={'第'} /> {index + 1}{' '}
                      <Trans i18nKey={'档'} />
                    </span>
                    <span className='num'>
                      {tier.conditions.length
                        ? tier.conditions
                            .map(
                              (condition) =>
                                `${condition.var} ${condition.op} ${condition.value}`
                            )
                            .join(' · ')
                        : i18n.t('默认')}
                    </span>
                    <span className='num'>
                      {dynamicTierPrice(tier.inputPrice, unit)}
                    </span>
                    <span className='num'>
                      {dynamicTierPrice(tier.outputPrice, unit)}
                    </span>
                    <span className='num'>
                      {dynamicTierPrice(tier.cacheCreatePrice, unit)}
                    </span>
                    <span className='num'>
                      {dynamicTierPrice(tier.cacheReadPrice, unit)}
                    </span>
                  </div>
                ))}
              </div>
            )}
            <div className='gtab cache-prices'>
              <div className='gr gh'>
                <span>
                  <Trans i18nKey={'分组'} />
                </span>
                <span>
                  <Trans i18nKey={'倍率'} />
                </span>
                <span>
                  <Trans i18nKey={'输入 /'} />
                  {unit}
                </span>
                <span>
                  <Trans i18nKey={'输出 /'} />
                  {unit}
                </span>
                <span>
                  {t('缓存写入')} /{unit}
                </span>
                <span>
                  {t('缓存读取')} /{unit}
                </span>
                <span />
              </div>
              {detailGroups.map((group) => (
                <div className='gr' key={group.name}>
                  <span className='gn'>
                    {group.name}
                    <small style={{ marginLeft: 6, opacity: 0.6 }}>
                      {group.sourceLabel}
                    </small>
                  </span>
                  <span className='num'>{Number(group.ratio.toFixed(4))}×</span>
                  {detailModel.quota_type === QUOTA_TYPE_VALUES.REQUEST ? (
                    <>
                      <span className='num'>
                        <b>{meteredPrice(detailModel, group.ratio)}</b>
                      </span>
                      <span className='num'>
                        <Trans i18nKey={'按量'} />
                      </span>
                      <span className='num'>—</span>
                      <span className='num'>—</span>
                    </>
                  ) : (
                    <>
                      <span className='num'>
                        <b>
                          {group.isSiteGroup
                            ? formatGroupPrice(
                                detailModel,
                                group.name,
                                'input',
                                unit,
                                false,
                                1,
                                1,
                                pricing.groupRatio
                              )
                            : marketplaceGroupPrice(
                                detailModel,
                                group.ratio,
                                'input',
                                unit
                              )}
                        </b>
                        <span className='u'> /{unit}</span>
                      </span>
                      <span className='num'>
                        <b>
                          {group.isSiteGroup
                            ? formatGroupPrice(
                                detailModel,
                                group.name,
                                'output',
                                unit,
                                false,
                                1,
                                1,
                                pricing.groupRatio
                              )
                            : marketplaceGroupPrice(
                                detailModel,
                                group.ratio,
                                'output',
                                unit
                              )}
                        </b>
                        <span className='u'> /{unit}</span>
                      </span>
                    </>
                  )}
                  {detailModel.quota_type !== QUOTA_TYPE_VALUES.REQUEST &&
                    (['create_cache', 'cache'] as const).map((type) => (
                      <span className='num' key={type}>
                        <b>
                          {group.isSiteGroup
                            ? formatGroupPrice(
                                detailModel,
                                group.name,
                                type,
                                unit,
                                false,
                                1,
                                1,
                                pricing.groupRatio
                              )
                            : marketplaceGroupPrice(
                                detailModel,
                                group.ratio,
                                type,
                                unit
                              )}
                        </b>
                      </span>
                    ))}
                  <Link
                    to='/market'
                    style={{
                      display: 'inline-flex',
                      alignItems: 'center',
                      gap: 4,
                      color: 'var(--dawn-copper)',
                      fontWeight: 700,
                      fontSize: 12,
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {t('gateway.useGroup')}
                    <ArrowRight size={12} aria-hidden='true' />
                  </Link>
                </div>
              ))}
            </div>
            <div className='gateway-detail-actions'>
              <Link
                className='btn'
                to='/pricing/$modelId'
                params={{ modelId: detailModel.model_name }}
                search={searchParams}
              >
                {t('Model details')}
                <ArrowUpRight size={15} />
              </Link>
              <Link
                className='btn primary'
                to='/playground'
                search={{ model: detailModel.model_name }}
              >
                {t('gateway.playground')}
                <ArrowUpRight size={15} />
              </Link>
              <Link className='btn' to='/keys'>
                {t('gateway.apiKeys')}
                <ArrowRight size={15} />
              </Link>
            </div>
            <div className='gateway-detail-code'>
              <CodeSamplesSection
                key={detailModel.model_name}
                model={detailModel}
                endpointMap={pricing.endpointMap}
              />
            </div>
            <div className='mfoot2'>
              <button className='btn' onClick={() => setDetail(null)}>
                <Trans i18nKey={'关闭'} />
              </button>
              <Link className='btn primary' to='/market'>
                <Store size={14} />
                <Trans i18nKey={'去市场选组'} />
              </Link>
            </div>
          </div>
        )}
      </DawnModal>
    </div>
  )
}

function groupsFor(model: PricingModel, allGroups: string[]): string[] {
  const enabled = Array.isArray(model.enable_groups) ? model.enable_groups : []
  if (!enabled.length) return []
  return enabled.filter((name) => allGroups.includes(name))
}

function cacheText(
  model: PricingModel,
  unit: TokenUnit,
  type: 'cache' | 'create_cache'
): string {
  const value = formatPrice(model, type, unit)
  return value.includes('NaN') || value === '-' ? '—' : value
}

function marketplaceGroupPrice(
  model: PricingModel,
  ratio: number,
  type: PriceType,
  unit: TokenUnit
): string {
  return formatGroupPrice(model, '_marketplace', type, unit, false, 1, 1, {
    _marketplace: ratio ?? 1,
  })
}

function baseModelPrice(
  model: PricingModel,
  type: PriceType,
  unit: TokenUnit
): string {
  if (model.quota_type === QUOTA_TYPE_VALUES.REQUEST) return meteredPrice(model)
  return formatGroupPrice(model, '_base', type, unit, false, 1, 1, {
    _base: 1,
  })
}

function dynamicTierPrice(value: unknown, unit: TokenUnit): string {
  const numeric = Number(value)
  if (value == null || !Number.isFinite(numeric) || numeric < 0) return '—'
  return formatDynamicUnitPrice(numeric, { tokenUnit: unit })
}

function PriceText({ model, unit }: { model: PricingModel; unit: TokenUnit }) {
  useTranslation()
  if (model.pricing_available === false) return <>—</>
  if (model.quota_type === QUOTA_TYPE_VALUES.REQUEST) {
    return <>{meteredPrice(model)}</>
  }
  const value = formatPrice(model, 'input', unit)
  const isFree = model.model_ratio === 0
  return (
    <span className={isFree ? 'price free' : undefined}>
      {isFree ? i18n.t('免费') : value}
    </span>
  )
}

/** 按量计费模型的每次单价。 */
function meteredPrice(model: PricingModel, ratio = 1): string {
  const price = model.model_price
  if (price == null || !Number.isFinite(Number(price))) return '—'
  return `${new Intl.NumberFormat(getIntlLocale(), {
    maximumFractionDigits: 8,
  }).format(Number(price) * ratio)} credits`
}

function ModelRow(props: {
  model: PricingModel & { vendorName: string }
  unit: TokenUnit
  onOpen: () => void
  groups: string[]
}) {
  useTranslation()
  const { model, unit, onOpen, groups } = props
  const metered = model.quota_type === QUOTA_TYPE_VALUES.REQUEST
  const isFree = !metered && model.model_ratio === 0
  return (
    <button className='tr' type='button' onClick={onOpen}>
      <span
        className='mn'
        style={{
          fontFamily: 'var(--dawn-mono)',
          fontSize: 12.5,
          fontWeight: 700,
          display: 'flex',
          alignItems: 'center',
          gap: 9,
          overflow: 'hidden',
        }}
      >
        <span
          style={{
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {model.model_name}
        </span>
        <span className='ven'>{displayVendor(model.vendorName)}</span>
        {metered && (
          <span className='btag'>
            <Trans i18nKey={'按量'} />
          </span>
        )}
      </span>
      <span
        data-label={i18n.t('Input')}
        className={`price${isFree ? 'free' : ''}`}
      >
        <b>
          {metered
            ? meteredPrice(model)
            : isFree
              ? i18n.t('免费')
              : formatPrice(model, 'input', unit)}
        </b>
        <span className='u'>
          {' '}
          {metered ? i18n.t('gateway.perRequest') : `/${unit}`}
        </span>
      </span>
      <span
        data-label={i18n.t('Output')}
        className={`price${isFree ? 'free' : ''}`}
      >
        <b>
          {metered
            ? i18n.t('按量')
            : isFree
              ? i18n.t('免费')
              : formatPrice(model, 'output', unit)}
        </b>
        <span className='u'>{metered ? '' : ` /${unit}`}</span>
      </span>
      <span className='cw' data-label={i18n.t('缓存写入')}>
        {metered ? '—' : cacheText(model, unit, 'create_cache')}
      </span>
      <span className='cw' data-label={i18n.t('缓存读取')}>
        {metered ? '—' : cacheText(model, unit, 'cache')}
      </span>
      <span className='gtags'>
        {groupsFor(model, groups)
          .slice(0, 3)
          .map((name) => (
            <span className='gtag' key={name}>
              {name}
            </span>
          ))}
      </span>
      <span
        style={{
          textAlign: 'right',
          color: 'var(--dawn-copper)',
          fontWeight: 700,
          fontSize: 12,
          display: 'inline-flex',
          alignItems: 'center',
          gap: 4,
        }}
      >
        <Trans i18nKey={'详情'} /> <ArrowRight size={12} />
      </span>
    </button>
  )
}
