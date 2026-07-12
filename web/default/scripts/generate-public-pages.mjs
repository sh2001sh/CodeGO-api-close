import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const projectRoot = path.resolve(__dirname, '..')
const distDir = path.join(projectRoot, 'dist')

const { SITE_NAME, SITE_ORIGIN, publicPageSeoEntries } = await import(
  pathToFileUrl(path.join(projectRoot, 'src/lib/public-page-seo.ts')).href
)

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

function buildJsonLd(entry) {
  const canonical = `${SITE_ORIGIN}${entry.path}`
  return [
    {
      '@context': 'https://schema.org',
      '@type': 'BreadcrumbList',
      itemListElement: [
        { '@type': 'ListItem', position: 1, name: SITE_NAME, item: `${SITE_ORIGIN}/` },
        {
          '@type': 'ListItem',
          position: 2,
          name: entry.h1,
          item: canonical,
        },
      ],
    },
    {
      '@context': 'https://schema.org',
      '@type': 'WebPage',
      name: entry.title,
      headline: entry.h1,
      description: entry.description,
      inLanguage: 'zh-CN',
      url: canonical,
      isPartOf: {
        '@type': 'WebSite',
        name: SITE_NAME,
        url: `${SITE_ORIGIN}/`,
      },
    },
  ]
}

function getRelatedLinks(currentPath) {
  const relatedMap = {
    '/pricing': [
      { href: '/guide', label: '使用说明', description: '继续看 API Key、套餐、盲盒与配置路径。' },
      { href: '/topics', label: '专题目录', description: '按 Codex API、Claude Code API、中转和排障继续阅读。' },
      { href: '/download', label: '桌面端下载', description: '把网站账号、浏览器授权和本地工具配置连接起来。' },
    ],
    '/guide': [
      { href: '/pricing', label: '模型广场', description: '看免费模型、价格分组和当前可用模型。' },
      { href: '/faq', label: '常见问题', description: '先快速理解 Code Go 的产品定位与使用方式。' },
      { href: '/download', label: '桌面端下载', description: '继续完成浏览器授权、Token 导入和本地工具配置。' },
    ],
    '/faq': [
      { href: '/about', label: '关于 Code Go', description: '看平台为什么强调长期 AI Coding 和持续积累。' },
      { href: '/guide', label: '使用说明', description: '直接进入注册、密钥、脚本、套餐和钱包路径。' },
      { href: '/pricing', label: '模型广场', description: '继续看模型分组、免费模型和价格结构。' },
    ],
    '/about': [
      { href: '/faq', label: '常见问题', description: '更快理解平台适合谁、和普通平台有什么区别。' },
      { href: '/guide', label: '使用说明', description: '顺着真实使用流程继续往下看。' },
      { href: '/pricing', label: '模型广场', description: '查看公开模型、价格和长期使用入口。' },
    ],
    '/download': [
      { href: '/guide', label: '使用说明', description: '看安装完成后如何继续配置站点与本地工具。' },
      { href: '/faq', label: '常见问题', description: '处理账号、授权、Token 与工作流的常见问题。' },
      { href: '/pricing', label: '模型广场', description: '回到模型与价格页继续决定任务使用路径。' },
    ],
    '/privacy-policy': [
      { href: '/user-agreement', label: '用户协议', description: '查看账号、额度、套餐与模型调用规则。' },
      { href: '/about', label: '关于 Code Go', description: '理解站点定位、产品理念和支持入口。' },
      { href: '/faq', label: '常见问题', description: '从公开问答角度理解平台与工作流。' },
    ],
    '/user-agreement': [
      { href: '/privacy-policy', label: '隐私政策', description: '继续查看账号、调用记录和数据处理方式。' },
      { href: '/about', label: '关于 Code Go', description: '理解产品定位与长期 AI Coding 主线。' },
      { href: '/guide', label: '使用说明', description: '回到实际使用步骤和配置路径。' },
    ],
  }

  return relatedMap[currentPath] || []
}

