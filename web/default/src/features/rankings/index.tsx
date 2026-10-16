import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { getIntlLocale } from '@/i18n/format'
import { Route } from '@/routes/rankings'
import {
  ArrowUpRight,
  ChartNoAxesCombined,
  Layers3,
  RefreshCw,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { PublicLayout } from '@/components/layout'
import { getMarketplaceGroups } from '@/features/marketplace/api'
import type { GroupFilters } from '@/features/marketplace/types'
import { ModelRankings } from './model-rankings'
import { RankingsTable } from './rankings-table'
import './rankings.css'

export function RankingsPage() {
  const { t } = useTranslation()
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const filters: GroupFilters = {
    search: '',
    model: '',
    source: '',
    provider: '',
    status: '',
    verification: '',
    sort: search.sort,
    direction: 'desc',
    window_hours: 24,
    page: search.page,
    page_size: 20,
  }
  const query = useQuery({
    queryKey: ['marketplace-groups', filters],
    queryFn: () => getMarketplaceGroups(filters),
    staleTime: 60_000,
    refetchOnWindowFocus: false,
  })
  const pageCount = Math.max(
    1,
    Math.ceil((query.data?.total ?? 0) / (query.data?.page_size ?? 20))
  )
  const number = new Intl.NumberFormat(getIntlLocale())

  return (
    <PublicLayout showMainContainer={false}>
      <main id='content' tabIndex={-1} className='solaris-rankings'>
        <aside className='rankings-rail'>
          <nav aria-label={t('rankings.overview')}>
            <a href='#model-usage'>
              <ChartNoAxesCombined size={17} aria-hidden='true' />
              {t('rankings.modelLeaderboard')}
            </a>
            <a href='#channel-rankings'>
              <Layers3 size={17} aria-hidden='true' />
              {t('rankings.channelLeaderboard')}
            </a>
          </nav>
          <div className='rankings-rail-note'>
            <Link to='/pricing'>
              {t('全部模型')}
              <ArrowUpRight size={14} aria-hidden='true' />
            </Link>
          </div>
        </aside>
        <div className='rankings-content'>
          <header className='rankings-intro'>
            <h1>
              {t('rankings.title')}
              <span aria-hidden='true'>.</span>
            </h1>
            <Link to='/market' className='rankings-market-link'>
              {t('rankings.exploreMarket')}
              <ArrowUpRight size={16} aria-hidden='true' />
            </Link>
          </header>
          <ModelRankings />
          <section
            id='channel-rankings'
            className='rankings-channel-section'
            aria-labelledby='rankings-list-title'
          >
            <div className='rankings-section-heading'>
              <div>
                <h2 id='rankings-list-title'>
                  {t('rankings.channelLeaderboard')}
                </h2>
              </div>
              <span className='rankings-period-label'>
                {t('rankings.last24Hours')}
              </span>
            </div>
            <div className='rankings-board'>
              <div className='rankings-toolbar'>
                <p aria-live='polite'>
                  {query.data
                    ? t('rankings.channelCountLabel', {
                        formattedCount: number.format(query.data.total),
                      })
                    : t('rankings.last24Hours')}
                </p>
                <div className='rankings-controls'>
                  <label htmlFor='ranking-sort' className='sr-only'>
                    {t('rankings.sortBy')}
                  </label>
                  <select
                    id='ranking-sort'
                    value={search.sort}
                    onChange={(event) => {
                      const sort = event.target.value as typeof search.sort
                      void navigate({ search: { ...search, sort, page: 1 } })
                    }}
                  >
                    <option value='score'>
                      {t('rankings.scoreDescending')}
                    </option>
                    <option value='success_rate'>
                      {t('rankings.successDescending')}
                    </option>
                    <option value='requests'>
                      {t('rankings.requestsDescending')}
                    </option>
                  </select>
                  <Button
                    variant='outline'
                    size='icon'
                    aria-label={t('rankings.refresh')}
                    disabled={query.isFetching}
                    onClick={() => void query.refetch()}
                  >
                    <RefreshCw size={16} aria-hidden='true' />
                  </Button>
                </div>
              </div>
              <div aria-busy={query.isFetching}>
                {query.isPending && (
                  <div className='rankings-state' role='status'>
                    <span
                      className='rankings-loading-line'
                      aria-hidden='true'
                    />
                    <p>{t('rankings.loading')}</p>
                  </div>
                )}
                {query.isError && (
                  <div className='rankings-state' role='alert'>
                    <h3>{t('rankings.loadError')}</h3>
                    <p>{t('rankings.loadErrorDescription')}</p>
                    <Button
                      variant='outline'
                      onClick={() => void query.refetch()}
                      disabled={query.isFetching}
                    >
                      {t('rankings.retry')}
                    </Button>
                  </div>
                )}
                {query.isSuccess && query.data.items.length === 0 && (
                  <div className='rankings-state'>
                    <h3>{t('rankings.empty')}</h3>
                    {search.page > 1 && (
                      <Button
                        variant='outline'
                        onClick={() =>
                          void navigate({ search: { ...search, page: 1 } })
                        }
                      >
                        {t('rankings.firstPage')}
                      </Button>
                    )}
                  </div>
                )}
                {query.isSuccess && query.data.items.length > 0 && (
                  <RankingsTable groups={query.data.items} />
                )}
              </div>
              {query.data && pageCount > 1 && !query.isError && (
                <nav
                  className='rankings-pagination'
                  aria-label={t('rankings.pages')}
                >
                  <Button
                    variant='outline'
                    disabled={search.page <= 1 || query.isFetching}
                    onClick={() =>
                      void navigate({
                        search: { ...search, page: search.page - 1 },
                      })
                    }
                  >
                    {t('rankings.previous')}
                  </Button>
                  <span>
                    {t('rankings.pageOf', {
                      page: number.format(search.page),
                      total: number.format(pageCount),
                    })}
                  </span>
                  <Button
                    variant='outline'
                    disabled={search.page >= pageCount || query.isFetching}
                    onClick={() =>
                      void navigate({
                        search: { ...search, page: search.page + 1 },
                      })
                    }
                  >
                    {t('rankings.next')}
                  </Button>
                </nav>
              )}
            </div>
          </section>
        </div>
      </main>
    </PublicLayout>
  )
}
