import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { Search } from 'lucide-react'
import { useTranslation } from '../lib/i18n'
import { publicModelsOptions, vendorOf } from '../lib/public-catalog'
import { ModelGroupPrice } from '../features/public/model-group-price'
import { Badge, CopyButton, EmptyState, ErrorMessage, Loading, Button } from '../components/ui'

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
  const filtered = models.filter(
    (model) =>
      (!search.vendor || model.vendor === search.vendor) &&
      (!query || model.name.toLowerCase().includes(query)),
  )
  const setSearch = (next: { q?: string; vendor?: string }) =>
    void navigate({ search: (prev) => ({ ...prev, ...next }), replace: true })

  return (
    <div className="site-container site-page">
      <header className="page-header">
        <div className="page-header-text">
          <h1>{t('模型')}</h1>
          <p>{t('价格已包含分组倍率，以 credits 展示。个别优惠和实际用量会影响最终结算。')}</p>
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
              placeholder={t('搜索模型名称，如 gpt-4o、claude…')}
              defaultValue={search.q ?? ''}
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
            <>
              <p className="subtle" style={{ marginBlock: 12 }}>
                {t('共')} {filtered.length} {t('个模型')}
              </p>
              {filtered.length === 0 ? (
                <EmptyState
                  title={models.length === 0 ? '暂无公开模型' : '没有匹配的模型'}
                  description={models.length === 0 ? undefined : '换个关键词或清除厂商筛选。'}
                />
              ) : (
                <ul className="model-list">
                  {filtered.map((model) => (
                    <li key={model.name} className="model-row">
                      <div className="model-row-main">
                        <div className="model-row-title">
                          <code className="mono">{model.name}</code>
                          <CopyButton value={model.name} label="复制模型名" />
                        </div>
                        <div className="model-row-meta">
                          <Badge>{t(model.vendor)}</Badge>
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
                      <details className="model-row-groups" open={model.groups.length > 1}>
                        <summary>{t('提供分组')}</summary>
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
                      </details>
                    </li>
                  ))}
                </ul>
              )}
            </>
          )}
          <p className="subtle" style={{ marginTop: 24 }}>
            {t('需要按分组比较延迟和成功率？')}{' '}
            <Link to="/channel-market" style={{ color: 'var(--accent)' }}>
              {t('前往渠道市场')}
            </Link>
          </p>
        </section>
      </div>
    </div>
  )
}
