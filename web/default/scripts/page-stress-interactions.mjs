/* Copyright (C) 2023-2026 QuantumNous; SPDX-License-Identifier: AGPL-3.0-or-later */
// Isolated browser and synthetic fixture only. Never submits inference or money.
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const output = path.resolve(root, '../../output/playwright/page-stress')
const configPath = path.join(output, 'config.json')
const base = 'http://127.0.0.1:3097'
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
  channel: 'chrome',
  headless: true,
})
const status = (await (await fetch(base + '/api/status')).json()).data
const fixtureFrontend = (await (await fetch(base + '/__stress/health')).json())
  .frontend
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
const results =
  process.env.STRESS_RESUME === 'true'
    ? JSON.parse(
        await fs.readFile(
          path.join(output, '50k-interaction-results.json'),
          'utf8'
        )
      )
    : []
const check = (value, message) => {
  if (!value) throw new Error(message)
}
const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms))
const count = (page, selector) => page.locator(selector).count()
async function until(predicate, message, timeout = 15000) {
  const start = performance.now()
  while (performance.now() - start < timeout) {
    if (await predicate()) return
    await wait(120)
  }
  throw new Error(message)
}
async function boundedSelect(page, selector, row, name, needle = '049999') {
  const trigger = page.locator(selector)
  await trigger.click()
  const search = page.locator('[cmdk-input]')
  await search.waitFor()
  const options = page.locator('[cmdk-list] [role="option"]')
  const mountedOptions = await options.count()
  check(
    mountedOptions > 0 && mountedOptions <= 50,
    `${name}: expected <=50 mounted options, got ${mountedOptions}`
  )
  const jump = page.locator(
    '[data-slot="popover-content"] input[type="number"]'
  )
  if (await jump.count()) {
    const last = await jump.getAttribute('max')
    await jump.fill(last)
    await wait(180)
    check((await options.count()) <= 50, `${name}: final page exceeded 50`)
  }
  await search.fill(needle)
  await until(
    async () => (await options.count()) === 1,
    `${name}: final record not found`
  )
  const label = await options.innerText()
  await options.click()
  await until(
    async () => (await trigger.innerText()).includes(needle),
    `${name}: selected label missing`
  )
  await trigger.click()
  await search.fill('no-such-stress-option-unique')
  await until(
    async () => (await options.count()) === 0,
    `${name}: no-match options remained`
  )
  await search.fill('')
  await until(
    async () => (await options.count()) > 0,
    `${name}: clear search did not restore options`
  )
  check((await options.count()) <= 50, `${name}: cleared options exceeded 50`)
  if (name === 'playground' && row.width === 390) {
    await page.screenshot({
      path: path.join(output, 'interaction-390-50000-playground-open.png'),
      animations: 'disabled',
    })
  }
  await page.keyboard.press('Escape')
  check(
    (await trigger.innerText()).includes(needle),
    `${name}: no-match/clear lost selected value`
  )
  row.checks.push({
    name,
    mountedOptions,
    finalRecord: label,
    noMatchOptions: 0,
    selectionRetained: true,
  })
}
async function invoiceOrders(page, row) {
  await until(
    async () => (await count(page, '.commerce-invoice-order')) === 50,
    'invoice expected 50 mounted orders'
  )
  const jump = page.locator('.commerce-invoice-pagination input[type="number"]')
  await jump.fill('1000')
  await until(
    async () =>
      (await page.locator('.commerce-invoice-list').innerText()).includes(
        'STRESS-ELIGIBLE-049999'
      ),
    'invoice last page unavailable'
  )
  const search = page.locator('#invoice-order-search')
  await search.fill('STRESS-ELIGIBLE-049999')
  await until(
    async () => (await count(page, '.commerce-invoice-order')) === 1,
    'invoice full catalogue search failed'
  )
  await search.fill('no-such-stress-invoice-unique')
  await until(
    async () => (await count(page, '.commerce-invoice-order')) === 0,
    'invoice no-match failed'
  )
  await search.fill('')
  await until(
    async () => (await count(page, '.commerce-invoice-order')) === 50,
    'invoice clear failed'
  )
  check((await jump.inputValue()) === '1', 'invoice search did not reset page')
  row.checks.push({
    name: 'eligible invoices last page/full search/empty/clear without download',
    mountedRows: 50,
    finalTradeNo: 'STRESS-ELIGIBLE-049999',
  })
}
const cases = [
  ['/invoices', invoiceOrders],
  ['/wallet?section=invoices', invoiceOrders],
  [
    '/pricing',
    async (page, row) => {
      await until(
        async () => (await count(page, '.gtable .tr:not(.th)')) === 50,
        'pricing 50 rows did not render'
      )
      const jump = page.locator('.gateway-pagination input[type="number"]')
      await jump.fill('1000')
      await until(
        async () =>
          (await page.locator('.gtable').innerText()).includes('gpt-4o'),
        'pricing last page unavailable under descending request-count sort'
      )
      const search = page.locator('#directory-search')
      await search.fill('stress-049999')
      await until(
        async () => (await count(page, '.gtable .tr:not(.th)')) === 1,
        'pricing global search did not find tail'
      )
      await search.fill('no-such-stress-model-unique')
      await until(
        async () => (await count(page, '.gtable .tr:not(.th)')) === 0,
        'pricing no-match rows remained'
      )
      await search.fill('')
      await until(
        async () => (await count(page, '.gtable .tr:not(.th)')) === 50,
        'pricing clear failed'
      )
      check(
        (await jump.inputValue()) === '1',
        'pricing clear did not reset page'
      )
      await page.getByRole('button', { name: /卡片/, exact: true }).click()
      await until(
        async () => (await count(page, '.mgrid .mcard')) === 50,
        'pricing card page unbounded or missing'
      )
      await page.getByRole('button', { name: /表格/, exact: true }).click()
      const clipped = await page
        .locator('.gtable .tr:not(.th)')
        .evaluateAll(
          (nodes) =>
            nodes.filter(
              (n) => n.getBoundingClientRect().right > innerWidth + 2
            ).length
        )
      check(
        clipped === 0 || row.width !== 390,
        'pricing mobile row extends beyond viewport'
      )
      row.checks.push({
        name: 'directory last page/global search/empty/clear/table/card',
        tableRows: 50,
        cardRows: 50,
        mobileClippedRows: clipped,
      })
    },
  ],
  [
    '/rankings',
    async (page, row) => {
      await until(
        async () => (await count(page, '.rankings-model-row')) === 50,
        'ranking rows missing'
      )
      const search = page.locator('.rankings-model-search input')
      await search.fill('gpt-4o')
      await until(
        async () => (await count(page, '.rankings-model-row')) === 1,
        'ranking least-requested global tail search unavailable'
      )
      const finalRankText = await page
        .locator('.rankings-model-row')
        .innerText()
      check(
        /50[,.]?000/.test(finalRankText),
        'ranking tail lost original global rank'
      )
      await search.fill('no-such-stress-model-unique')
      await until(
        async () => (await count(page, '.rankings-model-row')) === 0,
        'ranking no-match failed'
      )
      await search.fill('')
      await until(
        async () => (await count(page, '.rankings-model-row')) === 50,
        'ranking clear failed'
      )
      check(
        (await count(page, '.rankings-model-select option')) <= 51,
        'ranking native options unbounded'
      )
      row.checks.push({
        name: 'ranking 50 rows, global rank, empty and clear',
        finalRankText,
        nativeOptions: await count(page, '.rankings-model-select option'),
      })
    },
  ],
  [
    '/',
    async (page, row) => {
      const search = page.locator('#home-route-search')
      await search.waitFor()
      check(
        (await count(page, '#home-route-model option')) <= 51,
        'home options unbounded'
      )
      await search.fill('stress-049999')
      await until(
        async () =>
          (await page.locator('#home-route-model').innerText()).includes(
            'stress-049999'
          ),
        'home tail model missing'
      )
      const selected = await page
        .locator('#home-route-model option')
        .last()
        .getAttribute('value')
      await page.locator('#home-route-model').selectOption(selected)
      await search.fill('no-such-stress-model-unique')
      await until(
        async () => (await count(page, '#home-route-model')) === 0,
        'home no-match failed'
      )
      await search.fill('')
      await until(
        async () => (await count(page, '#home-route-model')) === 1,
        'home clear failed'
      )
      check(
        (await page.locator('#home-route-model').inputValue()) === selected,
        'home selected model lost after clear'
      )
      check(
        (await count(page, '#home-route-model option')) <= 51,
        'home restored options unbounded'
      )
      row.checks.push({
        name: 'home tail model selected and retained through empty/clear',
        selected,
        nativeOptions: await count(page, '#home-route-model option'),
      })
    },
  ],
  [
    '/playground',
    async (page, row) => {
      await page.locator('#pg-model').waitFor()
      await boundedSelect(page, '#pg-model', row, 'playground')
    },
  ],
  [
    '/group-status',
    async (page, row) => {
      await page
        .getByRole('combobox', { name: /按模型[筛篩]选|按模型篩選/ })
        .waitFor()
      const selector = '[role="combobox"][aria-label*="按模型"]'
      await boundedSelect(page, selector, row, 'group-status')
      await until(
        async () =>
          (await page.locator('main').innerText()).includes('STRESS 049999'),
        'group selected model did not filter cards'
      )
    },
  ],
  [
    '/channel-workbench',
    async (page, row) => {
      await until(
        async () => (await count(page, '.owner-channel-record')) === 50,
        'owner channels not bounded at 50'
      )
      await page
        .locator('.owner-channel-pagination input[type="number"]')
        .fill('1000')
      await until(
        async () =>
          (await page.locator('.owner-channel-record').allInnerTexts()).some(
            (t) => t.includes('049999')
          ),
        'owner final page missing'
      )
      const search = page.locator('.owner-channel-filters input')
      await search.fill('049999')
      await until(
        async () => (await count(page, '.owner-channel-record')) === 1,
        'owner tail search missing'
      )
      await search.fill('no-such-stress-channel-unique')
      await until(
        async () => (await count(page, '.owner-channel-record')) === 0,
        'owner empty filter failed'
      )
      await search.fill('')
      await until(
        async () => (await count(page, '.owner-channel-record')) === 50,
        'owner clear failed'
      )
      row.checks.push({
        name: 'owned channel 50-row last page/global search/empty/clear',
        mountedRows: 50,
      })
      const tabs = [
        [1, '[role="combobox"][aria-label*="收入"]', 'owner-income'],
        [
          2,
          '[role="combobox"][aria-label*="选择渠道"],[role="combobox"][aria-label*="選擇渠道"]',
          'owner-users',
        ],
        [3, '[role="combobox"][aria-label*="渠道使用"]', 'owner-logs'],
        [
          4,
          '[role="combobox"][aria-label*="选择渠道"],[role="combobox"][aria-label*="選擇渠道"]',
          'owner-security',
        ],
      ]
      for (const [name, selector, key] of tabs) {
        await page
          .locator('.owner-workspace-tabs [role="tab"]')
          .nth(name)
          .click()
        await page.locator(selector).waitFor({ timeout: 90000 })
        await boundedSelect(page, selector, row, key)
        if (key === 'owner-income') {
          check(
            (await count(page, '.owner-income-rank > article')) <= 50,
            'owner income rank unbounded'
          )
          const rankSearch = page.locator(
            '.owner-income-rank input:not([type="number"])'
          )
          await rankSearch.fill('049999')
          await until(
            async () =>
              (await count(page, '.owner-income-rank > article')) === 1,
            'owner income tail search failed'
          )
        }
      }
    },
  ],
  [
    '/blind-box',
    async (page, row) => {
      await page.getByRole('button', { name: /[开開][奖獎][历歷]史/ }).click()
      const dialog = page.getByRole('dialog')
      await dialog.waitFor()
      await until(
        async () => (await dialog.innerText()).includes('50000'),
        'blind-box history total not rendered'
      )
      await dialog.getByRole('button', { name: /下一[页頁]/ }).click()
      await until(
        async () =>
          row.requests.some(
            (x) =>
              x.endpoint === '/api/blind-box/history' &&
              /(?:page|p)=2(?:&|$)/.test(x.query)
          ),
        'blind-box second page API missing'
      )
      row.checks.push({
        name: 'blind-box actual history sheet first/next page',
        total: 50000,
        pageSize: 20,
        renderedTail: (await dialog.innerText()).slice(-140),
      })
    },
  ],
  [
    '/wallet',
    async (page, row) => {
      await page.locator('.commerce-wallet-records').click()
      const entries = page.getByRole('menuitem')
      row.recordMenu = await entries.allInnerTexts()
      await entries.first().click()
      const dialog = page.getByRole('dialog')
      await dialog.waitFor()
      await until(
        async () =>
          row.requests.some(
            (x) => x.endpoint === '/api/user/topup' && x.total === 50000
          ),
        'wallet billing scaled API missing'
      )
      const next = dialog.getByRole('button', { name: /Next page|下一[页頁]/ })
      if (await next.count()) {
        await next.click()
        await until(
          async () =>
            row.requests.some(
              (x) =>
                x.endpoint === '/api/user/topup' &&
                /(?:page|p)=2(?:&|$)/.test(x.query)
            ),
          'wallet next billing page missing'
        )
      }
      row.checks.push({
        name: 'wallet billing actual opened dialog',
        total: 50000,
        rows: row.requests
          .filter((x) => x.endpoint === '/api/user/topup')
          .map((x) => x.rows),
      })
      await page.keyboard.press('Escape')
      await page.locator('.commerce-wallet-records').click()
      await page.getByRole('menuitem').nth(1).click()
      await until(
        async () =>
          row.requests.some(
            (x) => x.endpoint === '/api/wallet/transfers' && x.total === 50000
          ),
        'wallet transfer actual sheet API missing'
      )
      const transfer = page.getByRole('dialog')
      await transfer
        .getByRole('button', { name: /Next page|下一[页頁]/ })
        .click()
      await until(
        async () =>
          row.requests.some(
            (x) =>
              x.endpoint === '/api/wallet/transfers' &&
              /(?:page|p)=2(?:&|$)/.test(x.query)
          ),
        'wallet transfer second page missing'
      )
      row.checks.push({
        name: 'wallet transfer actual sheet first/next',
        total: 50000,
        pageSize: 10,
      })
    },
  ],
]
try {
  await fs.writeFile(
    configPath,
    JSON.stringify({ scale: 50000, mode: 'normal', delayMs: 0, target: '*' })
  )
  for (const [route, run] of cases)
    for (const width of [1440, 390]) {
      if (
        process.env.STRESS_ROUTES &&
        !process.env.STRESS_ROUTES.split(',').includes(route)
      )
        continue
      const row = {
        route,
        width,
        scale: 50000,
        fixtureFrontend,
        checks: [],
        requests: [],
        errors: [],
      }
      const context = await browser.newContext({
        viewport: { width, height: width === 390 ? 844 : 900 },
        locale: 'zh-TW',
        reducedMotion: 'reduce',
      })
      await context.addCookies([
        {
          name: 'solaris_fixture_session',
          value: 'synthetic',
          domain: '127.0.0.1',
          path: '/',
        },
      ])
      await context.addInitScript(
        ({ user, status }) => {
          localStorage.setItem('user', JSON.stringify(user))
          localStorage.setItem('uid', String(user.id))
          localStorage.setItem('status', JSON.stringify(status))
          localStorage.setItem('theme', 'light')
          localStorage.setItem('i18nextLng', 'zh-TW')
          localStorage.setItem('setup_status_checked', 'true')
        },
        { user, status }
      )
      const page = await context.newPage()
      page.setDefaultTimeout(15000)
      const cdp = await context.newCDPSession(page)
      await cdp.send('Network.enable', {
        maxTotalBufferSize: 128 * 1024 * 1024,
        maxResourceBufferSize: 64 * 1024 * 1024,
      })
      page.on('pageerror', (error) => row.errors.push(String(error)))
      page.on('response', (response) => {
        if (!response.url().includes('/api/')) return
        void (async () => {
          try {
            const json = await response.json()
            row.requests.push({
              endpoint: new URL(response.url()).pathname,
              query: new URL(response.url()).search,
              status: response.status(),
              scale: response.headers()['x-stress-scale'],
              rows: Array.isArray(json.data)
                ? json.data.length
                : (json.data?.records?.length ??
                  json.data?.items?.length ??
                  json.data?.history?.items?.length),
              total:
                json.data?.total ?? json.data?.history?.total ?? json.total,
            })
          } catch {
            /* evidence retained by sweep on capture failures */
          }
        })()
      })
      const started = performance.now()
      try {
        await page.goto(base + route, {
          waitUntil: 'domcontentloaded',
          timeout: 45000,
        })
        await page.waitForFunction(
          () => document.querySelector('#root main')?.children.length,
          undefined,
          { timeout: 45000 }
        )
        await run(page, row)
        check(row.errors.length === 0, 'browser page errors')
        row.pass = true
      } catch (error) {
        row.pass = false
        row.error = String(error)
      }
      row.elapsedMs = Number((performance.now() - started).toFixed(1))
      row.domNodes = await page.locator('*').count()
      const filename = `interaction-${width}-50000-${route.replace(/[^a-z0-9]+/gi, '-').replace(/^-|-$/g, '') || 'home'}.png`
      await page
        .screenshot({
          path: path.join(output, filename),
          animations: 'disabled',
        })
        .catch((error) => {
          row.screenshotError = String(error)
        })
      row.screenshot = filename
      const previous = results.findIndex(
        (item) => item.route === route && item.width === width
      )
      if (previous >= 0) results.splice(previous, 1)
      results.push(row)
      await fs.writeFile(
        path.join(output, '50k-interaction-results.json'),
        JSON.stringify(results, null, 2)
      )
      process.stdout.write(
        JSON.stringify({
          route,
          width,
          pass: row.pass,
          checks: row.checks.length,
          error: row.error,
        }) + '\n'
      )
      await context.close()
    }
} finally {
  await fs.writeFile(
    configPath,
    JSON.stringify({ scale: 1000, mode: 'normal', delayMs: 0, target: '*' })
  )
  await browser.close()
}
