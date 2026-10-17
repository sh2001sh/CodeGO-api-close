import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { Converter } from 'opencc-js'

const projectRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const distDir = path.join(projectRoot, 'dist')
const baseUrl = 'https://codegoai.com'
const toHongKong = Converter({ from: 'cn', to: 'hk' })
const { getSearchPageFaq, getSearchPageSections, searchPageGroups, searchPages } = await import(
  pathToFileURL(path.join(projectRoot, 'src/features/search-pages/data.ts')).href
)
const topicStyles = await fs.readFile(path.join(projectRoot, 'src/features/search-pages/topics.css'), 'utf8')
// Static topic HTML uses the same semantic tokens as the React reading surface.
const themeSource = await fs.readFile(path.join(projectRoot, 'src/styles/theme.css'), 'utf8')
const topicTokenNames = new Set([
  '--bg-canvas', '--bg-subtle', '--surface-card', '--surface-glass',
  '--surface-obsidian', '--ink-primary', '--ink-secondary', '--ink-inverse',
  '--hairline-light', '--brand-orange', '--brand-orange-deep', '--brand-orange-ink',
  '--gradient-solaris', '--background', '--foreground', '--muted-foreground',
  '--primary', '--primary-foreground', '--border',
])
function topicThemeTokens(selector) {
  const block = themeSource.match(new RegExp(`${selector}\\s*\\{([\\s\\S]*?)\\n\\}`))?.[1]
  if (!block) throw new Error(`Missing shared theme block: ${selector}`)
  return [...block.matchAll(/(--[a-z-]+):([^;]+);/g)]
    .filter(([, name]) => topicTokenNames.has(name))
    .map(([declaration]) => declaration).join(' ')
}
const lightTopicTokens = topicThemeTokens(':root')
const darkTopicTokens = topicThemeTokens('\\.dark,[^{]*')
const fonts = await fs.readdir(path.join(distDir, 'static/font'))
const publicSans = fonts.find((name) => /^public-sans-latin-wght-normal\..*\.woff2$/.test(name))

function escapeHtml(value) {
  return String(value).replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&quot;').replaceAll("'", '&#39;')
}

