import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useRouterState } from '@tanstack/react-router'
import {
  INTERFACE_LANGUAGE_OPTIONS,
  normalizeInterfaceLanguage,
  toIntlLocale,
} from '@/i18n/languages'
import { ArrowUpRight, Check, ChevronDown, Copy, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { PublicHeader } from '@/components/layout'
import { SiteSeo } from '@/components/seo'
import { SiteFooter } from '@/components/site-footer'
import { SkipToMain } from '@/components/skip-to-main'
import { usePricingData } from '@/features/pricing/hooks/use-pricing-data'
import {
  docArticles,
  docArticleNeedsModelCatalog,
  resolveDocArticle,
  searchDocArticles,
  type ContentLanguage,
  type DocBlock,
} from './content'
import { docsMessages } from './messages'
import { docSample, type SampleLanguage } from './samples'
import './styles.css'

const groups = [
  'start',
  'users',
  'market',
  'suppliers',
  'api',
  'troubleshooting',
  'updates',
] as const

export function ApiDocs() {
  const { i18n } = useTranslation()
  const normalized = normalizeInterfaceLanguage(i18n.language)
  const locale =
    ({ 'zh-TW': 'zh-HK', zh: 'zh-CN' } as Record<string, string>)[normalized] ??
    normalized
  return <DocsPage key={locale} locale={locale} />
}

function DocsPage(props: { locale: string }) {
  const locale = props.locale
  const ui = docsMessages(locale)
  const location = useRouterState({ select: (state) => state.location })
  const [overrideLanguage, setOverrideLanguage] = useState<ContentLanguage>()
  const defaultLanguage = locale as ContentLanguage
  const language = overrideLanguage ?? defaultLanguage
  const [mobile, setMobile] = useState(
    () => window.matchMedia('(max-width: 768px)').matches
  )
  const [navigationOpen, setNavigationOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const [sampleLanguage, setSampleLanguage] = useState<SampleLanguage>('curl')
  const [model, setModel] = useState<string>()
  const slug =
    new URLSearchParams(location.searchStr).get('article') ?? undefined
  const article = resolveDocArticle(slug, location.hash)
  const needsModelCatalog = docArticleNeedsModelCatalog(article)
  const pricing = usePricingData(needsModelCatalog)
  const catalog = useMemo(
    () => ({
      data: pricing.models.map((item) => ({ name: item.model_name })),
      isError: Boolean(pricing.error),
    }),
    [pricing.models, pricing.error]
  )
  const selectedModel = model ?? catalog.data?.[0]?.name ?? ''
  const results = useMemo(
    () => searchDocArticles(searchQuery, language),
    [searchQuery, language]
  )
  const index = article ? docArticles.indexOf(article) : -1
  const heading = useRef<HTMLHeadingElement>(null)
  const previousSlug = useRef(article?.slug)
  const [activeSection, setActiveSection] = useState<string>()
  const base = window.location.origin
  const modelSuggestions = useMemo(() => {
    const normalizedModel = selectedModel.trim().toLocaleLowerCase()
    const matches = catalog.data.filter((item) =>
      item.name.toLocaleLowerCase().includes(normalizedModel)
    )
    return matches.slice(0, 50)
  }, [catalog.data, selectedModel])

  useEffect(() => {
    const timer = window.setTimeout(() => setSearchQuery(query), 180)
    return () => window.clearTimeout(timer)
  }, [query])

  useEffect(() => {
    const media = window.matchMedia('(max-width: 768px)')
    const update = () => setMobile(media.matches)
    media.addEventListener('change', update)
    return () => media.removeEventListener('change', update)
  }, [])
  useEffect(() => {
    if (previousSlug.current !== article?.slug)
      heading.current?.focus({ preventScroll: true })
    previousSlug.current = article?.slug
  }, [article?.slug])
  useEffect(() => {
    if (!article) return
    const elements = article.sections
      .map((section) => document.getElementById(section.id))
      .filter((element): element is HTMLElement => Boolean(element))
    const update = () => {
      let current = elements[0]?.id
      for (const element of elements) {
        if (element.getBoundingClientRect().top <= 180) current = element.id
      }
      setActiveSection(current)
    }
    update()
    window.addEventListener('scroll', update, { passive: true })
    return () => window.removeEventListener('scroll', update)
  }, [article])

  const renderBlock = (block: DocBlock, key: number) => {
    if (block.kind === 'paragraph')
      return <p key={key}>{block.text[language]}</p>
    if (block.kind === 'note')
      return (
        <aside key={key} className='docs-reading-note'>
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
        <div key={key} className='table-scroll'>
          <table>
            <thead>
              <tr>
                {block.headers.map((value, i) => (
                  <th scope='col' key={i}>
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
        <div key={key} className='docs-related-links'>
          {block.items.map((item) => (
            <a key={item.href} href={item.href}>
              {item.label[language]}{' '}
              <ArrowUpRight size={14} aria-hidden='true' />
            </a>
          ))}
        </div>
      )
    const code = docSample(block.sample, sampleLanguage, base, selectedModel)
    return (
      <div key={key} className='docs-example'>
        <div className='docs-example-settings'>
          {block.sample !== 'models' && block.sample !== 'environment' && (
            <>
              <label>
                {ui.model}
                <input
                  list='docs-models'
                  value={selectedModel}
                  onChange={(event) => setModel(event.target.value)}
                  placeholder={ui.noModels}
                  autoComplete='off'
                  spellCheck={false}
                  dir='ltr'
                />
              </label>
              {catalog.isError && (
                <p role='status' className='subtle'>
                  {ui.catalogError}
                </p>
              )}
            </>
          )}
          <div className='docs-code-tabs' role='group' aria-label='SDK'>
            <button
              type='button'
              aria-pressed={sampleLanguage === 'curl'}
              onClick={() => setSampleLanguage('curl')}
            >
              cURL
            </button>
            <button
              type='button'
              aria-pressed={sampleLanguage === 'python'}
              onClick={() => setSampleLanguage('python')}
            >
              Python
            </button>
            <button
              type='button'
              aria-pressed={sampleLanguage === 'node'}
              onClick={() => setSampleLanguage('node')}
            >
              Node.js
            </button>
          </div>
        </div>
        <div className='code-block' dir='ltr'>
          <div className='docs-code-toolbar'>
            <code>{codeResource(block.sample)}</code>
            <CopyButton value={code} label={ui.copy} />
          </div>
          <pre>
            <code>{code}</code>
          </pre>
        </div>
      </div>
    )
  }

  return (
    <div className='dawn docs-page'>
      <SkipToMain />
      <PublicHeader />
      <SiteSeo title={ui.title} description={ui.title} canonicalPath='/docs' />
      <main id='content' tabIndex={-1} className='docs-center'>
        <nav className='docs-navigation' aria-label={ui.sections}>
          <button
            className='docs-mobile-toggle'
            type='button'
            aria-expanded={navigationOpen}
            aria-controls='docs-navigation-content'
            onClick={() => setNavigationOpen(!navigationOpen)}
          >
            {ui.sections}
            <ChevronDown size={16} aria-hidden />
          </button>
          <div id='docs-navigation-content' hidden={mobile && !navigationOpen}>
            <Link to='/docs' className='docs-navigation-title'>
              {ui.title}
            </Link>
            <label className='docs-search'>
              <Search size={16} aria-hidden />
              <span className='sr-only'>{ui.search}</span>
              <input
                type='search'
                value={query}
                placeholder={ui.search}
                onChange={(event) => setQuery(event.target.value)}
              />
            </label>
            {results.length === 0 ? (
              <div className='docs-no-results' role='status'>
                <p>{ui.noResults}</p>
                <button type='button' onClick={() => setQuery('')}>
                  {ui.clear}
                </button>
              </div>
            ) : (
              groups.map((group) => {
                const members = results.filter((item) => item.group === group)
                return (
                  members.length > 0 && (
                    <section className='docs-navigation-group' key={group}>
                      <h2>{ui[group]}</h2>
                      {members.map((item) => (
                        <Link
                          key={item.slug}
                          to='/docs'
                          search={{ article: item.slug }}
                          aria-current={
                            article?.slug === item.slug ? 'page' : undefined
                          }
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
        <div className='docs-reading-column'>
          <div className='docs-reading-controls'>
            <label htmlFor='docs-content-language'>{ui.contentLanguage}</label>
            <select
              id='docs-content-language'
              value={language}
              onChange={(event) =>
                setOverrideLanguage(event.target.value as ContentLanguage)
              }
            >
              {INTERFACE_LANGUAGE_OPTIONS.map((option) => (
                <option key={option.code} value={toIntlLocale(option.code)}>
                  {option.label}
                </option>
              ))}
            </select>
          </div>
          {article ? (
            <article className='docs-article' lang={language} dir='ltr'>
              <header>
                <div className='docs-location'>
                  <Link to='/docs'>{ui.title}</Link>
                  <span aria-hidden='true'>/</span>
                  <span>{ui[article.group]}</span>
                </div>
                <h1 ref={heading} tabIndex={-1}>
                  {article.title[language]}
                </h1>
              </header>
              <details className='docs-mobile-outline'>
                <summary>{ui.onPage}</summary>
                <nav aria-label={ui.onPage}>
                  {article.sections.map((section) => (
                    <a
                      key={section.id}
                      href={`#${section.id}`}
                      aria-current={
                        activeSection === section.id ? 'location' : undefined
                      }
                    >
                      {section.title[language]}
                    </a>
                  ))}
                </nav>
              </details>
              {article.slug === 'quickstart' && (
                <div className='docs-base-url'>
                  <span>{ui.base}</span>
                  <CopyField value={`${base}/v1`} label={ui.base} />
                </div>
              )}
              {article.sections.map((section) => (
                <section className='docs-article-section' key={section.id}>
                  <h2 id={section.id}>{section.title[language]}</h2>
                  {section.blocks.map(renderBlock)}
                </section>
              ))}
              <nav className='docs-pagination' aria-label={ui.title}>
                {index > 0 ? (
                  <Link
                    to='/docs'
                    search={{ article: docArticles[index - 1].slug }}
                  >
                    <small>{ui.previous}</small>
                    {docArticles[index - 1].title[language]}
                  </Link>
                ) : (
                  <span />
                )}
                {index < docArticles.length - 1 && (
                  <Link
                    to='/docs'
                    search={{ article: docArticles[index + 1].slug }}
                  >
                    <small>{ui.next}</small>
                    {docArticles[index + 1].title[language]}
                  </Link>
                )}
              </nav>
            </article>
          ) : (
            <section className='docs-missing'>
              <h1>{ui.notFound}</h1>
              <Link to='/docs'>{ui.start}</Link>
            </section>
          )}
        </div>
        {article && (
          <nav className='docs-outline' aria-label={ui.onPage}>
            <h2>{ui.onPage}</h2>
            {article.sections.map((section) => (
              <a
                key={section.id}
                href={`#${section.id}`}
                aria-current={
                  activeSection === section.id ? 'location' : undefined
                }
              >
                {section.title[language]}
              </a>
            ))}
          </nav>
        )}
        {needsModelCatalog && (
          <datalist id='docs-models'>
            {modelSuggestions.map((item) => (
              <option key={item.name} value={item.name} />
            ))}
          </datalist>
        )}
      </main>
      <SiteFooter />
    </div>
  )
}

function codeResource(sample: import('./samples').DocSample) {
  if (sample === 'environment') return 'CODEGO_API_KEY'
  if (sample === 'models') return 'GET /v1/models'
  if (sample.startsWith('responses')) return 'POST /v1/responses'
  if (sample === 'embeddings') return 'POST /v1/embeddings'
  return 'POST /v1/chat/completions'
}

function CopyButton(props: { value: string; label: string }) {
  const { copiedText, copyToClipboard } = useCopyToClipboard()
  return (
    <button
      type='button'
      aria-label={props.label}
      title={props.label}
      onClick={() => void copyToClipboard(props.value)}
    >
      {copiedText === props.value ? (
        <Check size={16} aria-hidden />
      ) : (
        <Copy size={16} aria-hidden />
      )}
    </button>
  )
}
function CopyField(props: { value: string; label: string }) {
  return (
    <div className='docs-copy-field'>
      <code>{props.value}</code>
      <CopyButton {...props} />
    </div>
  )
}
