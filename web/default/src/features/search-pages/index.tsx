import { Link } from '@tanstack/react-router'
import { toIntlLocale } from '@/i18n/languages'
import { useTranslation } from 'react-i18next'
import { PublicLayout } from '@/components/layout'
import { SiteSeo } from '@/components/seo'
import '@/features/docs/reading.css'
import {
  getSearchPageBySlug,
  getSearchPageFaq,
  getSearchPageSections,
  searchPageGroups,
  searchPages,
} from './data'
import './topics.css'

const indexTitle =
  'Codex API、Claude Code API、Codex 中转、Claude 中转专题页 | Code Go'
const indexDescription =
  'Code Go 专题页汇总，覆盖 Codex API、Claude Code API、Codex 中转、Claude 中转、教程、配置、排障与模型选择。'

function TopicFrame({ children }: { children: React.ReactNode }) {
  return (
    <PublicLayout showMainContainer={false} showNotifications={false}>
      <main id='content' tabIndex={-1} className='market-reading topic-reading'>
        <div className='topic-layout'>{children}</div>
      </main>
    </PublicLayout>
  )
}

function EntryLinks() {
  const { t } = useTranslation()
  return (
    <nav className='topic-entry-links' aria-label={t('快速入口')}>
      <Link to='/topics'>{t('专题目录')}</Link>
      <Link to='/pricing'>{t('查看模型')}</Link>
      <Link to='/guide'>{t('使用教程')}</Link>
    </nav>
  )
}

function TopicLinks({ pages }: { pages: typeof searchPages }) {
  const { t } = useTranslation()
  return (
    <ul className='topic-link-list'>
      {pages.map((page) => (
        <li key={page.slug}>
          <Link to='/topics/$slug' params={{ slug: page.slug }}>
            {t(page.title)}
          </Link>
        </li>
      ))}
    </ul>
  )
}

export function SearchPage({ slug }: { slug: string }) {
  const { t, i18n } = useTranslation()
  const page = getSearchPageBySlug(slug)
  if (!page) {
    return (
      <TopicFrame>
        <article className='topic-article topic-not-found' role='status'>
          <h1>{t('未找到对应专题')}</h1>
          <EntryLinks />
        </article>
      </TopicFrame>
    )
  }
  const title = page.seoTitle.includes('Code Go')
    ? t(page.seoTitle)
    : `${t(page.seoTitle)} | Code Go`
  const description = `${t(page.description)} ${t(page.intro)}`.trim()
  const sections = getSearchPageSections(page)
  const faq = getSearchPageFaq(page)
  const relatedPages = searchPages
    .filter((item) => item.slug !== slug)
    .slice(0, 6)
  return (
    <>
      <SiteSeo
        title={title}
        description={description}
        keywords={page.keywords}
        canonicalPath={`/topics/${slug}`}
        ogType='article'
        jsonLd={[
          {
            '@context': 'https://schema.org',
            '@type': 'BreadcrumbList',
            itemListElement: [
              {
                '@type': 'ListItem',
                position: 1,
                name: 'Code Go',
                item: 'https://codegoai.com/',
              },
              {
                '@type': 'ListItem',
                position: 2,
                name: t('专题目录'),
                item: 'https://codegoai.com/topics',
              },
              {
                '@type': 'ListItem',
                position: 3,
                name: t(page.title),
                item: `https://codegoai.com/topics/${slug}`,
              },
            ],
          },
          {
            '@context': 'https://schema.org',
            '@type': 'TechArticle',
            headline: title,
            description,
            inLanguage: toIntlLocale(i18n.language),
            mainEntityOfPage: `https://codegoai.com/topics/${slug}`,
            keywords: page.keywords,
            author: { '@type': 'Organization', name: 'Code Go' },
            publisher: { '@type': 'Organization', name: 'Code Go' },
          },
          {
            '@context': 'https://schema.org',
            '@type': 'FAQPage',
            mainEntity: faq.map((item) => ({
              '@type': 'Question',
              name: t(item.question),
              acceptedAnswer: { '@type': 'Answer', text: t(item.answer) },
            })),
          },
        ]}
      />
      <TopicFrame>
        <aside className='topic-toc'>
          <nav aria-label={t('页内导航')}>
            <h2>{t('目录')}</h2>
            <ul className='topic-link-list'>
              {sections.map((section, index) => (
                <li key={section.heading}>
                  <a href={`#section-${index + 1}`}>{t(section.heading)}</a>
                </li>
              ))}
              <li>
                <a href='#faq'>{t('FAQ')}</a>
              </li>
            </ul>
          </nav>
        </aside>
        <article className='topic-article'>
          <header className='topic-heading'>
            <h1>{t(page.hero)}</h1>
            <EntryLinks />
          </header>
          {sections.map((section, index) => (
            <section key={section.heading} id={`section-${index + 1}`}>
              <h2>{t(section.heading)}</h2>
              {section.paragraphs.map((paragraph) => (
                <p key={paragraph}>{t(paragraph)}</p>
              ))}
            </section>
          ))}
          <section id='faq'>
            <h2>{t('FAQ')}</h2>
            <dl className='topic-faq'>
              {faq.map((item) => (
                <div key={item.question}>
                  <dt>{t(item.question)}</dt>
                  <dd>{t(item.answer)}</dd>
                </div>
              ))}
            </dl>
          </section>
        </article>
        <aside className='topic-related'>
          <nav aria-label={t('相关专题')}>
            <h2>{t('相关专题')}</h2>
            <TopicLinks pages={relatedPages} />
          </nav>
        </aside>
      </TopicFrame>
    </>
  )
}

export function SearchTopicsIndex() {
  const { t } = useTranslation()
  const groups = searchPageGroups.map((group) => ({
    ...group,
    pages: searchPages.filter((page) => group.match(page.slug)),
  }))
  return (
    <>
      <SiteSeo
        title={indexTitle}
        description={indexDescription}
        keywords='Codex API, Claude Code API, Codex中转, Claude中转, AI API 中转, 教程, 配置, 排障, Code Go'
        canonicalPath='/topics'
        ogType='website'
        jsonLd={{
          '@context': 'https://schema.org',
          '@type': 'CollectionPage',
          name: t('专题目录'),
          description: indexDescription,
          url: 'https://codegoai.com/topics',
          hasPart: searchPages.map((page) => ({
            '@type': 'TechArticle',
            name: t(page.title),
            url: `https://codegoai.com/topics/${page.slug}`,
          })),
        }}
      />
      <TopicFrame>
        <aside className='topic-toc'>
          <nav aria-label={t('页内导航')}>
            <h2>{t('目录')}</h2>
            <ul className='topic-link-list'>
              {groups.map((group, index) => (
                <li key={group.title}>
                  <a href={`#group-${index + 1}`}>{t(group.title)}</a>
                </li>
              ))}
            </ul>
          </nav>
        </aside>
        <article className='topic-article'>
          <header className='topic-heading'>
            <h1>{t('专题目录')}</h1>
            <EntryLinks />
          </header>
          {groups.map((group, index) => (
            <section key={group.title} id={`group-${index + 1}`}>
              <h2>{t(group.title)}</h2>
              <TopicLinks pages={group.pages} />
            </section>
          ))}
        </article>
        <aside className='topic-related'>
          <nav aria-label={t('热门专题')}>
            <h2>{t('热门专题')}</h2>
            <TopicLinks pages={searchPages.slice(0, 4)} />
          </nav>
        </aside>
      </TopicFrame>
    </>
  )
}