function titleFor(page) {
  return page.seoTitle.includes('Code Go') ? page.seoTitle : `${page.seoTitle} | Code Go`
}
function descriptionFor(page) {
  return `${page.description} ${page.intro}`.trim()
}
function topicLinks(pages) {
  return `<ul class="topic-link-list">${pages.map((page) => `<li><a href="/topics/${escapeHtml(page.slug)}">${escapeHtml(page.title)}</a></li>`).join('')}</ul>`
}
function entryLinks() {
  return '<nav class="topic-entry-links" aria-label="快速入口"><a href="/topics">专题目录</a><a href="/pricing">查看模型</a><a href="/guide">使用教程</a></nav>'
}
function pageFrame({ title, description, canonicalPath, keywords, ogType = 'article', jsonLd, body }) {
  const canonical = `${baseUrl}${canonicalPath}`
  return toHongKong(`<!doctype html>
<html lang="zh-HK">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <link rel="icon" type="image/svg+xml" href="/codego-mark.svg" />
  <link rel="apple-touch-icon" href="/codego-mark.svg" />
  <title>${escapeHtml(title)}</title>
  <meta name="title" content="${escapeHtml(title)}" />
  <meta name="description" content="${escapeHtml(description)}" />
  <meta name="keywords" content="${escapeHtml(keywords)}" />
  <meta name="robots" content="index,follow,max-image-preview:large" />
  <link rel="canonical" href="${canonical}" />
  <meta name="theme-color" content="#fefdfb" />
  <meta property="og:title" content="${escapeHtml(title)}" />
  <meta property="og:description" content="${escapeHtml(description)}" />
  <meta property="og:type" content="${ogType}" />
  <meta property="og:url" content="${canonical}" />
  <meta property="og:site_name" content="CodeGo" />
  <meta property="og:image" content="${baseUrl}/codego-mark.svg" />
  <meta name="twitter:card" content="summary_large_image" />
  <meta name="twitter:title" content="${escapeHtml(title)}" />
  <meta name="twitter:description" content="${escapeHtml(description)}" />
  <meta name="twitter:image" content="${baseUrl}/codego-mark.svg" />
  <style>
    ${publicSans ? `@font-face { font-family: 'Public Sans Variable'; font-style: normal; font-weight: 100 900; font-display: swap; src: url('/static/font/${publicSans}') format('woff2'); }` : ''}
    :root { color-scheme: light; ${lightTopicTokens} }
    * { box-sizing:border-box; }
    body { margin:0; background:var(--background); color:var(--foreground); font-family:'Public Sans Variable',system-ui,-apple-system,'PingFang TC','Microsoft YaHei',sans-serif; font-size:16px; line-height:1.75; caret-color:var(--primary); }
    a { color:inherit; text-underline-offset:5px; }
    a:focus-visible, main:focus-visible { outline:2px solid var(--primary); outline-offset:4px; }
    ::selection { background:var(--primary); color:var(--primary-foreground); }
    h1 { font-size:40px; line-height:1.25; font-weight:600; letter-spacing:-.025em; text-wrap:balance; }
    h2 { font-size:24px; line-height:1.35; font-weight:600; letter-spacing:-.02em; text-wrap:balance; }
    .skip-link { position:fixed; left:20px; top:-80px; z-index:50; background:var(--background); padding:12px; }
    .skip-link:focus { top:12px; }
    .topic-topbar { min-height:72px; border-bottom:1px solid var(--border); }
    .topic-topbar-inner { max-width:1280px; margin-inline:auto; padding:12px 48px; display:flex; justify-content:space-between; align-items:center; gap:20px; }
    .topic-brand { display:inline-flex; align-items:center; min-height:44px; gap:12px; font-weight:600; text-decoration:none; white-space:nowrap; }
    .topic-brand img { width:28px; height:28px; }
    .topic-topbar nav { display:flex; flex-wrap:wrap; gap:8px 24px; }
    .topic-topbar nav a { display:inline-flex; align-items:center; min-height:44px; text-decoration:none; }
    .topic-topbar a:hover { color:var(--primary); }
    ${topicStyles}
    @media (max-width:767px) { .topic-topbar-inner { padding:10px 20px; flex-wrap:wrap; gap:4px 20px; } .topic-topbar nav { gap:4px 20px; } }
    @media (prefers-color-scheme:dark) { :root { color-scheme:dark; ${darkTopicTokens} } }
    @media (prefers-reduced-motion:reduce) { *,*::before,*::after { scroll-behavior:auto !important; transition:none !important; animation:none !important; } }
  </style>
</head>
<body>
  <a class="skip-link" href="#content">跳转到主内容</a>
  <header class="topic-topbar"><div class="topic-topbar-inner">
    <a class="topic-brand" href="/"><img src="/codego-mark.svg" alt="" /><span>CodeGo</span></a>
    <nav aria-label="主导航"><a href="/">主页</a><a href="/pricing">模型</a><a href="/market">市场</a><a href="/docs">文档</a><a href="https://community.codegoai.com" target="_blank" rel="noopener noreferrer">社区</a><a href="/rankings">排行榜</a><a href="/dashboard/overview">控制台</a></nav>
  </div></header>
  <main id="content" tabindex="-1" class="market-reading topic-reading"><div class="topic-layout">${body}</div></main>
  <script type="application/ld+json">${JSON.stringify(jsonLd).replaceAll('<', '\\u003c')}</script>
</body>
</html>`)
}
function renderTopic(page) {
  const sections = getSearchPageSections(page)
  const faq = getSearchPageFaq(page)
  return pageFrame({
    title: titleFor(page), description: descriptionFor(page), keywords: page.keywords,
    canonicalPath: `/topics/${page.slug}`,
    jsonLd: [
      {
        '@context':'https://schema.org', '@type':'BreadcrumbList',
        itemListElement:[
          { '@type':'ListItem',position:1,name:'CodeGo',item:`${baseUrl}/` },
          { '@type':'ListItem',position:2,name:'专题目录',item:`${baseUrl}/topics` },
          { '@type':'ListItem',position:3,name:page.title,item:`${baseUrl}/topics/${page.slug}` },
        ],
      },
      {
        '@context':'https://schema.org', '@type':'TechArticle',headline:titleFor(page),description:descriptionFor(page),
        inLanguage:'zh-HK',mainEntityOfPage:`${baseUrl}/topics/${page.slug}`,keywords:page.keywords,
        author:{ '@type':'Organization',name:'Code Go' },publisher:{ '@type':'Organization',name:'Code Go' },
      },
      {
        '@context':'https://schema.org', '@type':'FAQPage',
        mainEntity:faq.map((item) => ({ '@type':'Question',name:item.question,acceptedAnswer:{ '@type':'Answer',text:item.answer } })),
      },
    ],
    body:`
      <aside class="topic-toc"><nav aria-label="页内导航"><h2>目录</h2><ul class="topic-link-list">
        ${sections.map((section,index) => `<li><a href="#section-${index+1}">${escapeHtml(section.heading)}</a></li>`).join('')}
        <li><a href="#faq">FAQ</a></li>
      </ul></nav></aside>
      <article class="topic-article">
        <header class="topic-heading"><h1>${escapeHtml(page.hero)}</h1>${entryLinks()}</header>
        ${sections.map((section,index) => `<section id="section-${index+1}"><h2>${escapeHtml(section.heading)}</h2>${section.paragraphs.map((paragraph) => `<p>${escapeHtml(paragraph)}</p>`).join('')}</section>`).join('')}
        <section id="faq"><h2>FAQ</h2><dl class="topic-faq">${faq.map((item) => `<div><dt>${escapeHtml(item.question)}</dt><dd>${escapeHtml(item.answer)}</dd></div>`).join('')}</dl></section>
      </article>
      <aside class="topic-related"><nav aria-label="相关专题"><h2>相关专题</h2>${topicLinks(searchPages.filter((item) => item.slug !== page.slug).slice(0,6))}</nav></aside>`,
  })
}
function renderIndex() {
  const title = 'Codex API、Claude Code API、Codex 中转、Claude 中转专题页 | Code Go'
  const description = 'Code Go 专题页汇总，覆盖 Codex API、Claude Code API、Codex 中转、Claude 中转、教程、配置、排障与模型选择。'
  const groups = searchPageGroups.map((group) => ({ ...group,pages:searchPages.filter((page) => group.match(page.slug)) }))
  return pageFrame({
    title,description,canonicalPath:'/topics',ogType:'website',
    keywords:'Codex API, Claude Code API, Codex中转, Claude中转, AI API 中转, 教程, 配置, 排障, Code Go',
    jsonLd:{
      '@context':'https://schema.org','@type':'CollectionPage',name:'专题目录',description,inLanguage:'zh-HK',url:`${baseUrl}/topics`,
      hasPart:searchPages.map((page) => ({ '@type':'TechArticle',name:page.title,url:`${baseUrl}/topics/${page.slug}` })),
    },
    body:`
      <aside class="topic-toc"><nav aria-label="页内导航"><h2>目录</h2><ul class="topic-link-list">${groups.map((group,index) => `<li><a href="#group-${index+1}">${escapeHtml(group.title)}</a></li>`).join('')}</ul></nav></aside>
      <article class="topic-article"><header class="topic-heading"><h1>专题目录</h1>${entryLinks()}</header>
        ${groups.map((group,index) => `<section id="group-${index+1}"><h2>${escapeHtml(group.title)}</h2>${topicLinks(group.pages)}</section>`).join('')}
      </article>
      <aside class="topic-related"><nav aria-label="热门专题"><h2>热门专题</h2>${topicLinks(searchPages.slice(0,4))}</nav></aside>`,
  })
}

await fs.mkdir(path.join(distDir, 'topics'), { recursive: true })
await fs.writeFile(path.join(distDir, 'topics/index.html'), renderIndex(), 'utf8')
for (const page of searchPages) {
  const targetDir = path.join(distDir, 'topics', page.slug)
  await fs.mkdir(targetDir, { recursive: true })
  await fs.writeFile(path.join(targetDir, 'index.html'), renderTopic(page), 'utf8')
}
console.log(`Generated ${searchPages.length + 1} static topic pages.`)