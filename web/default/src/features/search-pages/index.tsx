import { Link } from '@tanstack/react-router'
import { PublicLayout } from '@/components/layout'
import { SiteSeo } from '@/components/seo'
import {
  getSearchPageBySlug,
  getSearchPageFaq,
  getSearchPageSections,
  searchPages,
} from './data'

const topicGroups = [
  {
    title: '核心入口',
    description:
      '覆盖 Codex API、Claude Code API、Codex 中转、Claude 中转等主搜索词。',
    match: (slug: string) =>
      [
        'codex-api',
        'codex-zhongzhuan',
        'claude-code-api',
        'claude-zhongzhuan',
        'ai-api-zhongzhuan',
      ].includes(slug),
  },
  {
    title: '接入教程',
    description: '覆盖教程、上手、配置、怎么用、怎么接等高频入门词。',
    match: (slug: string) =>
      /jiaocheng|shangshou|jinjie|peizhi|zenme-yong|zenme-jie/.test(slug),
  },
  {
    title: '比较与排障',
    description: '覆盖区别、怎么选、稳定吗、报错怎么办等决策与问题词。',
    match: (slug: string) =>
      /vs|zenme-xuan|wending-ma|baocuo-zenmeban/.test(slug),
  },
]

const topicEntryLinks = [
  { label: '回到首页', to: '/' as const },
  { label: '查看模型', to: '/pricing' as const },
  { label: '使用教程', to: '/guide' as const },
]

const TOPICS_INDEX_TITLE =
  'Codex API、Claude Code API、Codex 中转、Claude 中转专题页总入口 | Code Go'
const TOPICS_INDEX_DESCRIPTION =
  'Code Go 专题页总入口，汇总 Codex API、Claude Code API、Codex 中转、Claude 中转、教程、配置、排障与模型选择，方便从搜索结果进入后快速分流。'

function slugToLabel(slug: string) {
  return slug.replaceAll('-', ' / ')
}

function buildTopicTitle(page: {
  seoTitle: string
  title: string
  slug: string
}) {
  return page.seoTitle.includes('Code Go')
    ? page.seoTitle
    : `${page.seoTitle} | Code Go`
}

function buildTopicDescription(page: {
  description: string
  title: string
  intro: string
}) {
  return `${page.description} ${page.intro}`.trim()
}

function topicKeywordsList(keywords: string) {
  return keywords
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
    .slice(0, 6)
}

function TopicPageFrame(props: { children: React.ReactNode }) {
  return (
    <PublicLayout
      showMainContainer={false}
      showNotifications={false}
      showThemeSwitch={false}
    >
      <main className='public-topbar-spacer px-4 pb-12 sm:px-6 sm:pb-16 xl:px-8'>
        <div className='mx-auto grid max-w-[1320px] gap-5 xl:grid-cols-[250px_minmax(0,1fr)_286px]'>
          {props.children}
        </div>
      </main>
    </PublicLayout>
  )
}

function TopicSurface(props: {
  id?: string
  children: React.ReactNode
  className?: string
}) {
  return (
    <section
      id={props.id}
      className={[
        'app-page-shell p-6',
        props.className,
      ]
        .filter(Boolean)
        .join(' ')}
    >
      {props.children}
    </section>
  )
}

function TopicNavCard(props: {
  title: string
  description: string
  href: string
  external?: boolean
}) {
  const className =
    'block rounded-xl border border-transparent px-4 py-3 transition-all duration-200 hover:-translate-y-0.5 hover:border-primary/25 hover:bg-muted'

  if (props.external) {
    return (
      <a href={props.href} className={className}>
        <div className='text-foreground text-sm font-semibold'>
          {props.title}
        </div>
        <div className='text-muted-foreground mt-1 text-sm leading-6'>
          {props.description}
        </div>
      </a>
    )
  }

  return (
    <Link to={props.href} className={className}>
      <div className='text-foreground text-sm font-semibold'>
        {props.title}
      </div>
      <div className='text-muted-foreground mt-1 text-sm leading-6'>
        {props.description}
      </div>
    </Link>
  )
}

