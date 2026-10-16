/* Copyright (C) 2023-2026 QuantumNous. AGPL-3.0-or-later. */
// Targeted production UI verification with synthetic read-only API data.
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const output = path.resolve(
  root,
  '../../output/playwright/concurrency-docs/frontend'
)
const configPath = path.join(output, 'config.json')
const base = process.env.STRESS_QA_BASE || 'http://127.0.0.1:3099'
if (new URL(base).hostname !== '127.0.0.1')
  throw new Error('QA browser requires loopback fixture')
const health = await (await fetch(base + '/__stress/health')).json()
const status = (await (await fetch(base + '/api/status')).json()).data
const cache = path.join(
  process.env.LOCALAPPDATA || path.join(os.homedir(), 'AppData/Local'),
  'npm-cache/_npx'
)
let runtime
for (const directory of await fs.readdir(cache)) {
  const filename = path.join(
    cache,
    directory,
    'node_modules/playwright/index.mjs'
  )
  try {
    await fs.access(filename)
    runtime = await import(pathToFileURL(filename).href)
    break
  } catch {
    /* next installed runtime */
  }
}
if (!runtime) throw new Error('No installed Playwright runtime')
const browser = await runtime.chromium.launch({
  headless: true,
  channel: 'chrome',
})
const resultsPath = path.join(output, 'browser-results.json')
const results =
  process.env.STRESS_RESUME === 'true'
    ? JSON.parse(await fs.readFile(resultsPath, 'utf8'))
    : []
const user = {
  id: 900001,
  username: 'solaris_fixture_admin',
  display_name: 'Synthetic stress QA',
  email: 'fixture@example.invalid',
  role: 100,
  status: 1,
  group: 'default',
  quota: 125000000,
  setting: '{}',
  permissions: { sidebar_settings: true },
}
const check = (condition, message) => {
  if (!condition) throw new Error(message)
}
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))
async function until(predicate, message, timeout = 15000) {
  const started = performance.now()
  while (performance.now() - started < timeout) {
    if (await predicate()) return
    await sleep(100)
  }
  throw new Error(message)
}
async function waitRows(page, selector, expected, message) {
  await until(
    async () => (await page.locator(selector).count()) === expected,
    message
  )
}
const internalFooterPaths = [
  '/',
  '/pricing',
  '/market',
  '/rankings',
  '/packages',
  '/dashboard/overview',
  '/docs',
  '/keys',
  '/playground',
  '/group-status',
  '/about',
  '/privacy-policy',
  '/user-agreement',
]
const socialURLs = [
  'https://community.codegoai.com',
  'https://x.com/9U58nYrVzG38231',
  'https://discord.gg/EWEWtZ23',
]

