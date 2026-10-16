/*
Copyright (C) 2023-2026 QuantumNous
SPDX-License-Identifier: AGPL-3.0-or-later
*/
// Independent headless Chromium; never attaches to the user's browser.
// STRESS_PHASE=before|sweep|high-risk|states; STRESS_LABEL=before|after
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { pathToFileURL, fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const output = path.resolve(root, '../../output/playwright/page-stress')
const configPath = path.join(output, 'config.json')
const base = 'http://127.0.0.1:3097'
const phase = process.env.STRESS_PHASE || 'before'
const label = (process.env.STRESS_LABEL || phase).replace(/[^a-z0-9-]/gi, '-')
const matrix = JSON.parse(
  await fs.readFile(path.join(output, 'route-matrix.json'), 'utf8')
).routes
async function playwright() {
  const cache = path.join(
    process.env.LOCALAPPDATA || path.join(os.homedir(), 'AppData/Local'),
    'npm-cache/_npx'
  )
  for (const directory of await fs.readdir(cache)) {
    const filename = path.join(
      cache,
      directory,
      'node_modules/playwright/index.mjs'
    )
    try {
      await fs.access(filename)
      return import(pathToFileURL(filename).href)
    } catch {
      /* next installed CLI */
    }
  }
  throw new Error('No cached Playwright CLI runtime found')
}
const { chromium } = await playwright()
let browser
try {
  browser = await chromium.launch({
    headless: true,
    args: ['--enable-precise-memory-info'],
  })
} catch {
  browser = await chromium.launch({
    headless: true,
    channel: 'chrome',
    args: ['--enable-precise-memory-info'],
  })
}
const user = {
  id: 900001,
  username: 'solaris_fixture_admin',
  display_name: 'Synthetic stress QA',
  email: 'fixture@example.invalid',
  role: 100,
  status: 1,
  group: 'default',
  quota: 125000000,
  used_quota: 2450000,
  request_count: 1248,
  setting: '{}',
  permissions: { sidebar_settings: true },
}
const status = (await (await fetch(base + '/api/status')).json()).data
const fixtureFrontend = (await (await fetch(base + '/__stress/health')).json())
  .frontend
const results =
  process.env.STRESS_RESUME === 'true'
    ? JSON.parse(
        await fs.readFile(
          path.join(output, `${label}-browser-results.json`),
          'utf8'
        )
      )
    : []
let writeQueue = Promise.resolve()
const active = new Set()
async function contextFor(width, anonymous) {
  const context = await browser.newContext({
    viewport: { width, height: width === 390 ? 844 : 900 },
    locale: 'zh-TW',
    reducedMotion: 'reduce',
  })
  if (!anonymous)
    await context.addCookies([
      {
        name: 'solaris_fixture_session',
        value: 'synthetic',
        domain: '127.0.0.1',
        path: '/',
        httpOnly: true,
        sameSite: 'Lax',
      },
    ])
  await context.addInitScript(
    ({ user, status, anonymous }) => {
      localStorage.setItem('setup_status_checked', 'true')
      localStorage.setItem('i18nextLng', 'zh-TW')
      localStorage.setItem('status', JSON.stringify(status))
      localStorage.setItem('theme', 'light')
      if (!anonymous) {
        localStorage.setItem('user', JSON.stringify(user))
        localStorage.setItem('uid', String(user.id))
      }
      window.__stressLongTasks = []
      try {
        new PerformanceObserver((list) =>
          window.__stressLongTasks.push(
            ...list.getEntries().map((entry) => ({
              start: entry.startTime,
              duration: entry.duration,
            }))
          )
        ).observe({ type: 'longtask', buffered: true })
      } catch {
        /* longtask unsupported */
      }
    },
    { user, status, anonymous }
  )
  active.add(context)
  return context
}
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))
const bounded = (promise, ms, name) =>
  Promise.race([
    promise,
    new Promise((_, reject) => {
      const timer = setTimeout(
        () => reject(new Error(`${name} exceeded ${ms}ms`)),
        ms
      )
      timer.unref()
    }),
  ])
