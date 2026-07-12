import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const projectRoot = path.resolve(__dirname, '..')
const distDir = path.join(projectRoot, 'dist')
const baseUrl = 'https://shu26.cfd'

const { getSearchPageFaq, getSearchPageSections, searchPages } = await import(
  pathToFileUrl(path.join(projectRoot, 'src/features/search-pages/data.ts')).href,
)

const topicGroups = [
  {
    title: '核心入口',
    description:
      '覆盖 Codex API、Claude Code API、Codex 中转、Claude 中转等主搜索词。',
    match: (slug) =>
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
    description:
      '覆盖教程、上手、配置、怎么用、怎么接等高频入门词。',
    match: (slug) =>
      /jiaocheng|shangshou|jinjie|peizhi|zenme-yong|zenme-jie/.test(slug),
  },
  {
    title: '比较与排障',
    description:
      '覆盖区别、怎么选、稳定吗、报错怎么办等决策与问题词。',
    match: (slug) => /vs|zenme-xuan|wending-ma|baocuo-zenmeban/.test(slug),
  },
]

function pathToFileUrl(filePath) {
  const resolved = path.resolve(filePath).replace(/\\/g, '/')
  return new URL(`file:///${resolved}`)
}

function escapeHtml(value) {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;')
}

function slugToLabel(slug) {
  return slug.replaceAll('-', ' / ')
}

function buildTopicTitle(page) {
  return page.seoTitle.includes('Code Go')
    ? page.seoTitle
    : `${page.seoTitle} | Code Go`
}

function buildTopicDescription(page) {
  return `${page.description} ${page.intro}`.trim()
}

function keywordList(keywords) {
  return keywords
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
    .slice(0, 6)
}