async function footer(page, row) {
  const surface = page.locator('footer.site-footer')
  await surface.waitFor()
  check(
    (await surface.locator('nav.site-footer-group').count()) === 4,
    'Footer navigation groups missing'
  )
  const anchors = await surface.locator('a').evaluateAll((nodes) =>
    nodes.map((node) => ({
      text: node.textContent.trim(),
      href: node.getAttribute('href'),
      target: node.target,
      rel: node.rel,
    }))
  )
  for (const href of internalFooterPaths)
    check(
      anchors.some((anchor) => new URL(anchor.href, base).pathname === href),
      `Footer missing ${href}`
    )
  for (const href of socialURLs) {
    const anchor = anchors.find((item) => item.href === href)
    check(
      anchor &&
        anchor.target === '_blank' &&
        anchor.rel.includes('noopener') &&
        anchor.rel.includes('noreferrer'),
      `Footer external-link contract failed: ${href}`
    )
  }
  check(
    anchors.some((anchor) => anchor.href === '/docs?article=quickstart') &&
      anchors.some((anchor) => anchor.href === '/docs?article=billing'),
    'Footer quickstart or billing documentation link missing'
  )
  check(
    (await surface.innerText()).includes('81318858'),
    'Business registration identity missing'
  )
  const geometry = await surface.evaluate((node) => ({
    columns: getComputedStyle(
      node.querySelector('.site-footer-directory')
    ).gridTemplateColumns.split(' ').length,
    background: getComputedStyle(node).backgroundColor,
    color: getComputedStyle(node).color,
    clipped: [...node.querySelectorAll('a')].filter((anchor) => {
      const r = anchor.getBoundingClientRect()
      return r.left < -1 || r.right > innerWidth + 1
    }).length,
  }))
  check(geometry.clipped === 0, 'Footer link clipped outside viewport')
  check(
    geometry.columns === (row.width === 390 ? 2 : 5),
    'Footer column layout differs from intended breakpoint'
  )
  await surface.locator('a').first().focus()
  await page.keyboard.press('Tab')
  const focus = await page.evaluate(() => ({
    href: document.activeElement?.getAttribute('href'),
    outline: getComputedStyle(document.activeElement).outlineStyle,
    outlineWidth: getComputedStyle(document.activeElement).outlineWidth,
  }))
  check(
    new URL(focus.href, base).pathname === '/pricing' &&
      focus.outline !== 'none' &&
      Number.parseFloat(focus.outlineWidth) >= 2,
    'Footer keyboard order or visible focus failed'
  )
  row.checks.push({
    name: 'footer links/theme/layout/keyboard',
    anchors,
    geometry,
    focus,
  })
  row.footerScreenshot = `footer-${row.width}-${row.theme}.png`
  await surface.evaluate((node) =>
    node.scrollIntoView({ behavior: 'instant', block: 'start' })
  )
  await page.evaluate(async () => {
    await document.fonts.ready
    await new Promise((resolve) =>
      requestAnimationFrame(() => requestAnimationFrame(resolve))
    )
  })
  await sleep(150)
  await surface.screenshot({
    path: path.join(output, row.footerScreenshot),
    animations: 'disabled',
  })
}

