import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowRight, Search } from 'lucide-react'
import { useTranslation } from '../lib/i18n'
import { modelsFromGroups, publicGroupsOptions } from '../lib/public-catalog'
import { sessionOptions } from '../lib/queries'
import { MarketBoard } from '../features/home/market-board'
import { Integration } from '../features/home/integration'
import { boardRows, boardSummary } from '../features/home/board-data'
import { FAQ } from '../features/public/faq'

function Count(props: { value: number | undefined; unit: string }) {
  const { t, locale } = useTranslation()
  return (
    <div className="home-count">
      <dd>{props.value === undefined ? '—' : props.value.toLocaleString(locale)}</dd>
      <dt>{t(props.unit)}</dt>
    </div>
  )
}

export default function HomePage() {
  const { t } = useTranslation()
  const session = useQuery({ ...sessionOptions(), throwOnError: false })
  const [query, setQuery] = useState('')
  const [search, setSearch] = useState('')
  useEffect(() => {
    const timer = window.setTimeout(() => setSearch(query.trim()), 250)
    return () => window.clearTimeout(timer)
  }, [query])
  const groups = useQuery(publicGroupsOptions(search))
  const rows = useMemo(() => (groups.data ? boardRows(groups.data) : undefined), [groups.data])
  const models = useMemo(() => modelsFromGroups(groups.data ?? []), [groups.data])
  const discovered = useMemo(() => {
    const vendors = new Set<string>()
    const entries: typeof models = []
    for (const model of models) {
      if (vendors.has(model.vendor)) continue
      vendors.add(model.vendor)
      entries.push(model)
      if (entries.length === 6) break
    }
    return entries
  }, [models])
  const summary = rows ? boardSummary(rows) : undefined
  const sampleModel = rows?.find((row) => row.models.length > 0)?.models[0] ?? 'YOUR_MODEL_ID'
  const signedIn = Boolean(session.data)
  const showMatches = (event: FormEvent) => {
    event.preventDefault()
    document.getElementById('live-market')?.scrollIntoView({
      behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth',
      block: 'start',
    })
  }

  return (
    <div className="home">
      <section className="home-intro site-container" aria-labelledby="home-title">
        <div className="home-intro-copy">
          <h1 id="home-title">{t('模型与 API')}</h1>
          <form className="home-search" onSubmit={showMatches} role="search">
            <label className="home-search-field">
              <Search size={20} aria-hidden />
              <span className="sr-only">{t('搜索分组或模型')}</span>
              <input
                type="search"
                maxLength={200}
                value={query}
                placeholder={t('搜索分组或模型')}
                onChange={(event) => setQuery(event.target.value)}
              />
            </label>
            <button type="submit" className="home-search-submit" aria-label={t('查看匹配分组')}>
              <ArrowRight size={20} aria-hidden />
            </button>
          </form>
          <nav className="home-intro-links" aria-label={t('更多入口')}>
            <Link to="/models">
              {t('模型与价格')} <ArrowRight size={14} aria-hidden />
            </Link>
            <Link to="/playground">
              {t('开始对话')} <ArrowRight size={14} aria-hidden />
            </Link>
            <Link to={signedIn ? '/keys' : '/sign-up'}>
              {t(signedIn ? '创建 API Key' : '注册账号')} <ArrowRight size={14} aria-hidden />
            </Link>
          </nav>
        </div>
        <figure className="home-intro-art">
          <img
            src="/brand/codego-routing-small.webp"
            alt=""
            width={768}
            height={512}
            fetchPriority="low"
            decoding="async"
          />
          <figcaption>{t('香港 · CodeGo AI Limited')}</figcaption>
        </figure>
      </section>

      {discovered.length > 0 && (
        <section className="site-container home-discovery" aria-labelledby="home-discovery-title">
          <div className="home-section-heading">
            <div>
              <h2 id="home-discovery-title">{t('探索模型')}</h2>
            </div>
            <Link to="/models" className="home-text-link">
              {t('查看全部')} <ArrowRight size={16} aria-hidden />
            </Link>
          </div>
          <ul className="home-model-directory">
            {discovered.map((model) => (
              <li key={model.name}>
                <Link to="/models" search={{ q: model.name }}>
                  <span className="home-model-vendor">
                    <bdi>{t(model.vendor)}</bdi>
                    <ArrowRight size={16} aria-hidden />
                  </span>
                  <code>{model.name}</code>
                  <small>
                    {model.groups.length} {t('个分组')}
                  </small>
                </Link>
              </li>
            ))}
          </ul>
        </section>
      )}

      <section
        className="site-container home-market"
        id="live-market"
        aria-label={t('模型路由行情')}
      >
        <div className="home-section-heading">
          <div>
            <h2>{t('比较分组')}</h2>
          </div>
          <Link to="/channel-market" className="home-text-link">
            {t('渠道市场')} <ArrowRight size={16} aria-hidden />
          </Link>
        </div>
        <dl className="home-counts" aria-live="polite">
          <Count value={summary?.models} unit="个模型" />
          <Count value={summary?.vendors} unit="家厂商" />
          <Count value={summary?.groups} unit="个分组" />
          <Count value={summary?.up} unit="个可用" />
        </dl>
        <MarketBoard
          rows={rows}
          query={query}
          onClearQuery={() => setQuery('')}
          pending={groups.isPending}
          error={groups.error}
          onRetry={() => void groups.refetch()}
          updatedAt={groups.dataUpdatedAt}
          footer={
            <Link to="/docs" hash="groups" className="home-text-link">
              {t('了解分组选择')} <ArrowRight size={16} aria-hidden />
            </Link>
          }
        />
      </section>

      <section className="home-connect" aria-labelledby="home-connect-title">
        <div className="site-container">
          <div className="home-section-heading">
            <div>
              <h2 id="home-connect-title">{t('接入与使用')}</h2>
            </div>
            <Link to="/docs" className="home-text-link">
              {t('接入文档')} <ArrowRight size={16} aria-hidden />
            </Link>
          </div>
          <ol className="home-steps">
            <li>
              <h3>{t('创建账号')}</h3>
              <Link to={signedIn ? '/dashboard' : '/sign-up'} className="home-text-link">
                {t(signedIn ? '进入控制台' : '注册账号')} <ArrowRight size={15} aria-hidden />
              </Link>
            </li>
            <li>
              <h3>{t('准备额度与密钥')}</h3>
              <Link to="/wallet" className="home-text-link">
                {t('前往钱包')} <ArrowRight size={15} aria-hidden />
              </Link>
            </li>
            <li>
              <h3>{t('发送第一条请求')}</h3>
              <Link to="/playground" className="home-text-link">
                {t('开始对话')} <ArrowRight size={15} aria-hidden />
              </Link>
            </li>
          </ol>
          <Integration model={sampleModel} />
        </div>
      </section>

      <section className="site-container home-reference" aria-label={t('模型、分组与账单')}>
        <div>
          <h2>{t('模型、分组与账单')}</h2>
          <div className="resource-actions">
            <Link to="/models">{t('模型与价格')}</Link>
            <Link to="/docs" hash="billing">
              {t('计费说明')}
            </Link>
            <Link to="/docs" hash="plans">
              {t('套餐与余额')}
            </Link>
          </div>
        </div>
        <div>
          <h2>{t('渠道工作台')}</h2>
          <Link to="/my-channels" className="home-text-link">
            {t('进入渠道工作台')} <ArrowRight size={16} aria-hidden />
          </Link>
        </div>
      </section>
      <section className="site-container home-faq" aria-labelledby="home-faq-title">
        <div className="home-section-heading">
          <h2 id="home-faq-title">{t('常见问题')}</h2>
          <Link to="/help" className="home-text-link">
            {t('查看全部问题')} <ArrowRight size={16} aria-hidden />
          </Link>
        </div>
        <FAQ limit={4} />
      </section>
    </div>
  )
}