async function visit(
  route,
  width,
  scale,
  iteration,
  mode = 'normal',
  delayMs = 0
) {
  const row = {
    route,
    width,
    scale,
    iteration,
    mode,
    delayMs,
    label,
    fixtureFrontend,
  }
  const specification = matrix.find((item) => item.url === route)
  const context = await contextFor(
    width,
    specification?.kind === 'authentication'
  )
  const page = await context.newPage()
  if (route === '/setup') {
    await page.route(/\/api\/setup(?:\?|$)/, (request) =>
      request.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: { status: false, root_init: false, database_type: 'synthetic' },
        }),
      })
    )
    row.fixtureOverride =
      'Synthetic setup status=false; render only; submission blocked'
  }
  page.setDefaultTimeout(10000)
  const cdp = await context.newCDPSession(page)
  await cdp.send('Performance.enable')
  await cdp.send('Network.enable', {
    maxTotalBufferSize: 128 * 1024 * 1024,
    maxResourceBufferSize: 64 * 1024 * 1024,
  })
  const errors = []
  const requests = []
  const jobs = []
  const pending = new Set()
  page.on('pageerror', (error) => errors.push(String(error)))
  page.on('crash', () => errors.push('PAGE_CRASH'))
  page.on('request', (request) => {
    if (new URL(request.url()).pathname.startsWith('/api/'))
      pending.add(request)
  })
  page.on('requestfailed', (request) => pending.delete(request))
  page.on('response', (response) => {
    if (!new URL(response.url()).pathname.startsWith('/api/')) return
    jobs.push(
      (async () => {
        const endpoint = new URL(response.url()).pathname
        const item = {
          endpoint,
          status: response.status(),
          method: response.request().method(),
          query: new URL(response.url()).search,
        }
        item.stressScale = response.headers()['x-stress-scale'] || null
        item.responseStartMs = Number(
          response.request().timing().responseStart.toFixed(2)
        )
        try {
          const body = await response.body()
          item.bytes = body.length
          item.transportMs = Number(
            response.request().timing().responseEnd.toFixed(2)
          )
          item.stressScale = response.headers()['x-stress-scale'] || null
          const json = JSON.parse(body.toString('utf8'))
          item.rows = Array.isArray(json.data)
            ? json.data.length
            : (json.data?.items?.length ??
              json.data?.records?.length ??
              json.data?.models?.length ??
              json.data?.history?.items?.length ??
              null)
          item.total =
            json.data?.total ?? json.data?.history?.total ?? json.total ?? null
          item.success = json.success
        } catch (error) {
          item.captureError = String(error)
        }
        requests.push(item)
        pending.delete(response.request())
      })()
    )
  })
  const start = performance.now()
  try {
    const navigation = await page.goto(base + route, {
      waitUntil: 'domcontentloaded',
      timeout: 45000,
    })
    row.httpStatus = navigation?.status() ?? null
    if (!navigation?.ok())
      throw new Error(`Document returned HTTP ${row.httpStatus}`)
    await page.waitForFunction(
      () =>
        Boolean(
          document.querySelector('#root')?.children.length &&
          document.querySelector(
            '#root main,#root h1,#root form,#root [role="alert"]'
          )
        ),
      undefined,
      { timeout: 45000 }
    )
    let stableSince = performance.now()
    while (performance.now() - start < 45000) {
      await sleep(120)
      if (pending.size) {
        stableSince = performance.now()
        continue
      }
      const loading = await bounded(
        page.evaluate(() =>
          Boolean(
            document.querySelector(
              'main [data-slot="skeleton"],main [aria-busy="true"]'
            )
          )
        ),
        10000,
        'loading observation'
      )
      if (loading) {
        stableSince = performance.now()
        continue
      }
      if (performance.now() - stableSince >= (mode === 'failure' ? 4500 : 700))
        break
    }
    row.waitingApiRequests = pending.size
    const state = await bounded(
      page.evaluate(() => {
        const main = document.querySelector('main') || document.body
        const rect = main.getBoundingClientRect()
        const text = main.innerText || ''
        return {
          url: location.pathname + location.search,
          title: document.title,
          textLength: text.length,
          headings: [...main.querySelectorAll('h1,h2')].map(
            (node) => node.textContent
          ),
          domNodes: document.querySelectorAll('*').length,
          appMounted: !!document.querySelector('#root')?.children.length,
          rows: main.querySelectorAll('tbody tr').length,
          tableRows: main.querySelectorAll('[role="row"]').length,
          options: main.querySelectorAll('select option').length,
          cards: main.querySelectorAll(
            '.dawn-model-card,.plaza-model-card,.market-group-card,.rankings-model-row'
          ).length,
          mainOverflow:
            rect.width > innerWidth + 2 ||
            rect.left < -2 ||
            rect.right > innerWidth + 2,
          overflow: document.documentElement.scrollWidth > innerWidth + 2,
          loading: main.querySelectorAll(
            '[data-slot="skeleton"],[aria-busy="true"]'
          ).length,
          alerts: [...main.querySelectorAll('[role="alert"]')].map((node) =>
            node.textContent?.slice(0, 300)
          ),
          errorUI:
            /Application error|Something went wrong|Minified React error/.test(
              text
            ),
          brokenImages: [...main.querySelectorAll('img')]
            .filter((image) => image.complete && image.naturalWidth === 0)
            .map((image) => image.src),
          longTasks: window.__stressLongTasks || [],
          heapBytes: performance.memory?.usedJSHeapSize ?? null,
          searchInput: [...main.querySelectorAll('input')].map((node) => ({
            placeholder: node.placeholder,
            type: node.type,
            ariaLabel: node.getAttribute('aria-label'),
          })),
          trailingText: text.slice(-500),
        }
      }),
      20000,
      'DOM measurement'
    )
    row.readyMs = Number((performance.now() - start).toFixed(2))
    Object.assign(row, state)
    row.displayItems = await page.evaluate(() => ({
      pricingRows: document.querySelectorAll('main .gtable .tr:not(.th)')
        .length,
      pricingCards: document.querySelectorAll('main .mgrid .mcard').length,
      rankingRows: document.querySelectorAll('main .rankings-model-list > li')
        .length,
      ownerChannelRows: document.querySelectorAll('main .owner-channel-record')
        .length,
      modelOptions: document.querySelectorAll(
        'main #home-route-model option,main .rankings-model-select option'
      ).length,
      groupStatusCards: document.querySelectorAll('main .group-status-monitor')
        .length,
    }))
    row.controlBounds = await page.evaluate(() =>
      [
        ...document.querySelectorAll(
          'main input,main select,main textarea,main button'
        ),
      ].flatMap((node) => {
        const rect = node.getBoundingClientRect()
        if (
          !rect.width ||
          !rect.height ||
          node.closest('[aria-hidden="true"],[hidden]') ||
          getComputedStyle(node).visibility === 'hidden'
        )
          return []
        let parent = node.parentElement
        let scrollable = false
        while (parent && parent !== document.documentElement) {
          const style = getComputedStyle(parent)
          if (
            /(auto|scroll)/.test(style.overflowX) &&
            parent.scrollWidth > parent.clientWidth + 2
          ) {
            scrollable = true
            break
          }
          parent = parent.parentElement
        }
        const clipped =
          (rect.left < -2 || rect.right > innerWidth + 2) && !scrollable
        return [
          {
            tag: node.tagName,
            id: node.id,
            label:
              node.getAttribute('aria-label') ||
              node.getAttribute('placeholder') ||
              node.textContent?.trim().slice(0, 45),
            left: Math.round(rect.left),
            right: Math.round(rect.right),
            width: Math.round(rect.width),
            scrollableAncestor: scrollable,
            clipped,
          },
        ]
      })
    )
    row.clippedControls = row.controlBounds.filter((item) => item.clipped)
    const metrics = await cdp.send('Performance.getMetrics')
    row.chromium = Object.fromEntries(
      metrics.metrics
        .filter((item) =>
          [
            'JSHeapUsedSize',
            'JSHeapTotalSize',
            'Nodes',
            'LayoutCount',
            'LayoutDuration',
            'RecalcStyleDuration',
            'ScriptDuration',
            'TaskDuration',
          ].includes(item.name)
        )
        .map((item) => [item.name, item.value])
    )
    row.longTaskTotalMs = Number(
      state.longTasks.reduce((sum, item) => sum + item.duration, 0).toFixed(2)
    )
    row.longTaskMaxMs = Math.max(
      0,
      ...state.longTasks.map((item) => item.duration)
    )
    if (route === '/pricing' && mode === 'normal') {
      const search = page.locator('main input').first()
      if (await search.count()) {
        const interactionStart = performance.now()
        await search.fill('no-such-stress-model-unique')
        await page.waitForTimeout(450)
        row.search = await page.evaluate(() => ({
          rows: document.querySelectorAll('main tbody tr').length,
          text: (document.querySelector('main')?.innerText || '').slice(-300),
        }))
        row.search.totalMs = Number(
          (performance.now() - interactionStart).toFixed(2)
        )
        await search.fill('')
        await page.waitForTimeout(450)
      }
    }
    if (
      phase !== 'sweep' ||
      row.overflow ||
      row.mainOverflow ||
      row.clippedControls?.length ||
      errors.length
    ) {
      const filename = `${label}-${width}-${scale}-${mode}-${route.replace(/[^a-z0-9]+/gi, '-').replace(/^-|-$/g, '') || 'home'}-${iteration}.png`
      await bounded(
        page.screenshot({
          path: path.join(output, filename),
          animations: 'disabled',
          timeout: 15000,
        }),
        18000,
        'screenshot'
      )
      row.screenshot = filename
    }
  } catch (error) {
    row.error = String(error)
  }
  await bounded(Promise.allSettled(jobs), 8000, 'response evidence').catch(
    (error) => {
      row.evidenceError = String(error)
    }
  )
  row.requests = requests
  row.pageErrors = errors
  row.augmentedApis = requests.filter((request) => request.stressScale != null)
  const relevant = [
    ...(specification?.apis || []),
    ...(specification?.additionalFullResponse || []),
    ...(specification?.additionalPagedResponses || []),
  ]
  row.scaledPageApis = row.augmentedApis.filter((request) =>
    relevant.includes(request.endpoint)
  )
  row.coverage =
    specification?.pagination === 'not applicable'
      ? 'fixed structure; no growing dataset'
      : mode === 'failure' &&
          requests.some(
            (request) =>
              relevant.includes(request.endpoint) && request.status === 503
          )
        ? 'synthetic 503 served for page dataset'
        : row.scaledPageApis.length
          ? 'scaled data served; inspect rendered rows and payloads'
          : 'unscaled data gap'
  row.apiFailures = requests.filter(
    (request) => request.status >= 400 || request.success === false
  )
  row.expectedAuthRejections = row.apiFailures.filter(
    (request) =>
      specification?.kind === 'authentication' &&
      route === '/oauth' &&
      row.url === '/sign-in' &&
      request.endpoint === '/api/user/self' &&
      request.status === 401
  )
  row.unexpectedApiFailures = row.apiFailures.filter(
    (request) => !row.expectedAuthRejections.includes(request)
  )
  row.emptyStateEvidence =
    mode !== 'empty' ||
    specification?.pagination === 'not applicable' ||
    row.scaledPageApis.some((request) => request.stressScale === '0')
  row.slowStateEvidence =
    mode !== 'slow' ||
    requests.some(
      (request) =>
        Math.max(request.responseStartMs || 0, request.transportMs || 0) >=
        delayMs
    )
  const documentContent =
    (row.trailingText || '') + ' ' + (row.alerts || []).join(' ')
  row.expectedFailureExposed =
    mode !== 'failure' ||
    (row.apiFailures.some((request) => request.status === 503) &&
      (/失敗|失败|重試|重试|錯誤|错误|不可用|無法|无法|重新|unavailable|error|retry|failed/i.test(
        documentContent
      ) ||
        (await page
          .locator('body')
          .innerText()
          .then((text) =>
            /失敗|失败|重試|重试|錯誤|错误|不可用|無法|无法|重新|unavailable|error|retry|failed/i.test(
              text
            )
          )
          .catch(() => false))))
  row.failures = [
    row.error && 'timeout-or-navigation-error',
    row.appMounted === false && 'app-not-mounted',
    row.domNodes < 45 && 'app-content-missing',
    row.textLength < 8 && 'blank-content',
    row.alerts?.some((text) => /Failed to load/.test(text)) &&
      'bootstrap-load-failure',
    errors.length > 0 && 'page-error',
    row.overflow && 'horizontal-overflow',
    row.mainOverflow && 'main-overflow',
    row.clippedControls?.length > 0 && 'clipped-controls',
    row.errorUI && 'runtime-error-ui',
    row.loading > 0 && 'unresolved-loading',
    row.waitingApiRequests > 0 && 'pending-api',
    row.brokenImages?.length > 0 && 'broken-images',
    mode !== 'failure' &&
      row.unexpectedApiFailures.length > 0 &&
      'unexpected-api-failure',
    row.coverage === 'unscaled data gap' && 'data-coverage-gap',
    !row.expectedFailureExposed && 'failure-state-not-exposed',
    !row.emptyStateEvidence && 'empty-dataset-not-served',
    !row.slowStateEvidence && 'artificial-delay-not-observed',
  ].filter(Boolean)
  row.performanceSignals = [
    row.domNodes > 5000 && 'DOM exceeds 5000 nodes',
    row.longTaskMaxMs > 200 && 'single main-thread task exceeds 200ms',
    row.readyMs > 5000 && 'ready observation exceeds 5s',
  ].filter(Boolean)
  row.performanceStatus = row.performanceSignals.length
    ? 'investigate; development build and local machine load affect time'
    : 'within observational budgets; not production capacity proof'
  row.pass = !row.failures.length
  const previous = results.findIndex(
    (item) =>
      item.route === route &&
      item.width === width &&
      item.scale === scale &&
      item.iteration === iteration &&
      item.mode === mode
  )
  if (previous >= 0) results.splice(previous, 1)
  results.push(row)
  writeQueue = writeQueue.then(() =>
    fs.writeFile(
      path.join(output, `${label}-browser-results.json`),
      JSON.stringify(results, null, 2)
    )
  )
  await writeQueue
  console.log(
    JSON.stringify({
      route,
      width,
      scale,
      iteration,
      mode,
      pass: row.pass,
      readyMs: row.readyMs,
      domNodes: row.domNodes,
      rows: row.rows,
      longTaskMaxMs: row.longTaskMaxMs,
      heapMiB:
        row.heapBytes == null
          ? null
          : Number((row.heapBytes / 1048576).toFixed(1)),
      failures: row.failures,
      coverage: row.coverage,
    })
  )
  await bounded(context.close(), 10000, 'context close').catch(() => {})
  active.delete(context)
}
const configure = (input) =>
  fs.writeFile(
    configPath,
    JSON.stringify({
      scale: 1000,
      mode: 'normal',
      delayMs: 0,
      target: '*',
      ...input,
    })
  )