async function docs(page, row) {
  const article = page.locator('.docs-article')
  await article.waitFor()
  const titles = {
    concurrency: {
      en: 'Concurrency, queues and retries',
      'zh-TW': '並行、排隊與重試',
    },
    'responses-streaming': {
      en: 'Responses streaming and events',
      'zh-TW': 'Responses 串流與事件',
    },
  }
  const slug =
    new URL(row.route, base).searchParams.get('article') || 'quickstart'
  const expected = titles[slug]?.[row.language]
  if (expected)
    check(
      (await article.locator('h1').innerText()) === expected,
      'Article language or content missing'
    )
  const language = await article.getAttribute('lang')
  check(
    language === (row.language === 'en' ? 'en' : 'zh-HK'),
    'Wrong document content language'
  )
  if (row.width === 390) {
    await page.locator('.docs-mobile-toggle').click()
    check(
      (await page
        .locator('.docs-mobile-toggle')
        .getAttribute('aria-expanded')) === 'true',
      'Mobile docs directory did not open'
    )
  }
  const initialArticles = await page.locator('.docs-navigation-group a').count()
  check(initialArticles > 2, 'Docs navigation missing article index')
  const search = page.locator('.docs-search input')
  await search.fill('no-such-codego-document-unique')
  await waitRows(
    page,
    '.docs-navigation-group a',
    0,
    'Documentation no-match search failed'
  )
  check(
    await page.locator('.docs-no-results').isVisible(),
    'Documentation empty search state absent'
  )
  await search.fill('')
  await waitRows(
    page,
    '.docs-navigation-group a',
    initialArticles,
    'Documentation clearing search did not restore full article index'
  )
  if (row.width === 390) await page.locator('.docs-mobile-toggle').click()
  const outline = page.locator(
    row.width === 390 ? '.docs-mobile-outline' : '.docs-outline'
  )
  if (row.width === 390) await outline.locator('summary').click()
  const anchor = outline.locator('a').last()
  const target = new URL(await anchor.getAttribute('href'), base).hash
  await anchor.click()
  await until(
    async () => new URL(page.url()).hash === target,
    'Documentation anchor URL failed'
  )
  await until(
    async () =>
      await outline
        .locator('a[aria-current="location"]')
        .last()
        .getAttribute('href')
        .then((href) => new URL(href, base).hash === target)
        .catch(() => false),
    'Documentation active outline anchor not synchronized'
  )
  const targetTop = await page
    .locator(`[id="${decodeURIComponent(target.slice(1))}"]`)
    .evaluate((node) => node.getBoundingClientRect().top)
  check(
    targetTop >= 0 && targetTop < (row.width === 390 ? 844 : 900),
    'Documentation anchor target is outside viewport'
  )
  const blocks = page.locator('.code-block')
  if (await blocks.count()) {
    const first = blocks.first()
    const code = await first.locator('pre code').innerText()
    await first.locator('.docs-code-toolbar button').click()
    let copied
    await until(async () => {
      copied = await page.evaluate(() => navigator.clipboard.readText())
      return copied.replaceAll('\r\n', '\n') === code.replaceAll('\r\n', '\n')
    }, 'Copy example differs from rendered code after platform newline normalization')
    const sdk = page.locator('.docs-code-tabs').first()
    await sdk.getByRole('button', { name: 'Python', exact: true }).click()
    check(
      (await sdk
        .getByRole('button', { name: 'Python', exact: true })
        .getAttribute('aria-pressed')) === 'true',
      'SDK Python tab failed'
    )
    await sdk.getByRole('button', { name: 'Node.js', exact: true }).click()
    check(
      (await sdk
        .getByRole('button', { name: 'Node.js', exact: true })
        .getAttribute('aria-pressed')) === 'true',
      'SDK Node tab failed'
    )
    const model = page.locator('input[list="docs-models"]').first()
    if (await model.count()) {
      await model.fill('stress-049999')
      await waitRows(
        page,
        '#docs-models option',
        1,
        'Docs model suggestions cannot reach tail of full catalogue'
      )
      await model.fill('stress-')
      await until(async () => {
        const size = await page.locator('#docs-models option').count()
        return size > 0 && size <= 50
      }, 'Docs datalist exceeds 50 suggestions')
    }
    const pre = first.locator('pre')
    const scrolling = await pre.evaluate((node) => ({
      scrollWidth: node.scrollWidth,
      clientWidth: node.clientWidth,
      overflow: getComputedStyle(node).overflowX,
    }))
    if (scrolling.scrollWidth > scrolling.clientWidth + 2) {
      await pre.hover()
      await page.mouse.wheel(500, 0)
      await until(
        async () => await pre.evaluate((node) => node.scrollLeft > 0),
        'Long code cannot scroll horizontally'
      )
    }
    row.checks.push({
      name: 'code copy/SDK/model suggestions/horizontal scroll',
      codeLength: code.length,
      clipboardLength: copied.length,
      clipboardComparison: 'Exact text after Windows CRLF to LF normalization',
      codeBlocks: await blocks.count(),
      suggestions: await page.locator('#docs-models option').count(),
      scrolling,
    })
  }
  check(
    (await page.locator('footer.site-footer').count()) === 1,
    'Docs shared footer missing or duplicated'
  )
  row.checks.push({
    name: 'docs language/search/mobile navigation/anchors',
    slug,
    language,
    articles: initialArticles,
    sections: await article.locator('.docs-article-section').count(),
    target,
    targetTop,
  })
  if (slug === 'concurrency') {
    const before = row.requestStarts.filter((item) =>
      item.startsWith('/api/pricing')
    ).length
    check(
      before === 0,
      'Text-only concurrency documentation requested full model catalogue'
    )
    const navigate = async (articleSlug) => {
      if (row.width === 390) await page.locator('.docs-mobile-toggle').click()
      await page
        .locator(
          `.docs-navigation-group a[href="/docs?article=${articleSlug}"]`
        )
        .click()
      await until(
        async () =>
          new URL(page.url()).searchParams.get('article') === articleSlug,
        'Documentation same-page navigation failed'
      )
    }
    await navigate('quickstart')
    await page.locator('input[list="docs-models"]').first().waitFor()
    await until(
      async () =>
        row.requestStarts.some((item) => item.startsWith('/api/pricing')),
      'Quickstart did not request needed model catalogue'
    )
    await page
      .locator('input[list="docs-models"]')
      .first()
      .fill('synthetic-custom-model')
    await navigate('concurrency')
    check(
      (await page.locator('#docs-models').count()) === 0,
      'Text-only documentation mounted model datalist'
    )
    await navigate('quickstart')
    check(
      (await page.locator('input[list="docs-models"]').first().inputValue()) ===
        'synthetic-custom-model',
      'Documentation model input reset during article navigation'
    )
    row.checks.push({
      name: 'docs catalogue requested only when needed',
      textOnlyRequests: before,
      quickstartRequests: row.requestStarts.filter((item) =>
        item.startsWith('/api/pricing')
      ).length,
      preservedModelInput: true,
    })
    await navigate('concurrency')
    await page.locator('.docs-article h1').waitFor()
  }
}