function renderPage({
  title,
  description,
  canonicalPath,
  ogType = 'article',
  keywords = '',
  body,
  jsonLd,
}) {
  const canonical = `${baseUrl}${canonicalPath}`
  const jsonLdText = JSON.stringify(jsonLd).replaceAll('<', '\\u003c')

  return `<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <link rel="icon" type="image/svg+xml" href="/code-go-logo.svg?v=2" />
    <link rel="shortcut icon" type="image/svg+xml" href="/code-go-logo.svg?v=2" />
    <link rel="apple-touch-icon" href="/code-go-logo.svg?v=2" />
    <title>${escapeHtml(title)}</title>
    <meta name="title" content="${escapeHtml(title)}" />
    <meta name="description" content="${escapeHtml(description)}" />
    <meta name="keywords" content="${escapeHtml(keywords)}" />
    <meta name="robots" content="index,follow,max-image-preview:large" />
    <link rel="canonical" href="${canonical}" />
    <meta name="theme-color" content="#f5f7fb" />
    <meta property="og:title" content="${escapeHtml(title)}" />
    <meta property="og:description" content="${escapeHtml(description)}" />
    <meta property="og:type" content="${escapeHtml(ogType)}" />
    <meta property="og:url" content="${canonical}" />
    <meta property="og:site_name" content="Code Go" />
    <meta property="og:image" content="${baseUrl}/code-go-logo.svg" />
    <meta name="twitter:card" content="summary_large_image" />
    <meta name="twitter:title" content="${escapeHtml(title)}" />
    <meta name="twitter:description" content="${escapeHtml(description)}" />
    <meta name="twitter:image" content="${baseUrl}/code-go-logo.svg" />
    <style>
      :root {
        color-scheme: light;
        --bg: #f5f7fb;
        --surface: rgba(255, 255, 255, 0.84);
        --surface-strong: rgba(255, 255, 255, 0.95);
        --surface-soft: #f8fafc;
        --text: #18202b;
        --muted: #586577;
        --line: rgba(145, 161, 182, 0.24);
        --primary: #d96a39;
        --primary-soft: rgba(217, 106, 57, 0.12);
        --info: #3e76d2;
        --shadow: 0 14px 38px rgba(15, 20, 27, 0.08);
      }
      * { box-sizing: border-box; }
      html { scroll-behavior: smooth; }
      body {
        margin: 0;
        font-family: "Public Sans", "Segoe UI", sans-serif;
        background:
          radial-gradient(circle at top left, rgba(217, 106, 57, 0.12), transparent 26%),
          radial-gradient(circle at 82% 6%, rgba(62, 118, 210, 0.1), transparent 24%),
          var(--bg);
        color: var(--text);
      }
      a {
        color: inherit;
        text-decoration: none;
      }
      .topbar {
        position: sticky;
        top: 0;
        z-index: 30;
        border-bottom: 1px solid rgba(145, 161, 182, 0.18);
        background: rgba(245, 247, 251, 0.86);
        backdrop-filter: blur(14px);
      }
      .topbar-inner,
      .shell {
        width: min(1320px, calc(100% - 32px));
        margin: 0 auto;
      }
      .topbar-inner {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 20px;
        padding: 16px 0;
      }
      .brand-link {
        display: inline-flex;
        align-items: center;
        gap: 14px;
      }
      .brand-mark {
        display: inline-flex;
        align-items: center;
        justify-content: center;
        width: 42px;
        height: 42px;
        border: 1px solid var(--line);
        border-radius: 14px;
        background: rgba(255, 255, 255, 0.96);
        box-shadow: 0 10px 24px rgba(15, 20, 27, 0.08);
      }
      .brand-mark img {
        width: 28px;
        height: 28px;
        object-fit: contain;
      }
      .brand-copy {
        display: flex;
        flex-direction: column;
        gap: 2px;
      }
      .brand-copy strong {
        font-size: 15px;
        line-height: 1.1;
      }
      .brand-copy span,
      .top-links a {
        color: var(--muted);
        font-size: 13px;
      }
      .top-links {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        justify-content: flex-end;
        gap: 10px;
      }
      .top-links a {
        display: inline-flex;
        align-items: center;
        padding: 8px 12px;
        border-radius: 999px;
        border: 1px solid transparent;
        transition: background 0.2s ease, border-color 0.2s ease, color 0.2s ease;
      }
      .top-links a:hover {
        border-color: var(--line);
        background: rgba(255, 255, 255, 0.72);
        color: var(--text);
      }
      .shell {
        display: grid;
        grid-template-columns: 250px minmax(0, 1fr) 286px;
        gap: 20px;
        padding: 24px 0 72px;
      }
      .surface,
      .hero,
      .section,
      .nav-panel,
      .rail-card {
        border: 1px solid var(--line);
        background: var(--surface);
        backdrop-filter: blur(14px);
        border-radius: 22px;
        box-shadow: var(--shadow);
      }
      .hero {
        padding: 30px 32px;
      }
      .eyebrow {
        display: inline-flex;
        align-items: center;
        gap: 7px;
        border-radius: 999px;
        border: 1px solid var(--line);
        background: var(--surface-strong);
        padding: 8px 14px;
        font-size: 12px;
        font-weight: 700;
        color: #425062;
      }
      h1 {
        margin: 18px 0 0;
        max-width: 20ch;
        font-size: clamp(2.25rem, 4.6vw, 4rem);
        line-height: 1.04;
        letter-spacing: -0.03em;
        text-wrap: balance;
      }
      .hero-copy {
        margin: 16px 0 0;
        max-width: 72ch;
        font-size: 15px;
        line-height: 1.95;
        color: var(--muted);
      }
      .hero-links,
      .keyword-list,
      .topic-grid,
      .toc-grid,
      .highlight-grid {
        display: flex;
        flex-wrap: wrap;
        gap: 12px;
      }
      .hero-links {
        margin-top: 24px;
      }
      .hero-meta {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
        margin-top: 18px;
      }
      .pill,
      .meta-pill {
        display: inline-flex;
        align-items: center;
        border-radius: 999px;
        border: 1px solid var(--line);
        background: var(--surface-strong);
        padding: 10px 15px;
        font-size: 13px;
        font-weight: 600;
        color: #405064;
      }
      .meta-pill {
        padding: 7px 12px;
        font-size: 12px;
        font-weight: 700;
      }
      .content-stack,
      .rail {
        display: flex;
        flex-direction: column;
        gap: 20px;
      }
      .section {
        padding: 24px;
      }
      .section h2,
      .group-head h2 {
        margin: 10px 0 0;
        font-size: 1.85rem;
        line-height: 1.2;
        letter-spacing: -0.02em;
        text-wrap: balance;
      }
      .nav-panel,
      .rail-card {
        padding: 18px;
      }
      .nav-panel {
        position: sticky;
        top: 86px;
        align-self: start;
      }
      .nav-block + .nav-block,
      .rail-card + .rail-card {
        margin-top: 16px;
      }
      .nav-label,
      .kicker {
        font-size: 12px;
        font-weight: 700;
        color: var(--primary);
      }
      .nav-title {
        margin-top: 10px;
        font-size: 16px;
        font-weight: 700;
        color: var(--text);
      }
      .nav-copy {
        margin-top: 6px;
        font-size: 13px;
        line-height: 1.7;
        color: var(--muted);
      }
      .nav-list,
      .text-list {
        display: flex;
        flex-direction: column;
        gap: 8px;
        margin: 14px 0 0;
        padding: 0;
        list-style: none;
      }
      .nav-link,
      .topic-link,
      .rail-link {
        display: block;
        border: 1px solid transparent;
        border-radius: 16px;
        background: rgba(255, 255, 255, 0.6);
        padding: 12px 14px;
        transition: border-color 0.2s ease, background 0.2s ease, transform 0.2s ease;
      }
      .nav-link:hover,
      .topic-link:hover,
      .rail-link:hover {
        border-color: rgba(217, 106, 57, 0.24);
        background: rgba(255, 255, 255, 0.96);
        transform: translateY(-1px);
      }
      .nav-link strong,
      .topic-link strong,
      .rail-link strong {
        display: block;
        font-size: 14px;
        color: var(--text);
      }
      .nav-link span,
      .topic-link span,
      .rail-link span {
        display: block;
        margin-top: 4px;
        font-size: 13px;
        line-height: 1.65;
        color: var(--muted);
      }
      p {
        margin: 0;
        max-width: 72ch;
        font-size: 15px;
        line-height: 1.95;
        color: var(--muted);
        text-wrap: pretty;
      }
      p + p {
        margin-top: 16px;
      }
      .highlight-grid,
      .topic-grid,
      .toc-grid {
        display: grid;
        gap: 14px;
      }
      .highlight-grid {
        grid-template-columns: repeat(3, minmax(0, 1fr));
      }
      .topic-grid,
      .toc-grid {
        grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
      }
      .highlight-card,
      .toc-card,
      .topic-card,
      .faq-item {
        border: 1px solid var(--line);
        background: var(--surface-strong);
        border-radius: 18px;
      }
      .highlight-card,
      .toc-card,
      .topic-card,
      .faq-item {
        padding: 18px;
      }
      .highlight-card strong,
      .toc-card strong,
      .topic-card strong,
      .faq-item strong {
        display: block;
        color: var(--text);
      }
      .card-index,
      .toc-index {
        font-size: 12px;
        font-weight: 700;
        color: var(--primary);
      }
      .highlight-card p,
      .toc-card p,
      .topic-card p,
      .faq-item p {
        margin-top: 8px;
        font-size: 14px;
        line-height: 1.85;
      }
      .faq-grid {
        display: grid;
        gap: 12px;
        margin-top: 18px;
      }
      .group-head p {
        margin-top: 10px;
      }
      .rail-list {
        display: flex;
        flex-direction: column;
        gap: 10px;
        margin-top: 14px;
      }
      .text-list li {
        position: relative;
        padding-left: 16px;
        color: var(--muted);
        font-size: 14px;
        line-height: 1.8;
      }
      .text-list li::before {
        content: "";
        position: absolute;
        left: 0;
        top: 10px;
        width: 6px;
        height: 6px;
        border-radius: 999px;
        background: rgba(217, 106, 57, 0.9);
      }
      @media (max-width: 1180px) {
        .shell {
          grid-template-columns: minmax(0, 1fr) 280px;
        }
        .nav-panel {
          display: none;
        }
        .highlight-grid {
          grid-template-columns: repeat(2, minmax(0, 1fr));
        }
      }
      @media (max-width: 860px) {
        .shell {
          grid-template-columns: 1fr;
        }
        .rail {
          order: 3;
        }
      }
      @media (max-width: 720px) {
        .shell {
          width: min(100% - 24px, 1320px);
          padding-top: 18px;
        }
        .topbar-inner {
          width: min(100% - 24px, 1320px);
          align-items: flex-start;
          flex-direction: column;
          padding: 14px 0;
        }
        .top-links {
          justify-content: flex-start;
        }
        .hero,
        .section {
          border-radius: 20px;
          padding: 20px;
        }
        .highlight-grid {
          grid-template-columns: 1fr;
        }
      }
    </style>
  </head>
  <body>
    ${body}
    <script type="application/ld+json">${jsonLdText}</script>
  </body>
</html>`
}