try {
  if (phase === 'before') {
    for (const scale of [1000, 10000]) {
      await configure({ scale })
      for (let iteration = 1; iteration <= 3; iteration += 1)
        await visit('/pricing', 1440, scale, iteration)
      for (const route of ['/', '/rankings']) await visit(route, 1440, scale, 1)
    }
  } else if (phase === 'sweep') {
    await configure({ scale: 10000 })
    const selected = (
      process.env.STRESS_ROUTES
        ? matrix.filter((item) =>
            process.env.STRESS_ROUTES.split(',').includes(item.url)
          )
        : matrix
    ).filter(
      (item) =>
        !process.env.STRESS_EXCLUDE_ROUTES?.split(',').includes(item.url)
    )
    const jobs = selected.flatMap((item) =>
      [1440, 390].map((width) => ({ route: item.url, width }))
    )
    let next = 0
    await Promise.all(
      Array.from({ length: 2 }, async () => {
        while (next < jobs.length) {
          const job = jobs[next++]
          await visit(job.route, job.width, 10000, 1)
        }
      })
    )
  } else if (phase === 'high-risk') {
    const selected = process.env.STRESS_ROUTES?.split(',') || [
      '/',
      '/pricing',
      '/market',
      '/rankings',
      '/dashboard/overview',
      '/dashboard/models',
      '/channel-workbench',
      '/group-status',
      '/keys',
      '/usage-logs/common',
      '/wallet',
      '/blind-box',
    ]
    for (const scale of process.env.STRESS_SCALES?.split(',').map(Number) || [
      1000, 10000, 50000,
    ]) {
      await configure({ scale })
      for (const route of selected)
        for (
          let iteration = 1;
          iteration <= Number(process.env.STRESS_ROUNDS || 3);
          iteration += 1
        )
          await visit(route, 1440, scale, iteration)
    }
  } else if (phase === 'states') {
    for (const mode of process.env.STRESS_MODES?.split(',') || [
      'empty',
      'failure',
      'slow',
    ]) {
      await configure({
        scale: 10000,
        mode: mode === 'slow' ? 'normal' : mode,
        delayMs: mode === 'slow' ? 1500 : 0,
        target: [
          '/api/pricing',
          '/api/marketplace/groups',
          '/api/perf-metrics/summary',
          '/api/data/self',
          '/api/blind-box/self',
          '/api/subscription/self',
          '/api/user/models',
          '/api/user/self/group-status',
          '/api/group-status',
          '/api/marketplace/channels/mine',
          '/api/invoices/eligible-orders',
        ],
      })
      for (const route of process.env.STRESS_ROUTES?.split(',') || [
        '/',
        '/pricing',
        '/market',
        '/rankings',
        '/dashboard/overview',
        '/wallet',
        '/blind-box',
        '/playground',
        '/group-status',
        '/channel-workbench',
        '/invoices',
      ])
        await visit(route, 1440, 10000, 1, mode, mode === 'slow' ? 1500 : 0)
    }
  } else throw new Error(`Unknown phase ${phase}`)
} finally {
  await configure({ scale: 1000 })
  for (const context of active)
    await bounded(context.close(), 10000, 'context close').catch(() => {})
  await bounded(browser.close(), 15000, 'browser close').catch(() => {})
}