async function invoices(page, row) {
  await waitRows(
    page,
    '.commerce-invoice-order',
    50,
    'Invoice page did not mount 50 records'
  )
  const jump = page.locator('.commerce-invoice-pagination input[type="number"]')
  await jump.fill('1000')
  await until(
    async () =>
      (await page.locator('.commerce-invoice-list').innerText()).includes(
        'STRESS-ELIGIBLE-049999'
      ),
    'Invoice final server page unavailable'
  )
  const search = page.locator('#invoice-order-search')
  await search.fill('STRESS-ELIGIBLE-049999')
  await waitRows(
    page,
    '.commerce-invoice-order',
    1,
    'Invoice global server search failed'
  )
  await search.fill('no-such-codego-invoice-unique')
  await waitRows(
    page,
    '.commerce-invoice-order',
    0,
    'Invoice server search empty state failed'
  )
  await search.fill('STRESS-ELIGIBLE-000001')
  await sleep(280)
  await search.fill('STRESS-ELIGIBLE-049999')
  await until(
    async () =>
      (await page.locator('.commerce-invoice-order').count()) === 1 &&
      (await page.locator('.commerce-invoice-order').innerText()).includes(
        'STRESS-ELIGIBLE-049999'
      ),
    'Rapid invoice search left stale page data'
  )
  await search.fill('')
  await waitRows(
    page,
    '.commerce-invoice-order',
    50,
    'Invoice clear did not restore first server page'
  )
  check(
    (await jump.inputValue()) === '1',
    'Invoice search did not reset to first page'
  )
  await until(
    async () =>
      row.requests.filter(
        (request) => request.endpoint === '/api/invoices/eligible-orders'
      ).length >= 5,
    'Invoice API evidence missing'
  )
  const requests = row.requests.filter(
    (request) => request.endpoint === '/api/invoices/eligible-orders'
  )
  check(
    requests.every(
      (request) =>
        !request.captureError &&
        request.rows <= 50 &&
        new URLSearchParams(request.query).get('page_size') === '50'
    ),
    'Invoice used unbounded API response or legacy array endpoint'
  )
  check(
    requests.some(
      (request) =>
        new URLSearchParams(request.query).get('p') === '1000' &&
        request.rows === 50
    ),
    'Invoice final page API not observed'
  )
  check(
    requests.some((request) => request.total === 1 && request.rows === 1),
    'Invoice search did not filter server total'
  )
  check(
    requests.some((request) => request.total === 0 && request.rows === 0),
    'Invoice empty server page not observed'
  )
  row.checks.push({
    name: 'invoice server50/last/global search/empty/debounce/clear',
    maxResponseRows: Math.max(...requests.map((request) => request.rows)),
    maxResponseBytes: Math.max(...requests.map((request) => request.bytes)),
    requests: requests.length,
    crossPageSelection:
      'No multi-order selection exists; bound order actions unchanged and never invoked',
  })
}