function renderBrandBar() {
  return `<header class="topbar">
    <div class="topbar-inner">
      <a class="brand-link" href="/">
        <span class="brand-mark">
          <img src="/code-go-logo.svg" alt="Code Go" />
        </span>
        <span class="brand-copy">
          <strong>Code Go</strong>
          <span>AI Coding Topics / 搜索专题页</span>
        </span>
      </a>
      <nav class="top-links" aria-label="Topic shortcuts">
        <a href="/">首页</a>
        <a href="/pricing">查看模型</a>
        <a href="/guide">使用教程</a>
        <a href="/topics">专题目录</a>
      </nav>
    </div>
  </header>`
}

function renderHeader(title, description, eyebrow, meta = '') {
  const links = [
    { label: '回到首页', href: '/' },
    { label: '查看模型', href: '/pricing' },
    { label: '使用教程', href: '/guide' },
  ]

  return `<section class="hero">
    <div class="eyebrow">${escapeHtml(eyebrow)}</div>
    <h1>${escapeHtml(title)}</h1>
    <p class="hero-copy">${escapeHtml(description)}</p>
    ${meta ? `<div class="hero-meta">${meta}</div>` : ''}
    <div class="hero-links">
      ${links
        .map(
          (item) =>
            `<a class="pill" href="${item.href}">${escapeHtml(item.label)}</a>`,
        )
        .join('')}
    </div>
  </section>`
}