function TopicSlugCard(props: {
  title: string
  description: string
  slug: string
  anchorHref?: string
}) {
  const content = (
    <>
      <div className='text-foreground text-sm font-semibold'>
        {props.title}
      </div>
      <div className='text-muted-foreground mt-1 text-sm leading-6'>
        {props.description}
      </div>
    </>
  )

  if (props.anchorHref) {
    return (
      <a
        href={props.anchorHref}
        className='block rounded-xl border border-transparent px-4 py-3 transition-all duration-200 hover:-translate-y-0.5 hover:border-primary/25 hover:bg-muted'
      >
        {content}
      </a>
    )
  }

  return (
    <Link
      to='/topics/$slug'
      params={{ slug: props.slug }}
      className='block rounded-xl border border-transparent px-4 py-3 transition-all duration-200 hover:-translate-y-0.5 hover:border-primary/25 hover:bg-muted'
    >
      {content}
    </Link>
  )
}

function TopicHero(props: {
  eyebrow: string
  title: string
  description: string
  meta?: string[]
}) {
  return (
    <TopicSurface>
      <div className='text-muted-foreground bg-muted inline-flex items-center rounded-full border border-transparent px-3.5 py-2 text-[12px] font-semibold'>
        {props.eyebrow}
      </div>
      <h1 className='text-foreground mt-5 max-w-5xl text-[2.25rem] font-semibold tracking-[-0.03em] sm:text-5xl'>
        {props.title}
      </h1>
      <p className='text-muted-foreground mt-4 max-w-4xl text-[15px] leading-8'>
        {props.description}
      </p>
      {props.meta && props.meta.length > 0 && (
        <div className='mt-5 flex flex-wrap gap-2'>
          {props.meta.map((item) => (
            <span
              key={item}
              className='text-muted-foreground bg-muted inline-flex items-center rounded-full border border-transparent px-3 py-1.5 text-[12px] font-semibold'
            >
              {item}
            </span>
          ))}
        </div>
      )}
      <div className='mt-6 flex flex-wrap gap-2.5'>
        {topicEntryLinks.map((item) => (
          <Link
            key={item.to}
            to={item.to}
            className='text-foreground bg-muted hover:bg-accent inline-flex items-center rounded-full border border-transparent px-4 py-2 text-sm font-medium transition-colors'
          >
            {item.label}
          </Link>
        ))}
      </div>
    </TopicSurface>
  )
}

function SectionHeading(props: { kicker: string; title: string; description?: string }) {
  return (
    <div>
      <div className='text-primary text-[12px] font-semibold'>{props.kicker}</div>
      <h2 className='text-foreground mt-3 text-[1.85rem] font-semibold tracking-[-0.02em]'>
        {props.title}
      </h2>
      {props.description ? (
        <p className='text-muted-foreground mt-3 max-w-[72ch] text-[15px] leading-8'>
          {props.description}
        </p>
      ) : null}
    </div>
  )
}

function TopicSidebar(props: {
  navTitle: string
  navDescription: string
  navItems: React.ReactNode
  extraItems?: React.ReactNode
}) {
  return (
    <aside className='hidden xl:block'>
      <TopicSurface className='sticky top-[86px] p-[18px]'>
        <div className='text-primary text-[12px] font-semibold'>页内导航</div>
        <div className='text-foreground mt-3 text-base font-semibold'>
          {props.navTitle}
        </div>
        <div className='text-muted-foreground mt-2 text-[13px] leading-6'>
          {props.navDescription}
        </div>
        <div className='mt-4 space-y-2'>{props.navItems}</div>
        {props.extraItems ? (
          <div className='border-border/70 mt-5 border-t pt-5'>
            {props.extraItems}
          </div>
        ) : null}
      </TopicSurface>
    </aside>
  )
}

function TopicRightRail(props: { children: React.ReactNode }) {
  return <aside className='space-y-5'>{props.children}</aside>
}

function TopicBulletList(props: { items: string[] }) {
  return (
    <ul className='mt-4 space-y-3'>
      {props.items.map((item) => (
        <li
          key={item}
          className='text-muted-foreground relative pl-4 text-sm leading-7 before:absolute before:top-2.5 before:left-0 before:size-1.5 before:rounded-full before:bg-primary'
        >
          {item}
        </li>
      ))}
    </ul>
  )
}