async function owner(page, row) {
  await waitRows(
    page,
    '.owner-channel-record',
    50,
    'Owned channel page did not mount 50 records'
  )
  await page
    .locator('.owner-channel-pagination input[type="number"]')
    .fill('1000')
  await until(
    async () =>
      (await page.locator('.owner-channel-record').allInnerTexts()).some(
        (text) => text.includes('049999')
      ),
    'Owner final page missing hydrated detail'
  )
  const search = page.locator('.owner-channel-filters input')
  await search.fill('049999')
  await waitRows(
    page,
    '.owner-channel-record',
    1,
    'Owner compact index global search failed'
  )
  await search.fill('no-such-codego-channel-unique')
  await waitRows(page, '.owner-channel-record', 0, 'Owner empty filter failed')
  await search.fill('')
  await waitRows(page, '.owner-channel-record', 50, 'Owner clear failed')
  await search.fill('gpt-4o')
  await waitRows(
    page,
    '.owner-channel-record',
    50,
    'Owner compact index model search failed'
  )
  check(
    (await page
      .locator('.owner-channel-pagination input[type="number"]')
      .getAttribute('max')) === '1000',
    'Owner model search did not retain full index matches'
  )
  await search.fill('')
  await page.locator('.owner-channel-filters select').selectOption('deleted')
  await waitRows(
    page,
    '.owner-channel-record',
    50,
    'Owner deleted-history filter failed'
  )
  check(
    !(await page.locator('.owner-channel-pagination').count()),
    'Deleted-history filter did not narrow compact index to 50 records'
  )
  await until(async () => {
    const deletedNames = await page
      .locator('.owner-channel-record')
      .allInnerTexts()
    return (
      deletedNames.length === 50 &&
      deletedNames.every((text, position) => {
        const index = position * 1000
        return text.includes(
          index % 29 === 0
            ? `STRESS-${index} ·`
            : `STRESS ${String(index).padStart(6, '0')}`
        )
      })
    )
  }, 'Deleted-history hydrated details do not match compact index')
  await page.locator('.owner-channel-filters select').selectOption('')
  await waitRows(page, '.owner-channel-record', 50, 'Owner status clear failed')
  await until(
    async () =>
      row.requests.some(
        (request) =>
          request.endpoint === '/api/marketplace/channels/mine' &&
          request.query.includes('view=index')
      ),
    'Owner compact index API missing'
  )
  const owned = row.requests.filter(
    (request) => request.endpoint === '/api/marketplace/channels/mine'
  )
  check(
    owned.every(
      (request) =>
        request.query &&
        (new URLSearchParams(request.query).get('view') === 'index' ||
          new URLSearchParams(request.query).getAll('ids').length > 0)
    ),
    'Owner requested legacy full detail array'
  )
  const details = owned.filter(
    (request) => new URLSearchParams(request.query).getAll('ids').length > 0
  )
  check(
    details.length >= 2 &&
      details.every(
        (request) =>
          request.rows <= 50 &&
          new URLSearchParams(request.query).getAll('ids').length <= 50
      ),
    'Owner detail request exceeds one page'
  )
  row.checks.push({
    name: 'owner compact index + 50 selected details/last/name/model/deleted/empty/clear',
    indexBytes: owned.find((request) => request.query.includes('view=index'))
      .bytes,
    detailRequests: details.length,
    maxDetailRows: Math.max(...details.map((request) => request.rows)),
    maxDetailBytes: Math.max(...details.map((request) => request.bytes)),
  })
}

