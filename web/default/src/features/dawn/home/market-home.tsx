/* Copyright (C) 2023-2026 QuantumNous. AGPL-3.0-or-later. */
import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from '@tanstack/react-router'
import '@/styles/market-home.css'
import { ArrowDown, ArrowRight, ArrowUpRight, Search, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { DEFAULT_LOGO } from '@/lib/constants'
import { SiteFooter } from '@/components/site-footer'
import { usePricingData } from '@/features/pricing/hooks/use-pricing-data'
import { DawnNav } from '../components/dawn-nav'
import { DeveloperExample } from './developer-example'
import { RouteField } from './route-field'

type HomeModel = { id: string; name: string; vendor?: string; sample: boolean }

// Original homepage examples; availability is supplied only by the API.
const EXAMPLE_MODELS: HomeModel[] = [
  { id: 'deepseek-r1', name: 'DeepSeek-R1 Full', sample: true },
  { id: 'claude-3-7-sonnet', name: 'Claude 3.7 Sonnet', sample: true },
  { id: 'gpt-4o-mini', name: 'GPT-4o Mini Omni', sample: true },
]

function getModelMeta(id: string, vendor?: string) {
  const lower = `${id} ${vendor ?? ''}`.toLowerCase()
  if (lower.includes('deepseek')) {
    return {
      provider: 'DeepSeek',
      logo: '/deepseek.svg',
    }
  }
  if (lower.includes('claude') || lower.includes('anthropic')) {
    return {
      provider: 'Anthropic',
      logo: '/claude.svg',
    }
  }
  if (
    lower.includes('gpt') ||
    lower.includes('openai') ||
    lower.includes('o1') ||
    lower.includes('o3')
  ) {
    return {
      provider: 'OpenAI',
      logo: '/openai.svg',
    }
  }
  if (lower.includes('gemini') || lower.includes('google')) {
    return {
      provider: 'Google',
      logo: DEFAULT_LOGO,
    }
  }
  return {
    provider: vendor || 'AI Model',
    logo: DEFAULT_LOGO,
  }
}

export function ImmersiveCurtainMarketHome() {
  const { t } = useTranslation()
  const pricing = usePricingData()
  const [query, setQuery] = useState('')
  const [settledQuery, setSettledQuery] = useState('')
  const [vendor, setVendor] = useState('')
  const [page, setPage] = useState(0)
  const [selectedRoute, setSelectedRoute] = useState('')
  const [detail, setDetail] = useState<HomeModel | null>(null)
  const detailDialog = useRef<HTMLDialogElement>(null)
  const detailTrigger = useRef<HTMLButtonElement | null>(null)
  const restoreDetailFocus = useRef(true)
  const composition = useRef(false)

  const models = useMemo<HomeModel[]>(() => {
    return pricing.models.map((model) => ({
      id: model.model_name,
      name: model.model_name,
      vendor: model.vendor_name,
      sample: false,
    }))
  }, [pricing.models])
  const routeModels = models.length ? models : EXAMPLE_MODELS
  const vendors = useMemo(
    () => [
      ...new Set(
        models.flatMap((model) => (model.vendor ? [model.vendor] : []))
      ),
    ],
    [models]
  )
  const filtered = useMemo(() => {
    const term = settledQuery.trim().toLocaleLowerCase()
    return models.filter(
      (model) =>
        (!vendor || model.vendor === vendor) &&
        (!term ||
          `${model.name} ${model.id} ${model.vendor ?? ''}`
            .toLocaleLowerCase()
            .includes(term))
    )
  }, [models, settledQuery, vendor])
  const currentPage = Math.min(
    page,
    Math.max(0, Math.ceil(filtered.length / 6) - 1)
  )
  const visibleModels = filtered.slice(currentPage * 6, currentPage * 6 + 6)
  const routeModel =
    routeModels.find((model) => model.id === selectedRoute) ?? routeModels[0]

  useEffect(() => {
    if (composition.current) return
    const timer = window.setTimeout(() => {
      setSettledQuery(query)
      setPage(0)
    }, 180)
    return () => window.clearTimeout(timer)
  }, [query])

  useEffect(() => {
    if (detail) detailDialog.current?.showModal()
  }, [detail])

  function clearFilters(): void {
    setQuery('')
    setSettledQuery('')
    setVendor('')
    setPage(0)
  }

  return (
    <div className='dawn market-home' id='top'>
      <DawnNav />
      <main id='content' tabIndex={-1}>
        <section className='mh-hero' aria-labelledby='home-title'>
          <div className='mh-hero-topline mh-container'>
            <span className='inline-flex items-center gap-2'>
              <span className='relative flex h-2 w-2'>
                <span className='bg-brand-orange absolute inline-flex h-full w-full animate-ping rounded-full opacity-75' />
                <span className='bg-brand-orange relative inline-flex h-2 w-2 rounded-full' />
              </span>
              {t('marketHome.marketIdentity')}
            </span>
            <Link to='/docs'>
              {t('marketHome.readDocs')}
              <ArrowUpRight aria-hidden='true' />
            </Link>
          </div>
          <div className='mh-hero-stage'>
            <div className='mh-structural-grid mh-container' aria-hidden='true'>
              <i />
              <i />
              <i />
              <i />
            </div>
            <RouteField
              models={routeModels}
              selectedId={routeModel.id}
              onSelect={setSelectedRoute}
            />
            <div className='mh-container mh-copy-container'>
              <div className='mh-hero-copy'>
                <div className='mh-wordmark' aria-label='CodeGo'>
                  CodeGo<span>.</span>
                </div>
                <h1 id='home-title'>
                  {t('智能，')}
                  <br />
                  <span>{t('自由汇流。')}</span>
                </h1>
                <div className='mh-hero-actions'>
                  <a className='mh-button mh-primary' href='#models'>
                    {t('探索模型')}
                    <ArrowDown aria-hidden='true' />
                  </a>
                  <a className='mh-text-link' href='#connect'>
                    {t('接入')}
                    <ArrowUpRight aria-hidden='true' />
                  </a>
                </div>
              </div>
            </div>
          </div>
          <div className='mh-hero-baseline mh-container'>
            <Link to='/market'>
              {t('marketHome.walkMarket')}
              <ArrowUpRight aria-hidden='true' />
            </Link>
          </div>
        </section>
        <section
          className='mh-models mh-container mh-section'
          id='models'
          aria-labelledby='models-title'
        >
          <div className='mh-section-head'>
            <h2 id='models-title'>
              {t('探索模型')}
              <span className='mh-heading-dot'>.</span>
            </h2>
            <Link className='mh-text-link' to='/pricing'>
              {t('模型目录')}
              <ArrowUpRight aria-hidden='true' />
            </Link>
          </div>
          <div className='mh-filters'>
            <div className='mh-search-field'>
              <label htmlFor='home-model-search'>{t('搜索模型')}</label>
              <div className='mh-search-control'>
                <Search aria-hidden='true' />
                <input
                  id='home-model-search'
                  type='search'
                  value={query}
                  autoComplete='off'
                  onChange={(event) => setQuery(event.target.value)}
                  onCompositionStart={() => {
                    composition.current = true
                  }}
                  onCompositionEnd={(event) => {
                    composition.current = false
                    setSettledQuery(event.currentTarget.value)
                    setPage(0)
                  }}
                  onKeyDown={(event) => {
                    if (
                      event.key === 'Enter' &&
                      !event.nativeEvent.isComposing
                    ) {
                      setSettledQuery(query)
                      setPage(0)
                    }
                  }}
                />
                {query && (
                  <button
                    className='mh-icon-button'
                    type='button'
                    aria-label={t('清除筛选')}
                    onClick={clearFilters}
                  >
                    <X aria-hidden='true' />
                  </button>
                )}
              </div>
            </div>
            <div className='mh-provider-field'>
              <label htmlFor='home-provider'>{t('提供方')}</label>
              <select
                id='home-provider'
                value={vendor}
                onChange={(event) => {
                  setVendor(event.target.value)
                  setPage(0)
                }}
              >
                <option value=''>{t('全部')}</option>
                {vendors.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </div>
          </div>
          {pricing.error && (
            <div className='mh-inline-status' role='alert'>
              <span>{t('加载失败')}</span>
              <button
                className='mh-text-link'
                type='button'
                onClick={() => void pricing.refetch()}
              >
                {t('重试')}
                <ArrowRight aria-hidden='true' />
              </button>
            </div>
          )}
          <div className='mh-model-grid' aria-busy={pricing.isLoading}>
            {pricing.isLoading && (
              <div className='mh-inline-status' role='status'>
                {t('加载中')}
              </div>
            )}
            {!pricing.isLoading && !visibleModels.length && (
              <div className='mh-empty'>
                <h3>{t(models.length ? '无匹配模型' : '无可用模型')}</h3>
                <button
                  className='mh-text-link'
                  type='button'
                  onClick={clearFilters}
                >
                  {t('清除筛选')}
                  <ArrowRight aria-hidden='true' />
                </button>
              </div>
            )}
            {visibleModels.map((model) => {
              const meta = getModelMeta(model.id, model.vendor)
              return (
                <div className='mh-bento-card' key={model.id}>
                  <div className='mh-bento-card-top'>
                    <div className='mh-bento-icon-wrap'>
                      <img
                        src={meta.logo}
                        alt={meta.provider}
                        width='22'
                        height='22'
                      />
                    </div>
                  </div>

                  <div className='mh-bento-card-mid'>
                    <span className='mh-bento-vendor'>
                      {model.vendor || meta.provider}
                    </span>
                    <h3 className='mh-bento-title'>{model.name}</h3>
                  </div>

                  <div className='mh-bento-card-bot'>
                    <div className='mh-bento-actions'>
                      <button
                        type='button'
                        className='mh-bento-detail-btn'
                        aria-label={t('查看 {{model}} 详情', {
                          model: model.name,
                        })}
                        onClick={(event) => {
                          detailTrigger.current = event.currentTarget
                          restoreDetailFocus.current = true
                          setDetail(model)
                        }}
                      >
                        <span>{t('详情')}</span>
                        <ArrowUpRight aria-hidden='true' />
                      </button>
                      <button
                        type='button'
                        className='mh-bento-connect-btn'
                        onClick={() => {
                          setSelectedRoute(model.id)
                          document
                            .getElementById('connect')
                            ?.scrollIntoView({ behavior: 'smooth' })
                        }}
                      >
                        <span>{t('接入')}</span>
                        <ArrowRight aria-hidden='true' />
                      </button>
                    </div>
                  </div>
                </div>
              )
            })}
          </div>
          <div className='mh-models-bottom'>
            <span role='status' aria-live='polite'>
              {t('{{count}} 个模型', { count: filtered.length })}
            </span>
            {filtered.length > 6 && (
              <div className='mh-pagination'>
                <button
                  type='button'
                  className='mh-text-link'
                  disabled={currentPage === 0}
                  onClick={() => setPage(currentPage - 1)}
                >
                  {t('上一页')}
                </button>
                <span>
                  {currentPage + 1} / {Math.ceil(filtered.length / 6)}
                </span>
                <button
                  type='button'
                  className='mh-text-link'
                  disabled={(currentPage + 1) * 6 >= filtered.length}
                  onClick={() => setPage(currentPage + 1)}
                >
                  {t('下一页')}
                </button>
              </div>
            )}
            <Link className='mh-text-link' to='/market'>
              {t('全部商品')}
              <ArrowRight aria-hidden='true' />
            </Link>
          </div>
        </section>
        <section
          className='mh-connect mh-section'
          id='connect'
          aria-labelledby='connect-title'
        >
          <div className='mh-container'>
            <div className='mh-section-head'>
              <h2 id='connect-title'>
                {t('即刻接入')}
                <span className='mh-heading-dot'>.</span>
              </h2>
              <Link className='mh-text-link' to='/docs'>
                {t('marketHome.readDocs')}
                <ArrowUpRight aria-hidden='true' />
              </Link>
            </div>
            <DeveloperExample
              selectedModelId={routeModel.sample ? undefined : routeModel.id}
            />
            <div className='mh-connect-links'>
              <Link to='/keys'>
                {t('marketHome.manageKeys')}
                <ArrowUpRight aria-hidden='true' />
              </Link>
              <Link to='/market'>
                {t('全部商品')}
                <ArrowUpRight aria-hidden='true' />
              </Link>
              <Link to='/channel-workbench'>
                {t('marketHome.sellerAction')}
                <ArrowUpRight aria-hidden='true' />
              </Link>
            </div>
          </div>
        </section>
        <section
          className='mh-finale mh-container'
          aria-labelledby='finale-title'
        >
          <h2 id='finale-title'>{t('自由汇流。')}</h2>
          <Link
            className='mh-finale-link'
            to='/market'
            aria-label={t('marketHome.exploreMarket')}
          >
            <ArrowUpRight aria-hidden='true' />
          </Link>
          <div className='mh-finale-links'>
            <Link to='/market'>{t('marketHome.market')}</Link>
            <Link to='/pricing'>{t('模型目录')}</Link>
            <Link to='/docs'>{t('marketHome.docs')}</Link>
            <Link to='/sign-in'>{t('Sign in')}</Link>
          </div>
        </section>
      </main>
      <SiteFooter />
      <dialog
        ref={detailDialog}
        className='mh-detail-dialog'
        aria-labelledby='home-model-title'
        onClick={(event) => {
          if (event.target === event.currentTarget) event.currentTarget.close()
        }}
        onClose={() => {
          setDetail(null)
          if (restoreDetailFocus.current) detailTrigger.current?.focus()
        }}
      >
        {detail && (
          <div className='mh-detail-content'>
            <div className='mh-detail-head'>
              <span>{t('模型详情')}</span>
              <button
                className='mh-icon-button'
                type='button'
                aria-label={t('Close')}
                onClick={() => detailDialog.current?.close()}
              >
                <X aria-hidden='true' />
              </button>
            </div>
            <h2 id='home-model-title'>{detail.name}</h2>
            <dl>
              <div>
                <dt>{t('模型 ID')}</dt>
                <dd>{detail.id}</dd>
              </div>
              <div>
                <dt>{t('提供方')}</dt>
                <dd>{detail.vendor || t('待核验')}</dd>
              </div>
              <div>
                <dt>{t('状态')}</dt>
                <dd>{t(detail.sample ? '示例' : '已收录')}</dd>
              </div>
            </dl>
            <div className='mh-detail-actions'>
              <button
                className='mh-button mh-primary'
                type='button'
                onClick={() => {
                  setSelectedRoute(detail.id)
                  restoreDetailFocus.current = false
                  detailDialog.current?.close()
                  requestAnimationFrame(() => {
                    document
                      .getElementById('connect')
                      ?.scrollIntoView({ behavior: 'instant' })
                    document
                      .getElementById('endpoint')
                      ?.focus({ preventScroll: true })
                  })
                }}
              >
                {t('接入')}
                <ArrowRight aria-hidden='true' />
              </button>
              {!detail.sample && (
                <Link
                  className='mh-text-link'
                  to='/pricing/$modelId'
                  params={{ modelId: detail.id }}
                >
                  {t('价格')}
                  <ArrowUpRight aria-hidden='true' />
                </Link>
              )}
            </div>
          </div>
        )}
      </dialog>
    </div>
  )
}