export function SearchPage(props: { slug: string }) {
  const page = getSearchPageBySlug(props.slug)

  if (!page) {
    return (
      <TopicPageFrame>
        <div className='xl:col-start-2'>
          <TopicHero
            eyebrow='Topic / Missing'
            title='未找到对应专题'
            description='该专题可能不存在，或者当前链接已经变更。你可以先回到专题页总入口，或者直接查看模型与教程。'
          />
          <div className='mt-5 grid gap-4 md:grid-cols-3'>
            <TopicSurface>
              <SectionHeading kicker='入口' title='返回专题目录' />
              <div className='mt-4'>
                <TopicSlugCard
                  title='专题页总入口'
                  description='先回到专题页总入口重新选择主题。'
                  slug='codex-api'
                />
              </div>
            </TopicSurface>
            <TopicSurface>
              <SectionHeading kicker='模型' title='查看模型' />
              <div className='mt-4'>
                <TopicNavCard
                  title='查看模型'
                  description='继续浏览免费模型、Claude、GPT 与相关价格结构。'
                  href='/pricing'
                />
              </div>
            </TopicSurface>
            <TopicSurface>
              <SectionHeading kicker='教程' title='查看教程' />
              <div className='mt-4'>
                <TopicNavCard
                  title='查看教程'
                  description='从平台说明、模型选择到配置步骤继续往下看。'
                  href='/guide'
                />
              </div>
            </TopicSurface>
          </div>
        </div>
      </TopicPageFrame>
    )
  }

  const keywordList = topicKeywordsList(page.keywords)
  const sections = getSearchPageSections(page)
  const faqItems = getSearchPageFaq(page)
  const relatedPages = searchPages
    .filter((item) => item.slug !== page.slug)
    .slice(0, 6)

  return (
    <>
      <SiteSeo
        title={buildTopicTitle(page)}
        description={buildTopicDescription(page)}
        keywords={page.keywords}
        canonicalPath={`/topics/${page.slug}`}
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
                item: 'https://shu26.cfd/',
              },
              {
                '@type': 'ListItem',
                position: 2,
                name: '专题页',
                item: 'https://shu26.cfd/topics',
              },
              {
                '@type': 'ListItem',
                position: 3,
                name: page.title,
                item: `https://shu26.cfd/topics/${page.slug}`,
              },
            ],
          },
          {
            '@context': 'https://schema.org',
            '@type': 'TechArticle',
            headline: buildTopicTitle(page),
            description: buildTopicDescription(page),
            inLanguage: 'zh-CN',
            mainEntityOfPage: `https://shu26.cfd/topics/${page.slug}`,
            keywords: page.keywords,
            author: { '@type': 'Organization', name: 'Code Go' },
            publisher: { '@type': 'Organization', name: 'Code Go' },
          },
          {
            '@context': 'https://schema.org',
            '@type': 'FAQPage',
            mainEntity: faqItems.map((item) => ({
              '@type': 'Question',
              name: item.question,
              acceptedAnswer: {
                '@type': 'Answer',
                text: item.answer,
              },
            })),
          },
        ]}
      />

      <TopicPageFrame>
        <TopicSidebar
          navTitle={page.title}
          navDescription='先按章节理解关键词，再决定下一步去模型页还是教程页。'
          navItems={
            <>
              <TopicNavCard
                title='总览'
                description='快速理解这个专题词的用途和阅读顺序。'
                href='#overview'
                external
              />
              {sections.map((section, index) => (
                <TopicNavCard
                  key={section.heading}
                  title={`${String(index + 1).padStart(2, '0')} ${section.heading}`}
                  description={section.paragraphs[0] || page.intro}
                  href={`#section-${index + 1}`}
                  external
                />
              ))}
              <TopicNavCard
                title='FAQ'
                description='集中看常见问题与对应判断方式。'
                href='#faq'
                external
              />
            </>
          }
          extraItems={
            <>
              <div className='text-primary text-[12px] font-semibold'>相关专题</div>
              <div className='mt-3 space-y-2'>
                {relatedPages.slice(0, 4).map((item) => (
                  <TopicSlugCard
                    key={item.slug}
                    title={item.title}
                    description={item.description}
                    slug={item.slug}
                  />
                ))}
              </div>
            </>
          }
        />

        <div className='space-y-5 xl:col-start-2'>
          <TopicHero
            eyebrow={`Topic / ${slugToLabel(page.slug)}`}
            title={page.hero}
            description={page.intro}
            meta={keywordList}
          />

          <TopicSurface id='overview'>
            <SectionHeading
              kicker='阅读方式'
              title='这是专题页，不是只给搜索引擎看的占位页'
              description='这一页会先解释“为什么有人会搜这个词”，再把模型、价格路径、教程入口和常见判断逻辑整理清楚。你可以先看总览，再按目录直接跳到最关心的章节。'
            />
            <div className='mt-5 grid gap-4 md:grid-cols-3'>
              <div className='app-subtle-panel p-5'>
                <div className='text-primary text-[12px] font-semibold'>01</div>
                <div className='text-foreground mt-3 text-base font-semibold'>
                  先判断你要解决什么问题
                </div>
                <p className='text-muted-foreground mt-2 text-sm leading-7'>
                  是找模型入口、看接入教程，还是正在比较稳定性、成本和错误处理。
                </p>
              </div>
              <div className='app-subtle-panel p-5'>
                <div className='text-primary text-[12px] font-semibold'>02</div>
                <div className='text-foreground mt-3 text-base font-semibold'>
                  再看这个词和 Code Go 的关系
                </div>
                <p className='text-muted-foreground mt-2 text-sm leading-7'>
                  看清它对应的模型、路由方式和最短使用路径，不在关键词里绕圈。
                </p>
              </div>
              <div className='app-subtle-panel p-5'>
                <div className='text-primary text-[12px] font-semibold'>03</div>
                <div className='text-foreground mt-3 text-base font-semibold'>
                  最后回到模型页或教程页
                </div>
                <p className='text-muted-foreground mt-2 text-sm leading-7'>
                  专题页负责解释和分流，真正下决策还是回到模型广场与教程入口。
                </p>
              </div>
            </div>
          </TopicSurface>

          <TopicSurface>
            <SectionHeading kicker='章节导航' title='按章节快速进入' />
            <div className='mt-5 grid gap-4 md:grid-cols-2'>
              {sections.map((section, index) => (
                <a
                  key={section.heading}
                  href={`#section-${index + 1}`}
                  className='app-subtle-panel px-5 py-4 transition-colors hover:border-primary/25'
                >
                  <div className='text-primary text-[12px] font-semibold'>
                    {String(index + 1).padStart(2, '0')}
                  </div>
                  <div className='text-foreground mt-2 text-sm font-semibold'>
                    {section.heading}
                  </div>
                  <p className='text-muted-foreground mt-1 text-sm leading-6'>
                    {section.paragraphs[0] || page.intro}
                  </p>
                </a>
              ))}
            </div>
          </TopicSurface>

          {sections.map((section, index) => (
            <TopicSurface key={section.heading} id={`section-${index + 1}`}>
              <SectionHeading
                kicker={`第 ${index + 1} 节`}
                title={section.heading}
              />
              <div className='mt-5 space-y-4'>
                {section.paragraphs.map((paragraph) => (
                  <p
                    key={paragraph}
                    className='text-muted-foreground max-w-[72ch] text-[15px] leading-8'
                  >
                    {paragraph}
                  </p>
                ))}
              </div>
            </TopicSurface>
          ))}

          <TopicSurface id='faq'>
            <SectionHeading kicker='常见问题' title='FAQ' />
            <div className='mt-5 space-y-3'>
              {faqItems.map((item) => (
                <div
                  key={item.question}
                  className='app-subtle-panel px-5 py-5'
                >
                  <div className='text-foreground text-base font-semibold'>
                    {item.question}
                  </div>
                  <p className='text-muted-foreground mt-2 text-sm leading-7'>
                    {item.answer}
                  </p>
                </div>
              ))}
            </div>
          </TopicSurface>
        </div>

        <TopicRightRail>
          <TopicSurface>
            <div className='text-primary text-[12px] font-semibold'>快速入口</div>
            <div className='mt-4 space-y-3'>
              <TopicNavCard
                title='查看模型'
                description='先看免费模型、Claude、GPT 与当前可用分组。'
                href='/pricing'
              />
              <TopicNavCard
                title='查看教程'
                description='从配置、接入到长期使用路径继续往下看。'
                href='/guide'
              />
              <TopicNavCard
                title='回到专题目录'
                description='继续切换到其它相关专题与教程入口。'
                href='/topics'
              />
            </div>
          </TopicSurface>

          <TopicSurface>
            <div className='text-primary text-[12px] font-semibold'>本页定位</div>
            <TopicBulletList
              items={[
                '承接搜索进入后的第一轮解释，而不是只做标题占位。',
                '帮助你快速判断这个词在实际接入流程里的位置。',
                '把首页、模型页、教程页和专题页串成同一条阅读链路。',
              ]}
            />
          </TopicSurface>

          <TopicSurface>
            <div className='text-primary text-[12px] font-semibold'>相关专题</div>
            <div className='mt-4 space-y-3'>
              {relatedPages.map((item) => (
                <TopicSlugCard
                  key={item.slug}
                  title={item.title}
                  description={item.description}
                  slug={item.slug}
                />
              ))}
            </div>
          </TopicSurface>
        </TopicRightRail>
      </TopicPageFrame>
    </>
  )
}

