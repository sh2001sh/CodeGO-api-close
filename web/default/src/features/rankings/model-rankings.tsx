import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { getIntlLocale } from '@/i18n/format'
import { Route } from '@/routes/rankings'
import {
  ArrowUpRight,
  ChevronLeft,
  ChevronRight,
  Cpu,
  RefreshCw,
  Search,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  getPerfMetrics,
  getPerfMetricsSummary,
} from '@/features/performance-metrics/api'
import { rankModels } from './data'
import { UsageChart } from './usage-chart'

const PAGE_SIZE = 50

export function ModelRankings() {
  const { t } = useTranslation()
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const [modelFilter, setModelFilter] = useState('')
  const [selectedModel, setSelectedModel] = useState('')
  const [pagination, setPagination] = useState({ scope: '', page: 1 })
  const summary = useQuery({
    queryKey: ['ranking-model-summary', search.hours],
    queryFn: async () => {
      const response = await getPerfMetricsSummary(search.hours)
      if (!response.success || !Array.isArray(response.data?.models))
        throw new Error('Model usage unavailable')
      return response.data.models
    },
    staleTime: 60_000,
    refetchOnWindowFocus: false,
  })
  const models = useMemo(
    () => rankModels(summary.data ?? [], search.metric),
    [summary.data, search.metric]
  )
  const visibleModels = useMemo(() => {
    const query = modelFilter.trim().toLocaleLowerCase()
    return models.filter((model) =>
      model.model_name.toLocaleLowerCase().includes(query)
    )
  }, [models, modelFilter])
  const ranks = useMemo(
    () => new Map(models.map((model, index) => [model.model_name, index + 1])),
    [models]
  )
  const pageScope = JSON.stringify([modelFilter, search.hours, search.metric])
  const pageCount = Math.max(1, Math.ceil(visibleModels.length / PAGE_SIZE))
  const page = Math.min(
    pagination.scope === pageScope ? pagination.page : 1,
    pageCount
  )
  if (pagination.scope !== pageScope || pagination.page !== page) {
    setPagination({ scope: pageScope, page })
  }
  const pageModels = visibleModels.slice(
    (page - 1) * PAGE_SIZE,
    page * PAGE_SIZE
  )
  const setPage = (value: number) => {
    if (!Number.isFinite(value)) return
    setPagination({
      scope: pageScope,
      page: Math.max(1, Math.min(pageCount, Math.trunc(value))),
    })
  }
  const activeModel =
    models.find((model) => model.model_name === selectedModel)?.model_name ??
    models[0]?.model_name ??
    ''
  const trend = useQuery({
    queryKey: ['ranking-model-trend', activeModel, search.hours],
    queryFn: async () => {
      const response = await getPerfMetrics(activeModel, search.hours)
      if (!response.success || !Array.isArray(response.data?.groups))
        throw new Error('Model trend unavailable')
      return response.data.groups
    },
    enabled: !!activeModel && summary.isSuccess,
    staleTime: 60_000,
    refetchOnWindowFocus: false,
  })
  const number = new Intl.NumberFormat(getIntlLocale(), {
    maximumFractionDigits: 0,
  })
  const percent = new Intl.NumberFormat(getIntlLocale(), {
    style: 'percent',
    maximumFractionDigits: 1,
  })
  const total = useMemo(
    () => models.reduce((sum, model) => sum + model[search.metric], 0),
    [models, search.metric]
  )
  const metricLabel =
    search.metric === 'request_count'
      ? t('rankings.requestsMetric')
      : t('rankings.inputTokens')

  return (
    <section
      id='model-usage'
      aria-label={t('rankings.modelLeaderboard')}
      className='rankings-model-section'
    >
      <div className='rankings-view-controls'>
        <div
          className='rankings-segmented'
          role='group'
          aria-label={t('rankings.period')}
        >
          <button
            type='button'
            aria-pressed={search.hours === 24}
            onClick={() => void navigate({ search: { ...search, hours: 24 } })}
          >
            {t('rankings.last24Hours')}
          </button>
          <button
            type='button'
            aria-pressed={search.hours === 168}
            onClick={() => void navigate({ search: { ...search, hours: 168 } })}
          >
            {t('rankings.last7Days')}
          </button>
          <button
            type='button'
            aria-pressed={search.hours === 720}
            onClick={() => void navigate({ search: { ...search, hours: 720 } })}
          >
            {t('rankings.last30Days')}
          </button>
        </div>
        <div className='rankings-metric-controls'>
          <div
            className='rankings-metric-toggle'
            role='group'
            aria-label={t('rankings.sortBy')}
          >
            <button
              type='button'
              aria-pressed={search.metric === 'request_count'}
              onClick={() =>
                void navigate({
                  search: { ...search, metric: 'request_count' },
                })
              }
            >
              {t('rankings.requestsMetric')}
            </button>
            <button
              type='button'
              aria-pressed={search.metric === 'input_tokens'}
              onClick={() =>
                void navigate({ search: { ...search, metric: 'input_tokens' } })
              }
            >
              {t('rankings.inputTokens')}
            </button>
          </div>
          <Button
            variant='ghost'
            size='icon'
            aria-label={t('rankings.refresh')}
            disabled={summary.isFetching || trend.isFetching}
            onClick={() => {
              void summary.refetch()
              if (activeModel) void trend.refetch()
            }}
          >
            <RefreshCw size={16} aria-hidden='true' />
          </Button>
        </div>
      </div>
      {summary.isPending && (
        <div className='rankings-state rankings-chart-panel' role='status'>
          <span className='rankings-loading-line' aria-hidden='true' />
          <p>{t('rankings.loadTrend')}</p>
        </div>
      )}
      {summary.isError && (
        <div className='rankings-state rankings-chart-panel' role='alert'>
          <h2>{t('rankings.modelError')}</h2>
          <p>{t('rankings.modelErrorDescription')}</p>
          <Button
            variant='outline'
            onClick={() => void summary.refetch()}
            disabled={summary.isFetching}
          >
            {t('rankings.retry')}
          </Button>
        </div>
      )}
      {summary.isSuccess && models.length === 0 && (
        <div className='rankings-state rankings-chart-panel'>
          <Cpu size={28} aria-hidden='true' />
          <h2>{t('rankings.noModelData')}</h2>
        </div>
      )}
      {summary.isSuccess && models.length > 0 && (
        <>
          <div className='rankings-chart-panel'>
            <div className='rankings-chart-heading'>
              <div>
                <h2>{t('rankings.usageTrend')}</h2>
              </div>
              <div className='rankings-model-controls'>
                <label className='rankings-model-search'>
                  <Search size={16} aria-hidden='true' />
                  <span className='sr-only'>{t('rankings.modelSearch')}</span>
                  <input
                    type='search'
                    value={modelFilter}
                    onChange={(event) => setModelFilter(event.target.value)}
                    placeholder={t('rankings.modelSearchPlaceholder')}
                  />
                </label>
                {visibleModels.length > 0 ? (
                  <label className='rankings-model-select'>
                    <span className='sr-only'>{t('rankings.selectModel')}</span>
                    <select
                      value={activeModel}
                      onChange={(event) => setSelectedModel(event.target.value)}
                    >
                      {!pageModels.some(
                        (model) => model.model_name === activeModel
                      ) && <option value={activeModel}>{activeModel}</option>}
                      {pageModels.map((model) => (
                        <option key={model.model_name} value={model.model_name}>
                          {model.model_name}
                        </option>
                      ))}
                    </select>
                  </label>
                ) : (
                  <span role='status'>{t('rankings.noMatches')}</span>
                )}
              </div>
            </div>
            {trend.isPending && (
              <div className='rankings-chart-placeholder' role='status'>
                {t('rankings.loadTrend')}
              </div>
            )}
            {trend.isError && (
              <div className='rankings-chart-placeholder' role='alert'>
                <p>{t('rankings.trendError')}</p>
                <Button
                  variant='outline'
                  onClick={() => void trend.refetch()}
                  disabled={trend.isFetching}
                >
                  {t('rankings.retry')}
                </Button>
              </div>
            )}
            {trend.isSuccess && (
              <UsageChart
                groups={trend.data}
                metric={search.metric}
                hours={search.hours}
              />
            )}
          </div>

          <div className='rankings-section-heading rankings-model-heading'>
            <div>
              <h2 id='model-usage-title'>{t('rankings.modelLeaderboard')}</h2>
              <p>
                {t('rankings.modelCountLabel', {
                  count: number.format(visibleModels.length),
                })}
              </p>
            </div>
            {pageCount > 1 && (
              <nav
                className='rankings-pagination rankings-model-pagination'
                aria-label={t('rankings.pages')}
              >
                <Button
                  variant='outline'
                  disabled={page === 1}
                  onClick={() => setPage(page - 1)}
                >
                  <ChevronLeft size={16} aria-hidden='true' />
                  {t('上一页')}
                </Button>
                <label>
                  <span className='sr-only'>{t('Page')}</span>
                  <input
                    type='number'
                    min={1}
                    max={pageCount}
                    value={page}
                    aria-label={t('Page')}
                    onChange={(event) => setPage(Number(event.target.value))}
                  />
                  <span>/ {number.format(pageCount)}</span>
                </label>
                <Button
                  variant='outline'
                  disabled={page === pageCount}
                  onClick={() => setPage(page + 1)}
                >
                  {t('下一页')}
                  <ChevronRight size={16} aria-hidden='true' />
                </Button>
              </nav>
            )}
          </div>
          <div className='rankings-model-board'>
            <div className='rankings-model-columns' aria-hidden='true'>
              <span>#</span>
              <span>{t('rankings.model')}</span>
              <span>{t('rankings.usageShare')}</span>
              <span>{metricLabel}</span>
              <span />
            </div>
            <ol
              className='rankings-model-list'
              aria-label={t('rankings.modelLeaderboard')}
            >
              {pageModels.map((model) => {
                const rank = ranks.get(model.model_name) ?? 0
                const share = total > 0 ? model[search.metric] / total : 0
                return (
                  <li
                    key={model.model_name}
                    className='rankings-model-row'
                    data-selected={activeModel === model.model_name}
                    value={rank}
                  >
                    <span
                      className='rankings-model-rank'
                      aria-label={t('rankings.rank')}
                    >
                      {String(rank).padStart(2, '0')}
                    </span>
                    <button
                      type='button'
                      className='rankings-model-name'
                      aria-pressed={activeModel === model.model_name}
                      onClick={() => setSelectedModel(model.model_name)}
                      title={t('rankings.selectModel')}
                    >
                      <span className='rankings-model-avatar'>
                        <Cpu size={18} aria-hidden='true' />
                      </span>
                      <span>{model.model_name}</span>
                    </button>
                    <div className='rankings-share'>
                      <div className='rankings-share-track' aria-hidden='true'>
                        <span style={{ width: `${share * 100}%` }} />
                      </div>
                      <span>{percent.format(share)}</span>
                    </div>
                    <span className='rankings-model-value'>
                      {number.format(model[search.metric])}
                      <small>{metricLabel}</small>
                    </span>
                    <Link
                      to='/pricing/$modelId'
                      params={{ modelId: model.model_name }}
                      className='rankings-model-detail'
                      aria-label={`${t('rankings.viewModel')}: ${model.model_name}`}
                    >
                      <ArrowUpRight size={17} aria-hidden='true' />
                    </Link>
                  </li>
                )
              })}
            </ol>
            {visibleModels.length === 0 && (
              <div className='rankings-state'>
                <h3>{t('rankings.noMatches')}</h3>
                <Button variant='outline' onClick={() => setModelFilter('')}>
                  {t('rankings.resetSearch')}
                </Button>
              </div>
            )}
          </div>
        </>
      )}
    </section>
  )
}