function renderTopicDetail(page) {
  const sections = getSearchPageSections(page)
  const faqItems = getSearchPageFaq(page)
  const relatedPages = searchPages.filter((item) => item.slug !== page.slug).slice(0, 6)
  const keywords = keywordList(page.keywords)

  const toc = sections
    .map(
      (section, index) => `<a class="toc-card" href="#section-${index + 1}">
  <div class="toc-index">${String(index + 1).padStart(2, '0')}</div>
  <strong>${escapeHtml(section.heading)}</strong>
  <p>${escapeHtml(section.paragraphs[0] || page.intro)}</p>
</a>`,
    )
    .join('')

  const sectionMarkup = sections
    .map(
      (section, index) => `<section class="section" id="section-${index + 1}">
  <div class="kicker">第 ${index + 1} 节</div>
  <h2>${escapeHtml(section.heading)}</h2>
  <div style="margin-top:18px;">
    ${section.paragraphs.map((paragraph) => `<p>${escapeHtml(paragraph)}</p>`).join('')}
  </div>
</section>`,
    )
    .join('')

  const faq = faqItems
    .map(
      (item) => `<div class="faq-item">
  <strong>${escapeHtml(item.question)}</strong>
  <p>${escapeHtml(item.answer)}</p>
</div>`,
    )
    .join('')

  const related = relatedPages
    .map(
      (item) => `<a class="topic-link" href="/topics/${item.slug}">
  <strong>${escapeHtml(item.title)}</strong>
  <span>${escapeHtml(item.description)}</span>
</a>`,
    )
    .join('')

  const meta = keywords
    .map((item) => `<span class="meta-pill">${escapeHtml(item)}</span>`)
    .join('')

  const navLinks = sections
    .map(
      (section, index) => `<a class="nav-link" href="#section-${index + 1}">
  <strong>${String(index + 1).padStart(2, '0')} ${escapeHtml(section.heading)}</strong>
  <span>${escapeHtml(section.paragraphs[0] || page.intro)}</span>
</a>`,
    )
    .join('')

  return renderPage({
    title: buildTopicTitle(page),
    description: buildTopicDescription(page),
    canonicalPath: `/topics/${page.slug}`,
    keywords: page.keywords,
    ogType: 'article',
    jsonLd: [
      {
        '@context': 'https://schema.org',
        '@type': 'BreadcrumbList',
        itemListElement: [
          { '@type': 'ListItem', position: 1, name: 'Code Go', item: `${baseUrl}/` },
          {
            '@type': 'ListItem',
            position: 2,
            name: '专题页',
            item: `${baseUrl}/topics`,
          },
          {
            '@type': 'ListItem',
            position: 3,
            name: page.title,
            item: `${baseUrl}/topics/${page.slug}`,
          },
        ],
      },
      {
        '@context': 'https://schema.org',
        '@type': 'TechArticle',
        headline: buildTopicTitle(page),
        description: buildTopicDescription(page),
        inLanguage: 'zh-CN',
        mainEntityOfPage: `${baseUrl}/topics/${page.slug}`,
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
    ],
    body: `${renderBrandBar()}
<main class="shell">
  <aside class="nav-panel">
    <div class="nav-block">
      <div class="nav-label">本页目录</div>
      <div class="nav-title">${escapeHtml(page.title)}</div>
      <div class="nav-copy">先按章节理解关键词，再决定下一步去模型页还是教程页。</div>
      <div class="nav-list">
        <a class="nav-link" href="#overview">
          <strong>总览</strong>
          <span>快速理解这个专题词的用途和阅读顺序。</span>
        </a>
        ${navLinks}
        <a class="nav-link" href="#faq">
          <strong>FAQ</strong>
          <span>集中看常见问题与对应判断方式。</span>
        </a>
      </div>
    </div>
    <div class="nav-block">
      <div class="nav-label">相关专题</div>
      <div class="nav-list">${relatedPages
        .slice(0, 4)
        .map(
          (item) => `<a class="nav-link" href="/topics/${item.slug}">
  <strong>${escapeHtml(item.title)}</strong>
  <span>${escapeHtml(item.description)}</span>
</a>`,
        )
        .join('')}</div>
    </div>
  </aside>

  <div class="content-stack">
    ${renderHeader(page.hero, page.intro, `Topic / ${slugToLabel(page.slug)}`, meta)}

    <section class="section" id="overview">
      <div class="kicker">阅读方式</div>
      <h2>这是专题页，不是只给搜索引擎看的占位页</h2>
      <p style="margin-top:16px;">这一页会先解释“为什么有人会搜这个词”，再把模型、价格路径、教程入口和常见判断逻辑整理清楚。你可以先看总览，再按目录直接跳到最关心的章节。</p>
      <div class="highlight-grid" style="margin-top:18px;">
        <div class="highlight-card">
          <div class="card-index">01</div>
          <strong style="margin-top:10px;">先判断你要解决什么问题</strong>
          <p>是找模型入口、看接入教程，还是正在比较稳定性、成本和错误处理。</p>
        </div>
        <div class="highlight-card">
          <div class="card-index">02</div>
          <strong style="margin-top:10px;">再看这个词和 Code Go 的关系</strong>
          <p>看清它对应的模型、路由方式和最短使用路径，不在关键词里绕圈。</p>
        </div>
        <div class="highlight-card">
          <div class="card-index">03</div>
          <strong style="margin-top:10px;">最后回到模型页或教程页</strong>
          <p>专题页负责解释和分流，真正下决策还是回到模型广场与教程入口。</p>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="kicker">章节导航</div>
      <h2>按章节快速进入</h2>
      <div class="toc-grid" style="margin-top:18px;">${toc}</div>
    </section>

    ${sectionMarkup}

    <section class="section" id="faq">
      <div class="kicker">常见问题</div>
      <h2>FAQ</h2>
      <div class="faq-grid">${faq}</div>
    </section>
  </div>

  <aside class="rail">
    <section class="rail-card">
      <div class="kicker">快速入口</div>
      <h2 style="margin:10px 0 0; font-size:1.2rem;">先继续往哪里看</h2>
      <div class="rail-list">
        <a class="rail-link" href="/pricing">
          <strong>查看模型</strong>
          <span>先看免费模型、Claude、GPT 与当前可用分组。</span>
        </a>
        <a class="rail-link" href="/guide">
          <strong>查看教程</strong>
          <span>从配置、接入到长期使用路径继续往下看。</span>
        </a>
        <a class="rail-link" href="/topics">
          <strong>回到专题目录</strong>
          <span>继续切换到其它相关专题与教程入口。</span>
        </a>
      </div>
    </section>

    <section class="rail-card">
      <div class="kicker">本页定位</div>
      <ul class="text-list">
        <li>承接搜索进入后的第一轮解释，而不是只做标题占位。</li>
        <li>帮助你快速判断这个词在实际接入流程里的位置。</li>
        <li>把首页、模型页、教程页和专题页串成同一条阅读链路。</li>
      </ul>
    </section>

    <section class="rail-card">
      <div class="kicker">相关专题</div>
      <div class="rail-list">${related}</div>
    </section>
  </aside>
</main>`,
  })
}

function renderTopicsIndex() {
  const groups = topicGroups
    .map((group) => ({
      ...group,
      items: searchPages.filter((item) => group.match(item.slug)),
    }))
    .filter((group) => group.items.length > 0)

  const hotTopics = searchPages.slice(0, 4)

  const sections = groups
    .map(
      (group, index) => `<section class="section" id="group-${index + 1}">
  <div class="group-head">
    <div class="kicker">Topic Group</div>
    <h2>${escapeHtml(group.title)}</h2>
    <p>${escapeHtml(group.description)}</p>
  </div>
  <div class="topic-grid" style="margin-top:18px;">
    ${group.items
      .map(
        (item) => `<a class="topic-card" href="/topics/${item.slug}">
  <strong>${escapeHtml(item.title)}</strong>
  <p>${escapeHtml(item.description)}</p>
</a>`,
      )
      .join('')}
  </div>
</section>`,
    )
    .join('')

  return renderPage({
    title: 'Codex API、Claude Code API、Codex 中转、Claude 中转专题页 | Code Go',
    description:
      'Code Go 专题页汇总，覆盖 Codex API、Claude Code API、Codex 中转、Claude 中转、教程、配置、排障与模型选择。',
    canonicalPath: '/topics',
    keywords:
      'Codex API, Claude Code API, Codex中转, Claude中转, AI API 中转, 教程, 配置, 排障, Code Go',
    ogType: 'website',
    jsonLd: {
      '@context': 'https://schema.org',
      '@type': 'CollectionPage',
      headline: 'Codex API、Claude Code API、Codex 中转、Claude 中转专题页',
      description:
        'Code Go 专题页汇总，覆盖 Codex API、Claude Code API、Codex 中转、Claude 中转、教程、配置、排障与模型选择。',
      inLanguage: 'zh-CN',
      url: `${baseUrl}/topics`,
    },
    body: `${renderBrandBar()}
<main class="shell">
  <aside class="nav-panel">
    <div class="nav-block">
      <div class="nav-label">页内导航</div>
      <div class="nav-title">Topics Index</div>
      <div class="nav-copy">按搜索意图看专题，不要先陷进零散关键词。</div>
      <div class="nav-list">
        <a class="nav-link" href="#overview">
          <strong>总览</strong>
          <span>先理解专题页存在的目的和阅读顺序。</span>
        </a>
        <a class="nav-link" href="#path">
          <strong>进入路径</strong>
          <span>先判断要看模型、教程还是比较与排障。</span>
        </a>
        ${groups
          .map(
            (group, index) => `<a class="nav-link" href="#group-${index + 1}">
  <strong>${escapeHtml(group.title)}</strong>
  <span>${escapeHtml(group.description)}</span>
</a>`,
          )
          .join('')}
      </div>
    </div>
    <div class="nav-block">
      <div class="nav-label">热门专题</div>
      <div class="nav-list">
        ${hotTopics
          .map(
            (item) => `<a class="nav-link" href="/topics/${item.slug}">
  <strong>${escapeHtml(item.title)}</strong>
  <span>${escapeHtml(item.description)}</span>
</a>`,
          )
          .join('')}
      </div>
    </div>
  </aside>

  <div class="content-stack">
  ${renderHeader(
    'Codex API、Claude Code API、Codex 中转、Claude 中转专题页',
    '这是 Code Go 的专题页总入口。适合从搜索直接进入的用户先看清关键词含义、适用场景、教程入口和模型选择路径，再决定下一步去模型页还是教程页。',
    'Topic / Index',
  )}
    <section class="section" id="overview">
      <div class="kicker">总览</div>
      <h2>先把词义、模型和下一步路径理顺</h2>
      <p style="margin-top:16px;">这个入口页的目标不是堆满 SEO 关键词，而是让从搜索直接进入的用户先看懂：这些词各自代表什么、适合去哪一页继续看，以及怎么最快完成配置与选择。</p>
      <div class="highlight-grid" style="margin-top:18px;">
        <div class="highlight-card">
          <div class="card-index">01</div>
          <strong style="margin-top:10px;">先看核心入口专题</strong>
          <p>如果你搜的是 Codex API、Claude Code API、Codex 中转、Claude 中转，先从核心入口看起。</p>
        </div>
        <div class="highlight-card">
          <div class="card-index">02</div>
          <strong style="margin-top:10px;">再按教程或排障分流</strong>
          <p>需要接入、配置、怎么用，就去教程；需要比较和排查，就去比较与排障。</p>
        </div>
        <div class="highlight-card">
          <div class="card-index">03</div>
          <strong style="margin-top:10px;">最后回到模型页或教程页</strong>
          <p>专题页负责解释搜索词，真正做模型选择和接入时还是回到模型广场与指南页。</p>
        </div>
      </div>
    </section>

    <section class="section" id="path">
      <div class="kicker">进入路径</div>
      <h2>按搜索意图进入，而不是逐个点开试</h2>
      <div class="toc-grid" style="margin-top:18px;">
        <a class="toc-card" href="#group-1">
          <div class="toc-index">01</div>
          <strong>核心入口</strong>
          <p>先看主搜索词和对应的产品入口，适合第一次判断模型与路由方式。</p>
        </a>
        <a class="toc-card" href="#group-2">
          <div class="toc-index">02</div>
          <strong>接入教程</strong>
          <p>看怎么接、怎么配、怎么用，把接入流程一次理顺。</p>
        </a>
        <a class="toc-card" href="#group-3">
          <div class="toc-index">03</div>
          <strong>比较与排障</strong>
          <p>如果你正在比较、排错或判断稳定性，直接去问题导向专题。</p>
        </a>
      </div>
    </section>

    ${sections}
  </div>

  <aside class="rail">
    <section class="rail-card">
      <div class="kicker">快速入口</div>
      <div class="rail-list">
        <a class="rail-link" href="/pricing">
          <strong>查看模型</strong>
          <span>先看免费模型、Claude、GPT 与当前模型分组。</span>
        </a>
        <a class="rail-link" href="/guide">
          <strong>使用教程</strong>
          <span>从接入、配置到使用路径继续往下看。</span>
        </a>
        <a class="rail-link" href="/">
          <strong>回到首页</strong>
          <span>查看平台入口概览、导航与主要能力说明。</span>
        </a>
      </div>
    </section>

    <section class="rail-card">
      <div class="kicker">为什么先看专题页</div>
      <ul class="text-list">
        <li>把零散搜索词整理成可阅读的入口结构。</li>
        <li>让用户在模型、教程、排障之间快速分流。</li>
        <li>避免进入首页后还要重新判断该点哪里。</li>
      </ul>
    </section>

    <section class="rail-card">
      <div class="kicker">热门专题</div>
      <div class="rail-list">
        ${hotTopics
          .map(
            (item) => `<a class="rail-link" href="/topics/${item.slug}">
  <strong>${escapeHtml(item.title)}</strong>
  <span>${escapeHtml(item.description)}</span>
</a>`,
          )
          .join('')}
      </div>
    </section>
  </aside>
</main>`,
  })
}

await fs.mkdir(path.join(distDir, 'topics'), { recursive: true })
await fs.writeFile(
  path.join(distDir, 'topics', 'index.html'),
  renderTopicsIndex(),
  'utf8',
)

for (const page of searchPages) {
  const targetDir = path.join(distDir, 'topics', page.slug)
  await fs.mkdir(targetDir, { recursive: true })
  await fs.writeFile(
    path.join(targetDir, 'index.html'),
    renderTopicDetail(page),
    'utf8',
  )
}

console.log(`Generated ${searchPages.length + 1} static topic pages.`)