export function SearchTopicsIndex() {
  const groupedTopics = topicGroups
    .map((group) => ({
      ...group,
      items: searchPages.filter((item) => group.match(item.slug)),
    }))
    .filter((group) => group.items.length > 0)

  const hotTopics = searchPages.slice(0, 4)

  return (
    <>
      <SiteSeo
        title={TOPICS_INDEX_TITLE}
        description={TOPICS_INDEX_DESCRIPTION}
        keywords='Codex API, Claude Code API, Codex中转, Claude中转, AI API 中转, 教程, 配置, 排障, Code Go'
        canonicalPath='/topics'
        ogType='website'
      />

      <TopicPageFrame>
        <TopicSidebar
          navTitle='Topics Index'
          navDescription='按搜索意图看专题，不要先陷进零散关键词。'
          navItems={
            <>
              <TopicNavCard
                title='总览'
                description='先理解专题页存在的目的和阅读顺序。'
                href='#overview'
                external
              />
              <TopicNavCard
                title='进入路径'
                description='先判断要看模型、教程还是比较与排障。'
                href='#path'
                external
              />
              {groupedTopics.map((group, index) => (
                <TopicNavCard
                  key={group.title}
                  title={group.title}
                  description={group.description}
                  href={`#group-${index + 1}`}
                  external
                />
              ))}
            </>
          }
          extraItems={
            <>
              <div className='text-primary text-[12px] font-semibold'>热门专题</div>
              <div className='mt-3 space-y-2'>
                {hotTopics.map((item) => (
                  <TopicSlugCard
                    key={item.slug}
                    title={item.title}
                    description={item.description}
                    slug={item.slug}
                  />
                ))}
              </div>
            </>
          }
        />

        <div className='space-y-5 xl:col-start-2'>
          <TopicHero
            eyebrow='Topic / Index'
            title='Codex API、Claude Code API、Codex 中转、Claude 中转专题页'
            description='这是 Code Go 的专题页总入口。适合从搜索直接进入的用户先看清关键词含义、适用场景、教程入口和模型选择路径，再决定下一步去模型页还是教程页。'
          />

          <TopicSurface id='overview'>
            <SectionHeading
              kicker='总览'
              title='先把词义、模型和下一步路径理顺'
              description='这个入口页的目标不是堆满 SEO 关键词，而是让从搜索直接进入的用户先看懂：这些词各自代表什么、适合去哪一页继续看，以及怎么最快完成配置与选择。'
            />
            <div className='mt-5 grid gap-4 md:grid-cols-3'>
              <div className='app-subtle-panel p-5'>
                <div className='text-primary text-[12px] font-semibold'>01</div>
                <div className='text-foreground mt-3 text-base font-semibold'>
                  先看核心入口专题
                </div>
                <p className='text-muted-foreground mt-2 text-sm leading-7'>
                  如果你搜的是 Codex API、Claude Code API、Codex 中转、Claude 中转，先从核心入口看起。
                </p>
              </div>
              <div className='app-subtle-panel p-5'>
                <div className='text-primary text-[12px] font-semibold'>02</div>
                <div className='text-foreground mt-3 text-base font-semibold'>
                  再按教程或排障分流
                </div>
                <p className='text-muted-foreground mt-2 text-sm leading-7'>
                  需要接入、配置、怎么用，就去教程；需要比较和排查，就去比较与排障。
                </p>
              </div>
              <div className='app-subtle-panel p-5'>
                <div className='text-primary text-[12px] font-semibold'>03</div>
                <div className='text-foreground mt-3 text-base font-semibold'>
                  最后回到模型页或教程页
                </div>
                <p className='text-muted-foreground mt-2 text-sm leading-7'>
                  专题页负责解释搜索词，真正做模型选择和接入时还是回到模型广场与指南页。
                </p>
              </div>
            </div>
          </TopicSurface>

          <TopicSurface id='path'>
            <SectionHeading
              kicker='进入路径'
              title='按搜索意图进入，而不是逐个点开试'
            />
            <div className='mt-5 grid gap-4 md:grid-cols-3'>
              <a
                href='#group-1'
                className='app-subtle-panel px-5 py-4 transition-colors hover:border-primary/25'
              >
                <div className='text-primary text-[12px] font-semibold'>01</div>
                <div className='text-foreground mt-2 text-sm font-semibold'>
                  核心入口
                </div>
                <p className='text-muted-foreground mt-1 text-sm leading-6'>
                  先看主搜索词和对应的产品入口，适合第一次判断模型与路由方式。
                </p>
              </a>
              <a
                href='#group-2'
                className='app-subtle-panel px-5 py-4 transition-colors hover:border-primary/25'
              >
                <div className='text-primary text-[12px] font-semibold'>02</div>
                <div className='text-foreground mt-2 text-sm font-semibold'>
                  接入教程
                </div>
                <p className='text-muted-foreground mt-1 text-sm leading-6'>
                  看怎么接、怎么配、怎么用，把接入流程一次理顺。
                </p>
              </a>
              <a
                href='#group-3'
                className='app-subtle-panel px-5 py-4 transition-colors hover:border-primary/25'
              >
                <div className='text-primary text-[12px] font-semibold'>03</div>
                <div className='text-foreground mt-2 text-sm font-semibold'>
                  比较与排障
                </div>
                <p className='text-muted-foreground mt-1 text-sm leading-6'>
                  如果你正在比较、排错或判断稳定性，直接去问题导向专题。
                </p>
              </a>
            </div>
          </TopicSurface>

          {groupedTopics.map((group, index) => (
            <TopicSurface key={group.title} id={`group-${index + 1}`}>
              <SectionHeading
                kicker='Topic Group'
                title={group.title}
                description={group.description}
              />
              <div className='mt-5 grid gap-4 md:grid-cols-2 xl:grid-cols-3'>
                {group.items.map((item) => (
                  <Link
                    key={item.slug}
                    to='/topics/$slug'
                    params={{ slug: item.slug }}
                    className='app-subtle-panel block p-5 transition-all duration-200 hover:-translate-y-0.5 hover:border-primary/25'
                  >
                    <div className='text-foreground text-base font-semibold'>
                      {item.title}
                    </div>
                    <p className='text-muted-foreground mt-2 text-sm leading-7'>
                      {item.description}
                    </p>
                  </Link>
                ))}
              </div>
            </TopicSurface>
          ))}
        </div>

        <TopicRightRail>
          <TopicSurface>
            <div className='text-primary text-[12px] font-semibold'>快速入口</div>
            <div className='mt-4 space-y-3'>
              <TopicNavCard
                title='查看模型'
                description='先看免费模型、Claude、GPT 与当前模型分组。'
                href='/pricing'
              />
              <TopicNavCard
                title='使用教程'
                description='从接入、配置到使用路径继续往下看。'
                href='/guide'
              />
              <TopicNavCard
                title='回到首页'
                description='查看平台入口概览、导航与主要能力说明。'
                href='/'
              />
            </div>
          </TopicSurface>

          <TopicSurface>
            <div className='text-primary text-[12px] font-semibold'>为什么先看专题页</div>
            <TopicBulletList
              items={[
                '把零散搜索词整理成可阅读的入口结构。',
                '让用户在模型、教程、排障之间快速分流。',
                '避免进入首页后还要重新判断该点哪里。',
              ]}
            />
          </TopicSurface>

          <TopicSurface>
            <div className='text-primary text-[12px] font-semibold'>热门专题</div>
            <div className='mt-4 space-y-3'>
              {hotTopics.map((item) => (
                <TopicSlugCard
                  key={item.slug}
                  title={item.title}
                  description={item.description}
                  slug={item.slug}
                />
              ))}
            </div>
          </TopicSurface>
        </TopicRightRail>
      </TopicPageFrame>
    </>
  )
}