function renderPage(entry) {
  const canonical = `${SITE_ORIGIN}${entry.path}`
  const jsonLd = JSON.stringify(buildJsonLd(entry)).replaceAll('<', '\\u003c')
  const relatedLinks = getRelatedLinks(entry.path)
  const sectionsMarkup = entry.sections
    .map(
      (section, index) => `<section class="section" id="section-${index + 1}">
  <div class="eyebrow">Section ${String(index + 1).padStart(2, '0')}</div>
  <h2>${escapeHtml(section.heading)}</h2>
  <div class="copy-stack">
    ${section.paragraphs.map((paragraph) => `<p>${escapeHtml(paragraph)}</p>`).join('')}
  </div>
</section>`,
    )
    .join('')

  const relatedMarkup = relatedLinks
    .map(
      (item) => `<a class="link-card" href="${item.href}">
  <strong>${escapeHtml(item.label)}</strong>
  <span>${escapeHtml(item.description)}</span>
</a>`,
    )
    .join('')

  return `<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <link rel="icon" type="image/svg+xml" href="/code-go-logo.svg?v=2" />
    <link rel="shortcut icon" type="image/svg+xml" href="/code-go-logo.svg?v=2" />
    <link rel="apple-touch-icon" href="/code-go-logo.svg?v=2" />
    <title>${escapeHtml(entry.title)}</title>
    <meta name="title" content="${escapeHtml(entry.title)}" />
    <meta name="description" content="${escapeHtml(entry.description)}" />
    <meta name="keywords" content="${escapeHtml(entry.keywords)}" />
    <meta name="robots" content="index,follow,max-image-preview:large" />
    <link rel="canonical" href="${canonical}" />
    <meta property="og:title" content="${escapeHtml(entry.title)}" />
    <meta property="og:description" content="${escapeHtml(entry.description)}" />
    <meta property="og:type" content="article" />
    <meta property="og:url" content="${canonical}" />
    <meta property="og:site_name" content="${SITE_NAME}" />
    <meta property="og:image" content="${SITE_ORIGIN}/code-go-logo.svg" />
    <meta name="twitter:card" content="summary_large_image" />
    <meta name="twitter:title" content="${escapeHtml(entry.title)}" />
    <meta name="twitter:description" content="${escapeHtml(entry.description)}" />
    <meta name="twitter:image" content="${SITE_ORIGIN}/code-go-logo.svg" />
    <style>
      :root {
        color-scheme: light;
        --bg: #f4f7fb;
        --surface: rgba(255, 255, 255, 0.86);
        --surface-strong: rgba(255, 255, 255, 0.96);
        --text: #18202b;
        --muted: #556375;
        --line: rgba(138, 155, 176, 0.24);
        --accent: #d96a39;
        --accent-soft: rgba(217, 106, 57, 0.12);
        --shadow: 0 18px 40px rgba(15, 20, 27, 0.08);
      }
      * { box-sizing: border-box; }
      body {
        margin: 0;
        font-family: "Public Sans", "Segoe UI", sans-serif;
        color: var(--text);
        background:
          radial-gradient(circle at top left, rgba(217, 106, 57, 0.14), transparent 26%),
          radial-gradient(circle at 88% 8%, rgba(62, 118, 210, 0.12), transparent 24%),
          linear-gradient(180deg, #f7f9fc 0%, #f4f7fb 38%, #edf2f8 100%);
      }
      a {
        color: inherit;
        text-decoration: none;
      }
      .topbar {
        position: sticky;
        top: 0;
        z-index: 20;
        border-bottom: 1px solid rgba(138, 155, 176, 0.18);
        background: rgba(244, 247, 251, 0.88);
        backdrop-filter: blur(14px);
      }
      .topbar-inner,
      .shell {
        width: min(1240px, calc(100% - 32px));
        margin: 0 auto;
      }
      .topbar-inner {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 18px;
        padding: 16px 0;
      }
      .brand {
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
        background: var(--surface-strong);
        box-shadow: 0 10px 24px rgba(15, 20, 27, 0.08);
      }
      .brand-mark img {
        width: 28px;
        height: 28px;
      }
      .brand-copy {
        display: flex;
        flex-direction: column;
        gap: 2px;
      }
      .brand-copy strong {
        font-size: 15px;
      }
      .brand-copy span,
      .top-links a {
        color: var(--muted);
        font-size: 13px;
      }
      .top-links {
        display: flex;
        flex-wrap: wrap;
        gap: 10px;
      }
      .top-links a {
        padding: 8px 12px;
        border-radius: 999px;
      }
      .top-links a:hover {
        background: rgba(255, 255, 255, 0.7);
      }
      .shell {
        display: grid;
        grid-template-columns: minmax(0, 1.6fr) minmax(280px, 0.8fr);
        gap: 20px;
        padding: 24px 0 72px;
      }
      .hero,
      .section,
      .rail-card {
        border: 1px solid var(--line);
        background: var(--surface);
        backdrop-filter: blur(14px);
        border-radius: 24px;
        box-shadow: var(--shadow);
      }
      .hero,
      .section {
        padding: 28px;
      }
      .hero {
        padding-top: 34px;
      }
      .eyebrow {
        display: inline-flex;
        align-items: center;
        border-radius: 999px;
        border: 1px solid var(--line);
        background: var(--surface-strong);
        padding: 8px 14px;
        font-size: 12px;
        font-weight: 700;
        color: #455468;
      }
      h1 {
        margin: 18px 0 0;
        max-width: 18ch;
        font-size: clamp(2.5rem, 5vw, 4.3rem);
        line-height: 1.04;
        letter-spacing: -0.04em;
      }
      h2 {
        margin: 14px 0 0;
        font-size: 1.8rem;
        line-height: 1.2;
        letter-spacing: -0.02em;
      }
      p {
        margin: 0;
        max-width: 74ch;
        color: var(--muted);
        font-size: 15px;
        line-height: 1.95;
      }
      .hero p {
        margin-top: 18px;
      }
      .copy-stack {
        display: flex;
        flex-direction: column;
        gap: 16px;
        margin-top: 18px;
      }
      .content-stack,
      .rail {
        display: flex;
        flex-direction: column;
        gap: 20px;
      }
      .rail-card {
        padding: 20px;
      }
      .link-list {
        display: flex;
        flex-direction: column;
        gap: 12px;
        margin-top: 16px;
      }
      .link-card {
        display: block;
        border: 1px solid var(--line);
        background: var(--surface-strong);
        border-radius: 18px;
        padding: 16px;
        transition: transform 0.2s ease, border-color 0.2s ease;
      }
      .link-card:hover {
        transform: translateY(-1px);
        border-color: rgba(217, 106, 57, 0.28);
      }
      .link-card strong {
        display: block;
        font-size: 14px;
      }
      .link-card span {
        display: block;
        margin-top: 6px;
        color: var(--muted);
        font-size: 13px;
        line-height: 1.7;
      }
      .pill-row {
        display: flex;
        flex-wrap: wrap;
        gap: 10px;
        margin-top: 22px;
      }
      .pill {
        display: inline-flex;
        align-items: center;
        border-radius: 999px;
        border: 1px solid var(--line);
        background: var(--surface-strong);
        padding: 10px 14px;
        font-size: 13px;
        font-weight: 600;
      }
      .keyword-box {
        border: 1px solid var(--line);
        background: linear-gradient(180deg, rgba(255,255,255,0.94), rgba(248,250,252,0.9));
        border-radius: 18px;
        padding: 16px;
      }
      .keyword-box strong {
        display: block;
        margin-bottom: 8px;
        font-size: 14px;
      }
      .keyword-box p {
        font-size: 13px;
        line-height: 1.8;
      }
      @media (max-width: 920px) {
        .shell {
          grid-template-columns: 1fr;
        }
      }
      @media (max-width: 720px) {
        .topbar-inner,
        .shell {
          width: min(100% - 24px, 1240px);
        }
        .topbar-inner {
          flex-direction: column;
          align-items: flex-start;
        }
        .hero,
        .section,
        .rail-card {
          padding: 20px;
          border-radius: 20px;
        }
      }
    </style>
  </head>
  <body>
    <header class="topbar">
      <div class="topbar-inner">
        <a class="brand" href="/">
          <span class="brand-mark">
            <img src="/code-go-logo.svg" alt="Code Go" />
          </span>
          <span class="brand-copy">
            <strong>${SITE_NAME}</strong>
            <span>Public SEO Page / 公开页面静态入口</span>
          </span>
        </a>
        <nav class="top-links" aria-label="Primary">
          <a href="/">首页</a>
          <a href="/pricing">模型广场</a>
          <a href="/guide">使用说明</a>
          <a href="/topics">专题目录</a>
        </nav>
      </div>
    </header>
    <main class="shell">
      <div class="content-stack">
        <section class="hero">
          <div class="eyebrow">${escapeHtml(entry.eyebrow)}</div>
          <h1>${escapeHtml(entry.h1)}</h1>
          <p>${escapeHtml(entry.intro)}</p>
          <div class="pill-row">
            <a class="pill" href="/pricing">查看模型</a>
            <a class="pill" href="/guide">阅读说明</a>
            <a class="pill" href="/topics">浏览专题页</a>
          </div>
        </section>
        ${sectionsMarkup}
      </div>
      <aside class="rail">
        <section class="rail-card">
          <div class="eyebrow">Meta</div>
          <h2>为什么这页要静态输出</h2>
          <div class="copy-stack">
            <p>这页在构建阶段就会生成独立 HTML，因此搜索引擎抓取时可以直接看到唯一标题、描述、H1 和正文，而不是只拿到首页的通用 head。</p>
            <p>这样更适合修复 Bing 报告里的短标题、缺 description、重复标题和内容不足问题。</p>
          </div>
        </section>
        <section class="rail-card">
          <div class="eyebrow">Keywords</div>
          <div class="keyword-box">
            <strong>本页关键词</strong>
            <p>${escapeHtml(entry.keywords)}</p>
          </div>
        </section>
        <section class="rail-card">
          <div class="eyebrow">Next</div>
          <h2>继续往哪里看</h2>
          <div class="link-list">${relatedMarkup}</div>
        </section>
      </aside>
    </main>
    <script type="application/ld+json">${jsonLd}</script>
  </body>
</html>`
}

function renderAliasPage() {
  const canonical = `${SITE_ORIGIN}/`

  return `<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Code Go 首页别名 | 跳转到主页</title>
    <meta name="robots" content="noindex,follow" />
    <link rel="canonical" href="${canonical}" />
    <meta http-equiv="refresh" content="0; url=/" />
  </head>
  <body>
    <main>
      <h1>Code Go 首页别名</h1>
      <p>这个地址是历史别名，已跳转到主页。</p>
      <p><a href="/">前往 Code Go 首页</a></p>
    </main>
  </body>
</html>`
}

for (const entry of publicPageSeoEntries) {
  const targetDir = path.join(distDir, entry.path.replace(/^\//, ''))
  await fs.mkdir(targetDir, { recursive: true })
  await fs.writeFile(path.join(targetDir, 'index.html'), renderPage(entry), 'utf8')
}

const brandAliasDir = path.join(distDir, 'brand')
await fs.mkdir(brandAliasDir, { recursive: true })
await fs.writeFile(path.join(brandAliasDir, 'index.html'), renderAliasPage(), 'utf8')

console.log(
  `Generated ${publicPageSeoEntries.length} static public pages and 1 alias page.`,
)
