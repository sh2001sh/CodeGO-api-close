import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useRouterState } from '@tanstack/react-router'
import { ChevronDown, Search } from 'lucide-react'
import { useTranslation } from '../lib/i18n'
import { publicModelsOptions } from '../lib/public-catalog'
import { CopyButton, CopyField } from '../components/ui'
import {
  docArticles,
  resolveDocArticle,
  searchDocArticles,
  type ContentLanguage,
  type DocBlock,
} from '../features/docs/content'
import { docsMessages } from '../features/docs/messages'
import { docSample, type SampleLanguage } from '../features/docs/samples'
import '../features/docs/docs.css'

const groups = [
  'start',
  'users',
  'market',
  'suppliers',
  'api',
  'troubleshooting',
  'updates',
] as const

export default function DocsPage() {
  const { locale } = useTranslation()
  const ui = docsMessages(locale)
  const location = useRouterState({ select: (state) => state.location })
  const [overrideLanguage, setOverrideLanguage] = useState<ContentLanguage>()
  const language =
    overrideLanguage ?? (locale === 'zh-HK' ? 'zh-HK' : locale === 'zh-CN' ? 'zh-CN' : 'en')
  const [mobile, setMobile] = useState(() => window.matchMedia('(max-width: 760px)').matches)
  const [navigationOpen, setNavigationOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [sampleLanguage, setSampleLanguage] = useState<SampleLanguage>('curl')
  const [model, setModel] = useState<string>()
  const catalog = useQuery(publicModelsOptions())
  const selectedModel = model ?? catalog.data?.[0]?.name ?? ''
  const slug = new URLSearchParams(location.searchStr).get('article') ?? undefined
  const article = resolveDocArticle(slug, location.hash)
  const results = searchDocArticles(query, language)
  const index = article ? docArticles.indexOf(article) : -1
  const heading = useRef<HTMLHeadingElement>(null)
  const previousSlug = useRef(article?.slug)
  const base = window.location.origin

  useEffect(() => {
    setOverrideLanguage(undefined)
  }, [locale])
  useEffect(() => {
    const media = window.matchMedia('(max-width: 760px)')
    const update = () => setMobile(media.matches)
    media.addEventListener('change', update)
    return () => media.removeEventListener('change', update)
  }, [])
  useEffect(() => {
    if (previousSlug.current !== article?.slug) heading.current?.focus({ preventScroll: true })
    previousSlug.current = article?.slug
  }, [article?.slug])

  const renderBlock = (block: DocBlock, key: number) => {
    if (block.kind === 'paragraph') return <p key={key}>{block.text[language]}</p>
    if (block.kind === 'note')
      return (
        <aside key={key} className="docs-reading-note">
          {block.text[language]}
        </aside>
      )
    if (block.kind === 'list') {
      const List = block.ordered ? 'ol' : 'ul'
      return (
        <List key={key}>
          {block.items.map((item, i) => (
            <li key={i}>{item[language]}</li>
          ))}
        </List>
      )
    }
    if (block.kind === 'table')
      return (
        <div key={key} className="table-scroll">
          <table>
            <thead>
              <tr>
                {block.headers.map((value, i) => (
                  <th scope="col" key={i}>
                    {value[language]}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {block.rows.map((row, i) => (
                <tr key={i}>
                  {row.map((value, j) => (
                    <td key={j}>{value[language]}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )
    if (block.kind === 'links')
      return (
        <div key={key} className="docs-related-links">
          {block.items.map((item) => (
            <a key={item.href} href={item.href}>
              {item.label[language]} <span aria-hidden>↗</span>
            </a>
          ))}
        </div>
      )
    const code = docSample(
      block.sample,
      block.sample === 'quickstart' ? sampleLanguage : 'curl',
      base,
      selectedModel,
    )
    return (
      <div key={key} className="docs-example">
        <div className="docs-example-settings">
          {block.sample !== 'models' && (
            <>
              <label>
                {ui.model}
                <input
                  list="docs-models"
                  value={selectedModel}
                  onChange={(event) => setModel(event.target.value)}
                  placeholder={ui.noModels}
                  autoComplete="off"
                  spellCheck={false}
                  dir="ltr"
                />
              </label>
              <p className="subtle">{ui.modelHint}</p>
              {catalog.isError && (
                <p role="status" className="subtle">
                  {ui.catalogError}
                </p>
              )}
            </>
          )}
          {block.sample === 'quickstart' && (
            <div className="docs-code-tabs" aria-label="SDK">
              <button
                type="button"
                aria-pressed={sampleLanguage === 'curl'}
                onClick={() => setSampleLanguage('curl')}
              >
                cURL
              </button>
              <button
                type="button"
                aria-pressed={sampleLanguage === 'python'}
                onClick={() => setSampleLanguage('python')}
              >
                Python
              </button>
              <button
                type="button"
                aria-pressed={sampleLanguage === 'node'}
                onClick={() => setSampleLanguage('node')}
              >
                Node.js
              </button>
            </div>
          )}
        </div>
        <div className="code-block" dir="ltr">
          <CopyButton value={code} label={ui.copy} />
          <pre>
            <code>{code}</code>
          </pre>
        </div>
        {block.sample === 'quickstart' && sampleLanguage !== 'curl' && (
          <p className="subtle">{ui.install}</p>
        )}
        <p className="subtle">{ui.noAutoRequest}</p>
      </div>
    )
  }

  return (
    <div className="site-container site-page docs-center">
      <nav className="docs-navigation" aria-label={ui.sections}>
        <button
          className="docs-mobile-toggle"
          type="button"
          aria-expanded={navigationOpen}
          aria-controls="docs-navigation-content"
          onClick={() => setNavigationOpen(!navigationOpen)}
        >
          {ui.sections}
          <ChevronDown size={16} aria-hidden />
        </button>
        <div id="docs-navigation-content" hidden={mobile && !navigationOpen}>
          <Link to="/docs" className="docs-navigation-title">
            {ui.title}
          </Link>
          <label className="docs-search">
            <Search size={16} aria-hidden />
            <span className="sr-only">{ui.search}</span>
            <input
              type="search"
              value={query}
              placeholder={ui.search}
              onChange={(event) => setQuery(event.target.value)}
            />
          </label>
          {results.length === 0 ? (
            <div className="docs-no-results" role="status">
              <p>{ui.noResults}</p>
              <button type="button" onClick={() => setQuery('')}>
                {ui.clear}
              </button>
            </div>
          ) : (
            groups.map((group) => {
              const members = results.filter((item) => item.group === group)
              return (
                members.length > 0 && (
                  <section className="docs-navigation-group" key={group}>
                    <h2>{ui[group]}</h2>
                    {members.map((item) => (
                      <Link
                        key={item.slug}
                        to="/docs"
                        search={{ article: item.slug }}
                        aria-current={article?.slug === item.slug ? 'page' : undefined}
                        onClick={() => setNavigationOpen(false)}
                      >
                        {item.title[language]}
                      </Link>
                    ))}
                  </section>
                )
              )
            })
          )}
        </div>
      </nav>
      <div className="docs-reading-column">
        <div className="docs-reading-controls">
          <label htmlFor="docs-content-language">{ui.contentLanguage}</label>
          <select
            id="docs-content-language"
            value={language}
            onChange={(event) => setOverrideLanguage(event.target.value as ContentLanguage)}
          >
            <option value="zh-HK">繁體中文（香港）</option>
            <option value="zh-CN">简体中文</option>
            <option value="en">English</option>
          </select>
        </div>
        {!locale.startsWith('zh') && locale !== 'en' && language === 'en' && (
          <p className="docs-language-notice" role="status">
            {ui.fallback}
          </p>
        )}
        {article ? (
          <article className="prose docs-article" lang={language} dir="ltr">
            <header>
              <p className="docs-article-eyebrow">{ui[article.group]}</p>
              <h1 ref={heading} tabIndex={-1}>
                {article.title[language]}
              </h1>
              <p className="lead">{article.summary[language]}</p>
            </header>
            <details className="docs-mobile-outline">
              <summary>{ui.onPage}</summary>
              <nav aria-label={ui.onPage}>
                {article.sections.map((section) => (
                  <Link
                    key={section.id}
                    to="/docs"
                    search={{ article: article.slug }}
                    hash={section.id}
                  >
                    {section.title[language]}
                  </Link>
                ))}
              </nav>
            </details>
            {article.slug === 'quickstart' && (
              <div className="docs-base-url">
                <span>{ui.base}</span>
                <CopyField value={`${base}/v1`} label={ui.base} />
              </div>
            )}
            {article.sections.map((section) => (
              <section className="docs-article-section" key={section.id}>
                <h2 id={section.id}>{section.title[language]}</h2>
                {section.blocks.map(renderBlock)}
              </section>
            ))}
            <nav className="docs-pagination" aria-label={ui.title}>
              {index > 0 ? (
                <Link to="/docs" search={{ article: docArticles[index - 1].slug }}>
                  <small>{ui.previous}</small>
                  {docArticles[index - 1].title[language]}
                </Link>
              ) : (
                <span />
              )}
              {index < docArticles.length - 1 && (
                <Link to="/docs" search={{ article: docArticles[index + 1].slug }}>
                  <small>{ui.next}</small>
                  {docArticles[index + 1].title[language]}
                </Link>
              )}
            </nav>
          </article>
        ) : (
          <section className="docs-missing">
            <h1>{ui.notFound}</h1>
            <p>{ui.notFoundHint}</p>
            <Link to="/docs">{ui.start}</Link>
          </section>
        )}
      </div>
      {article && (
        <nav className="docs-outline" aria-label={ui.onPage}>
          <h2>{ui.onPage}</h2>
          {article.sections.map((section) => (
            <Link key={section.id} to="/docs" search={{ article: article.slug }} hash={section.id}>
              {section.title[language]}
            </Link>
          ))}
        </nav>
      )}
      <datalist id="docs-models">
        {catalog.data?.map((item) => (
          <option key={item.name} value={item.name} />
        ))}
      </datalist>
    </div>
  )
}
