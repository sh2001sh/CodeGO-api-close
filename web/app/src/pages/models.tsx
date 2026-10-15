import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { Search } from 'lucide-react'
import { useTranslation } from '../lib/i18n'
import { publicModelsOptions, vendorOf, type PublicCatalogModel } from '../lib/public-catalog'
import { ModelGroupPrice } from '../features/public/model-group-price'
import {
  Badge,
  CopyButton,
  EmptyState,
  ErrorMessage,
  Loading,
  Button,
  Pagination,
} from '../components/ui'

const pageSize = 24

function ModelRow({ model }: { model: PublicCatalogModel }) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  return (
    <li className="model-row">
      <div className="model-row-main">
        <div className="model-row-title">
          <code className="mono">{model.name}</code>
          <CopyButton value={model.name} label="复制模型名" />
        </div>
        <div className="model-row-meta">
          <Badge>{t(model.vendor || vendorOf(model.name))}</Badge>
          <span>
            {model.groups.length} {t('个分组')}
          </span>
        </div>
      </div>
      <div className="model-row-price">
        <span className="subtle">{t('定价')}</span>
        {model.groups.length === 1 ? (
          <ModelGroupPrice group={model.groups[0]} />
        ) : (
          <span>{t('分组与定价')}</span>
        )}
      </div>
      <details
        className="model-row-groups"
        onToggle={(event) => setExpanded(event.currentTarget.open)}
      >
        <summary>{t('提供分组')}</summary>
        {expanded && (
          <ul>
            {model.groups.map((group) => (
              <li key={group.slug}>
                <span>{group.name}</span>
                {group.verified && <Badge tone="success">{t('已验证')}</Badge>}
                <span className="tabular">×{group.multiplier}</span>
                <ModelGroupPrice group={group} />
              </li>
            ))}
          </ul>
        )}
      </details>
    </li>
  )
}

function ModelResults({
  models,
  emptyCatalog,
}: {
  models: PublicCatalogModel[]
  emptyCatalog: boolean
}) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const activePage = Math.min(page, Math.max(1, Math.ceil(models.length / pageSize)))
  const visible = models.slice((activePage - 1) * pageSize, activePage * pageSize)
  return (
    <>
      <p className="subtle" style={{ marginBlock: 12 }} aria-live="polite">
        {t('共')} {models.length} {t('个模型')}
      </p>
      {models.length === 0 ? (
        <EmptyState title={emptyCatalog ? '暂无公开模型' : '没有匹配的模型'} />
      ) : (
        <>
          <ul className="model-list">
            {visible.map((model) => (
              <ModelRow key={model.name} model={model} />
            ))}
          </ul>
          {models.length > pageSize && (
            <Pagination
              page={activePage}
              pageSize={pageSize}
              total={models.length}
              onChange={setPage}
            />
          )}
        </>
      )}
    </>
  )
}

export default function ModelsPage() {
  const { t } = useTranslation()
  const search = useSearch({ from: '/_site/models' })
  const navigate = useNavigate({ from: '/models' })
  const catalog = useQuery(publicModelsOptions())
  const models = useMemo(
    () =>
      (catalog.data ?? []).map((model) => ({
        ...model,
        vendor: model.vendor || vendorOf(model.name),
      })),
    [catalog.data],
  )
  const vendorCounts = useMemo(() => {
    const counts = new Map<string, number>()
    for (const model of models) counts.set(model.vendor, (counts.get(model.vendor) ?? 0) + 1)
    return [...counts.entries()].sort((a, b) => b[1] - a[1])
  }, [models])
  const query = (search.q ?? '').trim().toLowerCase()
  const filtered = useMemo(
    () =>
      models.filter(
        (model) =>
          (!search.vendor || model.vendor === search.vendor) &&
          (!query || model.name.toLowerCase().includes(query)),
      ),
    [models, search.vendor, query],
  )
  const setSearch = (next: { q?: string; vendor?: string }) =>
    void navigate({ search: (prev) => ({ ...prev, ...next }), replace: true })

  return (
    <div className="site-container site-page">
      <header className="page-header">
        <div className="page-header-text">
          <h1>{t('模型')}</h1>
        </div>
      </header>
      <div className="models-layout">
        <aside className="filter-rail" aria-label={t('厂商筛选')}>
          <div className="nav-group-title">{t('厂商')}</div>
          <button
            type="button"
            className="filter-option"
            aria-pressed={!search.vendor}
            onClick={() => setSearch({ vendor: undefined })}
          >
            {t('全部')} <small>{models.length}</small>
          </button>
          {vendorCounts.map(([vendor, count]) => (
            <button
              key={vendor}
              type="button"
              className="filter-option"
              aria-pressed={search.vendor === vendor}
              onClick={() => setSearch({ vendor })}
            >
              {t(vendor)} <small>{count}</small>
            </button>
          ))}
        </aside>
        <section>
          <label className="search-field">
            <Search size={16} aria-hidden />
            <span className="sr-only">{t('搜索模型')}</span>
            <input
              type="search"
              placeholder={t('搜索模型')}
              value={search.q ?? ''}
              onChange={(event) => setSearch({ q: event.target.value || undefined })}
            />
          </label>
          <ErrorMessage error={catalog.error} />
          {catalog.isError && (
            <Button variant="secondary" onClick={() => void catalog.refetch()}>
              {t('重试')}
            </Button>
          )}
          {catalog.isPending && <Loading rows={6} />}
          {catalog.data && (
            <ModelResults
              key={`${search.vendor ?? ''}\0${query}`}
              models={filtered}
              emptyCatalog={models.length === 0}
            />
          )}
          <div style={{ marginTop: 24 }}>
            <Link to="/channel-market" className="button button-quiet">
              {t('前往渠道市场')}
            </Link>
          </div>
        </section>
      </div>
    </div>
  )
}