const cases = []
for (const width of [1440, 390])
  for (const theme of ['light', 'dark'])
    cases.push({
      kind: 'footer',
      route: '/',
      width,
      theme,
      language: 'zh-TW',
      run: footer,
    })
for (const route of [
  '/docs?article=concurrency',
  '/docs?article=responses-streaming',
])
  for (const language of ['zh-TW', 'en'])
    for (const width of [1440, 390])
      cases.push({
        kind: 'docs',
        route,
        width,
        language,
        theme: 'light',
        run: docs,
      })
for (const language of ['zh-TW', 'en'])
  cases.push({
    kind: 'docs',
    route: '/docs?article=quickstart',
    width: 390,
    language,
    theme: 'light',
    run: docs,
  })
for (const route of ['/invoices', '/wallet?section=invoices'])
  for (const width of [1440, 390])
    cases.push({
      kind: 'invoice',
      route,
      width,
      language: 'zh-TW',
      theme: 'light',
      run: invoices,
    })
for (const width of [1440, 390])
  cases.push({
    kind: 'owner',
    route: '/channel-workbench',
    width,
    language: 'zh-TW',
    theme: 'light',
    run: owner,
  })
try {
  await fs.writeFile(
    configPath,
    JSON.stringify({ scale: 50000, mode: 'normal', delayMs: 0, target: '*' })
  )
  for (const specification of cases) {
    if (
      process.env.STRESS_TARGETS &&
      !process.env.STRESS_TARGETS.split(',').includes(specification.kind)
    )
      continue
    const { run, ...sample } = specification
    const row = {
      ...sample,
      fixtureFrontend: health.frontend,
      synthetic: true,
      checks: [],
      requestStarts: [],
      requests: [],
      errors: [],
    }
    const context = await browser.newContext({
      viewport: { width: row.width, height: row.width === 390 ? 844 : 900 },
      locale: row.language,
      colorScheme: row.theme,
      reducedMotion: 'reduce',
      permissions: ['clipboard-read', 'clipboard-write'],
    })
    await context.addCookies([
      {
        name: 'solaris_fixture_session',
        value: 'synthetic',
        domain: '127.0.0.1',
        path: '/',
      },
      {
        name: 'vite-ui-theme',
        value: row.theme,
        domain: '127.0.0.1',
        path: '/',
      },
    ])
    await context.addInitScript(
      ({ user, status, language }) => {
        localStorage.setItem('user', JSON.stringify(user))
        localStorage.setItem('uid', String(user.id))
        localStorage.setItem('status', JSON.stringify(status))
        localStorage.setItem('i18nextLng', language)
        localStorage.setItem('setup_status_checked', 'true')
      },
      { user, status, language: row.language }
    )
    const page = await context.newPage()
    page.setDefaultTimeout(15000)
    const cdp = await context.newCDPSession(page)
    await cdp.send('Network.enable', {
      maxTotalBufferSize: 128 * 1024 * 1024,
      maxResourceBufferSize: 64 * 1024 * 1024,
    })
    const jobs = []
    page.on('pageerror', (error) => row.errors.push(String(error)))
    page.on('request', (request) => {
      const url = new URL(request.url())
      if (url.pathname.startsWith('/api/'))
        row.requestStarts.push(url.pathname + url.search)
    })
    page.on('response', (response) => {
      if (!new URL(response.url()).pathname.startsWith('/api/')) return
      jobs.push(
        (async () => {
          const item = {
            endpoint: new URL(response.url()).pathname,
            query: new URL(response.url()).search,
            method: response.request().method(),
            status: response.status(),
          }
          try {
            const headers = await response.allHeaders()
            if (
              Number(headers['content-length']) > 1024 * 1024 &&
              headers['x-stress-scale']
            ) {
              item.bytes = Number(headers['content-length'])
              item.rows = Number(headers['x-stress-rows'])
              item.total = headers['x-stress-total']
                ? Number(headers['x-stress-total'])
                : undefined
              item.success = headers['x-stress-success'] === 'true'
              item.measurement =
                'synthetic proxy response headers; browser body omitted to avoid inspector eviction'
              row.requests.push(item)
              return
            }
            const body = await response.body()
            const json = JSON.parse(body.toString())
            item.bytes = body.length
            item.rows = Array.isArray(json.data)
              ? json.data.length
              : json.data?.items?.length
            item.total = json.data?.total
            item.success = json.success
          } catch (error) {
            item.captureError = String(error)
          }
          row.requests.push(item)
        })()
      )
    })
    const started = performance.now()
    try {
      await page.goto(base + row.route, {
        waitUntil: 'domcontentloaded',
        timeout: 45000,
      })
      await page.waitForFunction(
        () => document.querySelector('#root main')?.children.length,
        undefined,
        { timeout: 30000 }
      )
      await page.waitForFunction(
        (theme) => document.documentElement.classList.contains(theme),
        row.theme
      )
      if (row.kind === 'docs') {
        await page.locator('.docs-article h1').waitFor()
        await page.evaluate(async () => {
          window.scrollTo({ top: 0, behavior: 'instant' })
          await document.fonts.ready
          await new Promise((resolve) =>
            requestAnimationFrame(() => requestAnimationFrame(resolve))
          )
        })
        row.topScreenshot = `docs-top-${row.width}-${row.language}-${new URL(row.route, base).searchParams.get('article')}.png`
        await page.screenshot({
          path: path.join(output, row.topScreenshot),
          animations: 'disabled',
        })
      }
      await run(page, row)
      await Promise.allSettled(jobs)
      check(row.errors.length === 0, 'Browser runtime error')
      check(
        !row.requests.some(
          (request) =>
            request.method !== 'GET' &&
            !(
              request.method === 'POST' &&
              request.endpoint === '/api/user/amount'
            )
        ),
        'Unexpected business write request'
      )
      check(
        !row.requests.some(
          (request) => request.status >= 400 || request.success === false
        ),
        'Unexpected failed API request'
      )
      const bounds = await page.evaluate(() => ({
        scrollWidth: document.documentElement.scrollWidth,
        viewport: innerWidth,
        dark: document.documentElement.classList.contains('dark'),
      }))
      check(
        bounds.scrollWidth <= bounds.viewport + 2,
        'Page has horizontal overflow'
      )
      row.bounds = bounds
      row.pass = true
    } catch (error) {
      row.pass = false
      row.error = String(error)
    }
    row.observedMs = Number((performance.now() - started).toFixed(2))
    row.domNodes = await page.locator('*').count()
    row.screenshot = `${row.kind}-${row.width}-${row.theme}-${row.language}-${new URL(row.route, base).searchParams.get('article') || new URL(row.route, base).pathname.replaceAll('/', '-') || 'home'}.png`
    await page
      .screenshot({
        path: path.join(output, row.screenshot),
        animations: 'disabled',
      })
      .catch((error) => {
        row.screenshotError = String(error)
      })
    const previous = results.findIndex(
      (item) =>
        item.kind === row.kind &&
        item.route === row.route &&
        item.width === row.width &&
        item.theme === row.theme &&
        item.language === row.language
    )
    if (previous >= 0) results.splice(previous, 1)
    results.push(row)
    await fs.writeFile(resultsPath, JSON.stringify(results, null, 2))
    process.stdout.write(
      JSON.stringify({
        kind: row.kind,
        route: row.route,
        width: row.width,
        theme: row.theme,
        language: row.language,
        pass: row.pass,
        error: row.error,
      }) + '\n'
    )
    await context.close()
  }
} finally {
  await browser.close()
  await fs.writeFile(
    configPath,
    JSON.stringify({ scale: 1000, mode: 'normal', delayMs: 0, target: '*' })
  )
}
